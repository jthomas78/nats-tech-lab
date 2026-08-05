// Mirrors seafreight-app/src/nats/useNatsConnection.spec.js's BR-033
// not-connected-error coverage for the tenant connection's subscribe() guard
// (Phase 23) — no NATS server, no mocking of @nats-io/nats-core needed since
// `nc` stays null throughout.
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useNatsConnection } from './useNatsConnection.js'

describe('useNatsConnection (tenant, Phase 23)', () => {
  beforeEach(() => {
    useNatsConnection().lastError.value = ''
  })

  it('subscribe() throws "not connected" when there is no live connection', () => {
    expect(() => useNatsConnection().subscribe('notify.acme.shipping.ship.changed', () => {})).toThrow(
      'not connected',
    )
  })

  it('subscribe() surfaces lastError instead of the bare transport symptom once one is known', () => {
    useNatsConnection().lastError.value = 'tenant is not active'
    expect(() => useNatsConnection().subscribe('notify.acme.shipping.ship.changed', () => {})).toThrow(
      'tenant is not active',
    )
  })

  it('switchTenant updates the tenant ref before connecting', async () => {
    // Stub fetch so the connect attempt this triggers fails fast and
    // synchronously-observable, rather than leaving a real network call
    // (and its eventual rejection) outliving this test.
    const fetchSpy = vi.spyOn(global, 'fetch').mockRejectedValue(new Error('no server in this test'))

    await useNatsConnection()
      .switchTenant('globex')
      .catch(() => {})
    expect(useNatsConnection().tenant.value).toBe('globex')
    fetchSpy.mockRestore()
  })
})
