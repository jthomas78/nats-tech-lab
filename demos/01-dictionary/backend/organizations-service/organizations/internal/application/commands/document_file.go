package commands

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/filetickets"
)

// DocumentObjectStore is the port for compliance document bytes — satisfied by
// internal/objectstore's NATS Object Store adapter. Narrow on purpose: the
// application layer moves bytes and never learns that JetStream is underneath.
type DocumentObjectStore interface {
	Put(ctx context.Context, name, fileName, contentType string, r io.Reader) (int64, error)
	Get(ctx context.Context, name string) (io.ReadCloser, error)
}

// ObjectStoreResolver resolves a tenant to its own bucket. Tenant isolation
// here is the NATS account boundary, exactly as it is for every stream and KV
// bucket in this repo — the tenant name always arrives from a redeemed ticket
// (BR-TP41), never from the HTTP request that spends it.
type ObjectStoreResolver interface {
	DocumentStore(tenant string) (DocumentObjectStore, error)
}

// TicketStore is the port for BR-TP41's capability tokens.
type TicketStore interface {
	Mint(g filetickets.Grant) (string, error)
	Redeem(token string, dir filetickets.Direction) (filetickets.Grant, error)
}

// DocumentFileHandler implements BR-TP41-BR-TP45 — minting the tickets that
// authorize a transfer, and performing the transfer itself.
//
// It is deliberately separate from ComplianceDocumentHandler: that handler
// owns the review lifecycle, which is pure metadata over NATS request/reply,
// while this one is the only place in the service that touches bytes or the
// HTTP ingress. Keeping them apart is what lets the mux allowlist (BR-TP17)
// stay a meaningful statement about which code can be reached over HTTP.
type DocumentFileHandler struct {
	docs    domain.ComplianceDocumentRepository
	tickets TicketStore
	stores  ObjectStoreResolver
	now     func() time.Time
}

func NewDocumentFileHandler(docs domain.ComplianceDocumentRepository, tickets TicketStore, stores ObjectStoreResolver) *DocumentFileHandler {
	return &DocumentFileHandler{docs: docs, tickets: tickets, stores: stores, now: time.Now}
}

// MintUploadTicket implements BR-TP41's upload half.
//
// The document's eligibility is checked *here*, at mint time, not only at
// redemption: telling an operator "this document already has a file" before
// their browser spends a minute uploading one is the difference between a
// clear refusal and a wasted transfer that fails at the end.
func (h *DocumentFileHandler) MintUploadTicket(ctx context.Context, tenant, contextKey, partnerID, documentID string) (string, error) {
	doc, err := h.docs.GetDocument(ctx, partnerID, documentID)
	if err != nil {
		return "", err
	}
	if doc.Status == domain.DocumentStatusSuperseded {
		return "", domain.ErrDocumentSuperseded
	}
	if doc.File != nil {
		return "", domain.ErrDocumentFileAlreadyAttached
	}
	return h.tickets.Mint(filetickets.Grant{
		Tenant:     tenant,
		Context:    contextKey,
		PartnerID:  partnerID,
		DocumentID: documentID,
		Direction:  filetickets.DirectionUpload,
	})
}

// MintDownloadTicket implements BR-TP41's download half. A superseded
// document is downloadable (BR-TP43 keeps its bytes); one that never had a
// file is refused up front for the same reason as above.
func (h *DocumentFileHandler) MintDownloadTicket(ctx context.Context, tenant, contextKey, partnerID, documentID string) (string, error) {
	doc, err := h.docs.GetDocument(ctx, partnerID, documentID)
	if err != nil {
		return "", err
	}
	if doc.File == nil {
		return "", domain.ErrDocumentFileMissing
	}
	return h.tickets.Mint(filetickets.Grant{
		Tenant:     tenant,
		Context:    contextKey,
		PartnerID:  partnerID,
		DocumentID: documentID,
		Direction:  filetickets.DirectionDownload,
	})
}

// Upload implements BR-TP42/BR-TP43/BR-TP44 — the blob-first write order.
//
// Order is forced and not a preference. Nothing spans the Object Store and
// Postgres transactionally, and the two failure modes are not symmetric:
// recording the file first and then failing to store the bytes leaves a
// projection (and, upstream, an immutable event log) asserting a document that
// cannot be retrieved. Storing first and then failing leaves at worst an
// orphan object — invisible to every reader, addressable by name, and
// harmless. So: write bytes, then record them.
func (h *DocumentFileHandler) Upload(ctx context.Context, token, fileName, contentType string, r io.Reader) (domain.ComplianceDocument, error) {
	grant, err := h.tickets.Redeem(token, filetickets.DirectionUpload)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}

	doc, err := h.docs.GetDocument(ctx, grant.PartnerID, grant.DocumentID)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	if doc.File != nil {
		return domain.ComplianceDocument{}, domain.ErrDocumentFileAlreadyAttached
	}
	if fileName == "" {
		return domain.ComplianceDocument{}, domain.ErrFileNameRequired
	}
	if contentType == "" {
		return domain.ComplianceDocument{}, domain.ErrContentTypeRequired
	}

	store, err := h.stores.DocumentStore(grant.Tenant)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}

	// BR-TP42: every token of the name is service-controlled. The client's
	// own filename is metadata, never identity.
	objectName := domain.DocumentObjectName(grant.Context, grant.PartnerID, doc.Type, doc.ID)

	// BR-TP44, second line of defence. The HTTP handler caps the request body
	// too, but that cap belongs to the transport; this one guarantees the
	// object store is never handed more than one byte over the limit even if
	// this handler is driven from somewhere else. Reading Max+1 is what makes
	// an over-limit upload detectable — stopping at exactly Max would be
	// indistinguishable from a file that happens to be the maximum size.
	size, err := store.Put(ctx, objectName, fileName, contentType, io.LimitReader(r, domain.MaxDocumentFileBytes+1))
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	if size > domain.MaxDocumentFileBytes {
		// The oversized object stays in the bucket, unreferenced. That is the
		// deliberate trade this whole ordering makes: an orphan is recoverable
		// by name, a dangling reference is not.
		return domain.ComplianceDocument{}, domain.ErrFileTooLarge
	}

	return h.docs.AttachFile(ctx, grant.PartnerID, grant.DocumentID, domain.DocumentFile{
		FileName:    fileName,
		ContentType: contentType,
		SizeBytes:   size,
		ObjectName:  objectName,
		UploadedAt:  h.now().Unix(),
	})
}

// Download implements BR-TP45's read half. The returned reader is the
// caller's to close. Object name comes from the projection, never recomputed:
// a name reconstructed from parts would silently diverge from the stored one
// the moment any part of the naming rule changed.
func (h *DocumentFileHandler) Download(ctx context.Context, token string) (domain.ComplianceDocument, io.ReadCloser, error) {
	grant, err := h.tickets.Redeem(token, filetickets.DirectionDownload)
	if err != nil {
		return domain.ComplianceDocument{}, nil, err
	}
	doc, err := h.docs.GetDocument(ctx, grant.PartnerID, grant.DocumentID)
	if err != nil {
		return domain.ComplianceDocument{}, nil, err
	}
	if doc.File == nil {
		return domain.ComplianceDocument{}, nil, domain.ErrDocumentFileMissing
	}
	body, err := h.stores.DocumentStore(grant.Tenant)
	if err != nil {
		return domain.ComplianceDocument{}, nil, err
	}
	rc, err := body.Get(ctx, doc.File.ObjectName)
	if err != nil {
		return domain.ComplianceDocument{}, nil, err
	}
	return doc, rc, nil
}

// ErrTicketRequired — the HTTP request carried no ticket at all. Separate
// from filetickets.ErrUnknownTicket so the handler can answer 400 rather than
// 403 for a malformed call.
var ErrTicketRequired = errors.New("a file ticket is required")
