/*
  The loader adapter interface (BR-AS08), plus the two guards that sit in front
  of every adapter.

  Phase 1a ships this with one adapter — `builtin`, which resolves a module the
  shell already bundles, synchronously. Phase 1b adds a `federated` adapter over
  @module-federation/vite. The point of the split is that everything above the
  adapter — when code loads, how often activate() runs, what a failure does to
  the plugin's status — is decided here and is provable without a network. If
  federation misbehaves in 1b, this contract has still shipped.

  An adapter is an object with one method:

      { async load(remote) -> module }

  where `module` is whatever the plugin's entry exports. The loader never
  inspects it beyond calling `activate` once.

  Two guards run before any adapter is reached:

  - the curated-remote check (BR-AS01), asserted here rather than assumed from
    the call graph — "the loader is only ever called with registry records" is
    a claim about today's callers, and this is a property;
  - the status machine, which is what makes "loaded exactly once" observable
    rather than merely intended.
*/

import { PLUGIN_STATUS } from '../registry/pluginStatus.js'

/**
 * @param {object} options
 * @param {{allows(url: string): boolean}} options.allowlist from the curated registry document
 * @param {Record<string, {load(remote: object): Promise<object>}>} options.adapters keyed by remote.kind
 * @param {Map<string, import('../registry/pluginStatus.js').PluginStatusRecord>} options.statuses
 */
export function createPluginLoader({ allowlist, adapters, statuses }) {
  /* Keyed by plugin id. Holds the in-flight promise, not just the settled
     module, so two components asking for the same plugin in the same tick
     share one load and one activate() (BR-AS08). */
  const inFlight = new Map()
  const modules = new Map()

  const fail = (plugin, code, error) => {
    const record = statuses.get(plugin.id)
    record?.transition(PLUGIN_STATUS.FAILED, {
      code,
      message: error?.message ?? String(error),
    })
    /* Rethrow so the caller's own boundary can render the failed state for
       that region. Isolation is the caller's job (a route component, an
       extension slot); the loader's job is to make sure the failure is
       recorded and does not poison the next attempt. */
    inFlight.delete(plugin.id)
    throw error
  }

  return {
    /** Already-loaded modules, for components that must not trigger a load. */
    peek(pluginId) {
      return modules.get(pluginId) ?? null
    },

    isLoaded(pluginId) {
      return modules.has(pluginId)
    },

    /**
     * Load a plugin's code and activate it, at most once.
     * @returns {Promise<object>} the plugin module
     */
    async load(plugin) {
      if (modules.has(plugin.id)) return modules.get(plugin.id)
      if (inFlight.has(plugin.id)) return inFlight.get(plugin.id)

      const record = statuses.get(plugin.id)
      if (record && !canLoad(record.status)) {
        throw new Error(
          `Plugin ${plugin.id} cannot be loaded from status ${record.status}`,
        )
      }

      const promise = (async () => {
        /* BR-AS01's second gate. A federated remote whose URL is not in the
           curated document is refused before the adapter sees it, whatever
           route the plugin record took to get here. */
        if (plugin.remote.kind === 'federated' && !allowlist.allows(plugin.remote.url)) {
          fail(
            plugin,
            'remote-not-curated',
            new Error(`Remote ${plugin.remote.url} is not in the curated plugin registry`),
          )
        }

        const adapter = adapters[plugin.remote.kind]
        if (!adapter) {
          fail(
            plugin,
            'no-loader-adapter',
            new Error(`No loader adapter for remote kind ${plugin.remote.kind}`),
          )
        }

        record?.transition(PLUGIN_STATUS.LOADING)

        let module
        try {
          module = await adapter.load(plugin.remote)
        } catch (error) {
          fail(plugin, 'chunk-load-failed', error)
        }
        if (!module || typeof module !== 'object') {
          fail(plugin, 'malformed-module', new Error(`Plugin ${plugin.id} exported no module`))
        }

        if (typeof module.activate === 'function') {
          try {
            /* activate() gets nothing from the shell here on purpose. Whatever
               a plugin needs — the extension registry, its connection — is
               handed to it by the host that renders it, so a plugin cannot
               reach the shell's internals just by being loaded. */
            await module.activate()
          } catch (error) {
            fail(plugin, 'activate-threw', error)
          }
        }

        modules.set(plugin.id, module)
        record?.transition(PLUGIN_STATUS.ACTIVE)
        inFlight.delete(plugin.id)
        return module
      })()

      inFlight.set(plugin.id, promise)
      /* Swallow nothing, but don't let an unobserved rejection from the shared
         promise surface as an unhandled rejection when only one caller awaits. */
      promise.catch(() => {})
      return promise
    },
  }
}

function canLoad(status) {
  return status === PLUGIN_STATUS.AVAILABLE || status === PLUGIN_STATUS.FAILED
}

/**
 * The Phase 1a adapter: modules the shell already bundles, resolved
 * synchronously. A built-in plugin is platform-controlled by construction —
 * it is in the shell's own bundle — so it needs no allowlist entry.
 *
 * @param {Record<string, object|(() => object|Promise<object>)>} registry
 */
export function createBuiltinAdapter(registry) {
  return {
    async load(remote) {
      const entry = registry[remote.module]
      if (!entry) throw new Error(`No built-in module named ${remote.module}`)
      return typeof entry === 'function' ? entry() : entry
    },
  }
}
