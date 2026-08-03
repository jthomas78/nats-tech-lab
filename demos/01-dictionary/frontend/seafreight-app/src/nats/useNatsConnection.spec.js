// BR-033 (BUSINESS_RULES-SHIPPING.md): when there is no live NATS connection,
// request()/subscribe() surface the *reason* the connection is gone rather
// than the bare transport symptom. The case that matters is a tenant
// suspended mid-session (ARCHITECTURE-ACCOUNTS.md § 2t-a): NATS evicts the
// connection, the auto-reconnect is refused by the connectInfo endpoint with
// "tenant is not active", and every subsequent command used to toast
// "Depart failed — not connected", which names the symptom and not the
// cause.
//
// These specs never open a connection, so `nc` stays null throughout and the
// guard under test is the only code path exercised — no NATS server, no
// mocking of @nats-io/nats-core needed.
import { beforeEach, describe, expect, it } from 'vitest'

import { request, subscribe, useNatsConnection } from './useNatsConnection.js'

describe('BR-033 not-connected errors carry the reason, not just the symptom', () => {
  beforeEach(() => {
    // Module-level singleton, shared across specs in this file.
    useNatsConnection().lastError.value = ''
  })

  it('falls back to a bare "not connected" when nothing is known about why', async () => {
    await expect(request('api.acme.shipping.ship.list.v1', {})).rejects.toThrow('not connected')
  })

  it('surfaces the connectInfo endpoint\'s refusal instead, once a suspended tenant has set lastError', async () => {
    useNatsConnection().lastError.value = 'tenant is not active'

    await expect(request('api.acme.shipping.ship.depart.v1', {})).rejects.toThrow('tenant is not active')
    // The toast built from this must not mention the transport symptom at all.
    await expect(request('api.acme.shipping.ship.depart.v1', {})).rejects.not.toThrow('not connected')
  })

  it('applies to subscribe() too, not only request()', () => {
    useNatsConnection().lastError.value = 'tenant is not active'
    expect(() => subscribe('notify.acme.shipping.ship.changed', () => {})).toThrow('tenant is not active')
  })
})
