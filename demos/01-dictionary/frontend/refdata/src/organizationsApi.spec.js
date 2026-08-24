// Specs for the organizations api.* client (Phase 26h; migrated from
// frontend/admin's own copy in Phase 36.2) — the one part of api.js that
// talks over the tenant connection rather than the PLATFORM one every other
// call in this file uses.
//
// The subject-building guard is the reason these exist. A context value
// containing a dot would produce
// "api.acme.north.organizations.organization.list.v1", which is still a *valid*
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
  listOrganizations,
  registerOrganization,
  activateOrganization,
  suspendOrganization,
  reactivateOrganization,
  getOrganizationAudit,
  listComplianceDocuments,
  listGitCertificates,
  addComplianceDocument,
  approveComplianceDocument,
  getTransporterProfile,
  updateOrganization,
  rejectComplianceDocument,
  updateGitCertificate,
  setGitCertificateExpiry,
  listFleetAssets,
  addFleetAsset,
  listOperatingAreas,
  addOperatingArea,
  removeOperatingArea,
  listTrackingCredentials,
  configureTrackingCredential,
} = await import('./api.js')

const apiModule = await import('./api.js')

describe('organizations api.* client', () => {
  beforeEach(() => {
    request.mockReset()
    request.mockResolvedValue({})
  })

  const subjectOf = () => request.mock.calls[0][0]
  const payloadOf = () => request.mock.calls[0][1]

  describe('subject construction', () => {
    it('builds a 6-token api.* subject with context in position 1', () => {
      listOrganizations('acme-default')

      expect(subjectOf()).toBe('api.acme-default.organizations.organization.list.v1')
      expect(subjectOf().split('.')).toHaveLength(6)
    })

    it('never emits an rpc.* subject', () => {
      // "a browser credential is never granted rpc.>" — an rpc.* subject here
      // would be denied by the JWT, not merely unconventional.
      for (const call of [
        () => listOrganizations('acme'),
        () => registerOrganization('acme', { name: 'X', type: 'SHIPPER' }),
        () => activateOrganization('acme', 'tp-1'),
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
      expect(() => listOrganizations(ctx)).toThrow(/invalid context/)
      expect(request).not.toHaveBeenCalled()
    })
  })

  describe('payloads', () => {
    it('sends the partner id in the body, not the subject', () => {
      // Contrast with REST, where id was a path segment. Keeping ids out of the
      // subject is what holds every subject at fixed arity.
      activateOrganization('acme', 'tp-42')

      expect(subjectOf()).toBe('api.acme.organizations.organization.activate.v1')
      expect(payloadOf()).toEqual({ id: 'tp-42' })
    })

    it('sends suspend reason alongside the id (BR-TP04)', () => {
      suspendOrganization('acme', 'tp-1', 'docs expired')
      expect(payloadOf()).toEqual({ id: 'tp-1', reason: 'docs expired' })
    })

    it('does not send a context field — the subject is the only source', () => {
      registerOrganization('acme', { name: 'Initech', type: 'SHIPPER' })
      expect(payloadOf()).not.toHaveProperty('context')
    })

    it('omits tenant from addFleetAsset but keeps the partner id', () => {
      // The signature keeps `tenant` so OrganizationsPanel.vue's call site is
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

      expect(subjectOf()).toBe('api.acme.organizations.fleet-asset.add.v1')
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
      updateOrganization('c', 'tp-9', 7, { name: 'Renamed', companyName: 'Renamed Ltd' })

      const payload = payloadOf()
      expect(payload).toHaveProperty('version', 7)
      expect(payload).toHaveProperty('name', 'Renamed')
      expect(payload).toHaveProperty('companyName', 'Renamed Ltd')
    })

    // BR-TP31. This spec exists because its absence let a real break ship:
    // Phase 38c-i changed the backend to address a document by its minted id,
    // and these calls kept sending `type`. Every payload assertion
    // still passed (they only checked the partner id), so approve/reject
    // reached the service with an empty documentId and failed at runtime with
    // "compliance document not found".
    it('addresses a document by documentId, never by type (BR-TP31)', () => {
      const transitions = [
        ['approve', approveComplianceDocument],
        ['reject', rejectComplianceDocument],
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

    it('sends approval-time insurance fields for a GIT review (BR-TP66)', () => {
      approveComplianceDocument('c', 'tp-9', 'doc-abc', {
        insurerName: 'Acme Insurance',
        insuranceContactName: 'Jane Reviewer',
        insuranceContactNumber: '+27 11 555 0100',
      })

      expect(payloadOf()).toEqual({
        id: 'tp-9', documentId: 'doc-abc', insurerName: 'Acme Insurance',
        insuranceContactName: 'Jane Reviewer', insuranceContactNumber: '+27 11 555 0100',
      })
    })

    it('keeps GIT details and expiry on their distinct commands', () => {
      updateGitCertificate('c', 'tp-9', 'doc-abc', {
        goodsTypes: ['GENERAL_FREIGHT'], coverageCents: 12300,
      })
      expect(subjectOf()).toBe('api.c.organizations.document.git-update.v1')
      expect(payloadOf()).toMatchObject({ id: 'tp-9', documentId: 'doc-abc', goodsTypes: ['GENERAL_FREIGHT'] })

      request.mockClear()
      setGitCertificateExpiry('c', 'tp-9', 'doc-abc', 1800000000)
      expect(subjectOf()).toBe('api.c.organizations.document.set-expiry.v1')
      expect(payloadOf()).toEqual({ id: 'tp-9', documentId: 'doc-abc', expiresAt: 1800000000 })
    })

    it('sends the partner id for every per-partner operation', () => {
      // Blanket guard: every operation that names a partner must carry its id
      // in the body now that there's no URL path to hold it. Anything missing
      // it reaches Postgres as an empty uuid.
      const perPartner = [
        ['activate', () => activateOrganization('c', 'tp-9')],
        ['suspend', () => suspendOrganization('c', 'tp-9', 'why')],
        ['reactivate', () => reactivateOrganization('c', 'tp-9')],
        ['audit', () => getOrganizationAudit('c', 'tp-9')],
        ['partner-profile', () => getTransporterProfile('c', 'tp-9')],
        ['partner-update', () => updateOrganization('c', 'tp-9', 1, { name: 'N' })],
        ['document-list', () => listComplianceDocuments('c', 'tp-9')],
        ['document-git-list', () => listGitCertificates('c', 'tp-9')],
        ['document-add', () => addComplianceDocument('c', 'tp-9', { type: 'CIPC', documentName: 'cipc.pdf' })],
        ['document-approve', () => approveComplianceDocument('c', 'tp-9', 'doc-1')],
        ['document-reject', () => rejectComplianceDocument('c', 'tp-9', 'doc-1')],
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
        [() => listOrganizations('c'), 'api.c.organizations.organization.list.v1'],
        [() => registerOrganization('c', {}), 'api.c.organizations.organization.register.v1'],
        [() => activateOrganization('c', 'i'), 'api.c.organizations.organization.activate.v1'],
        [() => suspendOrganization('c', 'i', 'r'), 'api.c.organizations.organization.suspend.v1'],
        [() => reactivateOrganization('c', 'i'), 'api.c.organizations.organization.reactivate.v1'],
        [() => getOrganizationAudit('c', 'i'), 'api.c.organizations.organization.audit.v1'],
        [() => listComplianceDocuments('c', 'i'), 'api.c.organizations.document.list.v1'],
        [() => listGitCertificates('c', 'i'), 'api.c.organizations.document.git-list.v1'],
        [() => addComplianceDocument('c', 'i', {}), 'api.c.organizations.document.add.v1'],
        [() => getTransporterProfile('c', 'i'), 'api.c.organizations.organization.profile.v1'],
        [() => updateOrganization('c', 'i', 1, {}), 'api.c.organizations.organization.update.v1'],
        [() => approveComplianceDocument('c', 'i', 'd'), 'api.c.organizations.document.approve.v1'],
        [() => rejectComplianceDocument('c', 'i', 'd'), 'api.c.organizations.document.reject.v1'],
        [() => updateGitCertificate('c', 'i', 'd', {}), 'api.c.organizations.document.git-update.v1'],
        [() => setGitCertificateExpiry('c', 'i', 'd', null), 'api.c.organizations.document.set-expiry.v1'],
        [() => listFleetAssets('c', 'i'), 'api.c.organizations.fleet-asset.list.v1'],
        [() => addFleetAsset('c', 'i', 't', {}), 'api.c.organizations.fleet-asset.add.v1'],
      ]

      const seen = new Set()
      for (const [call, expected] of calls) {
        request.mockReset()
        request.mockResolvedValue({})
        call()
        expect(subjectOf()).toBe(expected)
        seen.add(expected)
      }
      // Every organizations operation wrapped by this client must keep a
      // distinct subject; adding a feature without increasing this count
      // catches accidental aliasing onto an older endpoint.
      expect(seen.size).toBe(17)
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

    expect(subjectOf()).toBe('api.acme.organizations.operating-area.add.v1')
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

    expect(subjectOf()).toBe('api.acme.organizations.tracking-credential.configure.v1')
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

    expect(subjectOf()).toBe('api.acme.organizations.tracking-credential.list.v1')
    expect(payloadOf()).toEqual({ id: 'tp-1' })
  })
})
