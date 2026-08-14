package domain

import "context"

// TradingPartnerRepository persists TradingPartner aggregates and their
// Register/Activate/Suspend/Reactivate lifecycle (BR-TP01-BR-TP05). ID is
// server-generated at Register — no field the caller supplies is a stable
// enough natural key (name is not guaranteed unique per context).
type TradingPartnerRepository interface {
	Register(ctx context.Context, tp TradingPartner) (TradingPartner, error)
	Get(ctx context.Context, id string) (TradingPartner, error)
	List(ctx context.Context, contextKey string) ([]TradingPartner, error)
	Activate(ctx context.Context, id string) (TradingPartner, error)
	Suspend(ctx context.Context, id string, reason string) (TradingPartner, error)
	Reactivate(ctx context.Context, id string) (TradingPartner, error)
}

// ComplianceDocumentRepository persists ComplianceDocuments (BR-TP07-BR-TP11),
// keyed by (partner, type) — BR-TP08's one-per-type invariant is a
// repository-level upsert, not a separate document ID.
type ComplianceDocumentRepository interface {
	AddDocument(ctx context.Context, partnerID string, doc ComplianceDocument) (ComplianceDocument, error)
	ListDocuments(ctx context.Context, partnerID string) ([]ComplianceDocument, error)
	ApproveDocument(ctx context.Context, partnerID string, docType DocumentType) (ComplianceDocument, error)
	RejectDocument(ctx context.Context, partnerID string, docType DocumentType) (ComplianceDocument, error)
	ResubmitDocument(ctx context.Context, partnerID string, docType DocumentType) (ComplianceDocument, error)
}

// FleetAssetRepository persists FleetAssets (BR-TP12/BR-TP13), keyed by
// registrationNo — BR-TP13's global-uniqueness invariant is the primary
// key, not a separate surrogate ID.
type FleetAssetRepository interface {
	AddFleetAsset(ctx context.Context, partnerID string, asset FleetAsset) (FleetAsset, error)
	ListFleetAssets(ctx context.Context, partnerID string) ([]FleetAsset, error)
}
