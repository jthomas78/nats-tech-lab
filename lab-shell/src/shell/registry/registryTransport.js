import { validateRegistryDocument } from './manifestSchema.js'
import { REGISTRY_SCHEMA_VERSION } from '../versions.js'

export const SHELL_READ_SUBJECT = 'api._platform.registry.frontend-plugins.read.v1'

export function createRegistryTransport({ request, now = () => new Date().toISOString() }) {
  return {
    /* The conditional read is a revision, and only ever was. It travelled as
       a quoted ETag string while the catalogue came over HTTP; over a subject
       there is no header to shape, so the costume was removed (decision 27).
       The shell holds the number it was last served and asks for anything
       newer. */
    async fetchRegistry({ heldRevision: held = null } = {}) {
      const heldRevision = Number.isSafeInteger(held) && held >= 0 ? held : null
      let reply
      try {
        reply = await request(SHELL_READ_SUBJECT, { heldRevision })
      } catch (error) {
        const timeout = ['503', 'TIMEOUT'].includes(error?.code) || [error?.name, error?.cause?.name].some((name) => ['TimeoutError', 'NoRespondersError'].includes(name))
        return { ok: false, code: error instanceof SyntaxError ? 'registry-malformed' : timeout ? 'registry-timeout' : 'registry-unreachable' }
      }
      const malformed = { ok: false, code: 'registry-malformed' }
      if (!reply || reply.ok !== true || !Number.isSafeInteger(reply.revision) || reply.revision < 0) return malformed
      if (reply.unchanged === true) {
        if (reply.degraded || heldRevision === null || reply.revision !== heldRevision) return malformed
        return { ok: true, unchanged: true, heldRevision, revision: reply.revision, degraded: false, fetchedAt: now() }
      }
      const validated = validateRegistryDocument({
        schemaVersion: reply.schemaVersion ?? REGISTRY_SCHEMA_VERSION,
        revision: reply.revision,
        plugins: reply.entries,
        degraded: reply.degraded,
      })
      if (!validated.ok || (reply.degraded && reply.revision !== 0)) return malformed
      // Per-manifest validation remains bootShell.applyRegistry's job: one
      // incompatible entry is isolated, never a rejection of healthy peers.
      return {
        ok: true,
        unchanged: false,
        revision: reply.revision,
        plugins: validated.plugins,
        degraded: validated.degraded,
        heldRevision: validated.degraded ? null : reply.revision,
        fetchedAt: now(),
      }
    },
  }
}
