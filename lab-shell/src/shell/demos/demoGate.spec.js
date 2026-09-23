/* The pre-mount gate (BR-AS79, task 16e).

   Four claims, and they are the acceptance conditions rather than a list of
   behaviours: a demo that is not ready is never mounted; a demo that is ready
   is mounted and then left alone; the retry is a second check, not a reload;
   and a plugin with no associated demo passes through untouched. */
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import { withDemoGate } from './demoGate.js'
import { DEMO_STATE } from './readinessProbe.js'

const Plugin = defineComponent({
  name: 'PluginBody',
  setup: () => () => h('p', { class: 'plugin-body' }, 'the demo'),
})

const entry = { pluginId: 'demo-04', demo: '04-jetstream-cqrs', runCommand: 'docker compose up -d' }

/** A store stub whose answers are scripted, one per check. */
function storeReturning(...results) {
  const checkBeforeMount = vi.fn()
  for (const result of results) checkBeforeMount.mockResolvedValueOnce(result)
  checkBeforeMount.mockResolvedValue(results.at(-1))
  return { checkBeforeMount, entryFor: () => entry }
}

const available = { state: DEMO_STATE.AVAILABLE, cause: null, failing: [], checkedAt: null }
const notReady = {
  state: DEMO_STATE.UNAVAILABLE,
  cause: null,
  failing: [{ name: 'stream ODOMETER', code: 'missing' }],
  checkedAt: '2026-09-24T00:00:00.000Z',
}
const cannotTell = { state: DEMO_STATE.UNKNOWN, cause: 'timeout', failing: [], checkedAt: null }

const gate = (demoStore, { operator = () => false } = {}) =>
  mount(withDemoGate({ component: Plugin, pluginId: 'demo-04', demoStore, operator }))

describe('the demo readiness gate', () => {
  it('mounts the plugin when the demo is ready', async () => {
    const wrapper = gate(storeReturning(available))
    await flushPromises()
    expect(wrapper.find('.plugin-body').exists()).toBe(true)
    expect(wrapper.find('.demo-state-panel').exists()).toBe(false)
  })

  it('draws the shell\'s own panel instead of mounting an unready demo', async () => {
    const wrapper = gate(storeReturning(notReady))
    await flushPromises()
    expect(wrapper.find('.plugin-body').exists()).toBe(false)
    expect(wrapper.text()).toContain('This demo is not ready.')
    expect(wrapper.text()).toContain('stream ODOMETER')
  })

  /* The one wording rule, held at the place a reader actually sees. */
  it('never says a demo is stopped when it could not be reached', async () => {
    const wrapper = gate(storeReturning(cannotTell))
    await flushPromises()
    expect(wrapper.text()).toContain('Cannot reach demo services.')
    /* It may SAY the word, as one of two possibilities it explicitly cannot
       choose between. It may never ASSERT it. */
    expect(wrapper.text()).toContain('we could not tell')
    expect(wrapper.text()).not.toMatch(/(demo|it) is stopped/i)
  })

  it('checks once when the demo is opened', async () => {
    const store = storeReturning(available)
    gate(store)
    await flushPromises()
    expect(store.checkBeforeMount).toHaveBeenCalledTimes(1)
    expect(store.checkBeforeMount).toHaveBeenCalledWith('demo-04')
  })

  it('retries in place, and mounts the plugin when the answer changes', async () => {
    const store = storeReturning(notReady, available)
    const wrapper = gate(store)
    await flushPromises()
    await wrapper.find('.demo-state-retry').trigger('click')
    await flushPromises()
    expect(store.checkBeforeMount).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.plugin-body').exists()).toBe(true)
  })

  /* BR-AS79 splits ownership exactly at the mount: the shell owns before,
     the plugin owns after. The gate must not keep checking behind it. */
  it('steps out of the way once the plugin is mounted', async () => {
    const store = storeReturning(available)
    const wrapper = gate(store)
    await flushPromises()
    await flushPromises()
    expect(store.checkBeforeMount).toHaveBeenCalledTimes(1)
    expect(wrapper.find('.plugin-body').exists()).toBe(true)
  })

  /* F-5. A visitor deployment shows no run instructions, whatever the demo
     declared and whatever the build mode. */
  it('shows no run command to a visitor deployment', async () => {
    const wrapper = gate(storeReturning(notReady), { operator: () => false })
    await flushPromises()
    expect(wrapper.text()).not.toContain('docker compose')
  })

  it('shows the run command to an operator deployment', async () => {
    const wrapper = gate(storeReturning(notReady), { operator: () => true })
    await flushPromises()
    expect(wrapper.text()).toContain('docker compose up -d')
  })

  /* A registry plugin with no lab demo behind it. */
  it('is not applied at all when the shell has no demo store', () => {
    expect(withDemoGate({ component: Plugin, pluginId: 'x', demoStore: null })).toBe(Plugin)
  })
})
