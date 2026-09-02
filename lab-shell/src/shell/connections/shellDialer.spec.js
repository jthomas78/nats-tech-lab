import { describe, expect, it, vi } from 'vitest'
import { createShellDialer, SHELL_CONNECT_INFO } from './shellDialer.js'

describe('BR-AS27 — shell-only mint and long-lived connection', () => {
  it('mints afresh for each socket, names it, and never exhausts the reconnect budget', async () => {
    const fetch = vi.fn(async () => ({ ok: true, json: async () => ({ jwt: 'token', nkeySeed: 'seed', wsUrl: '/nats' }) }))
    const dial = vi.fn(async () => ({}))
    const authenticate = vi.fn(() => 'authenticator')
    const resolveWsUrl = vi.fn(() => 'wss://example.test/nats')
    const connect = createShellDialer({ fetch, dial, authenticate, resolveWsUrl, location: { host: 'example.test' } })
    expect(fetch).not.toHaveBeenCalled()
    await connect()
    await connect()
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(fetch).toHaveBeenCalledWith(SHELL_CONNECT_INFO, { cache: 'no-store' })
    expect(SHELL_CONNECT_INFO).toBe('/api/auth/shellConnectInfo')
    expect(dial).toHaveBeenCalledWith({ servers: 'wss://example.test/nats', authenticator: 'authenticator', name: 'lab-shell', maxReconnectAttempts: -1 })
  })

  it('does not dial if minting is refused', async () => {
    const dial = vi.fn()
    const connect = createShellDialer({ fetch: async () => ({ ok: false }), dial })
    await expect(connect()).rejects.toThrow('credential-unavailable')
    expect(dial).not.toHaveBeenCalled()
  })
})
