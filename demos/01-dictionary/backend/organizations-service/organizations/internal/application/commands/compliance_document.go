package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
)

// BR-TP71 removed the comment that used to stand here, which justified the
// absence of a document audit trail as having "no enforcement consequence in
// v1". Phase 46's change-log export falsifies that: every GIT certificate
// command now records an actor, and the TRANSPORTER stream is where that
// provenance lives (ADR-050). The other four document types remain
// pass-throughs onto domain.ComplianceDocumentRepository.
//
// CertificateAppender is the slice of the profile orchestrator the GIT
// document commands need (ADR-050 Option A). Narrow for the same reason
// ProfileEventAppender is: these commands own the certificate facts on that
// aggregate and nothing else on it.
type CertificateAppender interface {
	SetCertificateExpiry(ctx context.Context, contextKey, organizationID, documentID string, expiresAt *int64, now time.Time, actorName, sourceIP string) (profiledomain.State, error)
	RegisterCertificate(ctx context.Context, contextKey, organizationID string, doc domain.ComplianceDocument, actorName, sourceIP string) (profiledomain.State, error)
	AttachCertificateFile(ctx context.Context, contextKey, organizationID, documentID string, file domain.DocumentFile, actorName, sourceIP string) (profiledomain.State, error)
	ApproveCertificate(ctx context.Context, contextKey, organizationID, documentID, insurerName string, now time.Time, actorName, sourceIP string) (profiledomain.State, error)
	RejectCertificate(ctx context.Context, contextKey, organizationID, documentID, actorName, sourceIP string) (profiledomain.State, error)
	ResubmitCertificate(ctx context.Context, contextKey, organizationID, documentID, actorName, sourceIP string) (profiledomain.State, error)
	UpdateCertificateDetails(ctx context.Context, contextKey, organizationID, documentID, reference string, goodsTypes []string, coverageCents *int64, insurerName string, contactsChanged bool, actorName, sourceIP string) (profiledomain.State, error)
}

// CertificateAppenderResolver resolves a tenant to its own event store, for
// the same reason ProfileEventAppenderResolver does: the TRANSPORTER stream
// lives inside the tenant's NATS account.
type CertificateAppenderResolver interface {
	CertificateCommands(tenant string) (CertificateAppender, error)
}

type ComplianceDocumentHandler struct {
	partners     domain.OrganizationRepository
	docs         domain.ComplianceDocumentRepository
	goodsTypes   domain.GoodsTypeValidator
	certificates CertificateAppenderResolver
	newID        func() string
}

func NewComplianceDocumentHandler(partners domain.OrganizationRepository, docs domain.ComplianceDocumentRepository) *ComplianceDocumentHandler {
	return &ComplianceDocumentHandler{partners: partners, docs: docs, newID: func() string { return uuid.NewString() }}
}

// WithCertificateAppender supplies the aggregate write path for GIT documents.
// Registration mints the ID here rather than letting Postgres' column default
// mint it (BR-TP29's mechanism for the other four types): an event-sourced
// document has to be named before its first event is appended, and the
// projection row is written from that event afterwards.
func (h *ComplianceDocumentHandler) WithCertificateAppender(r CertificateAppenderResolver) *ComplianceDocumentHandler {
	h.certificates = r
	return h
}

// WithGoodsTypeValidator supplies 39a's fake/live refdata port without
// changing the legacy constructor used by non-GIT callers and existing tests.
func (h *ComplianceDocumentHandler) WithGoodsTypeValidator(v domain.GoodsTypeValidator) *ComplianceDocumentHandler {
	h.goodsTypes = v
	return h
}

// AddGitDocument is 39a's registration command, and the first write on this
// service that is event-sourced rather than a row insert: it appends
// document-registered to the TransporterProfile aggregate and returns what
// that event says. organizations.compliance_documents is a *projection* of
// that stream for this document type (decision 11).
//
// The command writes the projection row itself, immediately after the append,
// rather than leaving it to the projector alone. That is not a second source
// of truth — the row is written from the same certificate the event carries,
// through the same idempotent upsert the projector uses, so the projector's
// later write of the same event is byte-identical and a rebuild from replay
// still produces it. It closes a read-after-write window that was not
// theoretical: every later command on a GIT certificate (approve, reject,
// set-expiry) reads this projection first, so until the row landed those
// commands failed with "document not found" against a certificate the stream
// already had. Worse, ApproveGitDocument writes the insurance contact pair
// straight to the row (decision 25) — the only copy that will ever exist,
// since BR-TP72 keeps it off the stream — and that write is an UPDATE, so a
// missing row lost the contacts with no error at all.
//
// The append goes first. If it succeeds and this write fails, the projector
// still creates the row from the stream; the reverse order could leave a row
// no event backs.
//
// Registration is never gated (BR-TP68). A transporter may register a renewal
// while its current cover is live and approved; nothing about cover changes
// until the new certificate is approved.
func (h *ComplianceDocumentHandler) AddGitDocument(ctx context.Context, tenant, contextKey, partnerID, reference string, expiresAt, coverageCents *int64, goodsTypes []string, actor Actor) (domain.ComplianceDocument, error) {
	doc, err := h.buildGitDocument(ctx, tenant, contextKey, partnerID, reference, expiresAt, coverageCents, goodsTypes)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	appender, err := h.certificateCommands(tenant)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	if _, err := appender.RegisterCertificate(ctx, contextKey, partnerID, doc, actor.Name, actor.SourceIP); err != nil {
		return domain.ComplianceDocument{}, err
	}
	if err := h.docs.UpsertCertificate(ctx, partnerID, projectedFrom(doc)); err != nil {
		return domain.ComplianceDocument{}, err
	}
	return doc, nil
}

// projectedFrom narrows a freshly registered certificate to the projection's
// own shape. It is the inverse of the projector's own conversion and, like it,
// carries no contact fields — those are not on the stream and must never be
// written by anything that could be replayed (BR-TP72).
func projectedFrom(doc domain.ComplianceDocument) domain.ProjectedCertificate {
	return domain.ProjectedCertificate{
		ID: doc.ID, Status: doc.Status, Reference: doc.Reference,
		GoodsTypes:    append([]string(nil), doc.GoodsTypes...),
		CoverageCents: doc.CoverageCents, ExpiresAt: doc.ExpiresAt,
		InsurerName: doc.InsurerName, File: doc.File,
	}
}

// buildGitDocument is the validation shared by registration and any later
// re-validation of the same inputs. Vocabulary membership is an application
// concern (it is a tenant-scoped RPC); BR-TP64's cardinality rule stays on
// ComplianceDocument itself.
func (h *ComplianceDocumentHandler) buildGitDocument(ctx context.Context, tenant, contextKey, partnerID, reference string, expiresAt, coverageCents *int64, goodsTypes []string) (domain.ComplianceDocument, error) {
	tp, err := h.partners.Get(ctx, partnerID)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	doc, err := domain.AddDocument(tp.Type, domain.DocumentTypeGoodsInTransit, reference)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	doc.ID = h.newID()
	doc.GoodsTypes = append([]string(nil), goodsTypes...)
	doc.CoverageCents = coverageCents
	if err := doc.ValidateGitCertificate(); err != nil {
		return domain.ComplianceDocument{}, err
	}
	if h.goodsTypes == nil {
		return domain.ComplianceDocument{}, errors.New("goods-type validator is not configured")
	}
	for _, code := range doc.GoodsTypes {
		exists, err := h.goodsTypes.GoodsTypeExists(ctx, tenant, contextKey, code)
		if err != nil {
			return domain.ComplianceDocument{}, err
		}
		if !exists {
			return domain.ComplianceDocument{}, fmt.Errorf("%w: %q", domain.ErrGoodsTypeNotFound, code)
		}
	}
	if expiresAt != nil {
		if doc, err = doc.SetExpiry(expiresAt, time.Now().UTC()); err != nil {
			return domain.ComplianceDocument{}, err
		}
	}
	return doc, nil
}

// ApproveGitDocument is BR-TP66/BR-TP67/BR-TP69 on the event-sourced path.
//
// # The write order is forced, and for the same reason BR-TP53's is
//
// The insurance contact pair never reaches the stream (BR-TP72), so it is
// written straight to the projection row — the one exception to this table
// being replay-fed (decision 25). That direct write goes *first*:
//
//   - Events first, contacts second: a failure leaves an immutable log
//     asserting an approval whose required contact details were never
//     recorded anywhere. The log cannot be retracted, so it would
//     permanently claim an approval that did not meet BR-TP66.
//   - Contacts first, events second: a failure leaves contact details on a
//     certificate that was not approved. Nothing reads them until an
//     approval exists, and the next attempt overwrites them.
//
// The second is inert and self-correcting, so contacts go first.
func (h *ComplianceDocumentHandler) ApproveGitDocument(ctx context.Context, tenant, contextKey, partnerID, documentID, insurerName, contactName, contactNumber string, actor Actor) (domain.ComplianceDocument, error) {
	doc, err := h.docs.GetDocument(ctx, partnerID, documentID)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	// The guard is the domain's; only the fetch of the un-replayed values is
	// the handler's (Quality Rule 3). now is passed in for BR-TP67's second
	// expiry check — a certificate can sit in FOR_REVIEW until its expiry
	// passes, and approving it then would arm BR-TP60's timer on dead cover.
	now := time.Now().UTC()
	approved, err := doc.ApproveWithInsuranceDetails(insurerName, contactName, contactNumber, now)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	appender, err := h.certificateCommands(tenant)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	if err := h.docs.SetInsuranceContact(ctx, partnerID, documentID, insurerName, contactName, contactNumber); err != nil {
		return domain.ComplianceDocument{}, err
	}
	if _, err := appender.ApproveCertificate(ctx, contextKey, partnerID, documentID, insurerName, now, actor.Name, actor.SourceIP); err != nil {
		return domain.ComplianceDocument{}, err
	}
	return approved, nil
}

// RejectGitDocument keeps the GIT projection replay-safe by recording the
// review verdict on the TransporterProfile stream.
func (h *ComplianceDocumentHandler) RejectGitDocument(ctx context.Context, tenant, contextKey, partnerID, documentID string, actor Actor) (domain.ComplianceDocument, error) {
	doc, err := h.docs.GetDocument(ctx, partnerID, documentID)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	rejected, err := doc.Reject()
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	appender, err := h.certificateCommands(tenant)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	if _, err := appender.RejectCertificate(ctx, contextKey, partnerID, documentID, actor.Name, actor.SourceIP); err != nil {
		return domain.ComplianceDocument{}, err
	}
	return rejected, nil
}

func (h *ComplianceDocumentHandler) ResubmitGitDocument(ctx context.Context, tenant, contextKey, partnerID, documentID string, actor Actor) (domain.ComplianceDocument, error) {
	doc, err := h.docs.GetDocument(ctx, partnerID, documentID)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	// The landing state is the domain's to decide (BR-TP11 with BR-TP68), and
	// the document read here carries the file the rule reads.
	resubmitted, err := doc.Resubmit()
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	appender, err := h.certificateCommands(tenant)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	if _, err := appender.ResubmitCertificate(ctx, contextKey, partnerID, documentID, actor.Name, actor.SourceIP); err != nil {
		return domain.ComplianceDocument{}, err
	}
	return resubmitted, nil
}

// UpdateGitDocument persists the edit view's ordinary certificate fields.
// Expiry stays on SetGitDocumentExpiry because BR-TP70 admits that one
// correction on a superseded certificate while locking everything here.
func (h *ComplianceDocumentHandler) UpdateGitDocument(ctx context.Context, tenant, contextKey, partnerID, documentID, reference string, goodsTypes []string, coverageCents *int64, insurerName, contactName, contactNumber string, actor Actor) (domain.ComplianceDocument, error) {
	doc, err := h.docs.GetDocument(ctx, partnerID, documentID)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	if doc.Status == domain.DocumentStatusSuperseded {
		return domain.ComplianceDocument{}, domain.ErrDocumentSuperseded
	}
	probe, err := domain.AddDocument(domain.PartnerTypeTransporter, domain.DocumentTypeGoodsInTransit, reference)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	probe.GoodsTypes, probe.CoverageCents = append([]string(nil), goodsTypes...), coverageCents
	if err := probe.ValidateGitCertificate(); err != nil {
		return domain.ComplianceDocument{}, err
	}
	if h.goodsTypes == nil {
		return domain.ComplianceDocument{}, errors.New("goods-type validator is not configured")
	}
	for _, code := range goodsTypes {
		exists, err := h.goodsTypes.GoodsTypeExists(ctx, tenant, contextKey, code)
		if err != nil {
			return domain.ComplianceDocument{}, err
		}
		if !exists {
			return domain.ComplianceDocument{}, fmt.Errorf("%w: %q", domain.ErrGoodsTypeNotFound, code)
		}
	}
	contactsChanged := contactName != doc.InsuranceContactName || contactNumber != doc.InsuranceContactNumber
	insuranceChanged := insurerName != doc.InsurerName || contactsChanged
	appender, err := h.certificateCommands(tenant)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	// Contacts land before the append, matching ApproveGitDocument and
	// deliberately *not* registration's append-first order. Registration
	// appends first because its projection row does not exist yet and the
	// projector will re-create it from the log either way. These two columns
	// are never on the log (BR-TP72), so the event that records them as
	// changed must not outlive a failed write of the values themselves — an
	// audit trail claiming a change that never landed is worse than a
	// retryable error here.
	if insuranceChanged {
		if err := h.docs.SetInsuranceContact(ctx, partnerID, documentID, insurerName, contactName, contactNumber); err != nil {
			return domain.ComplianceDocument{}, err
		}
	}
	if _, err := appender.UpdateCertificateDetails(ctx, contextKey, partnerID, documentID, reference, goodsTypes, coverageCents, insurerName, contactsChanged, actor.Name, actor.SourceIP); err != nil {
		return domain.ComplianceDocument{}, err
	}
	doc.Reference, doc.GoodsTypes, doc.CoverageCents = reference, append([]string(nil), goodsTypes...), coverageCents
	doc.InsurerName, doc.InsuranceContactName, doc.InsuranceContactNumber = insurerName, contactName, contactNumber
	return doc, nil
}

// SetGitDocumentExpiry is BR-TP59's correction routed onto the aggregate.
// A direct projection write would be replayed away by the next certificate
// event, so an event-sourced document's expiry has to move on the stream.
func (h *ComplianceDocumentHandler) SetGitDocumentExpiry(ctx context.Context, tenant, contextKey, partnerID, documentID string, expiresAt *int64, actor Actor) (domain.ComplianceDocument, error) {
	doc, err := h.docs.GetDocument(ctx, partnerID, documentID)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	now := time.Now().UTC()
	updated, err := doc.SetExpiry(expiresAt, now)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	appender, err := h.certificateCommands(tenant)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	if _, err := appender.SetCertificateExpiry(ctx, contextKey, partnerID, documentID, expiresAt, now, actor.Name, actor.SourceIP); err != nil {
		return domain.ComplianceDocument{}, err
	}
	return updated, nil
}

func (h *ComplianceDocumentHandler) certificateCommands(tenant string) (CertificateAppender, error) {
	if h.certificates == nil {
		return nil, errors.New("certificate appender is not configured")
	}
	return h.certificates.CertificateCommands(tenant)
}

// AddDocument implements BR-TP07/BR-TP08 — looks up the partner's type to
// enforce BR-TP07's per-type restriction before persisting.
// expiresAt is optional (BR-TP59); when supplied it goes through the same
// domain guard the standalone set-expiry command uses, so a past date cannot
// enter by the registration door instead of the edit one.
func (h *ComplianceDocumentHandler) AddDocument(ctx context.Context, partnerID string, docType domain.DocumentType, reference string, expiresAt *int64) (domain.ComplianceDocument, error) {
	tp, err := h.partners.Get(ctx, partnerID)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	doc, err := domain.AddDocument(tp.Type, docType, reference)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	if expiresAt != nil {
		if doc, err = doc.SetExpiry(expiresAt, time.Now().UTC()); err != nil {
			return domain.ComplianceDocument{}, err
		}
	}
	return h.docs.AddDocument(ctx, partnerID, doc)
}

// SetDocumentExpiry implements BR-TP59's after-the-fact edit. The guard lives
// in the repository, applied against the locked row, so this is a pass-through
// rather than a second place the rule could drift.
func (h *ComplianceDocumentHandler) SetDocumentExpiry(ctx context.Context, partnerID, documentID string, expiresAt *int64) (domain.ComplianceDocument, error) {
	return h.docs.SetDocumentExpiry(ctx, partnerID, documentID, expiresAt)
}

// GetDocument reads one document by ID *including* superseded rows. The
// vetting workflow's decision-14 state read needs that: a required reference
// that has been superseded must be visible as superseded, not indistinguishable
// from one whose row was never written.
func (h *ComplianceDocumentHandler) GetDocument(ctx context.Context, partnerID, documentID string) (domain.ComplianceDocument, error) {
	return h.docs.GetDocument(ctx, partnerID, documentID)
}

func (h *ComplianceDocumentHandler) ListDocuments(ctx context.Context, partnerID string) ([]domain.ComplianceDocument, error) {
	return h.docs.ListDocuments(ctx, partnerID)
}

func (h *ComplianceDocumentHandler) ListGitCertificates(ctx context.Context, partnerID string) ([]domain.ComplianceDocument, error) {
	return h.docs.ListGitCertificates(ctx, partnerID)
}

// The three review transitions address a document by ID (BR-TP31), not by
// type — after BR-TP29 a type no longer identifies a single row.
func (h *ComplianceDocumentHandler) ApproveDocument(ctx context.Context, partnerID, documentID string) (domain.ComplianceDocument, error) {
	return h.docs.ApproveDocument(ctx, partnerID, documentID)
}

func (h *ComplianceDocumentHandler) RejectDocument(ctx context.Context, partnerID, documentID string) (domain.ComplianceDocument, error) {
	return h.docs.RejectDocument(ctx, partnerID, documentID)
}

func (h *ComplianceDocumentHandler) ResubmitDocument(ctx context.Context, partnerID, documentID string) (domain.ComplianceDocument, error) {
	return h.docs.ResubmitDocument(ctx, partnerID, documentID)
}
