import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { h } from 'vue'
import { demoCatalogManifest } from '../../plugins/demo-catalog/manifest.js'
import { bootShell } from '../bootShell.js'
import { createPermissionEvaluator } from '../auth/permissions.js'
import { createShellConnection } from '../connections/shellConnection.js'
import { createRegistrySession } from './registrySession.js'
import ShellFooter from '../ui/ShellFooter.vue'
import { SHELL } from '../shellKey.js'

const deferred = () => { let resolve; const promise = new Promise((r) => { resolve = r }); return { promise, resolve } }
const cleanup = []
afterEach(async () => { while (cleanup.length) await cleanup.pop()() })

async function fixture({ connect = async () => ({ close() {} }), afterPaint = async () => {}, client } = {}) {
  const shell = await bootShell({ builtins: [demoCatalogManifest], permissions: createPermissionEvaluator({ permissions: ['*'] }) })
  const connection = createShellConnection({ connect })
  let deliver
  connection.subscribe = vi.fn((_subject, handler) => { deliver = handler; return { unsubscribe: vi.fn() } })
  connection.flush = vi.fn(async () => {})
  const transport = client ?? { fetchRegistry: vi.fn(async () => ({ ok: true, revision: 12, etag: '"12"', plugins: [] })) }
  const onResult = vi.fn(shell.applyRegistry)
  const session = createRegistrySession({ connection, client: transport, shell, afterPaint, onResult })
  cleanup.push(() => session.stop())
  return { shell, connection, client: transport, session, onResult, deliver: (body) => deliver(body) }
}

describe('BR-AS30 — live host boot ordering', () => {
  it('renders built-in navigation before painting permits minting or connecting', async () => {
    const paint = deferred()
    const socket = deferred()
    const connect = vi.fn(() => socket.promise)
    const f = await fixture({ connect, afterPaint: () => paint.promise })
    const view = mount({ render: () => h('nav', f.shell.contributions.navigation.map((n) => n.label).join(', ')) })
    cleanup.push(() => view.unmount())
    const started = f.session.start()
    expect(view.text()).toContain('Demos')
    expect(connect).not.toHaveBeenCalled()
    paint.resolve()
    await flushPromises()
    expect(connect).toHaveBeenCalledOnce()
    expect(f.client.fetchRegistry).not.toHaveBeenCalled()
    socket.resolve({ close() {} })
    await started
    await flushPromises()
    expect(f.client.fetchRegistry).toHaveBeenCalled()
  })

  it('keeps built-ins after a refused connection and reflects a late read error reactively', async () => {
    const f = await fixture({ connect: async () => { throw new Error('refused') } })
    const view = mount(ShellFooter, { global: { provide: { [SHELL]: f.shell }, stubs: { PluginSlot: true } } })
    cleanup.push(() => view.unmount())
    await f.session.start()
    expect(f.shell.contributions.navigation).toHaveLength(1)
    f.shell.applyRegistry({ ok: false, code: 'registry-unreachable' })
    await view.vm.$nextTick()
    expect(view.find('[data-testid="registry-unavailable"]').exists()).toBe(true)
    f.shell.applyRegistry({ ok: true, unchanged: true })
    await view.vm.$nextTick()
    expect(view.find('[data-testid="registry-unavailable"]').exists()).toBe(false)
  })
})

describe('BR-AS28/29 — session convergence', () => {
  it('reads before subscribing, then closes the snapshot/subscription gap', async () => {
    const f = await fixture()
    await f.session.start()
    await flushPromises()
    expect(f.client.fetchRegistry.mock.calls.map(([opts]) => opts.etag)).toEqual([null, '"12"'])
    expect(f.client.fetchRegistry.mock.invocationCallOrder[0]).toBeLessThan(f.connection.subscribe.mock.invocationCallOrder[0])
    expect(f.connection.flush).toHaveBeenCalledOnce()
  })

  it('clears the token on failure/degradation and recovers at the same revision', async () => {
    const f = await fixture()
    await f.session.start()
    await flushPromises()
    f.client.fetchRegistry.mockResolvedValueOnce({ ok: false, code: 'registry-timeout' })
    f.deliver({ revision: 13 })
    await flushPromises()
    expect(f.shell.registry.etag).toBe(null)
    f.client.fetchRegistry.mockResolvedValueOnce({ ok: true, degraded: true, revision: 0, etag: null, plugins: [] })
    f.deliver({ revision: 13 })
    await flushPromises()
    expect(f.client.fetchRegistry).toHaveBeenLastCalledWith({ etag: null })
    expect(f.shell.registry.degraded).toBe(true)
    f.deliver({ revision: 12 })
    await flushPromises()
    expect(f.client.fetchRegistry).toHaveBeenLastCalledWith({ etag: null })
    expect(f.shell.registry.degraded).toBe(false)
  })

  it('queues every reconnect unconditionally even if a prior read is unresolved', async () => {
    const f = await fixture()
    await f.session.start()
    await flushPromises()
    const read = deferred()
    f.client.fetchRegistry.mockReturnValueOnce(read.promise)
    f.deliver({ revision: 13 })
    await flushPromises()
    f.connection.state.epoch++
    f.connection.state.epoch++
    read.resolve({ ok: true, revision: 13, etag: '"13"', plugins: [] })
    await flushPromises()
    expect(f.client.fetchRegistry.mock.calls.slice(-2).map(([opts]) => opts)).toEqual([{ etag: null }, { etag: null }])
  })

  it('does not connect after disposal during the first-paint wait', async () => {
    const paint = deferred()
    const connect = vi.fn()
    const f = await fixture({ connect, afterPaint: () => paint.promise })
    const started = f.session.start()
    await f.session.stop()
    paint.resolve()
    await started
    expect(connect).not.toHaveBeenCalled()
  })
})
