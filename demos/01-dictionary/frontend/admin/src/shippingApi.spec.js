// Specs for the shipping-service api.* client (Phase 33.8) — mirrors
// tradingPartnerApi.spec.js's subject-construction guard for the same class
// of bug: a context value containing a dot would shift every later token by
// one and make the service resolve the wrong context.
import { beforeEach, describe, expect, it, vi } from 'vitest'

const request = vi.fn()

vi.mock('./nats/useNatsConnection.js', () => ({
  useNatsConnection: () => ({ request }),
}))

const {
  arrivePort,
  departPort,
  registerContainer,
  loadContainer,
  unloadContainer,
  listContainers,
  getManifest,
  getPorts,
  registerPort,
  getKnownContainers,
} = await import('./api.js')

describe('shipping-service api.* client', () => {
  beforeEach(() => {
    request.mockReset()
    request.mockResolvedValue({})
  })

  const subjectOf = () => request.mock.calls[0][0]
  const payloadOf = () => request.mock.calls[0][1]

  it('builds ship/container/port/meta subjects with context in position 1', () => {
    arrivePort({ context: 'acme', shipID: 'S1', port: 'rotterdam' })
    expect(subjectOf()).toBe('api.acme.shipping.ship.arrive.v1')

    request.mockClear()
    departPort({ context: 'acme', shipID: 'S1', port: 'rotterdam' })
    expect(subjectOf()).toBe('api.acme.shipping.ship.depart.v1')

    request.mockClear()
    registerContainer({ context: 'acme', containerID: 'TCKU1234567' })
    expect(subjectOf()).toBe('api.acme.shipping.container.register.v1')

    request.mockClear()
    loadContainer({ context: 'acme', containerID: 'TCKU1234567', shipID: 'S1' })
    expect(subjectOf()).toBe('api.acme.shipping.container.load.v1')

    request.mockClear()
    unloadContainer({ context: 'acme', containerID: 'TCKU1234567', shipID: 'S1' })
    expect(subjectOf()).toBe('api.acme.shipping.container.unload.v1')

    request.mockClear()
    listContainers('acme')
    expect(subjectOf()).toBe('api.acme.shipping.container.list.v1')

    request.mockClear()
    getManifest('acme', 'S1')
    expect(subjectOf()).toBe('api.acme.shipping.container.manifest.v1')
    expect(payloadOf()).toEqual({ shipID: 'S1' })

    request.mockClear()
    getPorts('acme')
    expect(subjectOf()).toBe('api.acme.shipping.port.list.v1')

    request.mockClear()
    registerPort('acme', 'rotterdam')
    expect(subjectOf()).toBe('api.acme.shipping.port.register.v1')
    expect(payloadOf()).toEqual({ name: 'rotterdam' })

    request.mockClear()
    getKnownContainers('acme')
    expect(subjectOf()).toBe('api.acme.shipping.meta.known-containers.v1')
  })

  it('rejects a dotted context rather than silently shifting subject tokens', () => {
    expect(() => getPorts('acme.north')).toThrow(/invalid context/)
    expect(() => arrivePort({ context: 'acme.north', shipID: 'S1' })).toThrow(/invalid context/)
  })
})
