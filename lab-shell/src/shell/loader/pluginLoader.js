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

/*
  How long each step may take before the shell calls it failed.

  The Module Federation runtime bounds a classic-script remote at 20 s, but a
  `type: 'module'` remote — every remote here — goes through a bare
  `import()` with no timer at all. A remote that accepts the connection and
  never answers would hold the plugin in `loading` for the life of the page,
  with a spinner and no Retry. The load bound matches the runtime's own figure;
  it has to clear example-plugin-slow's deliberate six seconds. Activation is a
  plugin's own start-up code and gets less.
*/
export const DEFAULT_TIMEOUTS = Object.freeze({ load: 20_000, activate: 10_000 })

/**
 * @param {object} options
 * @param {{allows(url: string): boolean}} options.allowlist from the curated registry document
 * @param {Record<string, {load(remote: object): Promise<object>}>} options.adapters keyed by remote.kind
 * @param {Map<string, import('../registry/pluginStatus.js').PluginStatusRecord>} options.statuses
 * @param {{load?: number, activate?: number}} [options.timeouts] milliseconds per step
 */
export function createPluginLoader({ allowlist, adapters, statuses, timeouts = {} }) {
  const limits = { ...DEFAULT_TIMEOUTS, ...timeouts }
  /* Keyed by plugin id. Holds the in-flight promise, not just the settled
     module, so two components asking for the same plugin in the same tick
     share one load and one activate() (BR-AS08). */
  const inFlight = new Map()
  const modules = new Map()
  /*
    Keyed by module object: the activate() call already made for it.

    A timeout stops the shell WAITING; it does not stop the plugin's code. An
    activate() that timed out may still be running, and may finish. The
    federation runtime caches a remote, so a retry usually gets the SAME module
    object back — and calling its activate() a second time would start the
    plugin twice. So a retry on the same module waits on the first call
    instead of making a new one. A call that rejected is forgotten, because
    retrying a plugin whose activate() threw has always meant calling it again.
  */
  const activations = new WeakMap()

  const activateOnce = (module) => {
    let call = activations.get(module)
    if (!call) {
      /* Plugins receive only this explicitly versioned public surface. The
         shell's connection, credentials and registries remain private; an
         argument-ignoring v1 plugin still works unchanged. */
      call = Promise.resolve().then(() => module.activate(shellApi))
      activations.set(module, call)
      call.catch(() => activations.delete(module))
    }
    return call
  }

  const fail = (plugin, code, error) => {
    const record = statuses.get(plugin.id)
    /* A load that fails after the plugin was withdrawn is news about code
       nobody is showing. Recorded as the withdrawal's business, not as a
       plugin failure the Plugins screen should explain. */
    if (record?.status === PLUGIN_STATUS.WITHDRAWN) {
      record.settleWhileWithdrawn(PLUGIN_STATUS.FAILED)
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

        /* Each step is raced against its own timer. Once the race settles
           this attempt has its answer and moves on: a remote or an activate()
           that finishes AFTER its timeout resolves a promise nobody is
           awaiting any more, so it cannot publish a module, set `active`, or
           touch a newer attempt's state. A timeout also ends the attempt
           through `fail()`, which clears `inFlight`, so Retry starts a fresh
           one rather than joining the stalled one. */
        let module
        try {
          module = await bounded(adapter.load(plugin.remote), limits.load, 'load-timeout', plugin)
        } catch (error) {
          fail(plugin, error?.timeoutCode ?? 'chunk-load-failed', error)
        }
        if (!module || typeof module !== 'object') {
          fail(plugin, 'malformed-module', new Error(`Plugin ${plugin.id} exported no module`))
        }

        if (typeof module.activate === 'function') {
          try {
            await bounded(activateOnce(module), limits.activate, 'activate-timeout', plugin)
          } catch (error) {
            fail(plugin, error?.timeoutCode ?? 'activate-threw', error)
          }
        }

        modules.set(plugin.id, module)
        /* Withdrawn while this was in flight (BR-AS56). The module is kept —
           activate() has already run and the shell does not unload code — but
           the status stays withdrawn, and the return it is owed is `active`
           so nothing calls activate() a second time (BR-AS59). */
        if (record?.status === PLUGIN_STATUS.WITHDRAWN) {
          record.settleWhileWithdrawn(PLUGIN_STATUS.ACTIVE)
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

/* `work`, or a rejection tagged with `code` once `ms` have passed. The timer
   is cleared whichever way the race goes, so a load that settles in time
   leaves nothing scheduled behind it. */
function bounded(work, ms, code, plugin) {
  let timer
  const timeout = new Promise((_resolve, reject) => {
    timer = setTimeout(() => {
      const error = new Error(`Plugin ${plugin.id} did not finish ${code === 'load-timeout' ? 'loading' : 'activating'} within ${ms} ms`)
      error.timeoutCode = code
      reject(error)
    }, ms)
  })
  /* The losing side of a race still settles. A late rejection from the work
     would otherwise surface as unhandled. */
  Promise.resolve(work).catch(() => {})
  return Promise.race([work, timeout]).finally(() => clearTimeout(timer))
}

function canLoad(status) {
  return status === PLUGIN_STATUS.AVAILABLE || status === PLUGIN_STATUS.FAILED
}
