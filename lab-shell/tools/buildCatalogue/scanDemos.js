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

import { REGISTRY_SCHEMA_VERSION } from '../../src/shell/versions.js'

/** Where demos live, relative to the repository root. */
export const DEMOS_DIR = 'demos'

/** The opt-in file, relative to a demo directory. */
export const MANIFEST_PATH = join('frontend', 'public', 'manifest.json')

/* Raised when a manifest exists but cannot be read as JSON. It is deliberately
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
 * @returns {{ plugins: object[], files: string[] }} `files` is every manifest
 *   read, absolute, so a dev server can watch exactly what it depends on.
 */
export function scanDemoManifests({ repoRoot, fs = { readdirSync, readFileSync, statSync } }) {
  const demosDir = join(repoRoot, DEMOS_DIR)
  const plugins = []
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
    try {
      plugins.push(JSON.parse(raw))
    } catch (error) {
      throw new ManifestUnreadable(file, error)
    }
  }
  return { plugins, files }
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

/** Scan and render in one step — what both the dev server and the build call. */
export function generateCatalogue({ repoRoot, fs } = {}) {
  const { plugins, files } = scanDemoManifests({ repoRoot, fs })
  return { document: catalogueDocument(plugins), files }
}
