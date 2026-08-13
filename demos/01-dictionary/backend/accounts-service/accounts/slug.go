package accounts

import (
	"errors"
	"regexp"
	"strings"
)

// ErrInvalidContext is returned when a business unit's context slug is not a
// legal subject token (BR-AC27).
var ErrInvalidContext = errors.New("invalid business unit context")

// DefaultBUName is the display name every account's auto-created default
// business unit carries (BR-AC28).
const DefaultBUName = "Default"

// MaxContextLen caps a context slug. The value travels into KV bucket names as
// `refdata-{context}` and `refdata-{context}-v{N}`, so it is not free-form
// however permissive the charset is.
const MaxContextLen = 48

// contextPattern is deliberately stricter than refdata-service's own
// ValidateSubjectToken (`^[A-Za-z0-9_-]+$`, BR-D22): lowercase only, and no
// leading or trailing hyphen.
//
// The case restriction is the load-bearing one. NATS subject tokens are
// case-sensitive, so `Acme` and `acme` address two different subjects and two
// different KV buckets while reading as the same business unit to a human —
// exactly the kind of mismatch that surfaces as "the dropdown is populated but
// every lookup returns nothing". Refusing uppercase at the point of
// registration is the only place that footgun can be removed cheaply.
//
// A leading underscore is impossible by construction here, which keeps BR-D33's
// reserved `_` namespace intact without a second check.
var contextPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// nonSlugChars matches every run of characters that cannot appear in a slug.
var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// ValidateContext reports whether slug is usable as a `{context}` subject
// token and KV bucket-name component (BR-AC27).
//
// This runs at the point of write in accounts-service rather than being left to
// refdata-service's own validation, because the call into refdata-service is
// best-effort: before Phase 22b a business unit named `west coast` persisted
// here and then failed silently downstream, leaving a row that could never
// resolve to anything.
func ValidateContext(slug string) error {
	switch {
	case slug == "":
		return ErrInvalidContext
	case len(slug) > MaxContextLen:
		return ErrInvalidContext
	case !contextPattern.MatchString(slug):
		return ErrInvalidContext
	}
	return nil
}

// Slugify reduces a free-text display name to the slug alphabet: lowercased,
// with every run of illegal characters collapsed to a single hyphen and any
// leading/trailing hyphens trimmed. Returns "" when nothing usable remains
// (e.g. a name of only punctuation), which ValidateContext then rejects.
func Slugify(name string) string {
	return strings.Trim(nonSlugChars.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

// DeriveContext builds the default slug proposed for a new business unit:
// the tenant name, then the slugified display name.
//
// The tenant prefix is what makes the globally-unique constraint (BR-AC27)
// survive contact with reality — two tenants both registering "Pacific Fleet"
// would otherwise collide on first use. It is a naming convention for
// uniqueness and readability only: per ARCHITECTURE-COMMUNICATIONS.md § 2.3 the
// value stays opaque, and nothing may split it back apart on `-` to recover the
// tenant. Tenancy is the NATS account boundary, never a substring.
//
// A name that already leads with the tenant is not prefixed twice, so an
// operator typing "Acme Pacific Fleet" under tenant `acme` still gets
// `acme-pacific-fleet` rather than `acme-acme-pacific-fleet`.
func DeriveContext(tenant, name string) string {
	tenantSlug := Slugify(tenant)
	nameSlug := Slugify(name)
	switch {
	case nameSlug == "":
		return tenantSlug
	case tenantSlug == "":
		return nameSlug
	case nameSlug == tenantSlug || strings.HasPrefix(nameSlug, tenantSlug+"-"):
		return nameSlug
	}
	return tenantSlug + "-" + nameSlug
}

// DefaultContext is the slug of the account's auto-created default business
// unit (BR-AC28). An ordinary tenant-owned slug with no reserved prefix: the
// default is just a business unit that happens to be created for you, so it
// needs no exception to BR-D33 and no validation bypass.
func DefaultContext(tenant string) string {
	return Slugify(tenant) + "-default"
}
