import { readFileSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

import { parseUiCopySeed } from './parseUiCopySeed.mjs'

const output = fileURLToPath(new URL('../../shared/refdata/uiCopyFallback.en.js', import.meta.url))
const seedPath = fileURLToPath(new URL('../../refdata-service/refdata/seed.go', import.meta.url))
const seed = readFileSync(seedPath, 'utf8')
const { en: catalog } = parseUiCopySeed(seed)

const generated = `// GENERATED from refdata-service/refdata/seed.go by npm run gen:i18n. DO NOT EDIT.\nexport const uiCopyFallbackEn = ${JSON.stringify(catalog, null, 2)}\n`

if (process.argv.includes('--check')) {
  if (readFileSync(output, 'utf8') !== generated) {
    throw new Error('uiCopyFallback.en.js is stale; run npm run gen:i18n and commit the result')
  }
} else {
  writeFileSync(output, generated)
}
