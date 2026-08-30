/*
  Historical HTTP client retained for the Phase 2 characterization tests.
  Phase 4's host uses registryTransport exclusively; the HTTP endpoint is gone
  and this is NOT a fallback. RemoteAllowlist below still gates the loader.

  Curation is the whole point of this module. A domain service must not be able
  to advertise its own frontend and have the browser run it; the only URLs the
  loader will fetch are the ones an operator put in this document, served by
  accounts-service (Design decision 21). So the client returns not just the
  plugins but the allowlist derived from them, and the loader refuses anything
  outside it — see loader/. Two independent gates, because "the loader only
  ever gets called with registry records" is an argument about today's call
  graph, not a property that can be asserted.

  Built-in plugins are not served from here. They are compiled into the shell
  bundle, so they are platform-controlled by construction, and they are what
  keeps the shell useful when this endpoint is down (BR-AS04).
*/

import { validateRegistryDocument } from './manifestSchema.js'

/* accounts-service serves the curated registry (Design decision 21), under
   its own /api/platform/registry prefix rather than the accounts one — the
   registry is platform-wide, not account-scoped, and its own prefix is what
   lets the shell's proxy rule carry no credential while the admin app's
   carries one (BR-AS25, decision 50). The prefix is a deployment fact rather
   than a claim about ownership. */
export const REGISTRY_ENDPOINT = '/api/platform/registry/frontend-plugins'

/**
 * @param {object} options
 * @param {typeof fetch} [options.fetch] injected so specs need no network
 * @param {string} [options.endpoint]
 * @param {number} [options.timeoutMs] a hung registry must not hang the boot
 */
export function createRegistryClient({
  fetch: fetchImpl = globalThis.fetch,
  endpoint = REGISTRY_ENDPOINT,
  timeoutMs = 5000,
} = {}) {
  return {
    endpoint,

    /**
     * Never throws. A registry failure is a degraded shell, not a broken one
     * (BR-AS04), so every outcome comes back as a result object and the caller
     * decides what to render.
     *
     * @returns {Promise<{ok: true, plugins: object[]} | {ok: false, code: string, message: string}>}
     */
    async fetchRegistry({ etag = null } = {}) {
      let response
      const controller = new AbortController()
      const timer = setTimeout(() => controller.abort(), timeoutMs)
      try {
        response = await fetchImpl(endpoint, {
          headers: {
            Accept: 'application/json',
            /* The conditional read (BR-AS19, decision 27). A running shell
               re-reads on focus and on a slow interval; without this every
               one of those reads would ship the whole document to learn
               nothing. The revision is the ETag, so "unchanged" is a fact the
               server states rather than one the shell infers by comparison. */
            ...(etag ? { 'If-None-Match': etag } : {}),
          },
          signal: controller.signal,
          /* The registry is per-viewer: BR-AS05's claims decide which plugins
             an operator has published to this account, so the request carries
             credentials and must never be served from a shared cache. */
          credentials: 'same-origin',
          cache: 'no-store',
        })
      } catch (error) {
        return {
          ok: false,
          code: error?.name === 'AbortError' ? 'registry-timeout' : 'registry-unreachable',
          message: `Plugin registry at ${endpoint} could not be reached: ${error?.message ?? error}`,
        }
      } finally {
        clearTimeout(timer)
      }

      /* 304 is a success with no body: the shell keeps everything it has and
         the caller learns only that nothing moved. Deliberately not folded
         into the ok:false branch — an unchanged registry is not a failure,
         and a caller that treated it as one would degrade on a healthy
         read. */
      if (response.status === 304) {
        return { ok: true, unchanged: true, etag, fetchedAt: new Date().toISOString() }
      }

      if (!response.ok) {
        return {
          ok: false,
          code: `registry-http-${response.status}`,
          message: `Plugin registry at ${endpoint} returned HTTP ${response.status}`,
        }
      }

      let document
      try {
        document = await response.json()
      } catch (error) {
        return {
          ok: false,
          code: 'registry-malformed',
          message: `Plugin registry at ${endpoint} did not return JSON: ${error?.message ?? error}`,
        }
      }

      const validated = validateRegistryDocument(document)
      if (!validated.ok) {
        return { ok: false, code: validated.code, message: validated.message }
      }
      return {
        ok: true,
        unchanged: false,
        plugins: validated.plugins,
        revision: validated.revision,
        /* Carried through rather than inferred from an empty plugin list: a
           degraded registry and an empty one look identical otherwise, and
           only one of them means "this is not what is curated" (BR-AS22). */
        degraded: validated.degraded,
        /* Whatever the server will honour on the next If-None-Match. Never
           reconstructed from the revision here — the header is the server's
           to shape. */
        etag: response.headers?.get?.('ETag') ?? null,
        fetchedAt: new Date().toISOString(),
      }
    },
  }
}

/**
 * The set of remote entry URLs the operator curated. Built from validated
 * manifests only — a manifest rejected on metadata contributes no URL, so an
 * incompatible plugin cannot smuggle one in.
 */
export class RemoteAllowlist {
  #urls = new Set()

  add(plugin) {
    if (plugin?.remote?.kind === 'federated') this.#urls.add(plugin.remote.url)
    return this
  }

  allows(url) {
    return this.#urls.has(url)
  }

  get size() {
    return this.#urls.size
  }
}
