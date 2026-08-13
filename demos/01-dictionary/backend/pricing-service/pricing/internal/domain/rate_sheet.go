package domain

import (
	"errors"
	"math"
	"time"
)

// RateSheetType is BR-P07's controlled vocabulary. "fixed-rate" is the
// value a future load-accept check (Phase 25e) should consult directly,
// rather than introducing a second independently-set flag the way the
// source system's PricingType/RateSheetType.FIXED_RATE conflation did.
type RateSheetType string

const (
	RateSheetNormal    RateSheetType = "normal"
	RateSheetFixedRate RateSheetType = "fixed-rate"
)

// Draft/publish/rollback errors for RateSheetVersion (BR-P09) — kept
// distinct from FeeScaleVersion's (fee_scale.go) rather than shared via a
// common type, so that file's already-shipped literal shape (BR-P01–BR-P06)
// stays untouched.
var (
	ErrRateSheetDraftAlreadyExists         = errors.New("a draft already exists for this rate sheet")
	ErrRateSheetOnlyDraftCanPublish        = errors.New("only a draft version can be published")
	ErrRateSheetRollbackTargetNotPublished = errors.New("rollback target must be a published version")

	// ErrRateSheetNotFound — no RateSheet registered for this context+name.
	ErrRateSheetNotFound = errors.New("rate sheet not found")

	// ErrRateSheetDraftNotFound — Publish found no draft version to publish.
	ErrRateSheetDraftNotFound = errors.New("no draft version exists for this rate sheet")

	// ErrNoActiveRateSheetVersion — BR-P08/BR-P09: no eligible published
	// version exists (either none published yet, or the sheet is inactive).
	ErrNoActiveRateSheetVersion = errors.New("no active version exists for this rate sheet")

	// ErrRateSheetEntryNotFound — BR-P11: no lane entry for this route/vehicle
	// type combination in the resolved version.
	ErrRateSheetEntryNotFound = errors.New("no rate sheet entry for this route and vehicle type")

	// ErrEntryNotFound — BR-P22: no matching entry when resolving load pricing
	// via RateForLoad. Distinct from ErrRateSheetEntryNotFound (REST/command
	// layer) to keep the domain function's error vocabulary self-contained.
	ErrEntryNotFound = errors.New("no entry for this route and vehicle type")

	// ErrNoDieselPrice — BR-P21: no diesel price indexed on or before the
	// effective date; fail-closed rather than silently falling back.
	ErrNoDieselPrice = errors.New("no diesel price indexed on or before the effective date")
)

// RateSheet is a named, context-scoped, customer-scoped rate sheet
// (BR-P07). CustomerKey is an opaque identifier pricing-service owns
// itself — this POC has no customer aggregate to reference.
type RateSheet struct {
	Name        string        `json:"name"`
	Context     string        `json:"context"`
	CustomerKey string        `json:"customerKey"`
	Type        RateSheetType `json:"type"`
	Active      bool          `json:"active"`
}

// RateSheetEntry is one per-lane rate row within a published version
// (BR-P11). RouteKey/VehicleType are opaque identifiers, same rationale as
// RateSheet.CustomerKey. DieselPct/InitialDieselCents are the diesel
// baseline fields added by Phase 25i (BR-P19): the percentage of the rate
// exposed to diesel and the initial diesel price at authoring time.
type RateSheetEntry struct {
	RouteKey               string  `json:"routeKey"`
	VehicleType            string  `json:"vehicleType"`
	CentBaseRate           int64   `json:"centBaseRate"`
	DropPointCount         int     `json:"dropPointCount"`
	CentAdditionalDropRate int64   `json:"centAdditionalDropRate"`
	DieselPct              float64 `json:"dieselPct"`
	InitialDieselCents     int64   `json:"initialDieselCents"`
}

// AdditionalDropsCharge applies BR-P12's drop-charge formula, ported
// directly from the source system's getRateForLoad: only drops beyond the
// entry's included point count are charged.
func (e RateSheetEntry) AdditionalDropsCharge(addressCount int) int64 {
	extra := addressCount - e.DropPointCount
	if extra < 0 {
		extra = 0
	}
	return int64(extra) * e.CentAdditionalDropRate
}

// DieselPrice is one row in the diesel price index (BR-P18): the price
// in effect from ActiveDate onward. CoastalCents and InlandCents are in
// ZAR cents per litre.
type DieselPrice struct {
	ActiveDate   time.Time `json:"activeDate"`
	CoastalCents int64     `json:"coastalCents"`
	InlandCents  int64     `json:"inlandCents,omitempty"`
}

// DieselOverlay is an effective-dated adjusted-rate record appended by
// AppendDieselOverlay (BR-P20). StartDate is inclusive; EndDate, when set,
// is exclusive (the next overlay's StartDate).
type DieselOverlay struct {
	MinorVersion     int        `json:"minorVersion"`
	RouteKey         string     `json:"routeKey"`
	VehicleType      string     `json:"vehicleType"`
	StartDate        time.Time  `json:"startDate"`
	EndDate          *time.Time `json:"endDate,omitempty"`
	CentAdjustedRate int64      `json:"centAdjustedRate"`
}

// RateSheetVersion is one version in a RateSheet's draft/published/
// rolled-back history (BR-P09), carrying its lane entries (BR-P11) and an
// optional FeeScale override (BR-P10). Diesel-driven sub-versioning is a
// separate axis from publishing — a diesel price change appends an
// effective-dated overlay (minor version) rather than triggering a new
// major publish (Phase 25i, BR-P17).
type RateSheetVersion struct {
	Context          string           `json:"context"`
	Version          int              `json:"version"`
	MinorVersion     int              `json:"minorVersion"`
	Status           VersionStatus    `json:"status"`
	ParentVersion    *int             `json:"parentVersion,omitempty"`
	RolledBackBy     *int             `json:"rolledBackBy,omitempty"`
	Entries          []RateSheetEntry `json:"entries,omitempty"`
	Overlays         []DieselOverlay  `json:"overlays,omitempty"`
	FeeScaleOverride *string          `json:"feeScaleOverride,omitempty"`
}

// ResolvedFeeScaleName implements the resolvable half of BR-P10 — the
// default-fallback half (falling through to a customer's default fee
// scale when no override is set) is deferred until a customer aggregate
// exists to hang a default off of.
func (v RateSheetVersion) ResolvedFeeScaleName() (string, bool) {
	if v.FeeScaleOverride == nil {
		return "", false
	}
	return *v.FeeScaleOverride, true
}

// CanCreateRateSheetDraft enforces BR-P09 — at most one draft per rate sheet.
func CanCreateRateSheetDraft(versions []RateSheetVersion) error {
	for _, v := range versions {
		if v.Status == VersionDraft {
			return ErrRateSheetDraftAlreadyExists
		}
	}
	return nil
}

// CanPublish enforces BR-P09 — only a draft version can be published.
func (v RateSheetVersion) CanPublish() error {
	if v.Status != VersionDraft {
		return ErrRateSheetOnlyDraftCanPublish
	}
	return nil
}

// CanRollbackRateSheetTo enforces BR-P09 — rollback may only target an
// already-published version; the caller creates a new forward-numbered
// published version from it rather than mutating the target.
func CanRollbackRateSheetTo(v RateSheetVersion) error {
	if v.Status != VersionPublished {
		return ErrRateSheetRollbackTargetNotPublished
	}
	return nil
}

// ActiveRateSheetVersion resolves BR-P08 and BR-P09 together — version
// resolution is only eligible when the rate sheet itself is active, and the
// active version is the highest-numbered published one.
func ActiveRateSheetVersion(sheet RateSheet, versions []RateSheetVersion) (RateSheetVersion, bool) {
	if !sheet.Active {
		return RateSheetVersion{}, false
	}
	var active RateSheetVersion
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

// AdjustedRate computes the diesel-adjusted rate for an entry given the
// current diesel price in cents (BR-P20):
//
//	adjusted = base + base·(pct/100)·((current−initial)/initial)
func AdjustedRate(e RateSheetEntry, currentDieselCents int64) (int64, error) {
	delta := float64(currentDieselCents-e.InitialDieselCents) / float64(e.InitialDieselCents)
	surcharge := float64(e.CentBaseRate) * (e.DieselPct / 100.0) * delta
	return int64(math.Round(float64(e.CentBaseRate) + surcharge)), nil
}

// DieselPriceOn implements BR-P18 lookup: the price in effect on date is
// the one with the greatest ActiveDate that does not exceed date. Returns
// false when no price covers the date (BR-P21 fail-closed trigger).
func DieselPriceOn(prices []DieselPrice, date time.Time) (DieselPrice, bool) {
	var found DieselPrice
	ok := false
	for _, p := range prices {
		if p.ActiveDate.After(date) {
			continue
		}
		if !ok || p.ActiveDate.After(found.ActiveDate) {
			found = p
			ok = true
		}
	}
	return found, ok
}

// AppendDieselOverlay appends a new diesel overlay to the version (BR-P20):
// closes every currently open-ended overlay at price.ActiveDate, computes
// the adjusted rate per entry, appends one new overlay per entry, and
// increments MinorVersion. The original version is not mutated.
func (v RateSheetVersion) AppendDieselOverlay(price DieselPrice) (RateSheetVersion, error) {
	newMinor := v.MinorVersion + 1
	newOverlays := make([]DieselOverlay, len(v.Overlays))
	for i, o := range v.Overlays {
		if o.EndDate == nil {
			end := price.ActiveDate
			o.EndDate = &end
		}
		newOverlays[i] = o
	}
	for _, e := range v.Entries {
		if e.InitialDieselCents <= 0 {
			// No authored diesel baseline (BR-P19) — AdjustedRate's formula
			// divides by InitialDieselCents, so a zero baseline (the
			// zero-value default for any entry that predates Phase 25i or
			// was added without one) would otherwise silently corrupt the
			// rate to $0 via a NaN-to-int64 conversion. Leave the entry
			// un-overlaid; it keeps resolving to its base rate via
			// RateForLoad's BR-P23 fallback instead.
			continue
		}
		adj, err := AdjustedRate(e, price.CoastalCents)
		if err != nil {
			return RateSheetVersion{}, err
		}
		newOverlays = append(newOverlays, DieselOverlay{
			MinorVersion:     newMinor,
			RouteKey:         e.RouteKey,
			VehicleType:      e.VehicleType,
			StartDate:        price.ActiveDate,
			EndDate:          nil,
			CentAdjustedRate: adj,
		})
	}
	result := v
	result.MinorVersion = newMinor
	result.Overlays = newOverlays
	return result, nil
}

// AppendDieselOverlayFromIndex resolves the diesel price in effect on
// newActiveDate from prices (BR-P18) and delegates to AppendDieselOverlay.
// Returns ErrNoDieselPrice when no price covers newActiveDate (BR-P21).
func AppendDieselOverlayFromIndex(v RateSheetVersion, prices []DieselPrice, newActiveDate time.Time) (RateSheetVersion, error) {
	price, ok := DieselPriceOn(prices, newActiveDate)
	if !ok {
		return RateSheetVersion{}, ErrNoDieselPrice
	}
	price.ActiveDate = newActiveDate
	return v.AppendDieselOverlay(price)
}

// RateForLoad resolves the load rate for a route/vehicle on effectiveDate
// (BR-P22): finds the matching entry, finds the overlay window containing
// effectiveDate, returns the adjusted rate plus the additional-drops
// surcharge (BR-P12). Falls back to the authored CentBaseRate when
// effectiveDate precedes all overlays (BR-P23). Returns ErrEntryNotFound
// when no entry matches the route/vehicle combination.
func (v RateSheetVersion) RateForLoad(routeKey, vehicleType string, effectiveDate time.Time, addressCount int) (int64, error) {
	var entry RateSheetEntry
	found := false
	for _, e := range v.Entries {
		if e.RouteKey == routeKey && e.VehicleType == vehicleType {
			entry = e
			found = true
			break
		}
	}
	if !found {
		return 0, ErrEntryNotFound
	}
	for _, o := range v.Overlays {
		if o.RouteKey != routeKey || o.VehicleType != vehicleType {
			continue
		}
		if o.StartDate.After(effectiveDate) {
			continue
		}
		if o.EndDate != nil && !effectiveDate.Before(*o.EndDate) {
			continue
		}
		return o.CentAdjustedRate + entry.AdditionalDropsCharge(addressCount), nil
	}
	// No overlay window matched — authored base rate (BR-P23).
	return entry.CentBaseRate + entry.AdditionalDropsCharge(addressCount), nil
}
