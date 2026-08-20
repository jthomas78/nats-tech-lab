package domain

import (
	"errors"
	"strings"
)

// Region corpus constants (BR-D46). `region` holds first-level
// administrative subdivisions (ISO 3166-2) and hangs off the existing
// `country` corpus through a typed reference rather than a parent column —
// see BR-D47 and ReferenceRepository.
const (
	// RegionTypeKey is the dictionary type key for the region corpus.
	RegionTypeKey = "region"

	// CountryTypeKey is the existing country corpus a region points at.
	CountryTypeKey = "country"

	// RegionCountryRelation is the relation name linking a region to its
	// country. dictionary_references' PK is
	// (context, from_type_key, from_code, relation), so this relation is
	// single-valued by construction — BR-D47's "at most one" half needs no
	// guard, only its "at least one" half does.
	RegionCountryRelation = "country"
)

// ErrRegionCountryRequired — BR-D47: a region must declare a parent
// country. A region with none is not addressable in a two-level hierarchy,
// so it is refused rather than stored incomplete.
var ErrRegionCountryRequired = errors.New("region must declare a parent country")

// ValidateRegionCountry enforces BR-D47's country requirement. Charset
// legality of the code itself stays with ValidateCode (BR-D22) so region
// codes obey the same KV-key rules as every other item code.
//
// Deliberately NOT checked here: whether an ISO 3166-2 code's country
// prefix agrees with the declared country ("ZA-GP" under "BW"). BR-D47 as
// approved requires a country, not a well-formed ISO code, and the corpus
// is not contractually ISO-only — a non-ISO region code with a legitimate
// country would be refused by such a check. Seed-data consistency is
// covered where the seed data lives (cmd/seed-regions), not by a domain
// rule nobody approved.
func ValidateRegionCountry(code, countryCode string) error {
	if strings.TrimSpace(countryCode) == "" {
		return ErrRegionCountryRequired
	}
	return nil
}
