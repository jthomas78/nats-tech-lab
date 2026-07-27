// Shared by scripts/gen-i18n.mjs (build-time fallback generation) and
// src/App.spec.js (test-time catalog derivation) so both stay parsed
// identically — a format change to l10nSeed only needs fixing once.
//
// seedItem is {code, en, es, af-ZA} — the 4th (af-ZA) field is matched but
// intentionally unused here: seafreight-app's vue-i18n wiring only consumes
// en/es (Phase 11.10's scope), af-ZA isn't a selectable UI locale yet.
const ITEM = /\{"([^"]+)",\s*"((?:\\.|[^"])*)",\s*"((?:\\.|[^"])*)",\s*"(?:\\.|[^"])*"\}/g

export function parseL10nSeed(seedSource) {
  const start = seedSource.indexOf('var l10nSeed = []seedItem{')
  const end = seedSource.indexOf('\n}', start)
  if (start < 0 || end < 0) throw new Error('Could not find l10nSeed in backend/refdata-service/refdata/seed.go')

  const en = {}
  const es = {}
  for (const match of seedSource.slice(start, end).matchAll(ITEM)) {
    en[match[1]] = JSON.parse(`"${match[2]}"`)
    es[match[1]] = JSON.parse(`"${match[3]}"`)
  }
  if (Object.keys(en).length === 0) throw new Error('l10nSeed did not contain any entries')

  return { en, es }
}
