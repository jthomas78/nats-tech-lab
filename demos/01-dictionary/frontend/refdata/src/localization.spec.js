import { describe, expect, it } from 'vitest'

import {
  buildTranslationRows,
  filterTranslationRows,
  localeDisplayName,
  localeLabel,
  localeSelectOptions,
  orderLocales,
} from './localization'

describe('localeDisplayName', () => {
  it('resolves a well-formed locale tag to its English display name', () => {
    expect(localeDisplayName('fr-fr')).toMatch(/French/)
  })
  it('falls back to the raw code when Intl.DisplayNames cannot parse it', () => {
    expect(localeDisplayName('not-a-locale-!!')).toBe('not-a-locale-!!')
  })
})

describe('buildTranslationRows', () => {
  const locales = ['en-za', 'af-za', 'fr-fr', 'de-de']
  const defaultLocale = 'en-za'
  const localizations = [
    { locale: 'af-za', label: 'Voor Anker', description: '' },
    { locale: 'fr-fr', label: 'Au mouillage', description: 'Navire ancré' },
  ]
  const defaultLabel = 'At Anchor'

  it('produces one row per registered locale, not just locales with a localization', () => {
    const rows = buildTranslationRows({ locales, defaultLocale, localizations, defaultLabel })
    expect(rows.map((r) => r.locale)).toEqual(locales)
  })

  it('marks the context default locale as "default" even without an explicit localization', () => {
    const rows = buildTranslationRows({ locales, defaultLocale, localizations, defaultLabel })
    const row = rows.find((r) => r.locale === 'en-za')
    expect(row.status).toBe('default')
    expect(row.translation).toBe(defaultLabel)
    expect(row.hasLocalization).toBe(false)
  })

  it('marks a locale with an explicit localization as "complete"', () => {
    const rows = buildTranslationRows({ locales, defaultLocale, localizations, defaultLabel })
    const row = rows.find((r) => r.locale === 'fr-fr')
    expect(row.status).toBe('complete')
    expect(row.translation).toBe('Au mouillage')
    expect(row.description).toBe('Navire ancré')
    expect(row.hasLocalization).toBe(true)
  })

  it('marks a registered locale with no localization as "missing"', () => {
    const rows = buildTranslationRows({ locales, defaultLocale, localizations, defaultLabel })
    const row = rows.find((r) => r.locale === 'de-de')
    expect(row.status).toBe('missing')
    expect(row.translation).toBe('')
    expect(row.hasLocalization).toBe(false)
  })

  it('prefers an explicit localization on the default locale over the fallback label', () => {
    const withDefaultOverride = [...localizations, { locale: 'en-za', label: 'Anchored', description: '' }]
    const rows = buildTranslationRows({ locales, defaultLocale, localizations: withDefaultOverride, defaultLabel })
    const row = rows.find((r) => r.locale === 'en-za')
    expect(row.status).toBe('default')
    expect(row.translation).toBe('Anchored')
  })
})

describe('filterTranslationRows', () => {
  const rows = [
    { locale: 'en-za', displayName: 'English (South Africa)', status: 'default' },
    { locale: 'af-za', displayName: 'Afrikaans (South Africa)', status: 'complete' },
    { locale: 'de-de', displayName: 'German', status: 'missing' },
  ]

  it('returns all rows when no query and missingOnly is false', () => {
    expect(filterTranslationRows(rows)).toHaveLength(3)
  })

  it('filters to missing-only rows', () => {
    const filtered = filterTranslationRows(rows, { missingOnly: true })
    expect(filtered.map((r) => r.locale)).toEqual(['de-de'])
  })

  it('filters by a case-insensitive locale code match', () => {
    const filtered = filterTranslationRows(rows, { query: 'AF-za' })
    expect(filtered.map((r) => r.locale)).toEqual(['af-za'])
  })

  it('filters by a display-name substring match', () => {
    const filtered = filterTranslationRows(rows, { query: 'german' })
    expect(filtered.map((r) => r.locale)).toEqual(['de-de'])
  })

  it('combines a query with missingOnly: a query matching a non-missing row yields nothing', () => {
    const filtered = filterTranslationRows(rows, { query: 'af', missingOnly: true })
    expect(filtered).toHaveLength(0)
  })

  it('combines a query with missingOnly: a query matching the missing row still returns it', () => {
    const filtered = filterTranslationRows(rows, { query: 'ger', missingOnly: true })
    expect(filtered.map((r) => r.locale)).toEqual(['de-de'])
  })
})

// BR-D32 — the default locale is always shown first in any UI locale list,
// and is marked as the default where it renders as text.
describe('orderLocales (BR-D32)', () => {
  it('moves the default locale to the front, preserving the order of the rest', () => {
    expect(orderLocales(['af-za', 'en', 'fr-fr'], 'en')).toEqual(['en', 'af-za', 'fr-fr'])
  })
  it('leaves the list untouched when the default is already first', () => {
    expect(orderLocales(['en', 'af-za'], 'en')).toEqual(['en', 'af-za'])
  })
  it('leaves the list untouched when no default is set or the default is not registered', () => {
    expect(orderLocales(['af-za', 'fr-fr'], '')).toEqual(['af-za', 'fr-fr'])
    expect(orderLocales(['af-za', 'fr-fr'], 'de-de')).toEqual(['af-za', 'fr-fr'])
  })
  it('does not mutate the array it is given — callers pass reactive store state', () => {
    const input = ['af-za', 'en']
    orderLocales(input, 'en')
    expect(input).toEqual(['af-za', 'en'])
  })
})

describe('localeLabel (BR-D32)', () => {
  it('marks the default locale', () => {
    expect(localeLabel('en', 'en')).toBe('en (default)')
  })
  it('leaves a non-default locale as the bare code', () => {
    expect(localeLabel('af-za', 'en')).toBe('af-za')
  })
})

describe('localeSelectOptions (BR-D32)', () => {
  it('builds ordered, marked options for a Select', () => {
    expect(localeSelectOptions(['af-za', 'en'], 'en')).toEqual([
      { value: 'en', label: 'en (default)' },
      { value: 'af-za', label: 'af-za' },
    ])
  })
  it('pins a blank option above the default locale — it is not a locale', () => {
    const options = localeSelectOptions(['af-za', 'en'], 'en', { includeBlank: '(code)' })
    expect(options[0]).toEqual({ value: '', label: '(code)' })
    expect(options[1].value).toBe('en')
  })
})

describe('buildTranslationRows ordering (BR-D32)', () => {
  it('returns the default locale as the first row and marks its label', () => {
    const rows = buildTranslationRows({
      locales: ['af-za', 'fr-fr', 'en'],
      defaultLocale: 'en',
      localizations: [],
      defaultLabel: 'At Anchor',
    })
    expect(rows.map((r) => r.locale)).toEqual(['en', 'af-za', 'fr-fr'])
    expect(rows[0].label).toBe('en (default)')
    expect(rows[0].isDefault).toBe(true)
    expect(rows[1].label).toBe('af-za')
  })
})
