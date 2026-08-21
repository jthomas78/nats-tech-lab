package domain

import "errors"

// AreaLevel is the granularity of an operating-area assignment (BR-TP47).
// Two levels only — V2's GeoAreaEntity carries MUNICIPALITY and CUSTOM as
// well, but both belong to its polygon model, which holds zero rows in the
// live database; the flat Country -> Region list is what V2 actually runs.
type AreaLevel string

const (
	AreaLevelCountry AreaLevel = "COUNTRY"
	AreaLevelRegion  AreaLevel = "REGION"
)

// OperatingArea is one declaration of where a Transporter will carry.
//
// CountryCode is present on both levels. On a REGION it is the parent
// resolved from refdata's `country` relation (BR-D47) — not inferred from
// the code's ISO prefix, which would be a second source of truth for the
// same fact. On a COUNTRY it repeats Code, which keeps BR-TP48's overlap
// check a plain comparison over one field instead of a per-level special
// case.
type OperatingArea struct {
	Level       AreaLevel `json:"level"`
	Code        string    `json:"code"`
	CountryCode string    `json:"countryCode"`
}

var (
	// ErrOperatingAreaRequiresTransporter — BR-TP46.
	ErrOperatingAreaRequiresTransporter = errors.New("operating areas may only be assigned to a Transporter")

	// ErrOperatingAreaInvalidLevel — BR-TP47: level must be COUNTRY or REGION.
	ErrOperatingAreaInvalidLevel = errors.New("operating area level must be COUNTRY or REGION")

	// ErrOperatingAreaCodeRequired — BR-TP47.
	ErrOperatingAreaCodeRequired = errors.New("operating area code is required")

	// ErrOperatingAreaCountryRequired — BR-TP47: a region with no resolved
	// parent country cannot be evaluated by BR-TP48's overlap rule, so it is
	// refused rather than stored with unknown parentage.
	ErrOperatingAreaCountryRequired = errors.New("operating area country is required")

	// ErrOperatingAreaCountryMismatch — BR-TP47: a COUNTRY-level area is its
	// own country; a differing pair is a caller bug, not a coverage claim.
	ErrOperatingAreaCountryMismatch = errors.New("country-level operating area code must equal its country")

	// ErrOperatingAreaCoveredByCountry — BR-TP48: the region's country is
	// already assigned, so the region adds nothing.
	ErrOperatingAreaCoveredByCountry = errors.New("operating area is already covered by an assigned country")

	// ErrOperatingAreaCoversExistingRegion — BR-TP48: the country would
	// subsume regions already assigned.
	ErrOperatingAreaCoversExistingRegion = errors.New("country-level operating area would subsume an already-assigned region")
)

// ValidateOperatingAreaShape checks everything knowable without consulting
// refdata: the partner type (BR-TP46) and the level/code shape (BR-TP47).
//
// Split out from AddOperatingArea so the application layer can reject a
// doomed request before spending an rpc.* round trip resolving a code, while
// still running the identical checks. It deliberately does NOT take a
// country: the parent of a region is exactly the thing refdata has to
// answer, and inventing a stand-in value to satisfy a guard here would make
// the guard lie about what it verified.
func ValidateOperatingAreaShape(partnerType PartnerType, level AreaLevel, code string) error {
	if partnerType != PartnerTypeTransporter {
		return ErrOperatingAreaRequiresTransporter
	}
	if level != AreaLevelCountry && level != AreaLevelRegion {
		return ErrOperatingAreaInvalidLevel
	}
	if code == "" {
		return ErrOperatingAreaCodeRequired
	}
	return nil
}

// AddOperatingArea validates a new assignment against the partner's type and
// the areas it already holds (BR-TP46-BR-TP48). It returns the area to
// persist and never mutates existing.
//
// Redundant overlap is REJECTED, not collapsed. Auto-removing the subsumed
// rows would let one write delete rows the operator never touched — awkward
// to render, and worse to explain in BR-TP50's audit trail, which would
// record a deletion nobody requested. Rejecting makes the operator resolve
// the ambiguity explicitly.
//
// Duplicate assignment of the identical (level, code) is NOT checked here:
// BR-TP49 makes that a repository-level unique constraint, the same
// treatment BR-TP13 gives registrationNo — a uniqueness guard in the domain
// would be racy and would duplicate the constraint that actually enforces it.
func AddOperatingArea(partnerType PartnerType, existing []OperatingArea, level AreaLevel, code, countryCode string) (OperatingArea, error) {
	if err := ValidateOperatingAreaShape(partnerType, level, code); err != nil {
		return OperatingArea{}, err
	}
	if countryCode == "" {
		return OperatingArea{}, ErrOperatingAreaCountryRequired
	}
	if level == AreaLevelCountry && code != countryCode {
		return OperatingArea{}, ErrOperatingAreaCountryMismatch
	}

	for _, held := range existing {
		switch {
		case level == AreaLevelRegion && held.Level == AreaLevelCountry && held.Code == countryCode:
			return OperatingArea{}, ErrOperatingAreaCoveredByCountry
		case level == AreaLevelCountry && held.Level == AreaLevelRegion && held.CountryCode == code:
			return OperatingArea{}, ErrOperatingAreaCoversExistingRegion
		}
	}

	return OperatingArea{Level: level, Code: code, CountryCode: countryCode}, nil
}

var (
	// ErrOperatingAreaAlreadyAssigned — BR-TP49: (partner, level, code) is
	// unique. Raised by the repository from the database's own constraint,
	// not pre-checked in the domain, so two concurrent adds cannot both win.
	ErrOperatingAreaAlreadyAssigned = errors.New("operating area is already assigned to this partner")

	// ErrOperatingAreaNotAssigned — removing an area the partner does not
	// hold. Reported rather than silently succeeding: BR-TP50 writes an
	// audit entry for every change, and an entry describing a deletion that
	// never happened is worse than none.
	ErrOperatingAreaNotAssigned = errors.New("operating area is not assigned to this partner")
)

// ErrUnknownOperatingAreaCode — BR-TP47: the code is not an active item in
// refdata's corpus for its level. Like BR-TP14's ErrUnknownVehicleTypeCode,
// this is raised at the application layer, not by a pure domain guard.
var ErrUnknownOperatingAreaCode = errors.New("operating area code does not exist in the refdata corpus")
