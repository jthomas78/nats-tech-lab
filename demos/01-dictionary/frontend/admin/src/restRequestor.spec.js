// BR-AC35: every REST call this app makes declares the tab's own
// Nats-Requestor identity, the same value its api.* calls send — without it
// accounts-service's HTTP span has no caller to name and the Traces panel
// reads "no Nats-Requestor on this span" for every REST hop.
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./nats/usePlatformConnection.js', () => ({
  usePlatformConnection: () => ({ request: vi.fn() }),
}))

const { REQUESTOR_HEADER, REST_REQUESTOR_ID, TAB_ID, requestorID } = await import('./requestorId.js')
const { listAccounts, createAccount } = await import('./api.js')

describe('REST caller identity (BR-AC35)', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
    })
  })

  const headersOf = () => globalThis.fetch.mock.calls[0][1].headers

  it('sends the tab identity on a GET', async () => {
    await listAccounts()
    expect(headersOf()[REQUESTOR_HEADER]).toBe(REST_REQUESTOR_ID)
  })

  it('sends it on a POST too, alongside the Content-Type the body needs', async () => {
    await createAccount({ name: 'acme' })
    expect(headersOf()[REQUESTOR_HEADER]).toBe(REST_REQUESTOR_ID)
    expect(headersOf()['Content-Type']).toBe('application/json')
  })

  it('is instance-qualified as "<name>/<tab id>", the format BR-027 defines', () => {
    expect(REST_REQUESTOR_ID).toBe(`admin-app/${TAB_ID}`)
    expect(TAB_ID).toMatch(/^[0-9a-f]{16}$/)
  })

  it('shares one tab id with the PLATFORM connection, so both transports read as one actor', () => {
    expect(requestorID('admin-app')).toBe(`admin-app/${TAB_ID}`)
  })
})
