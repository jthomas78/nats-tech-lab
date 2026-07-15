import { execFileSync } from 'node:child_process'
import { readdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const srcDir = fileURLToPath(new URL('../src', import.meta.url))
const files = readdirSync(srcDir, { recursive: true })
  .filter((path) => path.endsWith('.vue'))
  .map((path) => `${srcDir}/${path}`)

const staticUiAttribute = /(?:^|\s)(?:label|header|placeholder|aria-label|title)="[^"{][^"]*"/m
const rawUiText = /<(?:h[1-4]|p|span|label)[^>]*>\s*[A-Za-z][^<{]*<\//

for (const file of files) {
  const source = readFileSync(file, 'utf8')
  const attributeMatch = source.match(staticUiAttribute)
  const textMatch = source.match(rawUiText)
  if (attributeMatch || textMatch) {
    throw new Error(`bare user-facing copy found in ${file}: ${attributeMatch?.[0] || textMatch[0]}`)
  }
}

execFileSync(process.execPath, ['scripts/gen-i18n.mjs', '--check'], { stdio: 'inherit' })
