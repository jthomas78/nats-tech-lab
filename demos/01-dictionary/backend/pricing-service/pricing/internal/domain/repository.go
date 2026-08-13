package domain

import "context"

// FeeScaleRepository persists FeeScale aggregates and their
// draft/published/rolled-back version history (BR-P01–BR-P06).
// Implementations must make Publish and Rollback transactional: the status
// transition and its range rows either commit together or not at all.
type FeeScaleRepository interface {
	Register(ctx context.Context, fs FeeScale) error
	Get(ctx context.Context, contextKey, name string) (FeeScale, error)
	List(ctx context.Context, contextKey string) ([]FeeScale, error)
	CreateDraft(ctx context.Context, contextKey, name string) (FeeScaleVersion, error)
	AddRange(ctx context.Context, contextKey, name string, version int, r FeeScaleRange) error
	Publish(ctx context.Context, contextKey, name string) (FeeScaleVersion, error)
	Rollback(ctx context.Context, contextKey, name string, targetVersion int) (FeeScaleVersion, error)
	Versions(ctx context.Context, contextKey, name string) ([]FeeScaleVersion, error)
	ActiveVersion(ctx context.Context, contextKey, name string) (FeeScaleVersion, error)
}

// RateSheetRepository persists RateSheet aggregates and their version
// history (BR-P07–BR-P12).
type RateSheetRepository interface {
	Register(ctx context.Context, rs RateSheet) error
	Get(ctx context.Context, contextKey, name string) (RateSheet, error)
	List(ctx context.Context, contextKey string) ([]RateSheet, error)
	CreateDraft(ctx context.Context, contextKey, name string) (RateSheetVersion, error)
	AddEntry(ctx context.Context, contextKey, name string, version int, e RateSheetEntry) error
	SetFeeScaleOverride(ctx context.Context, contextKey, name string, version int, feeScaleName string) error
	Publish(ctx context.Context, contextKey, name string) (RateSheetVersion, error)
	Rollback(ctx context.Context, contextKey, name string, targetVersion int) (RateSheetVersion, error)
	Versions(ctx context.Context, contextKey, name string) ([]RateSheetVersion, error)
	ActiveVersion(ctx context.Context, contextKey, name string) (RateSheetVersion, error)

	// Diesel overlay (Phase 25i, BR-P17–BR-P23).
	IndexDieselPrice(ctx context.Context, contextKey string, price DieselPrice) error
	ListDieselPrices(ctx context.Context, contextKey string) ([]DieselPrice, error)
	// PersistDieselOverlay writes the result of a domain.AppendDieselOverlay call:
	// bumps minor_version on the version row, closes open overlay windows, and
	// inserts the new minor-version overlay rows.
	PersistDieselOverlay(ctx context.Context, contextKey, name string, v RateSheetVersion) error
}

// FixedRateRepository persists FixedRate aggregates and their version
// history (BR-P13–BR-P15). A version's rate fields are set at draft
// creation, not added incrementally, since (unlike FeeScale ranges/
// RateSheet entries) they are scalar fields on the version itself.
type FixedRateRepository interface {
	Register(ctx context.Context, fr FixedRate) error
	Get(ctx context.Context, contextKey, name string) (FixedRate, error)
	List(ctx context.Context, contextKey string) ([]FixedRate, error)
	CreateDraft(ctx context.Context, contextKey, name string, centRate int64, pointCount int, centAdditionalDropRate int64) (FixedRateVersion, error)
	Publish(ctx context.Context, contextKey, name string) (FixedRateVersion, error)
	Rollback(ctx context.Context, contextKey, name string, targetVersion int) (FixedRateVersion, error)
	Versions(ctx context.Context, contextKey, name string) ([]FixedRateVersion, error)
	ActiveVersion(ctx context.Context, contextKey, name string) (FixedRateVersion, error)
}
