/* The Meta-Leader page's two tabs, and the two old links that forward to
   them. The shell router is a fake on the app's globalProperties, as the
   real one is: the plugin never imports vue-router (BR-AS91). */
import { mount } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

import { AT, event, snapshot } from '../fixtures.js'

import LegacyRoute from './LegacyRoute.vue'
import MetaLeaderRoute from './MetaLeaderRoute.vue'
import { forgetTab, PAGE_PATH } from './tabs.js'

function fakeRouter(path = PAGE_PATH, query = {}) {
  return {
    currentRoute: ref({ path, query, hash: '' }),
    push: vi.fn(() => Promise.resolve()),
    replace: vi.fn(() => Promise.resolve()),
  }
}

const opts = (props, router) => ({
  props,
  global: {
    plugins: [PrimeVue],
    config: router ? { globalProperties: { $router: router } } : {},
  },
})

// A /state answer with one pending publish and one history line.
const PENDING = snapshot({
  pending: [{ id: 4, kind: 'publish', cluster: 'za', lane: 'za', via: 'auto', at: AT, limitNs: 5e9, text: 'publish za' }],
  history: [event(1, 'action', 'publish za', { cmd: 4 })],
  historySeq: 1,
})

let calls
beforeEach(() => {
  vi.useFakeTimers()
  calls = []
  vi.stubGlobal('fetch', (url, init) => {
    calls.push({ url: String(url), method: init?.method ?? 'GET' })
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(structuredClone(PENDING)) })
  })
})
afterEach(() => {
  forgetTab()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

const settle = () => vi.advanceTimersByTimeAsync(0)
const polls = () => calls.filter((c) => c.url.includes('/state'))
const shown = (w) => w.find('[role="tab"][aria-selected="true"]').text()

describe('the Meta-Leader tabs', () => {
  it('opens the tab its URL names', async () => {
    const w = mount(MetaLeaderRoute, opts({ tab: 'live' }, fakeRouter()))
    await settle()
    expect(shown(w)).toBe('Live')
    expect(w.find('[data-testid="playground"]').exists()).toBe(true)
    w.unmount()
  })

  it('opens Overview on a bare link, and writes it into the URL in place', async () => {
    const router = fakeRouter(PAGE_PATH, { x: '1' })
    const w = mount(MetaLeaderRoute, opts({}, router))
    await settle()
    expect(shown(w)).toBe('Overview')
    expect(router.replace).toHaveBeenCalledWith({ path: `${PAGE_PATH}/overview`, query: { x: '1' }, hash: '' })
    expect(router.push).not.toHaveBeenCalled()
    w.unmount()
  })

  it('pushes a history entry on a tab click, keeping the query', async () => {
    const router = fakeRouter(`${PAGE_PATH}/overview`, { x: '1' })
    const w = mount(MetaLeaderRoute, opts({ tab: 'overview' }, router))
    await settle()
    await w.find('[data-testid="meta-leader-tab-live"]').trigger('click')
    expect(router.push).toHaveBeenCalledWith({ path: `${PAGE_PATH}/live`, query: { x: '1' }, hash: '' })
    w.unmount()
  })

  it('follows the URL on back and forward', async () => {
    const w = mount(MetaLeaderRoute, opts({ tab: 'live' }, fakeRouter()))
    await settle()
    await w.setProps({ tab: 'overview' })
    expect(shown(w)).toBe('Overview')
    await w.setProps({ tab: 'live' })
    expect(shown(w)).toBe('Live')
    w.unmount()
  })

  it('remembers the last tab for the session, until the page is loaded again', async () => {
    const first = mount(MetaLeaderRoute, opts({ tab: 'live' }, fakeRouter()))
    await settle()
    first.unmount()
    const router = fakeRouter()
    const again = mount(MetaLeaderRoute, opts({}, router))
    await settle()
    expect(shown(again)).toBe('Live')
    expect(router.replace).toHaveBeenCalledWith(expect.objectContaining({ path: `${PAGE_PATH}/live` }))
    again.unmount()
  })
})

describe('one controller above the tabs', () => {
  it('keeps the pending command and the history through a tab switch, with one poll loop', async () => {
    const w = mount(MetaLeaderRoute, opts({ tab: 'live' }, fakeRouter()))
    await settle()
    expect(w.find('.ev[data-kind="pending"]').exists()).toBe(true)
    const before = polls().length

    await w.setProps({ tab: 'overview' })
    await vi.advanceTimersByTimeAsync(1000)
    await w.setProps({ tab: 'live' })
    await settle()

    // Still pending, and the history line is not lost.
    expect(w.find('.ev[data-kind="pending"]').text()).toMatch(/za lane/)
    expect(w.text()).toContain('publish za')
    // The poll went on at its own pace, never restarted from seq 0, and no
    // command was sent by switching.
    const after = polls().slice(before)
    expect(after.length).toBe(2)
    for (const c of after) expect(c.url).toMatch(/after=1$/)
    expect(calls.every((c) => c.method === 'GET')).toBe(true)
    w.unmount()
  })

  it('keeps the client settings through a tab switch', async () => {
    const w = mount(MetaLeaderRoute, opts({ tab: 'live' }, fakeRouter()))
    await settle()
    w.findComponent({ name: 'ClientSettings' }).vm.$emit('update:timeoutS', 9)
    await w.setProps({ tab: 'overview' })
    await w.setProps({ tab: 'live' })
    expect(w.findComponent({ name: 'ClientSettings' }).props('timeoutS')).toBe(9)
    w.unmount()
  })

  it('stops polling when the reader leaves, and sends nothing to the rig', async () => {
    const w = mount(MetaLeaderRoute, opts({ tab: 'live' }, fakeRouter()))
    await settle()
    w.unmount()
    const n = calls.length
    await vi.advanceTimersByTimeAsync(5000)
    expect(calls.length).toBe(n)
    expect(calls.every((c) => c.method === 'GET')).toBe(true)
  })
})

describe('the old links', () => {
  it.each([
    ['playground', 'live'],
    ['overview', 'overview'],
  ])('/demo-03/%s forwards, in place, to the %s tab and polls nothing', async (routeId, tab) => {
    const router = fakeRouter(`/demo-03/${routeId}`, { x: '1' })
    const w = mount(LegacyRoute, opts({ routeId }, router))
    await settle()
    expect(router.replace).toHaveBeenCalledWith({ path: `${PAGE_PATH}/${tab}`, query: { x: '1' }, hash: '' })
    expect(router.push).not.toHaveBeenCalled()
    expect(calls).toEqual([])
    w.unmount()
  })
})
