/*
  The loader adapter interface (BR-AS08), plus the two guards that sit in front
  of every adapter.

  All plugins use a federated adapter. Loading, activation and failure
  isolation remain behind this network-independent contract.

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

import ExtensionRegion from '../ui/ExtensionRegion.vue'
import { SHELL_API_VERSION } from '../versions.js'

import { PLUGIN_STATUS } from '../registry/pluginStatus.js'

// One public API object for all plugins. Freeze the containers, not the Vue
// component: Vue may attach rendering metadata to component definitions.
const shellApi = Object.freeze({
  version: SHELL_API_VERSION,
  ui: Object.freeze({ ExtensionRegion }),
})

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
    /* A load that fails after the plugin was withdrawn is news about code
       nobody is showing. Recorded as the withdrawal's business, not as a
       plugin failure the Plugins screen should explain. */
    if (record?.status === PLUGIN_STATUS.WITHDRAWN) {
      record.restoreTo = PLUGIN_STATUS.FAILED
      inFlight.delete(plugin.id)
      throw error
    }
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
      if (record?.status === PLUGIN_STATUS.WITHDRAWN) {
        /* Named separately from the generic refusal below because it is the
           one a caller can hit through no fault of its own: a component may
           ask for a plugin that was withdrawn a tick ago. */
        throw new Error(`Plugin ${plugin.id} is withdrawn and cannot be loaded`)
      }
      if (record && !canLoad(record.status)) {
        throw new Error(
          `Plugin ${plugin.id} cannot be loaded from status ${record.status}`,
        )
      }

      const promise = (async () => {
        /* BR-AS01's second gate. A federated remote whose URL is not in the
           curated document is refused before the adapter sees it, whatever
           route the plugin record took to get here. */
        if (!allowlist.allows(plugin.remote.url)) {
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
            /* Plugins receive only this explicitly versioned public surface.
               The shell's connection, credentials and registries remain
               private; an argument-ignoring v1 plugin still works unchanged. */
            await module.activate(shellApi)
          } catch (error) {
            fail(plugin, 'activate-threw', error)
          }
        }

        modules.set(plugin.id, module)
        /* Withdrawn while this was in flight (BR-AS56). The module is kept —
           activate() has already run and the shell does not unload code — but
           the status stays withdrawn, and the return it is owed is `active`
           so nothing calls activate() a second time (BR-AS59). */
        if (record?.status === PLUGIN_STATUS.WITHDRAWN) {
          record.restoreTo = PLUGIN_STATUS.ACTIVE
          inFlight.delete(plugin.id)
          return module
        }
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
