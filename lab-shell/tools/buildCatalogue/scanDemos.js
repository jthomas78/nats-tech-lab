/* The scan that produces the `build` catalogue (BR-AS76, decision 5).

   One function, injected `fs`, no Vite: this is the single scan the whole
   phase derives from. BR-AS81 requires that anywhere a demo's source is listed
   outside the shell, it is DERIVED from this scan and never restated by hand —
   so the scan has to be importable on its own, not welded inside a plugin
   hook.

   The opt-in is the file. A demo frontend carrying `public/manifest.json` is a
   plugin; one without is not. Demo 01's three apps therefore do not appear,
   which is correct — they arrive through Phases 10-12, into `registry` mode.

   What this does NOT do:

   - It does not validate a manifest. `validateManifest` already does that, per
     plugin, at admission, and it is the same code in both sources. A generator
     that pre-judged would be a second, divergent rule.
   - It does not rewrite `remote.url`. Decision 5, amended at the gate: URLs
     stay relative and are resolved against the shell's own origin, so one
     build runs under any hostname and decision 2's same-origin property holds
     by construction. The generator stamps no origin at all.
   - It does not write anything back into the repository. A committed catalogue
     is a second source of truth that will disagree with the folder (BR-AS76).
*/
import { createHash } from 'node:crypto'
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'

import { demoReadinessPath } from '../../src/shell/demos/demoCatalogueLocation.js'
import { REGISTRY_SCHEMA_VERSION } from '../../src/shell/versions.js'

/* The demo catalogue's own version. It is NOT the registry schema version:
   this document is shell-owned, the registry never sees it, and tying the two
   would make a change here look like a change to the registry contract. */
export const DEMO_CATALOGUE_SCHEMA_VERSION = 1

/** Where demos live, relative to the repository root. */
export const DEMOS_DIR = 'demos'

/** The opt-in file, relative to a demo directory. */
export const MANIFEST_PATH = join('frontend', 'public', 'manifest.json')

/* The sibling, demo-owned metadata file (task 16c; extended by 16e).

   It exists because BR-AS77 says the catalogue and the development proxy
   mappings must derive from the SAME discovery data, and a proxy needs one
   fact a manifest must never carry: which port the demo's own dev server
   listens on. It cannot go in `manifest.json`, because BR-AS78 requires that
   file to be byte-identical whether a plugin is discovered by `build` or by
   `registry`, and a dev port means nothing to a registry plugin.

   It is OPTIONAL. A demo without one is still a plugin; it simply gets no dev
   proxy entry, which is the right answer for a plugin whose assets are served
   from the shell's own tree.

   Task 16e added two more facts to it, for the same reason and under the same
   rule (BR-AS79): an optional `readiness` declaration and an optional
   `runCommand`. Both are DECORATION ON A DEMO and carry no authority. Nothing
   read here can admit a plugin, enable a disabled one, contribute a route or
   supply a `remote.url` — discovery alone does that, and the readiness half of
   this file is never shown to the registry at all. */
export const DEMO_METADATA_PATH = join('frontend', 'public', 'demo.json')

/** A demo frontend's built output, relative to a demo directory. */
export const DEMO_DIST_PATH = join('frontend', 'dist')

/* Raised when a discovered file exists but cannot be read as JSON. It is deliberately
   fatal rather than a silent omission: a plugin that vanished because somebody
   left a trailing comma is debuggable only by guessing, which is the exact
   failure decision 4 refuses elsewhere. */
export class ManifestUnreadable extends Error {
  constructor(file, cause) {
    super(`${file} is not readable JSON: ${cause?.message ?? cause}`)
    this.name = 'ManifestUnreadable'
    this.file = file
    this.cause = cause
  }
}

/* A port, or null. A port written as a string must not silently produce
   `http://host:"20401"`, and a zero or a negative is not a port. */
function port(value) {
  const n = Number(value)
  return Number.isInteger(n) && n > 0 ? n : null
}

/* The readiness declaration, normalised, or null.

   Null when the demo declared nothing AND when it declared something
   unusable. A half-written declaration must not become a probe against
   port `NaN` that then reports the demo unreachable — "the demo is not
   set up to be checked" and "the demo cannot be reached" are different
   answers, and BR-AS79 rests on not confusing them. */
function readinessOf(metadata) {
  const declared = metadata?.readiness
  if (!declared || typeof declared !== 'object') return null
  const path = typeof declared.path === 'string' && declared.path.startsWith('/') ? declared.path : null
  const devPort = port(declared.devPort)
  const hostedUpstream = typeof declared.hostedUpstream === 'string' && declared.hostedUpstream !== ''
    ? declared.hostedUpstream
    : null
  if (path === null) return null
  if (devPort === null && hostedUpstream === null) return null
  return {
    path,
    devPort,
    hostedUpstream,
    /* A ceiling the SHELL applies, not the demo's backend. A demo may ask
       for longer than the default; it cannot ask for forever. */
    timeoutMs: Math.min(Math.max(Number(declared.timeoutMs) || 3000, 250), 15000),
  }
}

/* The demo API declaration, normalised, or null (BR-AS82, task 16j).

   Null when the demo declared nothing AND when it declared something
   unusable — the same rule `readinessOf` follows, and for the same reason: a
   half-written declaration must not become a proxy to port `NaN`.

   `routes` is the whole of what makes this a declared surface rather than a
   tunnel. Every path the shell will forward is named here, by the demo, and a
   path that is not named is not served. The shell therefore never has to
   decide what a demo's backend is willing to answer; it only forwards what the
   demo already said out loud.

   A route ending in `/` is a PREFIX — that is how Go's own mux reads a
   trailing slash, and demo 04's `/commands/` is written that way in
   `serve.go`. Anything else is matched exactly.

   Four shapes are dropped rather than forwarded. A path not starting with `/`
   has no meaning here. `//host` is an origin, not a path, and would let a
   declaration point the proxy off the demo. `..` climbs, and the prefix is the
   boundary as well as the layout — the same refusal `pluginAssets` makes for
   files. A `?` is a query, and a query is the caller's, never the
   declaration's. */
function apiOf(metadata) {
  const declared = metadata?.api
  if (!declared || typeof declared !== 'object') return null
  const devPort = port(declared.devPort)
  const hostedUpstream = typeof declared.hostedUpstream === 'string' && declared.hostedUpstream !== ''
    ? declared.hostedUpstream
    : null
  if (devPort === null && hostedUpstream === null) return null
  const routes = (Array.isArray(declared.routes) ? declared.routes : [])
    .filter((route) => typeof route === 'string')
    .filter((route) => route.startsWith('/') && !route.startsWith('//'))
    .filter((route) => !route.includes('..') && !route.includes('?') && !route.includes('#'))
  /* No routes is no surface. An `api` block naming a port and nothing else
     would otherwise read as "forward everything", which is the tunnel this
     rule exists to refuse. */
  if (routes.length === 0) return null
  return { devPort, hostedUpstream, routes: [...new Set(routes)].sort() }
}

/* The demo's own HOSTED asset upstream, normalised, or null (BR-AS77, task 16l).

   Why this exists. `plugin-source: build` copies each demo's built output into
   the shell's own served tree, so `/plugins/<id>/…` is a file on disk. A
   `plugin-source: registry` shell compiles no plugin and copies no plugin —
   that is BR-AS03 — so in a packaged registry deployment that prefix has
   nothing behind it and every asset is a 404. Development hid this, because
   the dev proxy forwards the prefix to the demo's own dev server.

   The repair is the same shape as `readiness` and `api`: the demo NAMES where
   its built assets are served in a hosted deployment, and the shell forwards
   the whole prefix there. The shell learns an upstream; it never learns a
   file layout, and it still compiles nothing.

   Origin only — scheme, host, optional port, and nothing else. A path here
   would be a second, hidden rewrite of the public layout, and a query or a
   fragment has no meaning for an upstream. `//host` is rejected with the rest,
   because it is not a URL this proxy can name. */
function assetsOf(metadata) {
  const declared = metadata?.assets
  if (!declared || typeof declared !== 'object') return null
  const hostedUpstream = typeof declared.hostedUpstream === 'string' ? declared.hostedUpstream : ''
  if (!/^https?:\/\/[A-Za-z0-9._-]+(?::[0-9]+)?$/.test(hostedUpstream)) return null
  return { hostedUpstream }
}

function directoriesIn(dir, fs) {
  let entries
  try {
    entries = fs.readdirSync(dir)
  } catch {
    /* No `demos/` at all is a lab with no demos, which is a correct empty
       catalogue rather than a broken one (decision 4). */
    return []
  }
  return entries
    .filter((name) => !name.startsWith('.'))
    .filter((name) => {
      try {
        return fs.statSync(join(dir, name)).isDirectory()
      } catch {
        return false
      }
    })
    .sort()
}

/**
 * Scan the repository for demo frontends that opted in.
 *
 * @param {object} options
 * @param {string} options.repoRoot Absolute path to the repository root.
 * @param {object} [options.fs] Injected for the specs; defaults to `node:fs`.
 * @returns {{ plugins: object[], entries: object[], files: string[] }}
 *   `plugins` is the raw manifests, in scan order — the catalogue's body.
 *   `entries` is the same discovery with the repository facts a manifest must
 *   not carry: the demo directory, its built output and its optional dev
 *   server. `files` is every file read, absolute, so a dev server can watch
 *   exactly what it depends on.
 */
export function scanDemoManifests({ repoRoot, fs = { readdirSync, readFileSync, statSync } }) {
  const demosDir = join(repoRoot, DEMOS_DIR)
  const plugins = []
  const entries = []
  const files = []
  for (const demo of directoriesIn(demosDir, fs)) {
    const file = join(demosDir, demo, MANIFEST_PATH)
    let raw
    try {
      raw = fs.readFileSync(file, 'utf8')
    } catch {
      continue // No manifest: this frontend is not a plugin. Not an error.
    }
    files.push(file)
    let manifest
    try {
      manifest = JSON.parse(raw)
    } catch (error) {
      throw new ManifestUnreadable(file, error)
    }
    plugins.push(manifest)

    /* The sibling file is read only for a demo that already opted in, so a
       stray `demo.json` beside no manifest admits nothing. */
    const metadataFile = join(demosDir, demo, DEMO_METADATA_PATH)
    let metadata = null
    let metadataRaw
    try {
      metadataRaw = fs.readFileSync(metadataFile, 'utf8')
    } catch {
      metadataRaw = null // Optional. Absence is the normal case.
    }
    if (metadataRaw !== null) {
      files.push(metadataFile)
      try {
        metadata = JSON.parse(metadataRaw)
      } catch (error) {
        throw new ManifestUnreadable(metadataFile, error)
      }
    }

    entries.push({
      demo,
      id: manifest?.id ?? null,
      manifest,
      manifestFile: file,
      metadataFile: metadata === null ? null : metadataFile,
      dist: join(demosDir, demo, DEMO_DIST_PATH),
      /* Normalised to a number or null here rather than at each reader, so a
         port written as a string does not silently produce `http://host:"20401"`. */
      devPort: port(metadata?.devServer?.port),
      /* Normalised here rather than at each of the four readers (the
         catalogue document, the dev proxy, the nginx snippet and the specs),
         so a half-written declaration is one `null` instead of four
         different guesses. */
      readiness: readinessOf(metadata),
      /* The demo's own command surface, normalised the same way and for the
         same reason (BR-AS82). Separate from `readiness`: one is a single
         probe the SHELL makes before mounting, the other is the demo's own
         calls made after it, and merging them would let a readiness
         declaration widen into a command route. */
      api: apiOf(metadata),
      /* Where this demo's BUILT assets are served in a hosted deployment
         (BR-AS77, task 16l). Separate from `devServer.port`, which is the
         same prefix in development, and separate from `dist`, which is the
         same prefix when the shell packages the files itself. Three ways to
         fill one public layout; a demo declares the ones that are true of it
         and the shell picks by how it was built. */
      assets: assetsOf(metadata),
      /* A local run command is information about the demo and is always
         true. Whether a READER is shown it is a property of the shell
         deployment, not of this file (F-5), so nothing is decided here. */
      runCommand: typeof metadata?.runCommand === 'string' && metadata.runCommand.trim() !== ''
        ? metadata.runCommand.trim()
        : null,
    })
  }
  return { plugins, entries, files }
}

/**
 * Build the catalogue document from a scan.
 *
 * The revision is a hash of the plugins themselves, so it changes exactly when
 * membership or a manifest changes, and two builds of the same tree agree.
 * `validateRegistryDocument` accepts a string revision.
 */
export function catalogueDocument(plugins) {
  const body = JSON.stringify(plugins)
  return {
    schemaVersion: REGISTRY_SCHEMA_VERSION,
    revision: createHash('sha256').update(body).digest('hex').slice(0, 16),
    /* Always present, always `false`: there is no service here to be
       half-available, so the shell must never render the "cannot vouch"
       state in this mode (decision 4). */
    degraded: false,
    plugins,
  }
}

/**
 * Build the SHELL-OWNED demo catalogue from the same scan (BR-AS79, R-1).
 *
 * A second document, not a second scan. It carries what the browser needs in
 * order to check a demo and to say something useful when the answer is no —
 * and nothing else. In particular it carries no port and no upstream: those
 * are facts about where a proxy forwards to, they differ between development
 * and a hosted deployment, and a page that knew them could be made to call
 * them directly, which is the cross-origin exception F-3 forbids.
 *
 * Two identifiers per entry, both stable, neither a plugin source:
 * `demo` is the directory name and owns the readiness route; `pluginId` is
 * the manifest's own id and is how a route being opened finds its demo. A
 * plugin discovered by `registry` and the same plugin discovered by `build`
 * match the same entry, which is the whole of R-1.
 *
 * An entry appears only when the demo declared readiness. A demo that
 * declared none is absent, and absence is the normal case, never a fault.
 */
export function demoCatalogueDocument(entries) {
  const demos = entries
    .filter((entry) => entry.readiness !== null && entry.id !== null)
    .map((entry) => ({
      demo: entry.demo,
      pluginId: entry.id,
      name: entry.manifest?.name ?? entry.demo,
      readiness: {
        /* The SHELL's own path, never the demo's. The demo's `/readyz` is
           reachable only through this route, on this origin. */
        url: demoReadinessPath(entry.demo),
        timeoutMs: entry.readiness.timeoutMs,
      },
      /* Always published; whether it is SHOWN is the deployment's decision
         (F-5). A demo does not know who is reading. */
      runCommand: entry.runCommand,
    }))
  return {
    schemaVersion: DEMO_CATALOGUE_SCHEMA_VERSION,
    revision: createHash('sha256').update(JSON.stringify(demos)).digest('hex').slice(0, 16),
    demos,
  }
}

/** Scan and render in one step — what both the dev server and the build call. */
export function generateCatalogue({ repoRoot, fs } = {}) {
  const { plugins, entries, files } = scanDemoManifests({ repoRoot, fs })
  return {
    document: catalogueDocument(plugins),
    demoCatalogue: demoCatalogueDocument(entries),
    entries,
    files,
  }
}
