import PrimeVue from 'primevue/config'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
const shared = vi.hoisted(() => ({ connection: null }))
vi.mock('../nats/usePlatformConnection.js', () => ({ usePlatformConnection: () => shared.connection }))
vi.mock('../api', () => ({ getRegistryEntries: vi.fn(), upsertRegistryEntry: vi.fn(), setRegistryEntryEnabled: vi.fn(), getRegistryAudit: vi.fn() }))
import { getRegistryEntries, setRegistryEntryEnabled, getRegistryAudit } from '../api'
import FrontendPluginsPanel from './FrontendPluginsPanel.vue'
import RegistryAuditPanel from './RegistryAuditPanel.vue'

enableAutoUnmount(afterEach)
const doc = { revision: 4, allowedOrigins: [], plugins: [{ id: 'fleet', name: 'Fleet', enabled: true, conforming: true, contributions: [] }] }
beforeEach(() => {
  vi.clearAllMocks()
  shared.connection = { epoch: ref(0) }
  getRegistryEntries.mockResolvedValue(doc)
  getRegistryAudit.mockResolvedValue([])
})
describe('BR-AS31 — operator panels on NATS', () => {
  it('retries a read when the initial PLATFORM connection finally arrives', async () => {
    getRegistryEntries.mockRejectedValueOnce(new Error('not connected'))
    const w = mount(FrontendPluginsPanel, { global: { plugins: [PrimeVue] } })
    await flushPromises()
    shared.connection.epoch.value++
    await flushPromises()
    expect(w.get('[data-testid="registry-revision"]').text()).toBe('4')
    expect(getRegistryEntries).toHaveBeenCalledTimes(2)
  })
  it('shows a stale refusal from flags, without an HTTP status', async () => {
    setRegistryEntryEnabled.mockRejectedValue(Object.assign(new Error('moved'), { conflict: true, body: { currentRevision: 9, yourRevision: 4 } }))
    const w = mount(FrontendPluginsPanel, { global: { plugins: [PrimeVue] } })
    await flushPromises()
    await w.get('[data-testid="toggle-enabled"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="stale-revision"]').text()).toContain('9')
    expect(w.get('[data-testid="stale-revision"]').text()).toContain('4')
  })
  it('refreshes the audit on a new connection epoch', async () => {
    mount(RegistryAuditPanel, { global: { plugins: [PrimeVue] } })
    await flushPromises()
    shared.connection.epoch.value++
    await flushPromises()
    expect(getRegistryAudit).toHaveBeenCalledTimes(2)
  })
})
