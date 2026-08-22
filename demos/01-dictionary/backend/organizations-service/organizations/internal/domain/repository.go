package domain

import "context"

// OrganizationRepository persists Organization aggregates and their
// Register/Activate/Suspend/Reactivate lifecycle (BR-TP01-BR-TP05). ID is
// server-generated at Register — no field the caller supplies is a stable
// enough natural key (name is not guaranteed unique per context).
type OrganizationRepository interface {
	Register(ctx context.Context, tp Organization) (Organization, error)
	Get(ctx context.Context, id string) (Organization, error)
	List(ctx context.Context, contextKey string) ([]Organization, error)
	Activate(ctx context.Context, id string) (Organization, error)
	Suspend(ctx context.Context, id string, reason string) (Organization, error)
	Reactivate(ctx context.Context, id string) (Organization, error)

	// UpdateDetails edits Company Information under BR-TP34's optimistic
	// concurrency guard. expectedVersion is the version the caller read; a
	// mismatch is ErrVersionConflict and writes nothing.
	UpdateDetails(ctx context.Context, id string, expectedVersion int, details Details) (Organization, error)
}

// ComplianceDocumentRepository persists ComplianceDocuments
// (BR-TP07-BR-TP11, BR-TP29-BR-TP31). Documents are keyed by a
// service-minted ID (BR-TP29), not by (partner, type) — the transitions
// therefore address a document by ID. Keeping one *current* document per
// (partner, type) is BR-TP30's supersession, applied inside AddDocument.
type ComplianceDocumentRepository interface {
	AddDocument(ctx context.Context, partnerID string, doc ComplianceDocument) (ComplianceDocument, error)
	// SetDocumentExpiry is BR-TP59's after-the-fact edit. Separate from
	// AddDocument because an expiry is corrected and renewed far more often
	// than a document is replaced.
	SetDocumentExpiry(ctx context.Context, partnerID, documentID string, expiresAt *int64) (ComplianceDocument, error)
	ListDocuments(ctx context.Context, partnerID string) ([]ComplianceDocument, error)
	// ListGitCertificates is Phase 39's history read: GIT-only, superseded
	// rows included, newest registration first. It deliberately does not
	// alter ListDocuments' BR-TP31 current-document semantics.
	ListGitCertificates(ctx context.Context, partnerID string) ([]ComplianceDocument, error)
	ApproveDocument(ctx context.Context, partnerID, documentID string) (ComplianceDocument, error)
	RejectDocument(ctx context.Context, partnerID, documentID string) (ComplianceDocument, error)
	ResubmitDocument(ctx context.Context, partnerID, documentID string) (ComplianceDocument, error)

	// GetDocument reads one document by ID including superseded rows —
	// BR-TP43 keeps their bytes retrievable, so a download needs to find them.
	GetDocument(ctx context.Context, partnerID, documentID string) (ComplianceDocument, error)

	// SetInsuranceContact writes the three approval-time insurance fields
	// directly to the projection row (decision 25). Two of them — the contact
	// name and number — never appear on the stream at all (BR-TP72), so this
	// is the only path by which they are ever stored, and a rebuild from
	// replay deliberately restores them as NULL.
	SetInsuranceContact(ctx context.Context, partnerID, documentID, insurerName, contactName, contactNumber string) error

	// UpsertCertificate writes one GIT certificate's replayed fields into the
	// projection. It deliberately does not touch the two contact columns:
	// they are not on the stream, so a projection write that included them
	// would blank the only copy that exists.
	UpsertCertificate(ctx context.Context, partnerID string, cert ProjectedCertificate) error

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
