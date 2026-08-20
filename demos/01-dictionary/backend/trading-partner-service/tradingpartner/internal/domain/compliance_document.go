package domain

import "errors"

// DocumentType is the controlled vocabulary of compliance documents a
// TradingPartner may carry (BR-TP07) — a trimmed subset of V2's
// DocumentTypes enum, kept as a Go enum rather than a refdata vocabulary in
// v1 (see the Phase 26 plan section: per-type rules like BR-TP07's
// partner-type restriction are domain logic, not lookup data).
type DocumentType string

const (
	DocumentTypeCIPC                   DocumentType = "CIPC"
	DocumentTypeDirectorID             DocumentType = "DIRECTOR_ID"
	DocumentTypeBankConfirmationLetter DocumentType = "BANK_CONFIRMATION_LETTER"
	DocumentTypeTermsAndConditions     DocumentType = "TERMS_AND_CONDITIONS"
	DocumentTypeGoodsInTransit         DocumentType = "GOODS_IN_TRANSIT"
)

// DocumentStatus is the Pending -> Approved / Pending -> Rejected ->
// Resubmit-to-Pending lifecycle (BR-TP09-BR-TP11), fully independent of the
// parent TradingPartner.status (BR-TP04's note).
type DocumentStatus string

const (
	DocumentStatusPending  DocumentStatus = "PENDING"
	DocumentStatusApproved DocumentStatus = "APPROVED"
	DocumentStatusRejected DocumentStatus = "REJECTED"

	// DocumentStatusSuperseded — BR-TP30 (38c-i). Terminal. Reached when a
	// newer document of the same type replaces this one. Retained for audit;
	// never returned by document.list (BR-TP31).
	DocumentStatusSuperseded DocumentStatus = "SUPERSEDED"
)

var (
	// ErrInvalidDocumentType — BR-TP07: type must be one of the controlled
	// vocabulary.
	ErrInvalidDocumentType = errors.New("compliance document type is not recognized")

	// ErrDocumentTypeNotAllowedForPartnerType — BR-TP07: GOODS_IN_TRANSIT is
	// Transporter-only.
	ErrDocumentTypeNotAllowedForPartnerType = errors.New("this document type is not valid for the trading partner's type")

	// ErrReferenceRequired — BR-TP08: a document must point at something.
	ErrReferenceRequired = errors.New("a reference is required to add a compliance document")

	// ErrDocumentNotPending — BR-TP09/BR-TP10: Approve/Reject are only
	// legal from Pending.
	ErrDocumentNotPending = errors.New("compliance document is not in Pending status")

	// ErrDocumentNotRejected — BR-TP11: Resubmit is only legal from
	// Rejected.
	ErrDocumentNotRejected = errors.New("compliance document is not in Rejected status")

	// ErrDocumentNotFound — no ComplianceDocument of the requested type
	// exists for the partner.
	ErrDocumentNotFound = errors.New("compliance document not found")

	// ErrDocumentSuperseded — BR-TP30: SUPERSEDED is terminal, so every
	// transition off it is rejected, including a second Supersede. This is a
	// distinct error from ErrDocumentNotPending on purpose: "this document
	// has been replaced by a newer one" is different advice to an operator
	// than "this document is already decided."
	ErrDocumentSuperseded = errors.New("compliance document has been superseded")

	// ErrFileNameRequired — BR-TP45: the original filename is the one piece
	// of the upload that only the client knows, and it is what a download
	// hands back, so an empty one is rejected rather than defaulted.
	ErrFileNameRequired = errors.New("a file name is required to attach a document file")

	// ErrContentTypeRequired — BR-TP45: download replays this verbatim as
	// the response Content-Type, so it must be recorded at upload time.
	ErrContentTypeRequired = errors.New("a content type is required to attach a document file")

	// ErrFileEmpty — BR-TP44: a zero-byte upload is rejected. It would
	// otherwise produce a document the log asserts has a file, whose
	// retrieval returns nothing — the same broken promise as a dangling
	// reference, reached by a different route.
	ErrFileEmpty = errors.New("document file is empty")

	// ErrFileTooLarge — BR-TP44: per-file cap at the service boundary.
	ErrFileTooLarge = errors.New("document file exceeds the maximum permitted size")

	// ErrDocumentFileAlreadyAttached — BR-TP43: a document's bytes are
	// write-once. Replacing them would purge bytes the event log still
	// references, which is precisely the failure ADR-048's service-minted
	// object name exists to prevent. Superseding the document and uploading
	// against its replacement is the supported path (BR-TP30).
	ErrDocumentFileAlreadyAttached = errors.New("compliance document already has a file attached")

	// ErrDocumentFileMissing — BR-TP45: download was asked for a document
	// whose bytes were never uploaded. Distinct from ErrDocumentNotFound:
	// the document exists, the file does not.
	ErrDocumentFileMissing = errors.New("compliance document has no file attached")
)

// MaxDocumentFileBytes is BR-TP44's per-file cap, enforced at the service
// boundary on the bytes actually read — never on a client-declared
// Content-Length, which a client controls and can understate.
//
// 10 MiB is ample for the scanned PDFs this models (CIPC registration,
// director ID, a GIT certificate) while keeping the bucket's own MaxBytes
// budget meaningful: document bytes and the TRANSPORTER event log compete
// for the same 1 GiB of tenant JetStream storage, so an uncapped upload path
// can stop event publishing for the whole tenant. See ADR-048.
const MaxDocumentFileBytes int64 = 10 << 20

// DocumentFile is the metadata of a compliance document's stored bytes
// (BR-TP45). The bytes themselves live in the NATS Object Store; this is the
// projection that lets a download set correct response headers, and a list
// show a file name and size, without touching the object store at all.
type DocumentFile struct {
	// FileName is the client's original name. It is deliberately *not* part
	// of ObjectName (BR-TP42) — a user-controlled object name makes object
	// identity user-controlled, and two uploads sharing a name would resolve
	// to one object, silently purging the earlier document's bytes while the
	// log still records both.
	FileName    string `json:"fileName"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
	// ObjectName is the service-minted object store key (BR-TP42), stored so
	// a reader never has to reconstruct it from parts.
	ObjectName string `json:"objectName"`
	UploadedAt int64  `json:"uploadedAt"`
}

// DocumentObjectName implements BR-TP42 — the object store key is composed
// entirely of values the service controls. It mirrors the repo's KV key
// convention ({context}.{entityType}.{id}) with the document type and the
// service-minted document ID appended, rather than inventing a second naming
// scheme for the same tenant-scoped storage.
//
// documentID is BR-TP29's document ID, which BR-TP36 already pinned as the
// single identifier shared by the projection row, the vetting workflow's
// document reference, and this object name.
func DocumentObjectName(context, partnerID string, docType DocumentType, documentID string) string {
	return context + ".transporter." + partnerID + "." + string(docType) + "." + documentID
}

// ComplianceDocument is one KYC/compliance record attached to a
// TradingPartner (BR-TP07-BR-TP11). Storage is metadata-only in v1:
// Reference is an opaque external locator, no file bytes are held here.
// CoverageCents/ExpiresAt are both nullable and carry no domain-level
// restriction on which DocumentType may set them — see the Phase 26 plan
// section's storage decision.
type ComplianceDocument struct {
	// ID is service-minted (BR-TP29, 38c-i) — never supplied by the caller,
	// same treatment as TradingPartner.ID. Empty until the repository has
	// persisted the document. This is also the vetting workflow's document
	// reference (BR-TP36) and the {documentID} token in 38c-ii's Object
	// Store object name, so one identifier serves all three.
	ID            string         `json:"id,omitempty"`
	Type          DocumentType   `json:"type"`
	Status        DocumentStatus `json:"status"`
	Reference     string         `json:"reference"`
	ExpiresAt     *int64         `json:"expiresAt,omitempty"`
	CoverageCents *int64         `json:"coverageCents,omitempty"`
	// File is nil until bytes have been uploaded (BR-TP45). A document
	// legitimately exists without one: 38c-i's document-add creates the row
	// (and mints the ID the object name needs) before any file arrives, and
	// the review lifecycle never required a file to progress.
	File *DocumentFile `json:"file,omitempty"`
}

// ValidateDocumentType implements BR-TP07 — every type in the shared subset
// is valid for either partner type; GOODS_IN_TRANSIT is valid only for a
// TRANSPORTER.
func ValidateDocumentType(partnerType PartnerType, docType DocumentType) error {
	switch docType {
	case DocumentTypeCIPC, DocumentTypeDirectorID, DocumentTypeBankConfirmationLetter, DocumentTypeTermsAndConditions:
		return nil
	case DocumentTypeGoodsInTransit:
		if partnerType != PartnerTypeTransporter {
			return ErrDocumentTypeNotAllowedForPartnerType
		}
		return nil
	default:
		return ErrInvalidDocumentType
	}
}

// AddDocument implements BR-TP08 — requires a non-empty reference and a
// type valid for the partner's type, always creates in Pending status.
//
// BR-TP29 (38c-i): this always produces a *new* document, never an update of
// an existing one. Keeping one current document per (partner, type) is the
// repository's job: it supersedes the incumbent (BR-TP30) and inserts this
// one. The returned document has no ID yet — the service mints it on insert.
func AddDocument(partnerType PartnerType, docType DocumentType, reference string) (ComplianceDocument, error) {
	if reference == "" {
		return ComplianceDocument{}, ErrReferenceRequired
	}
	if err := ValidateDocumentType(partnerType, docType); err != nil {
		return ComplianceDocument{}, err
	}
	return ComplianceDocument{
		Type:      docType,
		Status:    DocumentStatusPending,
		Reference: reference,
	}, nil
}

// AttachFile implements BR-TP43/BR-TP44/BR-TP45 — records uploaded bytes
// against an existing document.
//
// Write-once by design: there is no ReplaceFile. Overwriting an object would
// purge bytes that the immutable event log still references, and an event can
// only be compensated, not retracted (ADR-047's constraint, one layer up).
// The supported way to correct a file is BR-TP30's supersede-and-replace,
// which leaves both objects retrievable.
//
// Validation is here rather than in the HTTP handler because "how large may a
// compliance document be" and "must it carry a name" are business rules, not
// transport concerns — the handler's job is only to stop reading once the cap
// is exceeded, and then to ask this method.
func (d ComplianceDocument) AttachFile(f DocumentFile) (ComplianceDocument, error) {
	if d.Status == DocumentStatusSuperseded {
		return d, ErrDocumentSuperseded
	}
	if d.File != nil {
		return d, ErrDocumentFileAlreadyAttached
	}
	if f.FileName == "" {
		return d, ErrFileNameRequired
	}
	if f.ContentType == "" {
		return d, ErrContentTypeRequired
	}
	if f.SizeBytes <= 0 {
		return d, ErrFileEmpty
	}
	if f.SizeBytes > MaxDocumentFileBytes {
		return d, ErrFileTooLarge
	}
	d.File = &f
	return d, nil
}

// Approve implements BR-TP09 — legal only Pending -> Approved.
func (d ComplianceDocument) Approve() (ComplianceDocument, error) {
	if d.Status == DocumentStatusSuperseded {
		return d, ErrDocumentSuperseded
	}
	if d.Status != DocumentStatusPending {
		return d, ErrDocumentNotPending
	}
	d.Status = DocumentStatusApproved
	return d, nil
}

// Reject implements BR-TP10 — legal only Pending -> Rejected.
func (d ComplianceDocument) Reject() (ComplianceDocument, error) {
	if d.Status == DocumentStatusSuperseded {
		return d, ErrDocumentSuperseded
	}
	if d.Status != DocumentStatusPending {
		return d, ErrDocumentNotPending
	}
	d.Status = DocumentStatusRejected
	return d, nil
}

// Resubmit implements BR-TP11 — legal only Rejected -> Pending. There is no
// Approved -> anything transition via this method: an approved document is
// never un-approved or re-reviewed once decided. (Supersede is the one
// transition that may leave Approved — see BR-TP30 and Supersede's own
// comment for why that is not an un-approval.)
func (d ComplianceDocument) Resubmit() (ComplianceDocument, error) {
	if d.Status == DocumentStatusSuperseded {
		return d, ErrDocumentSuperseded
	}
	if d.Status != DocumentStatusRejected {
		return d, ErrDocumentNotRejected
	}
	d.Status = DocumentStatusPending
	return d, nil
}

// Supersede implements BR-TP30 — legal from Pending, Approved and Rejected
// alike, terminal once applied.
//
// Superseding an Approved document is deliberately legal, and is the one
// place BR-TP11's "no transition off Approved" is amended. It is not an
// un-approval: the approval stands over the record it was given for, and
// supersession retires that record because a newer document has replaced it.
// The replacement starts its own review at Pending, so nothing inherits the
// old approval.
//
// This replaces BR-TP08's upsert, which reached the same end state by
// destroying the previous row — losing exactly the history an audit of "what
// was approved, and when" needs.
func (d ComplianceDocument) Supersede() (ComplianceDocument, error) {
	if d.Status == DocumentStatusSuperseded {
		return d, ErrDocumentSuperseded
	}
	d.Status = DocumentStatusSuperseded
	return d, nil
}
