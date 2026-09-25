import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h, reactive } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'

import PluginErrorView from '../../views/PluginErrorView.vue'
import { createPluginLoader } from '../loader/pluginLoader.js'
import { validateManifest } from '../registry/manifestSchema.js'
import { PLUGIN_STATUS, PluginStatusRecord } from '../registry/pluginStatus.js'
import { RemoteAllowlist } from '../registry/remoteAllowlist.js'
import { SHELL } from '../shellKey.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { createShellRoutes } from './shellRoutes.js'
import { installWithdrawalGuard } from './withdrawnRoutes.js'

/*
  BR-AS04 — Retry recovers a failed plugin route WITHOUT a page reload.

  Everything here is the real thing except the network: the real router, the
  real route table, the real loader and status machine, the real error panel
  and its real Retry button. The adapter is the only fake, so a failure and
  then a success can be scripted.

  The defect this pins: vue-router keeps the first component a lazy route
  resolves to. The route used to resolve to the bare error panel, so Retry
  loaded the plugin and then re-rendered the cached panel, forever.
*/

const Lesson = defineComponent({
  name: 'LessonView',
  props: { routeId: { type: String, default: '' }, vid: { type: String, default: '' } },
  render() {
    return h('main', `lesson ${this.routeId} vehicle ${this.vid || '-'}`)
  },
})

const manifest = validateManifest({
  id: 'demo-04',
  name: 'Demo 04',
  schemaVersion: REGISTRY_SCHEMA_VERSION,
  shellApiVersion: SHELL_API_VERSION,
  remote: { kind: 'federated', url: 'http://localhost:7110/remoteEntry.js', module: './plugin' },
  contributions: [
    { kind: 'route', id: 'lesson-02', path: '/demo-04/lesson-02/:vid', title: 'Lesson 02', component: 'lesson' },
    { kind: 'route', id: 'lesson-01', path: '/demo-04/lesson-01', title: 'Lesson 01', component: 'lesson' },
  ],
}).plugin

const ready = () => ({
  checkBeforeMount: vi.fn(async () => ({ state: 'available' })),
  entryFor: () => null,
})

/* `outcomes` scripts the adapter: 'fail' rejects, anything else loads. */
const harness = async ({ outcomes = ['fail', 'ok'], demoStore = ready(), path = '/demo-04/lesson-02/V7' } = {}) => {
  const allowlist = new RemoteAllowlist()
  allowlist.add(manifest)
  // Reactive, as bootShell.js makes them, so the panel re-reads the cause.
  const record = reactive(new PluginStatusRecord(manifest.id))
  record.transition(PLUGIN_STATUS.AVAILABLE)
  const statuses = reactive(new Map([[manifest.id, record]]))
  const activate = vi.fn()
  const adapter = {
    load: vi.fn(async () => {
      if ((outcomes.shift() ?? 'ok') === 'fail') throw new Error('chunk-load-failed')
      return { activate, components: { lesson: Lesson } }
    }),
  }
  const loader = createPluginLoader({ allowlist, adapters: { federated: adapter }, statuses })

  const withdrawn = new Set()
  const contributions = {
    routes: manifest.contributions
      .filter((c) => c.kind === 'route')
      .map((c) => ({ ...c, pluginId: manifest.id, qualifiedId: `${manifest.id}/${c.id}` })),
    isWithdrawn: (id) => withdrawn.has(id),
  }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'home', component: { render: () => h('p', 'home') } },
      // The panel links here; present so the router has somewhere to resolve it.
      { path: '/plugins', name: 'plugins', component: { render: () => h('p', 'plugins') } },
      ...createShellRoutes({
        contributions,
        loader,
        manifestFor: (id) => (id === manifest.id ? manifest : null),
        errorComponent: PluginErrorView,
        demoStore,
      }),
    ],
  })
  installWithdrawalGuard({ router, contributions })

  const wrapper = mount(defineComponent({ render: () => h(RouterView) }), {
    global: {
      plugins: [router],
      provide: { [SHELL]: { statuses, loader, manifestFor: (id) => (id === manifest.id ? manifest : null) } },
    },
  })
  await router.push(path)
  await flushPromises()
  return { wrapper, router, record, adapter, activate, loader, withdrawn, demoStore }
}

const clickRetry = async (wrapper) => {
  const button = wrapper.findAll('button').find((b) => b.text() === 'Retry')
  await button.trigger('click')
  await flushPromises()
}

describe('BR-AS04 — Retry recovers a failed plugin route in place', () => {
  it('goes failure -> Retry -> the rendered plugin, on the same page', async () => {
    const { wrapper, router, record, adapter, activate } = await harness()
    const pageBefore = wrapper.vm
    expect(wrapper.text()).toContain('could not be loaded')
    expect(record.status).toBe(PLUGIN_STATUS.FAILED)

    await clickRetry(wrapper)

    expect(wrapper.text()).toContain('lesson lesson-02 vehicle V7')
    expect(wrapper.text()).not.toContain('could not be loaded')
    expect(record.status).toBe(PLUGIN_STATUS.ACTIVE)
    expect(adapter.load).toHaveBeenCalledTimes(2)
    expect(activate).toHaveBeenCalledTimes(1)
    // No reload and no navigation: the same mounted app, the same URL.
    expect(wrapper.vm).toBe(pageBefore)
    expect(router.currentRoute.value.fullPath).toBe('/demo-04/lesson-02/V7')
  })

  it('keeps the route props: params and the plugin-local routeId', async () => {
    const { wrapper } = await harness({ path: '/demo-04/lesson-02/X9' })

    await clickRetry(wrapper)

    expect(wrapper.text()).toContain('lesson lesson-02 vehicle X9')
  })

  it('stays on the panel, with the fresh cause, when the retry fails too', async () => {
    const { wrapper, record } = await harness({ outcomes: ['fail', 'fail'] })

    await clickRetry(wrapper)

    expect(wrapper.text()).toContain('could not be loaded')
    expect(wrapper.text()).toContain('chunk-load-failed')
    expect(record.status).toBe(PLUGIN_STATUS.FAILED)
  })

  it('puts a recovered plugin behind the readiness gate, like a first-time load', async () => {
    const demoStore = {
      checkBeforeMount: vi.fn(async () => ({ state: 'unavailable', failing: [] })),
      entryFor: () => null,
    }
    const { wrapper } = await harness({ demoStore })

    await clickRetry(wrapper)

    expect(demoStore.checkBeforeMount).toHaveBeenCalledWith('demo-04')
    expect(wrapper.text()).not.toContain('lesson lesson-02')
    expect(wrapper.text()).not.toContain('could not be loaded')
  })

  it('does not bring a plugin withdrawn after the failure back on screen', async () => {
    const { wrapper, record, adapter } = await harness()
    record.withdraw()

    await clickRetry(wrapper)

    expect(adapter.load).toHaveBeenCalledTimes(1)
    expect(record.status).toBe(PLUGIN_STATUS.WITHDRAWN)
    expect(wrapper.text()).not.toContain('lesson lesson-02')
  })

  it('still refuses re-entry to a withdrawn plugin route', async () => {
    const { router, withdrawn } = await harness()
    await router.push('/')
    withdrawn.add('demo-04')

    await router.push('/demo-04/lesson-02/V7')

    expect(router.currentRoute.value.fullPath).toBe('/')
  })

  it('renders the plugin on a later visit once it has loaded some other way', async () => {
    const { wrapper, router, loader } = await harness()
    await router.push('/')
    await loader.load(manifest)

    await router.push('/demo-04/lesson-02/V7')
    await flushPromises()

    expect(wrapper.text()).toContain('lesson lesson-02 vehicle V7')
  })

  it('leaves the success path alone: a first-time load resolves straight to the plugin', async () => {
    const { wrapper, adapter } = await harness({ outcomes: ['ok'] })

    expect(wrapper.text()).toContain('lesson lesson-02 vehicle V7')
    expect(adapter.load).toHaveBeenCalledTimes(1)
  })
})
