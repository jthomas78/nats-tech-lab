import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { SHELL } from '../shellKey.js'
import PluginSlot from './PluginSlot.vue'

const contribution = (overrides = {}) => ({
  kind: 'extension',
  pluginId: 'example-plugin',
  qualifiedId: 'example-plugin/home-panel',
  component: 'home-panel',
  ...overrides,
})

const Panel = defineComponent({
  props: { context: { type: Object, default: () => ({}) } },
  setup: (props) => () => h('p', { class: 'panel' }, `demo:${props.context.demoId ?? '-'}`),
})

const Throws = defineComponent({
  setup() {
    throw new Error('contribution exploded at http://localhost:7110/remoteEntry.js')
  },
})

const shellStub = ({ module = null, load, reasonCode = null, plugins = true } = {}) => ({
  loader: {
    peek: () => module,
    load: load ?? vi.fn(async () => ({ components: { 'home-panel': Panel } })),
  },
  manifestFor: (id) => (plugins && id === 'example-plugin' ? { id: 'example-plugin', name: 'Example Plugin' } : null),
  statuses: new Map([['example-plugin', { reasonCode }]]),
})

const mountSlot = (shell, props = {}) =>
  mount(PluginSlot, {
    props: { contribution: contribution(), ...props },
    global: { provide: { [SHELL]: shell } },
  })

beforeEach(() => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
})

describe('BR-AS08 — a contribution loads on first use, not at boot', () => {
  it('shows a placeholder and asks the loader once the slot mounts', async () => {
    const load = vi.fn(async () => ({ components: { 'home-panel': Panel } }))
    const wrapper = mountSlot(shellStub({ load }))

    expect(wrapper.find('[role="status"]').exists()).toBe(true)
    expect(load).toHaveBeenCalledTimes(1)

    await flushPromises()
    expect(wrapper.find('.panel').exists()).toBe(true)
  })

  it('renders an already-loaded module synchronously, with no placeholder flash', () => {
    const load = vi.fn()
    const wrapper = mountSlot(
      shellStub({ module: { components: { 'home-panel': Panel } }, load }),
    )

    expect(wrapper.find('.panel').exists()).toBe(true)
    expect(load).not.toHaveBeenCalled()
  })

  it('hands the region context through to the contribution (BR-AS07)', async () => {
    const wrapper = mountSlot(shellStub(), { context: Object.freeze({ demoId: '01-dictionary' }) })
    await flushPromises()

    expect(wrapper.find('.panel').text()).toBe('demo:01-dictionary')
  })
})

describe('BR-AS04 — a failing contribution is contained by its slot', () => {
  it('shows an error card, and no host error, when the chunk cannot be loaded', async () => {
    const load = vi.fn(async () => {
      throw new Error('Failed to fetch http://localhost:7110/remoteEntry.js')
    })
    const wrapper = mountSlot(shellStub({ load, reasonCode: 'load-failed' }))
    await flushPromises()

    const alert = wrapper.find('[role="alert"]')
    expect(alert.exists()).toBe(true)
    expect(alert.text()).toContain('load')
    expect(alert.text()).toContain('load-failed')
  })

  it('carries the loader-recorded cause, so activate() throwing is distinguishable', async () => {
    const load = vi.fn(async () => {
      throw new Error('boom')
    })
    const wrapper = mountSlot(shellStub({ load, reasonCode: 'activate-threw' }))
    await flushPromises()

    expect(wrapper.find('[role="alert"]').text()).toContain('activate-threw')
  })

  it('contains a contribution that throws while rendering', async () => {
    const load = vi.fn(async () => ({ components: { 'home-panel': Throws } }))
    const wrapper = mountSlot(shellStub({ load }))
    await flushPromises()

    const alert = wrapper.find('[role="alert"]')
    expect(alert.text()).toContain('render')
    expect(alert.text()).toContain('contribution-threw')
  })

  it('names the missing export when the module loads but the component is absent', async () => {
    const load = vi.fn(async () => ({ components: {} }))
    const wrapper = mountSlot(shellStub({ load }))
    await flushPromises()

    expect(wrapper.find('[role="alert"]').text()).toContain('component-not-exported')
  })

  it('refuses to render a contribution whose plugin is not registered', async () => {
    const load = vi.fn()
    const wrapper = mountSlot(shellStub({ load, plugins: false }))
    await flushPromises()

    expect(wrapper.find('[role="alert"]').text()).toContain('plugin-not-registered')
    expect(load).not.toHaveBeenCalled()
  })
})

describe('BR-AS04 — the error surface leaks no URL, token or message', () => {
  const secretful = [
    'Failed to fetch http://localhost:7110/remoteEntry.js',
    'GET /api/platform/accounts/frontend-plugins 401 (Authorization: Basic YWRtaW46cw==)',
  ]

  it.each(secretful)('keeps %s out of the rendered card', async (message) => {
    const load = vi.fn(async () => {
      throw new Error(message)
    })
    const wrapper = mountSlot(shellStub({ load, reasonCode: 'load-failed' }))
    await flushPromises()

    const text = wrapper.text()
    expect(text).not.toContain('http')
    expect(text).not.toContain('Basic')
    expect(text).not.toContain('7110')
    expect(text).not.toContain(message)
    // The detail is not lost — it goes where a developer looks for it.
    expect(console.error).toHaveBeenCalled()
  })

  it('keeps a render-time throw message off the screen too', async () => {
    const load = vi.fn(async () => ({ components: { 'home-panel': Throws } }))
    const wrapper = mountSlot(shellStub({ load }))
    await flushPromises()

    expect(wrapper.text()).not.toContain('http')
    expect(wrapper.text()).not.toContain('exploded')
  })
})

describe('BR-AS04 — a superseded load never paints', () => {
  /* A route-scoped control changes contribution while its chunk is in
     flight. Without a request token the first load finishes last and writes
     the previous plugin's component — or its failure card — over the current
     one. */
  const other = () => contribution({ pluginId: 'other-plugin', qualifiedId: 'other-plugin/home-panel' })

  const twoPlugins = (load) => ({
    loader: { peek: () => null, load },
    manifestFor: (id) => ({ id, name: id }),
    statuses: new Map([['example-plugin', { reasonCode: 'load-failed' }], ['other-plugin', { reasonCode: null }]]),
  })

  it('drops the result of a load its contribution has already replaced', async () => {
    let releaseFirst
    const Second = defineComponent({ setup: () => () => h('p', { class: 'second' }, 'second') })
    const load = vi.fn((plugin) => plugin.id === 'example-plugin'
      ? new Promise((resolve) => { releaseFirst = () => resolve({ components: { 'home-panel': Panel } }) })
      : Promise.resolve({ components: { 'home-panel': Second } }))

    const wrapper = mountSlot(twoPlugins(load))
    await wrapper.setProps({ contribution: other() })
    await flushPromises()
    releaseFirst()
    await flushPromises()

    expect(wrapper.find('.second').exists()).toBe(true)
    expect(wrapper.find('.panel').exists()).toBe(false)
  })

  it('drops the FAILURE of a load its contribution has already replaced', async () => {
    let rejectFirst
    const Second = defineComponent({ setup: () => () => h('p', { class: 'second' }, 'second') })
    const load = vi.fn((plugin) => plugin.id === 'example-plugin'
      ? new Promise((_resolve, reject) => { rejectFirst = () => reject(new Error('chunk gone')) })
      : Promise.resolve({ components: { 'home-panel': Second } }))

    const wrapper = mountSlot(twoPlugins(load))
    await wrapper.setProps({ contribution: other() })
    await flushPromises()
    rejectFirst()
    await flushPromises()

    expect(wrapper.find('.slot-error').exists()).toBe(false)
    expect(wrapper.find('.second').exists()).toBe(true)
  })
})
