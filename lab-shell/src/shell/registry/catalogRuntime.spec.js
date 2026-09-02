import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import PrimeVue from 'primevue/config'

import catalog from '../../../plugins/demo-catalog/public/manifest.json'
import * as catalogModule from '../../../plugins/demo-catalog/src/plugin.js'
import ExtensionRegion from '../../../plugins/demo-catalog/src/ExtensionRegion.js'
import { bootShell, withRuntime } from '../bootShell.js'
import { createPluginLoader } from '../loader/pluginLoader.js'
import { createShellRoutes } from '../routing/shellRoutes.js'
import { SHELL } from '../shellKey.js'
import App from '../../App.vue'
import HomeView from '../../views/HomeView.vue'
import PluginsView from '../../views/PluginsView.vue'
import NotFoundView from '../../views/NotFoundView.vue'

const permissions = { can: () => true }
const point = 'demo-catalog/details-sidebar/v1'

describe('BR-AS15 / decisions 90–92 — the federated catalog owns its regions', () => {
  it('renders the existing routes and a contribution inside the nested intro view', async () => {
    const contributor = { id: 'sidebar-plugin', name: 'Sidebar Plugin', schemaVersion: 1, shellApiVersion: 1,
      remote: { kind: 'federated', url: 'http://localhost:7999/remoteEntry.js', module: 'plugin' },
      contributions: [{ kind: 'extension', id: 'detail', target: point, component: 'panel' }] }
    // The owner is last on purpose: declarations precede indexing regardless of order.
    const shell = await bootShell({ permissions, registryClient: { fetchRegistry: async () => ({ ok: true, plugins: [contributor, catalog] }) } })
    expect(shell.contributions.extensionsFor(point)).toHaveLength(1)
    const panel = defineComponent({ props: { context: Object }, render() { return h('p', `Sidebar for ${this.context.demoId}`) } })
    const load = vi.fn(async (remote) => remote.url === catalog.remote.url ? catalogModule : { components: { panel } })
    const loader = createPluginLoader({ allowlist: shell.allowlist, statuses: shell.statuses, adapters: { federated: { load } } })
    const router = createRouter({ history: createMemoryHistory(), routes: createShellRoutes({ contributions: shell.contributions, loader, plugins }) })
    expect(load).not.toHaveBeenCalled()
    await router.push('/demos')
    const view = mount({ template: '<router-view />' }, { global: { plugins: [router, PrimeVue], provide: { [SHELL]: withRuntime(shell, { loader }) } } })
    await flushPromises()
    expect(view.text()).toContain('Dictionary POC')
    expect(router.resolve({ name: 'demo-catalog/intro', params: { id: '01-dictionary' } }).path).toBe('/demos/01-dictionary')
    await router.push('/demos/01-dictionary')
    await flushPromises()
    expect(view.find(`[data-extension-point="${point}"]`).text()).toContain('Sidebar for 01-dictionary')
    expect(view.find('article').text()).toContain('Dictionary')
    expect(shell.statuses.get('demo-catalog').status).toBe('active')
    expect(load.mock.calls.filter(([r]) => r.url === catalog.remote.url)).toHaveLength(1)
    view.unmount()
  })

  it('forwards attributes and slots through a wrapper below the activated entry', () => {
    const region = defineComponent({ props: { point: String }, setup: (props, { slots }) => () => h('section', { 'data-point': props.point }, slots.default?.()) })
    catalogModule.activate(Object.freeze({ version: 1, ui: Object.freeze({ ExtensionRegion: region }) }))
    const nested = defineComponent({ render: () => h(ExtensionRegion, { point, class: 'nested' }, { default: () => 'slot content' }) })
    const view = mount({ render: () => h('main', h(nested)) })
    expect(view.find('section.nested').attributes('data-point')).toBe(point)
    expect(view.text()).toBe('slot content')
    view.unmount()
  })
})

describe('BR-AS44 — the native frame survives without any plugin', () => {
  it.each([
    [{ ok: false, code: 'registry-unreachable' }, 'registry-unreachable'],
    [{ ok: false, code: 'registry-malformed' }, 'registry-malformed'],
    [{ ok: true, degraded: true, revision: 0, plugins: [] }, 'registry is degraded'],
  ])('renders the native frame with a reason and zero plugin loads: %j', async (discovery, reason) => {
    const shell = await bootShell({ permissions, registryClient: { fetchRegistry: async () => discovery } })
    const load = vi.fn()
    const router = createRouter({ history: createMemoryHistory(), routes: [
      { path: '/', component: HomeView, meta: { title: 'Home' } },
      { path: '/plugins', component: PluginsView, meta: { title: 'Plugins' } },
      { path: '/:pathMatch(.*)*', component: NotFoundView },
    ] })
    await router.push('/')
    const view = mount(App, { global: { plugins: [router], provide: { [SHELL]: withRuntime(shell, { loader: { load } }) } } })
    await flushPromises()
    expect(view.text()).toContain('No plugins have contributed')
    expect(view.findAll('a').map((a) => a.attributes('href'))).toContain('/plugins')
    expect(view.find('a[href="/demos"]').exists()).toBe(false)
    await router.push('/plugins')
    await flushPromises()
    expect(view.text()).toContain(reason)
    expect(view.text()).not.toMatch(/built-in/i)
    expect(shell.plugins).toEqual([])
    expect(shell.inventory).toEqual([])
    expect(load).not.toHaveBeenCalled()
    await router.push('/missing-feature')
    await flushPromises()
    expect(view.find('a[href="/demos"]').exists()).toBe(false)
    view.unmount()
  })
})
