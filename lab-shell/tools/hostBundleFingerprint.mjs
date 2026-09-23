#!/usr/bin/env node
/*
  The no-host-rebuild proof (BR-AS03, task 1b-8).

  The claim under test is narrow and worth stating precisely: *deploying a
  plugin does not change the shell*. Not "should not" — the check builds the
  host, fingerprints every emitted asset, and refuses if a byte moved.

  How a reviewer uses it (see BUSINESS_RULES-APP-SHELL.md § BR-AS03):

      node tools/hostBundleFingerprint.mjs --record        # before
      (cd plugins/example-plugin && edit something visible && npm run build)
      node tools/hostBundleFingerprint.mjs --verify        # after — must pass

  Between the two, the running shell shows the plugin's change on reload,
  having been rebuilt zero times. That is the whole argument, and it is a
  scripted check rather than a paragraph.

  One footgun: `VITE_PLUGIN_SOURCE` is inlined by Vite at BUILD time, so
  `build` mode and `registry` mode emit different bundles and therefore
  different digests. `--record` and `--verify` must run in the SAME mode. The
  bare `vite build` below leaves the variable unset, which resolves to
  `registry` (see src/shell/pluginSource.js) — that is the recorded baseline.

  The second assertion is the reason the first one holds: the host bundle
  contains no plugin's name, container or URL anywhere. If it did, the
  fingerprint would only be stable because nobody had added a plugin yet.
*/
import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { readdirSync, readFileSync, statSync, writeFileSync, existsSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const shellRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const distDir = join(shellRoot, 'dist')
const fingerprintFile = join(shellRoot, 'tools', '.host-bundle-fingerprint.json')

/* The generated `build`-mode catalogue (task 16b). It is emitted beside the
   bundle, not into it, and it is CONFIGURATION rather than host code — the
   list of which plugins exist. Fingerprinting it would assert the opposite of
   BR-AS03: that adding a plugin must not change the list of plugins. So it is
   excluded from the digest and from the name scan, and the claim the tool
   makes is the exact one BR-AS03 makes — adding a plugin leaves every host
   chunk untouched. If a plugin's code or URL ever reached a hashed host chunk,
   this exclusion would not hide it; the chunk is still fingerprinted and still
   scanned. */
const GENERATED_CATALOGUE = 'plugin-catalogue.json'

/* The same argument again, for the assets themselves (task 16c). The build
   copies each opted-in demo's built output into `dist/plugins/<id>/` so the
   shell serves BR-AS77's one public path layout. Those bytes are the PLUGIN's
   bytes — compiled by the plugin's own build, from the plugin's own sources —
   and counting them would make the check say that deploying a plugin must not
   change the plugin. What BR-AS03 claims is that the HOST is untouched, so the
   host's own chunks are what is fingerprinted. A plugin's code reaching a
   hashed host chunk is still caught: that chunk is outside this prefix. */
const PLUGIN_ASSET_DIR = 'plugins'

/* The demo readiness artefacts (task 16e), excluded on the same argument as
   the plugin catalogue above: both are GENERATED CONFIGURATION emitted beside
   the bundle, never compiled into it.

   `demo-catalogue.json` is the list of which demos exist and where their
   readiness route is. `deploy/` holds the reverse-proxy rules that route
   those checks, and it is removed from the served tree by the Dockerfile, so
   it is a deployment's routing config rather than a page. Fingerprinting
   either would make this check say that adding a DEMO must not change the
   list of demos. BR-AS03's claim is about the host's own chunks, and those
   are still fingerprinted and still scanned. */
const GENERATED_DEMO_CATALOGUE = 'demo-catalogue.json'
const DEPLOY_CONFIG_DIR = 'deploy'

const isPluginArtefact = (file) => {
  const name = relative(distDir, file)
  return name === GENERATED_CATALOGUE
    || name === GENERATED_DEMO_CATALOGUE
    || name === PLUGIN_ASSET_DIR
    || name.startsWith(`${PLUGIN_ASSET_DIR}/`)
    || name === DEPLOY_CONFIG_DIR
    || name.startsWith(`${DEPLOY_CONFIG_DIR}/`)
}

/*
  Two tiers, because BR-AS03 and BR-AS66 both hold and they touch the same
  words. Narrowed 2026-09-23 after this check had been red since 2026-09-01
  with nothing wrong: `FirstBootNote.vue` landed the day after the fingerprint
  was recorded, and BR-AS66 REQUIRES it — "the lab-shell intro copy must state
  this", naming the preloaded plugin and the announced fixtures separately, as
  FirstBootNote.spec.js asserts. A red check that is right to be red is a
  check nobody reads, so the ban is now the one BR-AS03 actually makes.

  What BR-AS03 claims is that the compiler never saw a plugin's identity **as
  data** — a remote origin, a federation container, a module specifier. It
  does not claim the shell may never SAY a plugin's name to a human. The first
  is a deployment coupling; the second is documentation, and the host bundle
  changing because the copy changed is a copy edit, not a plugin deployment.
*/

/* Tier 1 — never, in any position. A remote origin and a federation container
   name have no prose reading: if either is in the bundle, the host was
   compiled against a specific plugin. */
const FORBIDDEN_ALWAYS = [
  ...[7111, 7112, 7113, 7114, 7115].map((port) => `localhost:${port}`),
  'example_plugin', 'demo_catalog',
  // The README belongs to the catalog build, never the host.
  'Dictionary POC', 'Admin UI layout — data flow top to bottom',
]

/* Tier 2 — the human-readable plugin ids. Banned as data, allowed as prose,
   because BR-AS66 puts them on the screen on purpose. */
const FORBIDDEN_AS_DATA = ['example-plugin', 'demo-catalog']

/* An id is being used as data when it sits near a URL, a path or a dynamic
   import. Deliberately generous — a false FAIL here is cheap to read and a
   false PASS is the thing this file exists to prevent. Note that tier 1
   already catches every real case seen so far: a remote URL carries its
   port and a container carries its underscore form, so tier 2 is the
   backstop, not the primary guard. */
const DATA_MARKERS = ['://', 'localhost', 'remoteEntry', '/assets/', 'import(', '.js"', ".js'"]
const DATA_WINDOW = 60

/* The exemption is tied to the note that earns it. If BR-AS66's copy is ever
   removed from the bundle, tier 2 goes back to an outright ban with no edit
   here. */
const PROSE_ANCHOR = 'A fresh lab serves only its preloaded plugin'

function usedAsData(text, index, needle) {
  const window = text.slice(Math.max(0, index - DATA_WINDOW), index + needle.length + DATA_WINDOW)
  return DATA_MARKERS.some((marker) => window.includes(marker))
}

function offencesFor(text, needles, { proseAllowed }) {
  const hits = []
  for (const needle of needles) {
    let at = text.indexOf(needle)
    while (at !== -1) {
      if (!proseAllowed || usedAsData(text, at, needle)) hits.push(needle)
      at = text.indexOf(needle, at + needle.length)
    }
  }
  return [...new Set(hits)]
}

function walk(dir) {
  const out = []
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry)
    if (statSync(full).isDirectory()) out.push(...walk(full))
    else out.push(full)
  }
  return out.sort()
}

function build() {
  execFileSync('npx', ['vite', 'build'], { cwd: shellRoot, stdio: 'inherit' })
}

function fingerprint() {
  const files = walk(distDir).filter((file) => !isPluginArtefact(file))
  const entries = files.map((file) => ({
    path: relative(distDir, file),
    sha256: createHash('sha256').update(readFileSync(file)).digest('hex'),
  }))
  const combined = createHash('sha256')
  for (const entry of entries) combined.update(`${entry.path}:${entry.sha256}\n`)
  return { files: entries, digest: combined.digest('hex') }
}

function assertNoPluginNames() {
  const offenders = []
  let proseSeen = false
  for (const file of walk(distDir)) {
    if (!/\.(js|css|html|json)$/.test(file)) continue
    if (isPluginArtefact(file)) continue
    const text = readFileSync(file, 'utf8')
    const name = relative(distDir, file)
    const proseAllowed = text.includes(PROSE_ANCHOR)
    if (proseAllowed) proseSeen = true
    for (const hit of offencesFor(text, FORBIDDEN_ALWAYS, { proseAllowed: false })) {
      offenders.push(`${name} contains ${hit}`)
    }
    for (const hit of offencesFor(text, FORBIDDEN_AS_DATA, { proseAllowed })) {
      offenders.push(`${name} uses ${hit} as data, not prose`)
    }
  }
  if (offenders.length) {
    console.error('FAIL — the host bundle names a plugin (BR-AS03):')
    for (const line of offenders) console.error(`  ${line}`)
    process.exit(1)
  }
  console.log('ok — the host bundle names no plugin container, remote URL or module path')
  if (proseSeen) {
    console.log('   (BR-AS66 first-boot copy found; its plugin ids are read as prose)')
  }
}

const mode = process.argv[2] ?? '--record'

build()
assertNoPluginNames()
const current = fingerprint()

if (mode === '--record') {
  writeFileSync(fingerprintFile, `${JSON.stringify(current, null, 2)}\n`)
  console.log(`recorded ${current.files.length} host assets — digest ${current.digest}`)
  process.exit(0)
}

if (mode !== '--verify') {
  console.error('usage: hostBundleFingerprint.mjs [--record|--verify]')
  process.exit(2)
}

if (!existsSync(fingerprintFile)) {
  console.error('FAIL — nothing recorded. Run --record before the plugin is redeployed.')
  process.exit(1)
}

const recorded = JSON.parse(readFileSync(fingerprintFile, 'utf8'))
if (recorded.digest === current.digest) {
  console.log(`ok — host bundle unchanged across the plugin deployment (${current.digest})`)
  process.exit(0)
}

console.error('FAIL — the host bundle changed (BR-AS03):')
const before = new Map(recorded.files.map((f) => [f.path, f.sha256]))
const after = new Map(current.files.map((f) => [f.path, f.sha256]))
for (const [path, sha] of after) {
  if (!before.has(path)) console.error(`  added   ${path}`)
  else if (before.get(path) !== sha) console.error(`  changed ${path}`)
}
for (const path of before.keys()) if (!after.has(path)) console.error(`  removed ${path}`)
process.exit(1)
