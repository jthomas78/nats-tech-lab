package domain

import "errors"

// Draft/publish/rollback errors for FixedRateVersion (BR-P14) — kept
// distinct from FeeScaleVersion's/RateSheetVersion's rather than shared via
// a common type, so those files' already-shipped literal shapes stay
// untouched.
var (
	ErrFixedRateDraftAlreadyExists         = errors.New("a draft already exists for this fixed rate")
	ErrFixedRateOnlyDraftCanPublish        = errors.New("only a draft version can be published")
	ErrFixedRateRollbackTargetNotPublished = errors.New("rollback target must be a published version")

	// ErrFixedRateNotFound — no FixedRate registered for this context+name.
	ErrFixedRateNotFound = errors.New("fixed rate not found")

	// ErrFixedRateDraftNotFound — Publish found no draft version to publish.
	ErrFixedRateDraftNotFound = errors.New("no draft version exists for this fixed rate")

	// ErrNoActiveFixedRateVersion — BR-P13/BR-P14: no eligible published
	// version exists (either none published yet, or the fixed rate is
	// inactive).
	ErrNoActiveFixedRateVersion = errors.New("no active version exists for this fixed rate")
)

// FixedRate is a customer-route-specific contracted rate (BR-P13).
// CustomerKey/RouteKey are opaque identifiers pricing-service owns itself.
type FixedRate struct {
	Name        string `json:"name"`
	Context     string `json:"context"`
	CustomerKey string `json:"customerKey"`
	RouteKey    string `json:"routeKey"`
	Active      bool   `json:"active"`
}

// FixedRateVersion is one version in a FixedRate's draft/published/
// rolled-back history (BR-P14). Diesel-driven sub-versioning is deferred
// to Phase 25j (FixedRate overlay) — for now only RateSheet carries the
// effective-dated diesel overlay (Phase 25i).
type FixedRateVersion struct {
	Context                string        `json:"context"`
	Version                int           `json:"version"`
	Status                 VersionStatus `json:"status"`
	ParentVersion          *int          `json:"parentVersion,omitempty"`
	RolledBackBy           *int          `json:"rolledBackBy,omitempty"`
	CentRate               int64         `json:"centRate"`
	PointCount             int           `json:"pointCount"`
	CentAdditionalDropRate int64         `json:"centAdditionalDropRate"`
}

// AdditionalDropsCharge applies BR-P15's drop-charge formula — the same
// shape as RateSheetEntry.AdditionalDropsCharge (BR-P12), against this
// version's own point count/rate.
func (v FixedRateVersion) AdditionalDropsCharge(addressCount int) int64 {
	extra := addressCount - v.PointCount
	if extra < 0 {
		extra = 0
	}
	return int64(extra) * v.CentAdditionalDropRate
}

// CanCreateFixedRateDraft enforces BR-P14 — at most one draft per fixed rate.
func CanCreateFixedRateDraft(versions []FixedRateVersion) error {
	for _, v := range versions {
		if v.Status == VersionDraft {
			return ErrFixedRateDraftAlreadyExists
		}
	}
	return nil
}

// CanPublish enforces BR-P14 — only a draft version can be published.
func (v FixedRateVersion) CanPublish() error {
	if v.Status != VersionDraft {
		return ErrFixedRateOnlyDraftCanPublish
	}
	return nil
}

// CanRollbackFixedRateTo enforces BR-P14 — rollback may only target an
// already-published version.
func CanRollbackFixedRateTo(v FixedRateVersion) error {
	if v.Status != VersionPublished {
		return ErrFixedRateRollbackTargetNotPublished
	}
	return nil
}

// ActiveFixedRateVersion resolves BR-P13 and BR-P14 together — version
// resolution is only eligible when the fixed rate itself is active, and the
// active version is the highest-numbered published one.
func ActiveFixedRateVersion(fr FixedRate, versions []FixedRateVersion) (FixedRateVersion, bool) {
	if !fr.Active {
		return FixedRateVersion{}, false
	}
	var active FixedRateVersion
	found := false
	for _, v := range versions {
		if v.Status != VersionPublished {
			continue
		}
		if !found || v.Version > active.Version {
			active = v
			found = true
		}
	}
	return active, found
}
