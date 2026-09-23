/* The `build`-mode catalogue client (decision 4, BR-AS76).

   It returns the registry transport's EXACT result shape, so `decideRead`
   stays pure and never learns that sources exist. Compare
   `registryTransport.js`: same seven keys on a document read, same six on an
   unchanged read, and `createRegistrySession` calls this the same way it calls
   that.

   `degraded` is always `false`. There is no service here to be half-available,
   so the shell must never render the "cannot vouch" state in this mode.

   The distinction decision 4 added at the gate, and the reason this file is
   longer than a fetch: two very different situations both end in an empty
   screen and must not look alike.

   - Generated correctly, zero entries, because no demo carries a
     `public/manifest.json` yet — a SUCCESSFUL empty read (`ok: true,
     plugins: []`). `decideRead` renders it as `outcome: 'document'` that
     happens to hold nothing.
   - Missing, or will not parse — a FAILED read carrying a code, which
     `decideRead`'s existing failure branch renders with a reason.

   Treating any absence as emptiness is the rejected alternative: a simpler
   client that makes a broken build indistinguishable from a lab with no demos.
*/
import { BUILD_CATALOGUE_PATH } from './buildCatalogueLocation.js'
import { validateRegistryDocument } from './manifestSchema.js'

/* Distinct from the transport's `registry-*` codes on purpose. A reader who
   sees one of these knows which source failed without being told the mode. */
export const CATALOGUE_UNREACHABLE = 'build-catalogue-unreachable'
export const CATALOGUE_MISSING = 'build-catalogue-missing'
export const CATALOGUE_UNREADABLE = 'build-catalogue-unreadable'
export const CATALOGUE_MALFORMED = 'build-catalogue-malformed'

export function createBuildCatalogueClient({
  fetch,
  url = BUILD_CATALOGUE_PATH,
  now = () => new Date().toISOString(),
} = {}) {
  return {
    async fetchRegistry({ heldRevision: held = null } = {}) {
      /* The transport normalises `heldRevision` to a non-negative integer
         because its revisions come from Postgres. Ours is a content hash, so
         the normalisation here is "a non-empty string, or nothing". */
      const heldRevision = typeof held === 'string' && held !== '' ? held : null

      let response
      try {
        response = await fetch(url, { cache: 'no-store' })
      } catch {
        return { ok: false, code: CATALOGUE_UNREACHABLE }
      }
      /* Four codes rather than one, because decision 4's whole point is that a
         reader should not have to guess. A 404 is a shell deployed without its
         catalogue; any other refusal is the dev server reporting that it could
         not generate one (a manifest that will not parse). Both are failures,
         and neither is an empty lab — but they are fixed in different places. */
      if (!response?.ok) {
        return { ok: false, code: response?.status === 404 ? CATALOGUE_MISSING : CATALOGUE_UNREADABLE }
      }

      let document
      try {
        document = await response.json()
      } catch {
        return { ok: false, code: CATALOGUE_MALFORMED }
      }

      /* The same validator the NATS transport uses. The document is the
         authority in this mode, but "authority" is not "unchecked" — a
         truncated deploy must fail loudly rather than empty the menu. */
      const validated = validateRegistryDocument(document)
      if (!validated.ok) return { ok: false, code: CATALOGUE_MALFORMED }

      const revision = validated.revision
      const fetchedAt = now()

      if (heldRevision !== null && revision !== null && heldRevision === revision) {
        return { ok: true, unchanged: true, heldRevision, revision, degraded: false, fetchedAt }
      }

      return {
        ok: true,
        unchanged: false,
        revision,
        plugins: validated.plugins,
        degraded: false,
        heldRevision: revision,
        fetchedAt,
      }
    },
  }
}
