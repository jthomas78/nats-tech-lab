import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'

import { SHELL } from '../shellKey.js'
import ShellFooter from './ShellFooter.vue'

const mountWith = (shell) =>
  mount(ShellFooter, {
    global: {
      provide: {
        [SHELL]: reactive({
          statuses: new Map(),
          contributions: { shellFooter: [] },
          registryError: null,
          registry: { revision: null, degraded: false },
          connection: { connected: true, epoch: 1, error: null },
          ...shell,
        }),
      },
      stubs: { PluginSlot: true },
    },
  })

describe('BR-AS22 — the registry degrades, it does not fail', () => {
  it('says nothing about the registry when it answered normally', () => {
    const w = mountWith({ registry: { revision: '50', degraded: false } })
    expect(w.find('[data-testid="registry-degraded"]').exists()).toBe(false)
    expect(w.find('[data-testid="registry-unavailable"]').exists()).toBe(false)
  })

  it('distinguishes a degraded answer from an empty catalog', () => {
    // An empty registry is a legitimate state (nothing curated yet). Only the
    // service's own degraded:true earns the notice.
    expect(mountWith({ registry: { revision: '0', degraded: false } })
      .find('[data-testid="registry-degraded"]').exists()).toBe(false)
    expect(mountWith({ registry: { revision: '0', degraded: true } })
      .find('[data-testid="registry-degraded"]').exists()).toBe(true)
  })

  it('distinguishes a degraded answer from no answer at all', () => {
    const w = mountWith({ registryError: { code: 'registry-unreachable', message: 'down' } })
    expect(w.find('[data-testid="registry-unavailable"]').exists()).toBe(true)
    expect(w.find('[data-testid="registry-degraded"]').exists()).toBe(false)
  })

  it('shows the revision and never the endpoint (BR-AS04)', () => {
    const w = mountWith({ registry: { revision: '50', degraded: true } })
    expect(w.text()).toContain('50')
    expect(w.text()).not.toContain('/api/')
  })
})

/*
  BR-AS30 — the connection is reported, and reporting it is not free.

  A NATS socket flaps. An indicator bound straight to `connected` would blink
  on every reconnect the shell recovers from on its own, which trains the
  operator to ignore the one time it matters. So the DOWN state is debounced:
  the shell has to have been disconnected for a while before it says so.
  Recovery is not debounced — good news is shown immediately.
*/
describe('BR-AS30 — the shell reports its connection without crying wolf', () => {
  const DEBOUNCE_MS = 5000

  it('says nothing while connected', () => {
    const w = mountWith({ connection: { connected: true, epoch: 1, error: null } })
    expect(w.find('[data-testid="shell-disconnected"]').exists()).toBe(false)
  })

  it('does not announce a disconnect that has only just happened', async () => {
    vi.useFakeTimers()
    try {
      const w = mountWith({ connection: { connected: false, epoch: 1, error: null } })
      await w.vm.$nextTick()
      expect(w.find('[data-testid="shell-disconnected"]').exists()).toBe(false)
    } finally {
      vi.useRealTimers()
    }
  })

  it('announces one that persists', async () => {
    vi.useFakeTimers()
    try {
      const w = mountWith({ connection: { connected: false, epoch: 1, error: null } })
      await vi.advanceTimersByTimeAsync(DEBOUNCE_MS + 1)
      await w.vm.$nextTick()
      expect(w.find('[data-testid="shell-disconnected"]').exists()).toBe(true)
    } finally {
      vi.useRealTimers()
    }
  })

  it('clears the notice the moment the connection comes back', async () => {
    vi.useFakeTimers()
    try {
      const shell = reactive({
        statuses: new Map(),
        contributions: { shellFooter: [] },
        registryError: null,
        registry: { revision: '50', degraded: false },
        connection: { connected: false, epoch: 1, error: null },
      })
      const w = mount(ShellFooter, {
        global: { provide: { [SHELL]: shell }, stubs: { PluginSlot: true } },
      })
      await vi.advanceTimersByTimeAsync(DEBOUNCE_MS + 1)
      await w.vm.$nextTick()
      expect(w.find('[data-testid="shell-disconnected"]').exists()).toBe(true)

      shell.connection.connected = true
      shell.connection.epoch = 2
      await w.vm.$nextTick()
      expect(w.find('[data-testid="shell-disconnected"]').exists()).toBe(false)
    } finally {
      vi.useRealTimers()
    }
  })

  /* Decision 54, said in the UI: a shell that never connected is a shell
     running its built-ins, not a broken one. The notice is a notice — there
     is no verb here that unloads, retries destructively, or blocks the page. */
  it('offers no affordance that tears anything down', async () => {
    vi.useFakeTimers()
    try {
      const w = mountWith({ connection: { connected: false, epoch: 0, error: { code: 'connect-refused' } } })
      await vi.advanceTimersByTimeAsync(DEBOUNCE_MS + 1)
      await w.vm.$nextTick()
      const text = w.text().toLowerCase()
      for (const verb of ['unload', 'disable', 'remove', 'reload the page']) {
        expect(text).not.toContain(verb)
      }
    } finally {
      vi.useRealTimers()
    }
  })

  /* BR-AS04 again, on the one string in this component that comes from a
     transport error rather than from the shell's own vocabulary. */
  it('names no host, port or subject when the connection was refused', async () => {
    vi.useFakeTimers()
    try {
      const w = mountWith({
        connection: { connected: false, epoch: 0, error: { code: 'connect-refused' } },
      })
      await vi.advanceTimersByTimeAsync(DEBOUNCE_MS + 1)
      await w.vm.$nextTick()
      const text = w.text()
      expect(text).not.toContain('4222')
      expect(text).not.toContain('ws://')
      expect(text).not.toContain('api._platform')
    } finally {
      vi.useRealTimers()
    }
  })

  /* The disconnect touches the transport and nothing that is already on
     screen. Plugins keep running; the revision the shell last read stays the
     revision it holds. */
  it('keeps showing what the shell already read while disconnected', async () => {
    vi.useFakeTimers()
    try {
      const w = mountWith({
        registry: { revision: '50', degraded: false },
        connection: { connected: false, epoch: 1, error: null },
      })
      await vi.advanceTimersByTimeAsync(DEBOUNCE_MS + 1)
      await w.vm.$nextTick()
      expect(w.text()).toContain('50')
      expect(w.find('[data-testid="registry-degraded"]').exists()).toBe(false)
    } finally {
      vi.useRealTimers()
    }
  })
})
