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
  getTransporterProfile,
  updateTradingPartner,
  rejectComplianceDocument,
  resubmitComplianceDocument,
  listFleetAssets,
  addFleetAsset,
  listOperatingAreas,
  addOperatingArea,
  removeOperatingArea,
  listTrackingCredentials,
  configureTrackingCredential,
} = await import('./api.js')

const apiModule = await import('./api.js')

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

    // BR-TP34. A save that omits the version cannot be rejected as stale, so
    // the guard silently degrades to last-write-wins — the exact failure the
    // rule exists to prevent, and one that looks like success from the client.
    it('sends the version the caller read on every update (BR-TP34)', () => {
      request.mockReset()
      request.mockResolvedValue({})
      updateTradingPartner('c', 'tp-9', 7, { name: 'Renamed', companyName: 'Renamed Ltd' })

      const payload = payloadOf()
      expect(payload).toHaveProperty('version', 7)
      expect(payload).toHaveProperty('name', 'Renamed')
      expect(payload).toHaveProperty('companyName', 'Renamed Ltd')
    })

    // BR-TP31. This spec exists because its absence let a real break ship:
    // Phase 38c-i changed the backend to address a document by its minted id,
    // and these three calls kept sending `type`. Every payload assertion
    // still passed (they only checked the partner id), so approve/reject/
    // resubmit reached the service with an empty documentId and failed at
    // runtime with "compliance document not found".
    it('addresses a document by documentId, never by type (BR-TP31)', () => {
      const transitions = [
        ['approve', approveComplianceDocument],
        ['reject', rejectComplianceDocument],
        ['resubmit', resubmitComplianceDocument],
      ]

      for (const [label, call] of transitions) {
        request.mockReset()
        request.mockResolvedValue({})
        call('c', 'tp-9', 'doc-abc')

        const payload = payloadOf()
        expect(payload, `${label} must carry documentId`).toHaveProperty('documentId', 'doc-abc')
        expect(payload, `${label} must not address the document by type`).not.toHaveProperty('type')
      }
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
        ['partner-profile', () => getTransporterProfile('c', 'tp-9')],
        ['partner-update', () => updateTradingPartner('c', 'tp-9', 1, { name: 'N' })],
        ['document-list', () => listComplianceDocuments('c', 'tp-9')],
        ['document-add', () => addComplianceDocument('c', 'tp-9', { type: 'CIPC', reference: 'r' })],
        ['document-approve', () => approveComplianceDocument('c', 'tp-9', 'doc-1')],
        ['document-reject', () => rejectComplianceDocument('c', 'tp-9', 'doc-1')],
        ['document-resubmit', () => resubmitComplianceDocument('c', 'tp-9', 'doc-1')],
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
        [() => getTransporterProfile('c', 'i'), 'api.c.trading-partner.partner.profile.v1'],
        [() => updateTradingPartner('c', 'i', 1, {}), 'api.c.trading-partner.partner.update.v1'],
        [() => approveComplianceDocument('c', 'i', 'd'), 'api.c.trading-partner.document.approve.v1'],
        [() => rejectComplianceDocument('c', 'i', 'd'), 'api.c.trading-partner.document.reject.v1'],
        [() => resubmitComplianceDocument('c', 'i', 'd'), 'api.c.trading-partner.document.resubmit.v1'],
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
      // 15 of the service's 16 endpoints; partner.get.v1 has no api.js caller
      // (the panel lists rather than fetches one at a time).
      expect(seen.size).toBe(15)
    })
  })
})

// Phase 38d-ii.
describe('operating areas (BR-TP46-BR-TP50)', () => {
  beforeEach(() => {
    request.mockReset()
    request.mockResolvedValue({})
  })

  const subjectOf = () => request.mock.calls[0][0]
  const payloadOf = () => request.mock.calls[0][1]

  it('sends level and code on add', () => {
    addOperatingArea('acme', 'tp-1', 'REGION', 'ZA-GP')

    expect(subjectOf()).toBe('api.acme.trading-partner.operating-area.add.v1')
    expect(payloadOf()).toMatchObject({ id: 'tp-1', level: 'REGION', code: 'ZA-GP' })
  })

  it('never sends a countryCode', () => {
    // BR-TP48 resolves parentage from refdata's own `country` relation
    // (BR-D47). A browser-supplied parent would let a caller misfile a
    // region under the wrong country and defeat the overlap check, so the
    // absence of this field is a rule, not an omission.
    addOperatingArea('acme', 'tp-1', 'REGION', 'ZA-GP')

    expect(payloadOf()).not.toHaveProperty('countryCode')
    expect(payloadOf()).not.toHaveProperty('country')
  })

  it('sends the partner id on every operation', () => {
    for (const call of [
      () => listOperatingAreas('acme', 'tp-1'),
      () => addOperatingArea('acme', 'tp-1', 'COUNTRY', 'ZA'),
      () => removeOperatingArea('acme', 'tp-1', 'COUNTRY', 'ZA'),
    ]) {
      request.mockReset()
      request.mockResolvedValue({})
      call()
      expect(payloadOf()).toMatchObject({ id: 'tp-1' })
    }
  })
})

describe('tracking credentials (BR-TP51-BR-TP55)', () => {
  beforeEach(() => {
    request.mockReset()
    request.mockResolvedValue({})
  })

  const subjectOf = () => request.mock.calls[0][0]
  const payloadOf = () => request.mock.calls[0][1]

  it('sends the payload on configure', () => {
    configureTrackingCredential('acme', 'tp-1', {
      provider: 'CARTRACK',
      credentialType: 'API_KEY',
      payload: 'sk-live-secret',
    })

    expect(subjectOf()).toBe('api.acme.trading-partner.tracking-credential.configure.v1')
    expect(payloadOf()).toMatchObject({
      id: 'tp-1',
      provider: 'CARTRACK',
      credentialType: 'API_KEY',
      payload: 'sk-live-secret',
    })
  })

  it('exposes no way to read a payload back', () => {
    // BR-TP52: there is no api.* subject that returns credential material,
    // so the client must not offer a function implying one exists. If a
    // "reveal"/"get" helper is ever added here, this fails — which is the
    // point, because the UI would then be able to promise something the
    // backend cannot deliver.
    const api = Object.keys(apiModule)
    const readish = api.filter(
      (name) =>
        /TrackingCredential/.test(name) &&
        !/^(listTrackingCredentials|configureTrackingCredential)$/.test(name),
    )
    expect(readish).toEqual([])
  })

  it('lists without sending any credential material', () => {
    listTrackingCredentials('acme', 'tp-1')

    expect(subjectOf()).toBe('api.acme.trading-partner.tracking-credential.list.v1')
    expect(payloadOf()).toEqual({ id: 'tp-1' })
  })
})
