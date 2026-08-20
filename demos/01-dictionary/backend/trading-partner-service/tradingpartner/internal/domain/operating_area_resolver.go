package domain

import "context"

// OperatingAreaResolver is BR-TP47's port — whether an operating-area code
// exists in refdata-service's corpus, and (for a region) which country
// BR-D47's `country` relation says it belongs to.
//
// It mirrors VehicleTypeValidator's shape and exists for the same reason: a
// live rpc.* call to refdata-service (BR-D28 forbids a REST fallback
// backend-to-backend) needs a tenant-scoped NATS connection, so it cannot be
// a pure domain function and the caller must name the tenant.
//
// Note what is NOT a parameter: the business-unit context. Regions and
// countries are `standards`-category corpora seeded in `_platform`
// (BR-D46), and refdata's item lookup is an exact context match with no
// ancestor walk — inheritance is resolved at its corpus/version layer, not
// on a direct item.get. So the implementation always queries `_platform`,
// and taking a contextKey here would falsely imply a per-business-unit
// region list exists.
type OperatingAreaResolver interface {
	// ResolveArea reports whether code exists at the given level and, for a
	// region, the country BR-D47 records as its parent. For a country-level
	// lookup the returned country is the code itself.
	//
	// found=false with a nil error is "refdata says no such code"; an error
	// is a genuine transport/unexpected failure. The distinction matters —
	// BR-TP47 rejects the first and must not swallow the second.
	ResolveArea(ctx context.Context, tenant string, level AreaLevel, code string) (countryCode string, found bool, err error)
}
