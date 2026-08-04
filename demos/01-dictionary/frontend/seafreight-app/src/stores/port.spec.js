import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { usePortStore } from './port.js'

// Regression coverage for the tenant/context-switch flicker: connect()
// resets ships/containers to {} synchronously, before its bootstrap fetches
// (listShips/listContainers/getPorts/knownContainers) resolve. The `loading`
// flag is what lets a panel distinguish "still loading" from "genuinely
// empty" during that window — see App.spec.js's FleetPanel-facing test for
// the render-level assertion this backs.

vi.mock('../api', () => ({
  getPorts: vi.fn(),
  getBusinessUnits: vi.fn(),
  knownContainers: vi.fn(),
  listContainers: vi.fn(),
  listShips: vi.fn(),
  notifySubject: (context, entity) => `notify.${context}.shipping.${entity}.changed`,
  registerPort: vi.fn(),
}))

vi.mock('../nats/useNatsConnection', () => ({
  useNatsConnection: () => ({ subscribe: vi.fn(() => () => {}) }),
}))

import { getPorts, knownContainers, listContainers, listShips } from '../api'

describe('usePortStore.connect() loading state (Phase 16g)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('sets loading true and clears the previous context\'s ships/containers synchronously, then clears loading once the bootstrap reads land', async () => {
    let resolveShips
    listShips.mockReturnValue(new Promise((resolve) => { resolveShips = resolve }))
    listContainers.mockResolvedValue([])
    getPorts.mockResolvedValue([])
    knownContainers.mockResolvedValue([])

    const store = usePortStore()
    store.ships = { 'old-tenant-ship': { shipID: 'old-tenant-ship' } }

    await store.connect()

    // connect() only fires the bootstrap reads without awaiting them (same
    // as before this fix), so by the time it resolves the previous
    // context's ships are already gone and loading is already up.
    expect(store.loading).toBe(true)
    expect(store.ships).toEqual({})

    resolveShips([{ shipID: 'new-tenant-ship' }])
    await vi.waitFor(() => expect(store.loading).toBe(false))

    expect(store.ships).toEqual({ 'new-tenant-ship': { shipID: 'new-tenant-ship' } })
  })

  it('clears loading even when a bootstrap read fails', async () => {
    listShips.mockRejectedValue(new Error('boom'))
    listContainers.mockResolvedValue([])
    getPorts.mockResolvedValue([])
    knownContainers.mockResolvedValue([])

    const store = usePortStore()
    await store.connect()

    expect(store.loading).toBe(true)
    await vi.waitFor(() => expect(store.loading).toBe(false))
  })
})
