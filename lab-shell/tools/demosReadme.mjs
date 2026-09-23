#!/usr/bin/env node
/* Writes the generated `demos/README.md` (BR-AS81, decision 11).

   The one place in this repository that writes a generated file back into the
   tree, and it is deliberately NOT a Vite plugin hook. `pluginAssets.js` and
   `demoReadiness.js` both carry a guard against writing into the repository,
   because a dev-server shutdown once did exactly that — so a repo write goes
   through a command somebody ran on purpose, never through a build that
   happened to end.

     node lab-shell/tools/demosReadme.mjs            # write
     node lab-shell/tools/demosReadme.mjs --check    # exit 1 if stale, write nothing
*/
import { readFileSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { DEMOS_README_FILE, generateDemosReadme } from './buildCatalogue/demosReadme.js'

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..')
const file = join(repoRoot, DEMOS_README_FILE)
const { text, rows } = generateDemosReadme({ repoRoot })

if (process.argv.includes('--check')) {
  let current = null
  try { current = readFileSync(file, 'utf8') } catch { current = null }
  if (current === text) {
    console.log(`ok — ${DEMOS_README_FILE} matches the demo folders (${rows.length} rows)`)
    process.exit(0)
  }
  console.error(`FAIL — ${DEMOS_README_FILE} is stale. Run: npm --prefix lab-shell run demos:readme`)
  process.exit(1)
}

writeFileSync(file, text)
console.log(`wrote ${DEMOS_README_FILE} — ${rows.length} rows`)
