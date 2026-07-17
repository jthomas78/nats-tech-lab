package domain

import (
	"errors"
	"strings"
)

// ErrInvalidLocaleFormat — BR-D20: a locale code must be lower case.
var ErrInvalidLocaleFormat = errors.New("locale code must be lower case")

// ValidateLocale enforces BR-D20 — every locale code entry (registered
// context locale or per-item localization) must be lower case, e.g. "af-za"
// not "af-ZA". BCP-47 conventionally upper-cases the region subtag, but this
// system standardizes on lower case throughout so locale-code equality is a
// plain string comparison everywhere it's used (Postgres, NATS KV keys,
// frontend cache keys) without a canonicalization step.
func ValidateLocale(locale string) error {
	if locale != strings.ToLower(locale) {
		return ErrInvalidLocaleFormat
	}
	return nil
}

// Localization is one locale's label/description for an item.
type Localization struct {
	TypeKey     string `json:"typeKey"`
	Code        string `json:"code"`
	Context     string `json:"context"`
	Locale      string `json:"locale"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Source      string `json:"source"` // "manual" | "ai" (BR-D07 — not yet enforced)
}

// ImplicitDefaultLocale is the default locale a context falls back to when no
// locale is explicitly marked default (BR-D15).
const ImplicitDefaultLocale = "en"

// EffectiveDefaultLocale applies BR-D15: the explicitly marked default when
// one exists, ImplicitDefaultLocale otherwise.
func EffectiveDefaultLocale(marked string) string {
	if marked == "" {
		return ImplicitDefaultLocale
	}
	return marked
}

// ResolveLabel implements BR-D03's fallback chain: requested locale ->
// language -> default locale -> code. Resolution never fails outright for an
// existing item — if nothing matches, the code itself is returned as the
// label.
func ResolveLabel(typeKey, code, itemContext, requestedLocale, defaultLocale string, localizations []Localization) Localization {
	tried := make([]string, 0, 3)
	if requestedLocale != "" {
		tried = append(tried, requestedLocale)
		if lang := languageOf(requestedLocale); lang != requestedLocale {
			tried = append(tried, lang)
		}
	}
	if defaultLocale != "" && defaultLocale != requestedLocale {
		tried = append(tried, defaultLocale)
	}

	for _, locale := range tried {
		for _, loc := range localizations {
			if loc.Locale == locale {
				return loc
			}
		}
	}

	return Localization{
		TypeKey: typeKey, Code: code, Context: itemContext,
		Locale: requestedLocale, Label: code,
	}
}

func languageOf(locale string) string {
	if i := strings.Index(locale, "-"); i >= 0 {
		return locale[:i]
	}
	return locale
}
