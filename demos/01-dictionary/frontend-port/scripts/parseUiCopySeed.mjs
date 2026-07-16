// Shared by scripts/gen-i18n.mjs (build-time fallback generation) and
// src/App.spec.js (test-time catalog derivation) so both stay parsed
// identically — a format change to uiCopySeed only needs fixing once.
const ITEM = /\{"([^"]+)",\s*"((?:\\.|[^"])*)",\s*"((?:\\.|[^"])*)"\}/g

export function parseUiCopySeed(seedSource) {
  const start = seedSource.indexOf('var uiCopySeed = []seedItem{')
  const end = seedSource.indexOf('\n}', start)
  if (start < 0 || end < 0) throw new Error('Could not find uiCopySeed in refdata-service/refdata/seed.go')

  const en = {}
  const es = {}
  for (const match of seedSource.slice(start, end).matchAll(ITEM)) {
    en[match[1]] = JSON.parse(`"${match[2]}"`)
    es[match[1]] = JSON.parse(`"${match[3]}"`)
  }
  if (Object.keys(en).length === 0) throw new Error('uiCopySeed did not contain any entries')

  return { en, es }
}
