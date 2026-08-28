/*
  Pinia store namespacing (BR-AS02, BR-AS06).

  A Pinia store id is a global key. The audit of the three apps due for
  migration found three that would each register a store called `tenant` and
  two that would register `dictionary` — today that is harmless because they
  are three separate bundles with three separate Pinia instances, and the
  moment they share a shell it is one store silently serving two features.
  The failure is quiet: the second registration wins, and the first feature
  reads the second's state.

  So a plugin never names a store globally. It is handed this factory, which
  prefixes every id with the plugin's own — the same {plugin-id}/{local-id}
  rule the contribution registry uses, for the same reason: uniqueness should
  follow from plugin ids being unique rather than from every author
  independently choosing well.
*/

import { defineStore } from 'pinia'

export function qualifyStoreId(pluginId, storeId) {
  if (typeof pluginId !== 'string' || pluginId === '') {
    throw new Error('A plugin store needs a plugin id')
  }
  if (typeof storeId !== 'string' || storeId === '') {
    throw new Error(`Plugin ${pluginId} defined a store with no id`)
  }
  /* A plugin cannot opt out by supplying a qualified id itself — that would
     let it reach another plugin's namespace through the same API that exists
     to keep it out. */
  if (storeId.includes('/')) {
    throw new Error(`Plugin ${pluginId} store id ${storeId} must not be namespaced by the plugin`)
  }
  return `${pluginId}/${storeId}`
}

/**
 * The store factory handed to one plugin. Everything it defines lands under
 * its own prefix, whatever it calls the store locally.
 */
export function createPluginStoreFactory(pluginId) {
  const defined = new Map()

  return {
    pluginId,

    define(storeId, setup, options) {
      const qualified = qualifyStoreId(pluginId, storeId)
      /* Idempotent by design: a plugin module re-evaluated (HMR, a retry after
         a failed activate) must get the same store back rather than a second
         definition under the same id. */
      if (!defined.has(qualified)) {
        defined.set(qualified, defineStore(qualified, setup, options))
      }
      return defined.get(qualified)
    },

    get ids() {
      return [...defined.keys()]
    },
  }
}
