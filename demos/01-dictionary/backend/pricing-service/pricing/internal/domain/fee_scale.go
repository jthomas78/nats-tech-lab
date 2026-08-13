// Package domain holds the pricing-service domain model. FeeScale
// schedules are plain Postgres CRUD wrapped in the same corpus-style
// draft/published/rolled-back lifecycle refdata-service uses for its
// versioned reference data (see obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md
// § "Event Sourcing vs Plain CRUD") — nothing here is ever replayed from a
// log; "what fee schedule was in effect" is answered by querying the
// latest published version, not by reconstructing history.
package domain

import (
	"errors"
	"math"
)

// VersionStatus mirrors refdata-service's corpus lifecycle (BR-P02):
// draft -> published, with rollback producing a new forward-numbered
// published version rather than rewriting history.
type VersionStatus string

const (
	VersionDraft      VersionStatus = "draft"
	VersionPublished  VersionStatus = "published"
	VersionRolledBack VersionStatus = "rolled-back"
)

// RateType is the controlled vocabulary for how a FeeScaleRange charges
// (BR-P04) — never both a flat amount and a percentage on the same range.
type RateType string

const (
	RateFlat       RateType = "flat"
	RatePercentage RateType = "percentage"
)

var (
	// ErrDraftAlreadyExists — BR-P02: at most one draft per fee scale.
	ErrDraftAlreadyExists = errors.New("a draft already exists for this fee scale")

	// ErrOnlyDraftCanPublish — BR-P02: only a draft version can be published.
	ErrOnlyDraftCanPublish = errors.New("only a draft version can be published")

	// ErrRollbackTargetNotPublished — BR-P02: rollback must target a
	// published version.
	ErrRollbackTargetNotPublished = errors.New("rollback target must be a published version")

	// ErrInvalidRateType — BR-P04: a range's rate type must be flat or
	// percentage.
	ErrInvalidRateType = errors.New("fee scale range rate type must be flat or percentage")

	// ErrBidAboveHighestRange — BR-P05: fixes the source system's fail-open
	// bug (silently charging zero fee above the top range) with an explicit
	// error instead.
	ErrBidAboveHighestRange = errors.New("bid amount exceeds the highest configured fee range")

	// ErrFeeScaleNotFound — no FeeScale registered for this context+name.
	ErrFeeScaleNotFound = errors.New("fee scale not found")

	// ErrFeeScaleDraftNotFound — Publish found no draft version to publish
	// (repository-level: nothing to act on, distinct from CanPublish's
	// value-level check on an already-fetched version).
	ErrFeeScaleDraftNotFound = errors.New("no draft version exists for this fee scale")

	// ErrNoActiveFeeScaleVersion — BR-P06: no published version exists yet.
	ErrNoActiveFeeScaleVersion = errors.New("no active version exists for this fee scale")
)

// FeeScale is a named, context-scoped fee schedule (BR-P01).
type FeeScale struct {
	Name    string `json:"name"`
	Context string `json:"context"`
	Deleted bool   `json:"deleted"`
}

// FeeScaleRange is one cent-bounded band of a published FeeScaleVersion
// (BR-P03/BR-P04).
type FeeScaleRange struct {
	CentLowerLimit int64    `json:"centLowerLimit"`
	CentUpperLimit int64    `json:"centUpperLimit"`
	RateType       RateType `json:"rateType"`
	CentFee        int64    `json:"centFee"`
	PercentageFee  float64  `json:"percentageFee"`
}

// ValidateRange enforces BR-P04 — the range's rate type must be one of the
// controlled vocabulary, so CalculateFee always applies exactly one
// calculation, never a blend of both.
func ValidateRange(r FeeScaleRange) error {
	switch r.RateType {
	case RateFlat, RatePercentage:
		return nil
	default:
		return ErrInvalidRateType
	}
}

// FeeScaleVersion is one version in a FeeScale's draft/published/rolled-back
// history (BR-P02), scoped to the FeeScale's context. Ranges are ordered by
// CentUpperLimit ascending.
type FeeScaleVersion struct {
	Context       string          `json:"context"`
	Version       int             `json:"version"`
	Status        VersionStatus   `json:"status"`
	ParentVersion *int            `json:"parentVersion,omitempty"`
	RolledBackBy  *int            `json:"rolledBackBy,omitempty"`
	Ranges        []FeeScaleRange `json:"ranges,omitempty"`
}

// CanCreateDraft enforces BR-P02 — at most one draft may exist at a time
// for a fee scale.
func CanCreateDraft(versions []FeeScaleVersion) error {
	for _, v := range versions {
		if v.Status == VersionDraft {
			return ErrDraftAlreadyExists
		}
	}
	return nil
}

// CanPublish enforces BR-P02 — only a draft version can be published.
func (v FeeScaleVersion) CanPublish() error {
	if v.Status != VersionDraft {
		return ErrOnlyDraftCanPublish
	}
	return nil
}

// CanRollbackTo enforces BR-P02 — rollback may only target a published
// version; the caller creates a new forward-numbered published version
// from it rather than mutating the target.
func CanRollbackTo(v FeeScaleVersion) error {
	if v.Status != VersionPublished {
		return ErrRollbackTargetNotPublished
	}
	return nil
}

// VisibleFeeScales implements BR-P16 — a soft-deleted fee scale (BR-P01) is
// excluded from a listing, even though it still exists and remains reachable
// by name via Get/Versions/etc.
func VisibleFeeScales(all []FeeScale) []FeeScale {
	visible := make([]FeeScale, 0, len(all))
	for _, fs := range all {
		if !fs.Deleted {
			visible = append(visible, fs)
		}
	}
	return visible
}

// ActiveVersion resolves BR-P06 — the active version is the highest-numbered
// published version; drafts are never eligible, and there is no date-based
// fallback the way the source system's RateSheet/FeeScale resolution had.
func ActiveVersion(versions []FeeScaleVersion) (FeeScaleVersion, bool) {
	var active FeeScaleVersion
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

// CalculateFee resolves the fee for a bid amount (in cents) against this
// version's ranges. BR-P03: the range whose lower limit is zero matches
// inclusively at both bounds (so a zero-value bid still gets a fee); every
// other range matches exclusive-lower, inclusive-upper. BR-P04: a matched
// range applies exactly one of a flat cent fee or a percentage of the bid,
// rounded half-up. BR-P05: a bid above every configured range is rejected
// rather than silently charged zero fee.
func (v FeeScaleVersion) CalculateFee(centBid int64) (int64, error) {
	for _, r := range v.Ranges {
		var matches bool
		if r.CentLowerLimit == 0 {
			matches = centBid >= r.CentLowerLimit && centBid <= r.CentUpperLimit
		} else {
			matches = centBid > r.CentLowerLimit && centBid <= r.CentUpperLimit
		}
		if !matches {
			continue
		}
		if r.RateType == RatePercentage {
			return int64(math.Round(r.PercentageFee * float64(centBid))), nil
		}
		return r.CentFee, nil
	}
	return 0, ErrBidAboveHighestRange
}
