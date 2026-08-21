package commands

import (
	"context"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
)

// ComplianceDocumentHandler is a thin pass-through onto
// domain.ComplianceDocumentRepository — no audit trail (BR-TP06 covers only
// the Organization lifecycle, not document review, which has no
// enforcement consequence in v1 either).
type ComplianceDocumentHandler struct {
	partners domain.OrganizationRepository
	docs     domain.ComplianceDocumentRepository
}

func NewComplianceDocumentHandler(partners domain.OrganizationRepository, docs domain.ComplianceDocumentRepository) *ComplianceDocumentHandler {
	return &ComplianceDocumentHandler{partners: partners, docs: docs}
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

func (h *ComplianceDocumentHandler) ListDocuments(ctx context.Context, partnerID string) ([]domain.ComplianceDocument, error) {
	return h.docs.ListDocuments(ctx, partnerID)
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
