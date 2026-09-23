/* The generated `demos/README.md` table (BR-AS81, decision 11).

   The question it answers is "which demos does the shell load in which mode,
   and why can I not see it from the tree?" — asked 2026-09-23. The answer is
   NOT a folder rename: `plugin-source` belongs to the shell's boot, not to a
   demo, so encoding it in a path would turn a deployment change into a
   repository-wide rename. Legibility is the real need, so it is answered with
   a derived page instead of a moved folder.

   Every cell here is READ, never restated:

   - a frontend carrying `frontend/public/manifest.json` is build-sourced —
     the same opt-in `scanDemos.js` uses, so this table and the catalogue can
     never disagree about which demos are plugins;
   - a frontend built by a demo's own compose band is registry-sourced — read
     from the compose file, so a service that is renamed or re-ported here
     moves the table with it;
   - the port is the compose mapping's default, or the demo's declared dev
     port;
   - the one-line description is the demo README's own H1.

   This file writes nothing by itself. `tools/demosReadme.mjs` writes, and
   `demosReadme.spec.js` fails when what is committed is not what the folder
   currently says — which is what stops the page from becoming the second
   source of truth BR-AS76 forbids. It is a human-readable DERIVATION, not an
   admission source: nothing reads it back, and no plugin is admitted, routed
   or enabled by anything written here.
*/
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'

import { DEMOS_DIR, scanDemoManifests } from './scanDemos.js'

/** The demo-owned prose whose H1 supplies the one-line description. */
export const DEMO_README_PATH = 'README.md'

/** Where the generated table is written, relative to the repository root. */
export const DEMOS_README_FILE = join(DEMOS_DIR, 'README.md')

export const SOURCE_BUILD = 'build'
export const SOURCE_REGISTRY = 'registry'

/* A demo's own title, and nothing else. The first paragraph was the obvious
   candidate and is the wrong one: demo 01's runs to eight lines, so a "one
   line each" column would be a wall. An H1 is already written to be read at a
   glance, and the `Demo NN — ` prefix is dropped because the demo column
   beside it has just said that. */
export function demoTitle(readme) {
  const line = String(readme ?? '').split('\n').find((l) => l.startsWith('# '))
  if (!line) return ''
  return line.slice(2).replace(/^Demo\s+\d+\s*[—-]\s*/u, '').trim()
}

/* Compose is read line by line rather than parsed, deliberately: adding a YAML
   dependency to the shell so a README can be written is a poor trade, and the
   two facts wanted here — which Dockerfile a service builds and which host
   port it publishes — are single lines inside one service block.

   The pattern is exact on purpose. `demos/<demo>/frontend/<app>/Dockerfile`
   matches the three migrating apps and nothing else: demo 01's backends are
   under `backend/`, the plugin fixtures build from `lab-shell/plugins/`, and
   the docs site builds from its own context. A service this misses is a
   service that is not a demo frontend. */
const DOCKERFILE = /^\s*dockerfile:\s*demos\/([^/\s]+)\/frontend\/([^/\s]+)\/Dockerfile\s*$/
const HOST_PORT = /^\s*-\s*"?\$\{[A-Z0-9_]+:-(\d+)\}:/

export function composeFrontends(compose) {
  const found = []
  const lines = String(compose ?? '').split('\n')
  for (let i = 0; i < lines.length; i += 1) {
    const hit = DOCKERFILE.exec(lines[i])
    if (!hit) continue
    /* The port lives further down the SAME service block. The block ends at
       the next line indented two spaces, which is the next service key. */
    let port = null
    for (let j = i + 1; j < lines.length; j += 1) {
      if (/^ {2}\S/.test(lines[j])) break
      const mapped = HOST_PORT.exec(lines[j])
      if (mapped) { port = Number(mapped[1]); break }
    }
    found.push({ demo: hit[1], frontend: hit[2], port })
  }
  return found
}

const composeFiles = (repoRoot, fs, demo) => {
  const out = []
  const walk = (dir) => {
    let names
    try { names = fs.readdirSync(dir) } catch { return }
    for (const name of names) {
      const full = join(dir, name)
      if (fs.statSync(full).isDirectory()) { walk(full); continue }
      if (/^(docker-)?compose.*\.ya?ml$/.test(name)) out.push(full)
    }
  }
  walk(join(repoRoot, DEMOS_DIR, demo, 'deploy'))
  return out.sort()
}

/**
 * One row per frontend, plus a bare row for a demo that has none.
 * @returns {{rows: object[], files: string[]}}
 */
export function scanFrontends({ repoRoot, fs = { readdirSync, readFileSync, statSync } }) {
  const { entries, files } = scanDemoManifests({ repoRoot, fs })
  const byDemo = new Map(entries.map((e) => [e.demo, e]))
  const read = []
  const rows = []

  const demos = fs.readdirSync(join(repoRoot, DEMOS_DIR))
    .filter((name) => !name.startsWith('.'))
    .filter((name) => fs.statSync(join(repoRoot, DEMOS_DIR, name)).isDirectory())
    .sort()

  for (const demo of demos) {
    const readmeFile = join(repoRoot, DEMOS_DIR, demo, DEMO_README_PATH)
    let title = ''
    try { title = demoTitle(fs.readFileSync(readmeFile, 'utf8')); read.push(readmeFile) } catch { /* no README, no title */ }

    const frontends = []

    /* Build-sourced first, and it WINS. A demo whose frontend carries a
       manifest is a plugin this shell can serve itself; a compose service
       that also builds it does not change how the catalogue found it. */
    const entry = byDemo.get(demo)
    if (entry) frontends.push({ frontend: 'frontend', source: SOURCE_BUILD, port: entry.devPort })

    for (const file of composeFiles(repoRoot, fs, demo)) {
      read.push(file)
      for (const hit of composeFrontends(fs.readFileSync(file, 'utf8'))) {
        if (hit.demo !== demo) continue
        if (frontends.some((f) => f.frontend === hit.frontend)) continue
        frontends.push({ frontend: hit.frontend, source: SOURCE_REGISTRY, port: hit.port })
      }
    }

    if (frontends.length === 0) { rows.push({ demo, title, frontend: null, source: null, port: null }); continue }
    for (const f of frontends) rows.push({ demo, title, ...f })
  }

  return { rows, files: [...files, ...read] }
}

const cell = (value) => (value === null || value === '' ? '—' : String(value))

/* The demo's title is printed once per demo, not once per frontend: three
   identical sentences down a column say nothing three times. */
export function demosReadme(rows) {
  const lines = [
    '<!-- GENERATED FILE — do not edit by hand.',
    '     Written by lab-shell/tools/demosReadme.mjs from the demo folders themselves.',
    '     Regenerate with `npm --prefix lab-shell run demos:readme`.',
    '     `demosReadme.spec.js` fails when this file and the folders disagree. -->',
    '',
    '# Demos',
    '',
    'One row per demo frontend. **`plugin-source` is a property of the running',
    'shell, not of a demo** — it says how THIS shell would obtain that frontend\'s',
    'entry, and the same plugin can be discovered through either source. A demo',
    'with no frontend is still a demo; it simply has no plugin to source.',
    '',
    '| Demo | Frontend | `plugin-source` | Port | What it is |',
    '| --- | --- | --- | --- | --- |',
  ]
  let seen = null
  for (const row of rows) {
    /* A continuation row's cell is left EMPTY, never `—`: the dash means
       "there is none", and demo 01 has a description — it was said one row
       up. */
    const title = row.demo === seen ? '' : cell(row.title)
    seen = row.demo
    lines.push(`| \`${row.demo}\` | ${cell(row.frontend)} | ${cell(row.source)} | ${cell(row.port)} | ${title} |`)
  }
  lines.push('')
  return lines.join('\n')
}

export function generateDemosReadme({ repoRoot, fs = { readdirSync, readFileSync, statSync } }) {
  const { rows, files } = scanFrontends({ repoRoot, fs })
  return { text: demosReadme(rows), rows, files }
}
