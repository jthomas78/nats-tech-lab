// Pure helpers for the per-item Translations table and the bulk Translation
// Matrix (Phase 11.11) — kept dependency-free from Vue/PrimeVue so they're
// unit-testable without mounting a component.
//
// Locale ordering and the "(default)" marker come from the shared
// @refdata/locales.js helpers (BR-D32) so this app and the shipping apps
// present locale lists identically; re-exported here because the components
// in this app already import their locale helpers from this module.

import { localeLabel, orderLocales } from '@refdata/locales.js'

export { isDefaultLocale, localeLabel, localeSelectOptions, orderLocales } from '@refdata/locales.js'

// `Intl.DisplayNames` throws on a locale tag it can't parse (e.g. malformed
// input mid-typing in the "+ Add locale" field) — fall back to the raw code
// rather than let the whole table crash.
export function localeDisplayName(locale) {
  try {
    return new Intl.DisplayNames(['en'], { type: 'language' }).of(locale) || locale
  } catch {
    return locale
  }
}

// One row per registered locale for a single item, regardless of whether that
// locale has an explicit localization recorded yet:
// - the context's default locale is always 'default' — its translation falls
//   back to the item's own default label (attrs.name) when no localization
//   override exists, mirroring the backend's BR-D03 fallback chain;
// - any other locale is 'complete' once a localization row exists, else
//   'missing'.
//
// Rows come back default-locale-first and the default row carries a marked
// `label` (BR-D32) — ordering is done here rather than in the table so every
// consumer of these rows inherits it.
export function buildTranslationRows({ locales, defaultLocale, localizations, defaultLabel }) {
  const byLocale = new Map((localizations || []).map((l) => [l.locale, l]))
  return orderLocales(locales, defaultLocale).map((locale) => {
    const entry = byLocale.get(locale)
    const isDefault = locale === defaultLocale
    const status = isDefault ? 'default' : entry ? 'complete' : 'missing'
    return {
      locale,
      label: localeLabel(locale, defaultLocale),
      isDefault,
      displayName: localeDisplayName(locale),
      translation: entry?.label ?? (isDefault ? defaultLabel || '' : ''),
      description: entry?.description ?? '',
      status,
      hasLocalization: Boolean(entry),
    }
  })
}

export function filterTranslationRows(rows, { query = '', missingOnly = false } = {}) {
  const q = query.trim().toLowerCase()
  return rows.filter((row) => {
    if (missingOnly && row.status !== 'missing') return false
    if (!q) return true
    return row.locale.toLowerCase().includes(q) || row.displayName.toLowerCase().includes(q)
  })
}
