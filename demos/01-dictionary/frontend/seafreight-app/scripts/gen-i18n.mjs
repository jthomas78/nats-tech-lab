import { readFileSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

import { parseL10nSeed } from './parseL10nSeed.mjs'

const output = fileURLToPath(new URL('../../../shared/refdata/l10nFallback.en.js', import.meta.url))
const seedPath = fileURLToPath(new URL('../../../backend/refdata-service/refdata/seed.go', import.meta.url))
const seed = readFileSync(seedPath, 'utf8')
const { en: catalog } = parseL10nSeed(seed)

const generated = `// GENERATED from backend/refdata-service/refdata/seed.go by npm run gen:i18n. DO NOT EDIT.\nexport const l10nFallbackEn = ${JSON.stringify(catalog, null, 2)}\n`

if (process.argv.includes('--check')) {
  if (readFileSync(output, 'utf8') !== generated) {
    throw new Error('l10nFallback.en.js is stale; run npm run gen:i18n and commit the result')
  }
} else {
  writeFileSync(output, generated)
}
