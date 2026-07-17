import { describe, expect, it } from 'vitest'

import { buildTranslationRows, filterTranslationRows, localeDisplayName } from './localization'

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
