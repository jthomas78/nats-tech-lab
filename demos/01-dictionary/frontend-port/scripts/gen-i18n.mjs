import { readFileSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const output = fileURLToPath(new URL('../../shared/refdata/uiCopyFallback.en.js', import.meta.url))
const seedPath = fileURLToPath(new URL('../../refdata-service/refdata/seed.go', import.meta.url))
const seed = readFileSync(seedPath, 'utf8')
const start = seed.indexOf('var uiCopySeed = []seedItem{')
const end = seed.indexOf('\n}', start)

if (start < 0 || end < 0) throw new Error('Could not find uiCopySeed in refdata-service/refdata/seed.go')

const catalog = {}
const item = /\{"([^"]+)",\s*"((?:\\.|[^"])*)",\s*"((?:\\.|[^"])*)"\}/g
for (const match of seed.slice(start, end).matchAll(item)) {
  catalog[match[1]] = JSON.parse(`"${match[2]}"`)
}

if (Object.keys(catalog).length === 0) throw new Error('uiCopySeed did not contain any entries')

const generated = `// GENERATED from refdata-service/refdata/seed.go by npm run gen:i18n. DO NOT EDIT.\nexport const uiCopyFallbackEn = ${JSON.stringify(catalog, null, 2)}\n`

if (process.argv.includes('--check')) {
  if (readFileSync(output, 'utf8') !== generated) {
    throw new Error('uiCopyFallback.en.js is stale; run npm run gen:i18n and commit the result')
  }
} else {
  writeFileSync(output, generated)
}
