import { beforeEach, describe, expect, it, vi } from 'vitest'
const { request } = vi.hoisted(() => ({ request: vi.fn() }))
vi.mock('./nats/usePlatformConnection.js', () => ({ usePlatformConnection: () => ({ request }) }))
import { getRegistryEntries, upsertRegistryEntry, setRegistryEntryEnabled, getRegistryAudit, getRegistryPublishers, upsertPublisher, addPublisherKey, setPublisherKeyState, transferPlugin } from './api.js'

beforeEach(() => { request.mockReset().mockResolvedValue({ revision: 1, plugins: [] }) })
describe('BR-AS31 — registry curation uses the existing PLATFORM connection', () => {
  it('names exactly the four operator subjects and carries revisions in the payload', async () => {
    await getRegistryEntries()
    await upsertRegistryEntry({ id: 'fleet' }, 0)
    await setRegistryEntryEnabled('fleet', false, 1)
    await getRegistryAudit(200)
    expect(request.mock.calls).toEqual([
      ['api._platform.registry.entries.curated.v1', {}],
      ['api._platform.registry.entries.upsert.v1', { ifRevision: 0, entryId: 'fleet', entry: { id: 'fleet' } }],
      ['api._platform.registry.entries.set-enabled.v1', { ifRevision: 1, entryId: 'fleet', enabled: false }],
      ['api._platform.registry.audit.list.v1', { limit: 200 }],
    ])
  })

  // Phase 7b — four trust-table ops over one write subject. They are one
  // capability over one curated table, all revision-checked identically, so
  // the op rides in the payload rather than splitting the grant four ways.
  it('sends every publisher op on the one write subject, each carrying its revision', async () => {
    await getRegistryPublishers()
    await upsertPublisher({ id: 'platform-team', name: 'Platform Team' }, 7)
    await addPublisherKey('platform-team', 'UAKEY', 8)
    await setPublisherKeyState('platform-team', 'UAKEY', 'revoked', 9)
    await transferPlugin('partner-co', 'fleet', 10)
    expect(request.mock.calls).toEqual([
      ['api._platform.registry.publishers.list.v1', {}],
      ['api._platform.registry.publishers.write.v1', { op: 'publisher-upsert', publisherId: 'platform-team', publisher: { id: 'platform-team', name: 'Platform Team' }, ifRevision: 7 }],
      ['api._platform.registry.publishers.write.v1', { op: 'publisher-add-key', publisherId: 'platform-team', publicKey: 'UAKEY', ifRevision: 8 }],
      ['api._platform.registry.publishers.write.v1', { op: 'publisher-set-key-state', publisherId: 'platform-team', publicKey: 'UAKEY', keyState: 'revoked', ifRevision: 9 }],
      ['api._platform.registry.publishers.write.v1', { op: 'publisher-transfer', publisherId: 'partner-co', pluginId: 'fleet', ifRevision: 10 }],
    ])
  })

  it('preserves structured stale refusals', async () => {
    const error = Object.assign(new Error('moved'), { conflict: true, body: { error: 'moved', currentRevision: 9, yourRevision: 4 } })
    request.mockRejectedValue(error)
    await expect(setRegistryEntryEnabled('fleet', false, 4)).rejects.toBe(error)
  })

  it('does not surface socket addresses in the registry panel', async () => {
    request.mockRejectedValue(new Error('127.0.0.1:4222 no responders'))
    await expect(getRegistryEntries()).rejects.toThrow('The registry could not be reached')
  })
})
