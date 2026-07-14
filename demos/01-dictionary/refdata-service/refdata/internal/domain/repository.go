package domain

import "context"

// TypeRepository is the type-registry port.
type TypeRepository interface {
	Register(ctx context.Context, t DictionaryType) error
	Get(ctx context.Context, typeKey string) (DictionaryType, error)
	List(ctx context.Context) ([]DictionaryType, error)
}

// ItemRepository is the dictionary-item port. Identity is the natural
// composite key (context, typeKey, code) — reference data has no surrogate
// key, unlike Container (see Phase 8.3 rationale: no external interchange
// standard forces a correction here).
type ItemRepository interface {
	Exists(ctx context.Context, typeKey, itemContext, code string) (bool, error)
	Create(ctx context.Context, item DictionaryItem) error
	Get(ctx context.Context, typeKey, itemContext, code string) (DictionaryItem, error)
	List(ctx context.Context, typeKey, itemContext string) ([]DictionaryItem, error)
	Deprecate(ctx context.Context, typeKey, itemContext, code string) error
	Delete(ctx context.Context, typeKey, itemContext, code string) error
}

// ReferenceRepository is the typed-reference port.
type ReferenceRepository interface {
	Create(ctx context.Context, ref DictionaryReference) error
	IsReferenced(ctx context.Context, typeKey, itemContext, code string) (bool, error)
	Get(ctx context.Context, itemContext, fromTypeKey, fromCode, relation string) (DictionaryReference, error)
	// ListFrom returns every outbound reference recorded from this item —
	// used to assemble the item's full KV cache entry (Phase 11.3).
	ListFrom(ctx context.Context, itemContext, fromTypeKey, fromCode string) ([]DictionaryReference, error)
}

// LocalizationRepository is the per-item, per-locale label/description port.
type LocalizationRepository interface {
	Upsert(ctx context.Context, loc Localization) error
	ListForItem(ctx context.Context, typeKey, itemContext, code string) ([]Localization, error)
	// CountLocalized returns how many items of typeKey/itemContext have a
	// localization row for locale — the numerator of a completeness ratio.
	CountLocalized(ctx context.Context, typeKey, itemContext, locale string) (int, error)
}

// LocaleRepository is the per-context registry of known locales, one of
// which may be marked default.
type LocaleRepository interface {
	Add(ctx context.Context, itemContext, locale string, isDefault bool) error
	List(ctx context.Context, itemContext string) ([]string, error)
	// Default returns the context's default locale, or "" if none is registered.
	Default(ctx context.Context, itemContext string) (string, error)
}

// VersionRepository tracks each {context, type}'s atomically-bumped set
// version (BR-D04) — the versioned-read protocol's source of truth.
type VersionRepository interface {
	// Bump atomically increments and returns the new version for a type's
	// set within a context, creating it (starting at 1) if absent.
	Bump(ctx context.Context, itemContext, typeKey string) (int, error)
	// Current returns the type's current version without bumping it, or 0
	// if no mutation has ever bumped it.
	Current(ctx context.Context, itemContext, typeKey string) (int, error)
}

// ChangeNotifier is called after every committed mutation to an item's type
// set (create/deprecate/delete an item, add/update a localization, create a
// reference). It bumps the set version, refreshes the KV cache entry, and
// publishes a change-event pointer — the write side of the Q5 versioned-read
// protocol. Kept as a port so the command handlers stay decoupled from KV/
// JetStream concrete types.
type ChangeNotifier interface {
	NotifyItemChanged(ctx context.Context, itemContext, typeKey, code string) error
}
