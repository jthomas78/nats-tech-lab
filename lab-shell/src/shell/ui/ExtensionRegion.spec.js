import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { SHELL } from '../shellKey.js'
import ExtensionRegion from './ExtensionRegion.vue'

const POINT = 'shell/home-main/v1'

const contribution = (id, component) => ({
  kind: 'extension',
  pluginId: id,
  qualifiedId: `${id}/${component}`,
  component,
  target: POINT,
})

const Ok = defineComponent({
  props: { context: { type: Object, default: () => ({}) } },
  setup: (props) => () => h('p', { class: 'ok' }, String(props.context.demoId)),
})

const Mutating = defineComponent({
  props: { context: { type: Object, default: () => ({}) } },
  setup(props) {
    // A contributor reaching back into host state through a legal API.
    try {
      // eslint-disable-next-line vue/no-mutating-props -- the point of the spec
      props.context.demoId = 'hijacked'
    } catch {
      /* strict mode throws; sloppy mode silently no-ops. Both are fine. */
    }
    return () => h('p', { class: 'mutating' })
  },
})

const Throws = defineComponent({
  setup() {
    throw new Error('nope')
  },
})

const shellStub = (contributions, modules) => ({
  contributions: { extensionsFor: (point) => (point === POINT ? contributions : []) },
  loader: {
    peek: () => null,
    load: async (plugin) => {
      const module = modules[plugin.id]
      if (module instanceof Error) throw module
      return module
    },
  },
  manifestFor: (id) => (contributions.some((c) => c.pluginId === id) ? { id, name: id } : null),
  statuses: new Map(contributions.map((c) => [c.pluginId, { reasonCode: 'load-failed' }])),
})

const mountRegion = (shell, context = { demoId: '01-dictionary' }) =>
  mount(ExtensionRegion, {
    props: { point: POINT, context },
    global: { provide: { [SHELL]: shell } },
  })

beforeEach(() => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
})

describe('BR-AS07 — a host renders a point, not a plugin', () => {
  it('renders one slot per placed contribution, in the order the registry placed them', async () => {
    const contributions = [contribution('a', 'panel'), contribution('b', 'panel')]
    const wrapper = mountRegion(
      shellStub(contributions, {
        a: { components: { panel: Ok } },
        b: { components: { panel: Ok } },
      }),
    )
    await flushPromises()

    expect(wrapper.findAll('.plugin-slot').map((s) => s.attributes('data-contribution'))).toEqual([
      'a/panel',
      'b/panel',
    ])
  })

  it('renders nothing at all when no plugin contributes to the point', () => {
    const wrapper = mountRegion(shellStub([], {}))
    expect(wrapper.findAll('.plugin-slot')).toHaveLength(0)
  })

  it('freezes the context itself rather than trusting the caller to have frozen it', async () => {
    const contributions = [contribution('a', 'panel')]
    const wrapper = mountRegion(
      shellStub(contributions, { a: { components: { panel: Mutating } } }),
      { demoId: '01-dictionary' }, // deliberately a live object
    )
    await flushPromises()

    expect(wrapper.find('.mutating').exists()).toBe(true)
    expect(wrapper.props('context').demoId).toBe('01-dictionary')
  })
})

describe('BR-AS04 — one broken contribution does not take the region with it (task 1b-7)', () => {
  it('keeps healthy siblings rendered when a neighbour cannot load', async () => {
    const contributions = [contribution('a', 'panel'), contribution('bad', 'panel'), contribution('b', 'panel')]
    const wrapper = mountRegion(
      shellStub(contributions, {
        a: { components: { panel: Ok } },
        bad: new Error('Failed to fetch http://localhost:7110/gone.js'),
        b: { components: { panel: Ok } },
      }),
    )
    await flushPromises()

    expect(wrapper.findAll('.ok')).toHaveLength(2)
    expect(wrapper.findAll('[role="alert"]')).toHaveLength(1)
    expect(wrapper.findAll('.plugin-slot')).toHaveLength(3)
  })

  it('keeps healthy siblings rendered when a neighbour throws while rendering', async () => {
    const contributions = [contribution('a', 'panel'), contribution('bad', 'panel')]
    const wrapper = mountRegion(
      shellStub(contributions, {
        a: { components: { panel: Ok } },
        bad: { components: { panel: Throws } },
      }),
    )
    await flushPromises()

    expect(wrapper.findAll('.ok')).toHaveLength(1)
    expect(wrapper.find('[role="alert"]').text()).toContain('contribution-threw')
  })
})
