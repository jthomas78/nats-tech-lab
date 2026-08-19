// Specs for the trading-partner api.* client (Phase 26h; migrated from
// frontend/admin's own copy in Phase 36.2) — the one part of api.js that
// talks over the tenant connection rather than the PLATFORM one every other
// call in this file uses.
//
// The subject-building guard is the reason these exist. A context value
// containing a dot would produce
// "api.acme.north.trading-partner.partner.list.v1", which is still a *valid*
// subject — it just shifts every later token by one, so the service would read
// "north" as the service token and resolve the wrong context. That fails
// silently, which is exactly the class of bug worth a test.
import { beforeEach, describe, expect, it, vi } from 'vitest'

const request = vi.fn()

vi.mock('./nats/useTenantConnection.js', () => ({
  useTenantConnection: () => ({ request }),
}))
vi.mock('./nats/useRefdataAdminConnection.js', () => ({
  useRefdataAdminConnection: () => ({ request: vi.fn() }),
}))

const {
  listTradingPartners,
  registerTradingPartner,
  activateTradingPartner,
  suspendTradingPartner,
  reactivateTradingPartner,
  getTradingPartnerAudit,
  listComplianceDocuments,
  addComplianceDocument,
  approveComplianceDocument,
  rejectComplianceDocument,
  resubmitComplianceDocument,
  listFleetAssets,
  addFleetAsset,
} = await import('./api.js')

describe('trading-partner api.* client', () => {
  beforeEach(() => {
    request.mockReset()
    request.mockResolvedValue({})
  })

  const subjectOf = () => request.mock.calls[0][0]
  const payloadOf = () => request.mock.calls[0][1]

  describe('subject construction', () => {
    it('builds a 6-token api.* subject with context in position 1', () => {
      listTradingPartners('acme-default')

      expect(subjectOf()).toBe('api.acme-default.trading-partner.partner.list.v1')
      expect(subjectOf().split('.')).toHaveLength(6)
    })

    it('never emits an rpc.* subject', () => {
      // "a browser credential is never granted rpc.>" — an rpc.* subject here
      // would be denied by the JWT, not merely unconventional.
      for (const call of [
        () => listTradingPartners('acme'),
        () => registerTradingPartner('acme', { name: 'X', type: 'SHIPPER' }),
        () => activateTradingPartner('acme', 'tp-1'),
      ]) {
        request.mockReset()
        request.mockResolvedValue({})
        call()
        expect(subjectOf()).toMatch(/^api\./)
        expect(subjectOf()).not.toMatch(/^rpc\./)
      }
    })

    it.each([
      ['a dot', 'acme.north'],
      ['a space', 'acme north'],
      ['a wildcard', 'acme*'],
      ['a full wildcard', 'acme>'],
      ['empty', ''],
    ])('throws rather than emitting a malformed subject when context contains %s', (_label, ctx) => {
      expect(() => listTradingPartners(ctx)).toThrow(/invalid context/)
      expect(request).not.toHaveBeenCalled()
    })
  })

  describe('payloads', () => {
    it('sends the partner id in the body, not the subject', () => {
      // Contrast with REST, where id was a path segment. Keeping ids out of the
      // subject is what holds every subject at fixed arity.
      activateTradingPartner('acme', 'tp-42')

      expect(subjectOf()).toBe('api.acme.trading-partner.partner.activate.v1')
      expect(payloadOf()).toEqual({ id: 'tp-42' })
    })

    it('sends suspend reason alongside the id (BR-TP04)', () => {
      suspendTradingPartner('acme', 'tp-1', 'docs expired')
      expect(payloadOf()).toEqual({ id: 'tp-1', reason: 'docs expired' })
    })

    it('does not send a context field — the subject is the only source', () => {
      registerTradingPartner('acme', { name: 'Initech', type: 'SHIPPER' })
      expect(payloadOf()).not.toHaveProperty('context')
    })

    it('omits tenant from addFleetAsset but keeps the partner id', () => {
      // The signature keeps `tenant` so TradingPartnersPanel.vue's call site is
      // unchanged, but the service derives it from the NATS account. Sending it
      // would imply it were honoured.
      //
      // The `id` assertion is here because its absence was a real bug: REST
      // carried the partner id in the URL path, and the first version of this
      // migration dropped it from the body, which Postgres rejected with
      // 'invalid input syntax for type uuid: ""'. An earlier version of this
      // spec checked only what was removed, never what had to be kept — which
      // is why the "every operation sends its partner id" spec below now
      // covers all of them rather than trusting each call site.
      addFleetAsset('acme', 'tp-1', 'globex', {
        registrationNo: 'ABC123',
        vin: 'VIN1',
        make: 'Volvo',
        model: 'FH16',
        vehicleTypeCode: 'TAUTLINER',
      })

      expect(subjectOf()).toBe('api.acme.trading-partner.fleet-asset.add.v1')
      expect(payloadOf()).not.toHaveProperty('tenant')
      expect(payloadOf()).toMatchObject({
        id: 'tp-1',
        registrationNo: 'ABC123',
        vehicleTypeCode: 'TAUTLINER',
      })
    })

    it('sends the partner id for every per-partner operation', () => {
      // Blanket guard: every operation that names a partner must carry its id
      // in the body now that there's no URL path to hold it. Anything missing
      // it reaches Postgres as an empty uuid.
      const perPartner = [
        ['activate', () => activateTradingPartner('c', 'tp-9')],
        ['suspend', () => suspendTradingPartner('c', 'tp-9', 'why')],
        ['reactivate', () => reactivateTradingPartner('c', 'tp-9')],
        ['audit', () => getTradingPartnerAudit('c', 'tp-9')],
        ['document-list', () => listComplianceDocuments('c', 'tp-9')],
        ['document-add', () => addComplianceDocument('c', 'tp-9', { type: 'CIPC', reference: 'r' })],
        ['document-approve', () => approveComplianceDocument('c', 'tp-9', 'CIPC')],
        ['document-reject', () => rejectComplianceDocument('c', 'tp-9', 'CIPC')],
        ['document-resubmit', () => resubmitComplianceDocument('c', 'tp-9', 'CIPC')],
        ['fleet-asset-list', () => listFleetAssets('c', 'tp-9')],
        ['fleet-asset-add', () => addFleetAsset('c', 'tp-9', 't', { registrationNo: 'R1' })],
      ]

      for (const [label, call] of perPartner) {
        request.mockReset()
        request.mockResolvedValue({})
        call()
        expect(payloadOf(), `${label} must send the partner id`).toHaveProperty('id', 'tp-9')
      }
    })
  })

  describe('subject coverage', () => {
    it('maps every operation to its own distinct subject', () => {
      const calls = [
        [() => listTradingPartners('c'), 'api.c.trading-partner.partner.list.v1'],
        [() => registerTradingPartner('c', {}), 'api.c.trading-partner.partner.register.v1'],
        [() => activateTradingPartner('c', 'i'), 'api.c.trading-partner.partner.activate.v1'],
        [() => suspendTradingPartner('c', 'i', 'r'), 'api.c.trading-partner.partner.suspend.v1'],
        [() => reactivateTradingPartner('c', 'i'), 'api.c.trading-partner.partner.reactivate.v1'],
        [() => getTradingPartnerAudit('c', 'i'), 'api.c.trading-partner.partner.audit.v1'],
        [() => listComplianceDocuments('c', 'i'), 'api.c.trading-partner.document.list.v1'],
        [() => addComplianceDocument('c', 'i', {}), 'api.c.trading-partner.document.add.v1'],
        [() => approveComplianceDocument('c', 'i', 't'), 'api.c.trading-partner.document.approve.v1'],
        [() => rejectComplianceDocument('c', 'i', 't'), 'api.c.trading-partner.document.reject.v1'],
        [() => resubmitComplianceDocument('c', 'i', 't'), 'api.c.trading-partner.document.resubmit.v1'],
        [() => listFleetAssets('c', 'i'), 'api.c.trading-partner.fleet-asset.list.v1'],
        [() => addFleetAsset('c', 'i', 't', {}), 'api.c.trading-partner.fleet-asset.add.v1'],
      ]

      const seen = new Set()
      for (const [call, expected] of calls) {
        request.mockReset()
        request.mockResolvedValue({})
        call()
        expect(subjectOf()).toBe(expected)
        seen.add(expected)
      }
      // 13 of the service's 14 endpoints; partner.get.v1 has no api.js caller
      // (the panel lists rather than fetches one at a time).
      expect(seen.size).toBe(13)
    })
  })
})
