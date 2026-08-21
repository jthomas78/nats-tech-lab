// Specs for connectionFactory's request() error decoding (Phase 38d-i).
//
// These exist because of a specific near-miss. Every service in this repo
// replies to a failed api.* call with browserrpc's ErrorResponse envelope,
// which carries `notFound` and `conflict` discriminators alongside the message
// — and request() used to throw `new Error(body.error)`, dropping both. A 409
// was therefore indistinguishable from a 500 in the browser, so BR-TP39's
// conflict banner could only have been driven by matching on the message
// *prose*, one backend rewording away from silently degrading into a generic
// error toast.
//
// The flags are the contract these specs pin down. The message must keep
// working untouched, since almost every existing caller only reads it.
import { beforeEach, describe, expect, it, vi } from 'vitest'

const encoder = new TextEncoder()

const ncRequest = vi.fn()
const wsconnect = vi.fn()

vi.mock('@nats-io/nats-core', () => ({
  wsconnect: (...args) => wsconnect(...args),
  jwtAuthenticator: () => () => ({}),
  headers: () => new Map(),
}))

const { createConnectionState } = await import('./connectionFactory.js')

// reply builds the wire message a service would send back for one api.* call.
function reply(body) {
  return { data: encoder.encode(JSON.stringify(body)) }
}

async function connectedState() {
  wsconnect.mockResolvedValue({
    request: (...args) => ncRequest(...args),
    close: vi.fn(),
    closed: () => new Promise(() => {}),
    status: () => (async function* () {})(),
  })
  const state = createConnectionState({
    fetchConnectInfo: async () => ({ wsUrl: 'ws://x', jwt: 'j', nkeySeed: 's' }),
    connectionName: 'spec',
  })
  await state.connect()
  return state
}

describe('connectionFactory request() error envelope', () => {
  beforeEach(() => {
    ncRequest.mockReset()
    wsconnect.mockReset()
  })

  it('returns the decoded body on success', async () => {
    const state = await connectedState()
    ncRequest.mockResolvedValue(reply({ id: 'tp-1', version: 3 }))
    await expect(state.request('api.acme.organizations.organization.list.v1')).resolves.toEqual({
      id: 'tp-1',
      version: 3,
    })
  })

  // BR-TP39 depends on this: the banner is shown for a conflict and nothing
  // else, so the flag has to survive the throw.
  it('marks a conflict reply so a 409 is distinguishable from a 500', async () => {
    const state = await connectedState()
    ncRequest.mockResolvedValue(
      reply({ error: 'organization has been modified by someone else', conflict: true }),
    )
    await expect(state.request('api.acme.organizations.organization.update.v1')).rejects.toMatchObject({
      message: 'organization has been modified by someone else',
      conflict: true,
      notFound: false,
    })
  })

  it('marks a not-found reply', async () => {
    const state = await connectedState()
    ncRequest.mockResolvedValue(reply({ error: 'organization not found', notFound: true }))
    await expect(state.request('api.acme.organizations.organization.profile.v1')).rejects.toMatchObject({
      message: 'organization not found',
      notFound: true,
      conflict: false,
    })
  })

  // A plain failure must not read as a conflict. If it did, the conflict
  // banner — which offers to overwrite someone else's data — would appear on
  // unrelated errors.
  it('leaves both flags false for an error carrying neither', async () => {
    const state = await connectedState()
    ncRequest.mockResolvedValue(reply({ error: 'boom' }))
    await expect(state.request('api.acme.organizations.organization.update.v1')).rejects.toMatchObject({
      message: 'boom',
      conflict: false,
      notFound: false,
    })
  })

  it('still throws a plain Error, so callers reading only .message are unaffected', async () => {
    const state = await connectedState()
    ncRequest.mockResolvedValue(reply({ error: 'boom', conflict: true }))
    await expect(state.request('api.acme.organizations.organization.update.v1')).rejects.toBeInstanceOf(Error)
  })

  it('treats an empty reply as an empty body rather than an error', async () => {
    const state = await connectedState()
    ncRequest.mockResolvedValue({ data: new Uint8Array() })
    await expect(state.request('api.acme.organizations.organization.list.v1')).resolves.toEqual({})
  })
})
