import { createPinia, setActivePinia } from 'pinia'
import { ref } from 'vue'
import { beforeEach, describe, expect, it } from 'vitest'

import { createPluginStoreFactory, qualifyStoreId } from './pluginStores.js'

const tenantStore = () => {
  const name = ref('unset')
  return { name }
}

describe('BR-AS06 — plugin stores are namespaced by their plugin', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('prefixes the store id with the plugin id', () => {
    expect(qualifyStoreId('fleet-ops', 'tenant')).toBe('fleet-ops/tenant')
  })

  it('keeps two plugins that both define `tenant` apart', () => {
    // This is the collision the migration audit found in three of the apps.
    const a = createPluginStoreFactory('fleet-ops').define('tenant', tenantStore)
    const b = createPluginStoreFactory('port-insights').define('tenant', tenantStore)

    const first = a()
    const second = b()
    first.name = 'acme'

    expect(second.name).toBe('unset')
    expect(a.$id).not.toBe(b.$id)
  })

  it('returns the same store for the same plugin and id', () => {
    const factory = createPluginStoreFactory('fleet-ops')

    expect(factory.define('tenant', tenantStore)).toBe(factory.define('tenant', tenantStore))
  })

  it('refuses a plugin that tries to namespace itself into someone else', () => {
    const factory = createPluginStoreFactory('fleet-ops')

    expect(() => factory.define('port-insights/tenant', tenantStore)).toThrow(/must not be namespaced/)
  })

  it('refuses a store with no id', () => {
    expect(() => createPluginStoreFactory('fleet-ops').define('', tenantStore)).toThrow()
  })

  it('reports what a plugin has defined, for the Plugins screen', () => {
    const factory = createPluginStoreFactory('fleet-ops')
    factory.define('tenant', tenantStore)
    factory.define('dictionary', tenantStore)

    expect(factory.ids).toEqual(['fleet-ops/tenant', 'fleet-ops/dictionary'])
  })
})
