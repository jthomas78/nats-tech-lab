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

	// UpdateDetails edits Company Information under BR-TP34's optimistic
	// concurrency guard. expectedVersion is the version the caller read; a
	// mismatch is ErrVersionConflict and writes nothing.
	UpdateDetails(ctx context.Context, id string, expectedVersion int, details Details) (TradingPartner, error)
}

// ComplianceDocumentRepository persists ComplianceDocuments
// (BR-TP07-BR-TP11, BR-TP29-BR-TP31). Documents are keyed by a
// service-minted ID (BR-TP29), not by (partner, type) — the transitions
// therefore address a document by ID. Keeping one *current* document per
// (partner, type) is BR-TP30's supersession, applied inside AddDocument.
type ComplianceDocumentRepository interface {
	AddDocument(ctx context.Context, partnerID string, doc ComplianceDocument) (ComplianceDocument, error)
	ListDocuments(ctx context.Context, partnerID string) ([]ComplianceDocument, error)
	ApproveDocument(ctx context.Context, partnerID, documentID string) (ComplianceDocument, error)
	RejectDocument(ctx context.Context, partnerID, documentID string) (ComplianceDocument, error)
	ResubmitDocument(ctx context.Context, partnerID, documentID string) (ComplianceDocument, error)

	// GetDocument reads one document by ID including superseded rows —
	// BR-TP43 keeps their bytes retrievable, so a download needs to find them.
	GetDocument(ctx context.Context, partnerID, documentID string) (ComplianceDocument, error)

	// AttachFile records uploaded bytes against a document (BR-TP45),
	// applying AttachFile's write-once guard under a row lock.
	AttachFile(ctx context.Context, partnerID, documentID string, file DocumentFile) (ComplianceDocument, error)
}

// FleetAssetRepository persists FleetAssets (BR-TP12/BR-TP13), keyed by
// registrationNo — BR-TP13's global-uniqueness invariant is the primary
// key, not a separate surrogate ID.
type FleetAssetRepository interface {
	AddFleetAsset(ctx context.Context, partnerID string, asset FleetAsset) (FleetAsset, error)
	ListFleetAssets(ctx context.Context, partnerID string) ([]FleetAsset, error)
}

// OperatingAreaRepository is BR-TP46-BR-TP50's persistence port.
//
// ListOperatingAreas is not just a read for the UI: BR-TP48 needs the
// partner's whole current set on every add to evaluate overlap, so the
// command path calls it before every write.
type OperatingAreaRepository interface {
	AddOperatingArea(ctx context.Context, partnerID string, area OperatingArea) (OperatingArea, error)
	ListOperatingAreas(ctx context.Context, partnerID string) ([]OperatingArea, error)
	RemoveOperatingArea(ctx context.Context, partnerID string, level AreaLevel, code string) error
}

// TrackingCredentialRepository is BR-TP51/BR-TP54's persistence port. It
// carries no payload accessor by design (BR-TP52) — the secret's only home
// is the sealed KV bucket, reached through a separate port.
type TrackingCredentialRepository interface {
	UpsertTrackingCredential(ctx context.Context, partnerID string, cred TrackingCredential) error
	ListTrackingCredentials(ctx context.Context, partnerID string) ([]TrackingCredential, error)
}
