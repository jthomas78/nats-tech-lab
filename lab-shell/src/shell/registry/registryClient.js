/*
  The registry client — the shell's only source of *remote* plugins (BR-AS01).

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

/* accounts-service serves the curated registry (Design decision 21). The path
   sits under /api/platform/accounts because that prefix is already proxied to
   accounts-service in every frontend here — the registry is platform-wide, not
   account-scoped, and the prefix is a deployment fact rather than a claim
   about ownership. */
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
    async fetchRegistry() {
      let response
      const controller = new AbortController()
      const timer = setTimeout(() => controller.abort(), timeoutMs)
      try {
        response = await fetchImpl(endpoint, {
          headers: { Accept: 'application/json' },
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
      return { ok: true, plugins: validated.plugins, revision: validated.revision, fetchedAt: new Date().toISOString() }
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
