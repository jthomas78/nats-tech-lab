import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { installShellRoutes } from './shellRoutes.js'

describe('BR-AS12 — cold deep links survive deferred discovery', () => {
  it('re-resolves a painted catch-all when its plugin metadata arrives', async () => {
    const component = { render: () => null }
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', name: 'not-found', component }] })
    await router.push('/fleet/vessels?tab=details#ship')
    const loader = { load: vi.fn(async () => ({ components: { vessels: component } })) }
    expect(router.currentRoute.value.name).toBe('not-found')
    await installShellRoutes({ router, contributions: { routes: [{ qualifiedId: 'fleet/vessels', pluginId: 'fleet', path: '/fleet/vessels', component: 'vessels' }] }, loader, manifestFor: (id) => (id === 'fleet' ? { id: 'fleet' } : null) })
    expect(router.currentRoute.value.name).toBe('fleet/vessels')
    expect(router.currentRoute.value.fullPath).toBe('/fleet/vessels?tab=details#ship')
    expect(loader.load).toHaveBeenCalledOnce()
  })
  it('does not navigate or load an unrelated newly discovered plugin', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', name: 'not-found', component: { render: () => null } }] })
    await router.push('/unknown')
    const loader = { load: vi.fn() }
    await installShellRoutes({ router, contributions: { routes: [{ qualifiedId: 'fleet/home', pluginId: 'fleet', path: '/fleet', component: 'home' }] }, loader, manifestFor: () => null })
    expect(router.currentRoute.value.fullPath).toBe('/unknown')
    expect(loader.load).not.toHaveBeenCalled()
  })
})
