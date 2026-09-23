/* Reading the shell-owned demo catalogue (BR-AS79, R-1).

   One fetch of one static file, served in BOTH plugin sources. Nothing here
   knows how a plugin was discovered, and nothing here may learn: readiness is
   a fact about a demo's services, and the same demo gives the same answer
   whether the shell found its plugin by scanning the repository or by reading
   the registry.

   The important difference from `buildCatalogueClient.js`: a MISSING demo
   catalogue is an EMPTY catalogue, not a fault.

   That looks like the rejected alternative in the other file, and it is the
   opposite case. The plugin catalogue IS the shell's plugins — absent, the
   screen is empty and the reader must be told why. The demo catalogue is
   decoration: absent, every demo is simply `unknown`, every plugin still
   mounts, and there is nothing for a reader to fix. A deployment that ships no
   demos at all is the normal case, not a broken one.

   A file that exists but will not parse is different, and is reported — that
   one really is a build that went wrong.
*/
import { DEMO_CATALOGUE_PATH } from './demoCatalogueLocation.js'

export const DEMO_CATALOGUE_UNREADABLE = 'demo-catalogue-unreadable'

/** The empty answer. Frozen, because it is shared by every absence path. */
const EMPTY = Object.freeze({ ok: true, revision: null, demos: Object.freeze([]) })

/* A catalogue entry, or null. Shape-checked rather than schema-validated: this
   document is produced by the shell's own build and read by the shell, so the
   only realistic fault is a stale file from an older build. Dropping an entry
   that lacks what a probe needs is better than probing `undefined`. */
function entryOf(raw) {
  if (!raw || typeof raw !== 'object') return null
  const { demo, pluginId } = raw
  if (typeof demo !== 'string' || demo === '') return null
  if (typeof pluginId !== 'string' || pluginId === '') return null
  const url = raw.readiness?.url
  if (typeof url !== 'string' || !url.startsWith('/')) return null
  const timeoutMs = Number(raw.readiness?.timeoutMs)
  return Object.freeze({
    demo,
    pluginId,
    name: typeof raw.name === 'string' && raw.name !== '' ? raw.name : demo,
    readiness: Object.freeze({
      /* Same-origin by construction: a relative path, resolved against this
         page. A catalogue that somehow carried an absolute URL is dropped by
         the check above rather than followed (F-3). */
      url,
      timeoutMs: Number.isFinite(timeoutMs) && timeoutMs > 0 ? timeoutMs : 3000,
    }),
    runCommand: typeof raw.runCommand === 'string' && raw.runCommand !== '' ? raw.runCommand : null,
  })
}

export function createDemoCatalogueClient({ fetch, url = DEMO_CATALOGUE_PATH } = {}) {
  return {
    /**
     * @returns {Promise<{ok: boolean, revision: string|null, demos: object[], code?: string}>}
     *   Always a usable `demos` array. `ok: false` says a catalogue was there
     *   and could not be read, which a deployment can fix; it never means
     *   "no demos".
     */
    async fetchDemoCatalogue() {
      let response
      try {
        response = await fetch(url, { cache: 'no-store' })
      } catch {
        return EMPTY // Not served here. Every demo is `unknown`, which is true.
      }
      if (!response?.ok) return EMPTY

      let document
      try {
        document = await response.json()
      } catch {
        return { ok: false, code: DEMO_CATALOGUE_UNREADABLE, revision: null, demos: [] }
      }

      const demos = Array.isArray(document?.demos)
        ? document.demos.map(entryOf).filter(Boolean)
        : []
      return {
        ok: true,
        revision: typeof document?.revision === 'string' ? document.revision : null,
        demos,
      }
    },
  }
}
