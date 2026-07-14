// Package domain holds the reference-data model. Dictionary types/items are
// plain Postgres CRUD, not event-sourced (see ARCHITECTURE.md § "Event
// Sourcing vs Plain CRUD") — nothing ever replays a lookup value, so there is
// no aggregate/event log here, only entities and the rules that guard writes.
package domain

import "errors"

type ItemStatus string

const (
	StatusActive     ItemStatus = "active"
	StatusDeprecated ItemStatus = "deprecated"
)

// DictionaryType is a type-registry entry, e.g. "currency", "country".
type DictionaryType struct {
	TypeKey     string `json:"typeKey"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// DictionaryItem is one lookup value within a type, scoped to a context
// (tenant/region), e.g. (currency, EUR, emea-acme). Versioning is a property
// of the type's whole set (BR-D04, kvcache.Entry.Version), not of one item,
// so there is no per-item version field here.
type DictionaryItem struct {
	TypeKey string         `json:"typeKey"`
	Code    string         `json:"code"`
	Context string         `json:"context"`
	Status  ItemStatus     `json:"status"`
	Attrs   map[string]any `json:"attrs"`
}

// DictionaryReference is a typed, single-valued relation from one item to
// another, e.g. (country ZA, defaultCurrency, currency ZAR).
type DictionaryReference struct {
	Context     string `json:"context"`
	FromTypeKey string `json:"fromTypeKey"`
	FromCode    string `json:"fromCode"`
	Relation    string `json:"relation"`
	ToTypeKey   string `json:"toTypeKey"`
	ToCode      string `json:"toCode"`
}

var (
	ErrTypeNotFound = errors.New("dictionary type not found")
	ErrItemNotFound = errors.New("dictionary item not found")

	// ErrDuplicateItemCode — BR-D01: item codes are unique per {type, context}.
	ErrDuplicateItemCode = errors.New("item code already registered for this type and context")

	// ErrItemReferenced — BR-D02: a referenced item cannot be hard-deleted, only deprecated.
	ErrItemReferenced = errors.New("item is referenced and cannot be deleted; deprecate instead")

	// ErrReferenceTargetWrongType — BR-D05: the target must be of the relation's declared type.
	ErrReferenceTargetWrongType = errors.New("reference target is not of the relation's declared type")

	// ErrReferenceTargetNotFound — BR-D05: the target item must exist.
	ErrReferenceTargetNotFound = errors.New("reference target item does not exist")

	// ErrReferenceTargetNotActive — BR-D05: the target item must be active.
	ErrReferenceTargetNotActive = errors.New("reference target item is not active")

	// ErrReferenceNotFound — the named relation has no reference recorded from this item.
	ErrReferenceNotFound = errors.New("no reference found for this relation")
)

// FilterAssignable applies BR-D06's default listing rule: deprecated items
// still resolve on direct Get, but are excluded from "assignable values"
// listings unless explicitly requested.
func FilterAssignable(items []DictionaryItem) []DictionaryItem {
	out := make([]DictionaryItem, 0, len(items))
	for _, it := range items {
		if it.Status == StatusActive {
			out = append(out, it)
		}
	}
	return out
}
