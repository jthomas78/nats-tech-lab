package domain

import (
	"errors"
	"strings"
)

// ErrInvalidLocaleFormat — BR-D20: a locale code must be lower case.
var ErrInvalidLocaleFormat = errors.New("locale code must be lower case")

// ErrInvalidSource — BR-D07: a localization's source must be "manual" or "ai".
var ErrInvalidSource = errors.New(`source must be "manual" or "ai"`)

// SourceManual and SourceAI are the only valid values of Localization.Source (BR-D07).
const (
	SourceManual = "manual"
	SourceAI     = "ai"
)

// ValidateSource enforces BR-D07 — a localization's source must be exactly
// "manual" or "ai". An empty string is treated as "manual" by callers
// (SetLocalization defaults it before validating) rather than being valid on
// its own, so this rejects "" too.
func ValidateSource(source string) error {
	if source != SourceManual && source != SourceAI {
		return ErrInvalidSource
	}
	return nil
}

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
	Source      string `json:"source"` // "manual" | "ai" (BR-D07)
	// IsFallback is only meaningful on a value returned from ResolveLabel
	// (BR-D03): false when the requested locale (or its bare language)
	// matched exactly; true for either of the other two tiers — a
	// default-locale substitution (nothing requested matched, but the
	// context's default locale had real data) or the terminal code-echo
	// (nothing matched at all, Label degraded to the code itself). Zero-value
	// (false) on a Localization read any other way (e.g. from a repository
	// listing), since resolution never happened for those.
	IsFallback bool `json:"isFallback"`
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
// label. The default-locale tier is tried separately from the requested
// locale/language tiers (rather than folded into one "tried" list) so a
// caller asking for a bogus locale (e.g. "e") that happens to fall through to
// the default locale's real data is still tagged IsFallback — it must not
// look identical to a caller who actually asked for that locale and got an
// exact match.
func ResolveLabel(typeKey, code, itemContext, requestedLocale, defaultLocale string, localizations []Localization) Localization {
	requestedTiers := make([]string, 0, 2)
	if requestedLocale != "" {
		requestedTiers = append(requestedTiers, requestedLocale)
		if lang := languageOf(requestedLocale); lang != requestedLocale {
			requestedTiers = append(requestedTiers, lang)
		}
	}

	for _, locale := range requestedTiers {
		for _, loc := range localizations {
			if loc.Locale == locale {
				loc.IsFallback = false
				return loc
			}
		}
	}

	if defaultLocale != "" && defaultLocale != requestedLocale {
		for _, loc := range localizations {
			if loc.Locale == defaultLocale {
				loc.IsFallback = true
				return loc
			}
		}
	}

	return Localization{
		TypeKey: typeKey, Code: code, Context: itemContext,
		Locale: requestedLocale, Label: code, IsFallback: true,
	}
}

func languageOf(locale string) string {
	if i := strings.Index(locale, "-"); i >= 0 {
		return locale[:i]
	}
	return locale
}
