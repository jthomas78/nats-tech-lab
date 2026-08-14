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
)

// ComplianceDocument is one KYC/compliance record attached to a
// TradingPartner (BR-TP07-BR-TP11). Storage is metadata-only in v1:
// Reference is an opaque external locator, no file bytes are held here.
// CoverageCents/ExpiresAt are both nullable and carry no domain-level
// restriction on which DocumentType may set them — see the Phase 26 plan
// section's storage decision.
type ComplianceDocument struct {
	Type          DocumentType   `json:"type"`
	Status        DocumentStatus `json:"status"`
	Reference     string         `json:"reference"`
	ExpiresAt     *int64         `json:"expiresAt,omitempty"`
	CoverageCents *int64         `json:"coverageCents,omitempty"`
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
// The "at most one per (partner, type)" invariant is a repository-level
// upsert, not enforced here (deferred to 26d, same scoping as BR-TP06).
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

// Approve implements BR-TP09 — legal only Pending -> Approved.
func (d ComplianceDocument) Approve() (ComplianceDocument, error) {
	if d.Status != DocumentStatusPending {
		return d, ErrDocumentNotPending
	}
	d.Status = DocumentStatusApproved
	return d, nil
}

// Reject implements BR-TP10 — legal only Pending -> Rejected.
func (d ComplianceDocument) Reject() (ComplianceDocument, error) {
	if d.Status != DocumentStatusPending {
		return d, ErrDocumentNotPending
	}
	d.Status = DocumentStatusRejected
	return d, nil
}

// Resubmit implements BR-TP11 — legal only Rejected -> Pending. There is no
// Approved -> anything transition in v1: an approved document is never
// un-approved or re-reviewed once decided.
func (d ComplianceDocument) Resubmit() (ComplianceDocument, error) {
	if d.Status != DocumentStatusRejected {
		return d, ErrDocumentNotRejected
	}
	d.Status = DocumentStatusPending
	return d, nil
}
