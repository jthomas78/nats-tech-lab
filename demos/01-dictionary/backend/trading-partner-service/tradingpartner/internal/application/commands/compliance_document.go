package commands

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
)

// ComplianceDocumentHandler is a thin pass-through onto
// domain.ComplianceDocumentRepository — no audit trail (BR-TP06 covers only
// the TradingPartner lifecycle, not document review, which has no
// enforcement consequence in v1 either).
type ComplianceDocumentHandler struct {
	partners domain.TradingPartnerRepository
	docs     domain.ComplianceDocumentRepository
}

func NewComplianceDocumentHandler(partners domain.TradingPartnerRepository, docs domain.ComplianceDocumentRepository) *ComplianceDocumentHandler {
	return &ComplianceDocumentHandler{partners: partners, docs: docs}
}

// AddDocument implements BR-TP07/BR-TP08 — looks up the partner's type to
// enforce BR-TP07's per-type restriction before persisting.
func (h *ComplianceDocumentHandler) AddDocument(ctx context.Context, partnerID string, docType domain.DocumentType, reference string) (domain.ComplianceDocument, error) {
	tp, err := h.partners.Get(ctx, partnerID)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	doc, err := domain.AddDocument(tp.Type, docType, reference)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	return h.docs.AddDocument(ctx, partnerID, doc)
}

func (h *ComplianceDocumentHandler) ListDocuments(ctx context.Context, partnerID string) ([]domain.ComplianceDocument, error) {
	return h.docs.ListDocuments(ctx, partnerID)
}

func (h *ComplianceDocumentHandler) ApproveDocument(ctx context.Context, partnerID string, docType domain.DocumentType) (domain.ComplianceDocument, error) {
	return h.docs.ApproveDocument(ctx, partnerID, docType)
}

func (h *ComplianceDocumentHandler) RejectDocument(ctx context.Context, partnerID string, docType domain.DocumentType) (domain.ComplianceDocument, error) {
	return h.docs.RejectDocument(ctx, partnerID, docType)
}

func (h *ComplianceDocumentHandler) ResubmitDocument(ctx context.Context, partnerID string, docType domain.DocumentType) (domain.ComplianceDocument, error) {
	return h.docs.ResubmitDocument(ctx, partnerID, docType)
}
