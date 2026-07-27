// Shared locale-presentation helpers (BR-D32). Pure and dependency-free from
// Vue/PrimeVue so every frontend — the refdata admin UI and the two shipping
// apps — presents a locale list the same way and this stays unit-testable
// without mounting a component.
//
// The rule: the context's default locale is always shown first in any locale
// list (dropdown, table, matrix column, chip row), and is labelled as the
// default when displayed. Ordering and labelling are separate functions
// because some surfaces need one without the other — a DataTable orders rows
// but renders its own cells, a Select needs both.

// DEFAULT_SUFFIX is appended to the default locale's display text. Kept here
// rather than inline at each call site so the wording is consistent and
// changing it is a one-line edit.
export const DEFAULT_SUFFIX = ' (default)'

// orderLocales puts the default locale first and preserves the incoming order
// of the rest — the backend's own ordering stays meaningful for non-defaults.
// Non-mutating: callers often pass a reactive store array straight in.
export function orderLocales(locales, defaultLocale) {
  const list = [...(locales || [])]
  if (!defaultLocale) return list
  const at = list.indexOf(defaultLocale)
  if (at <= 0) return list // already first, or not registered at all
  list.splice(at, 1)
  return [defaultLocale, ...list]
}

// localeLabel is the display text for one locale: the bare code, plus the
// default marker when it is the context's default — e.g. "en (default)".
export function localeLabel(locale, defaultLocale) {
  if (!locale) return ''
  return locale === defaultLocale ? `${locale}${DEFAULT_SUFFIX}` : locale
}

// isDefaultLocale — the same comparison every surface would otherwise inline,
// so "which one is default" has a single definition.
export function isDefaultLocale(locale, defaultLocale) {
  return Boolean(locale) && locale === defaultLocale
}

// localeSelectOptions builds ordered {value,label} options for a PrimeVue
// Select. An `includeBlank` label (e.g. "(codes)") stays pinned above the
// default locale — it isn't a locale, it's the "no locale" escape hatch.
export function localeSelectOptions(locales, defaultLocale, { includeBlank = '' } = {}) {
  const options = orderLocales(locales, defaultLocale).map((locale) => ({
    value: locale,
    label: localeLabel(locale, defaultLocale),
  }))
  return includeBlank ? [{ value: '', label: includeBlank }, ...options] : options
}
