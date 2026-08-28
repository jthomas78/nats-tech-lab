import { describe, expect, it, vi } from 'vitest'

import {
  connectionKey,
  CREDENTIAL_PROFILE,
  createConnectionRegistry,
} from './connectionRegistry.js'

const registry = () => {
  const connect = vi.fn(async ({ key }) => ({ key, close: vi.fn() }))
  return { registry: createConnectionRegistry({ connect }), connect }
}

describe('BR-AS10 — the four credential profiles never merge', () => {
  it('gives each profile its own connection', async () => {
    const { registry: r } = registry()

    const connections = await Promise.all([
      r.acquire(CREDENTIAL_PROFILE.ADMIN_PLATFORM),
      r.acquire(CREDENTIAL_PROFILE.OPERATOR_REFDATA_PLATFORM),
      r.acquire(CREDENTIAL_PROFILE.OPERATOR_TENANT, 'acme'),
      r.acquire(CREDENTIAL_PROFILE.SEAFREIGHT_TENANT, 'acme'),
    ])

    expect(new Set(connections.map((c) => c.key)).size).toBe(4)
  })

  it('does not merge the two PLATFORM profiles, though they share an account', () => {
    // Same account, different grants. Sharing the connection would hand the
    // refdata-admin token the Admin UI's cross-account reach.
    expect(connectionKey(CREDENTIAL_PROFILE.ADMIN_PLATFORM)).not.toBe(
      connectionKey(CREDENTIAL_PROFILE.OPERATOR_REFDATA_PLATFORM),
    )
  })

  it('keys a tenant-scoped profile by its tenant', async () => {
    const { registry: r, connect } = registry()

    const acme = await r.acquire(CREDENTIAL_PROFILE.SEAFREIGHT_TENANT, 'acme')
    const globex = await r.acquire(CREDENTIAL_PROFILE.SEAFREIGHT_TENANT, 'globex')

    expect(acme.key).not.toBe(globex.key)
    expect(connect).toHaveBeenCalledTimes(2)
  })

  it('shares one connection between callers naming the same profile and tenant', async () => {
    const { registry: r, connect } = registry()

    const [a, b] = await Promise.all([
      r.acquire(CREDENTIAL_PROFILE.OPERATOR_TENANT, 'acme'),
      r.acquire(CREDENTIAL_PROFILE.OPERATOR_TENANT, 'acme'),
    ])

    expect(a).toBe(b)
    expect(connect).toHaveBeenCalledTimes(1)
  })

  it('has no current connection to switch, so a tenant switch cannot leak', async () => {
    // The useRefdataLabels.js shape: a module-global `transport` that the last
    // connect overwrites. A registry with no default cannot express it — the
    // first tenant's connection is still addressable after the second exists.
    const { registry: r } = registry()

    const acme = await r.acquire(CREDENTIAL_PROFILE.OPERATOR_TENANT, 'acme')
    await r.acquire(CREDENTIAL_PROFILE.OPERATOR_TENANT, 'globex')

    expect(await r.acquire(CREDENTIAL_PROFILE.OPERATOR_TENANT, 'acme')).toBe(acme)
    expect(r.keys).toEqual(['operator-tenant#acme', 'operator-tenant#globex'])
  })

  it('refuses a tenant-scoped profile with no tenant', () => {
    expect(() => connectionKey(CREDENTIAL_PROFILE.SEAFREIGHT_TENANT)).toThrow(/needs a tenant/)
  })

  it('refuses a tenant on a platform profile', () => {
    // Otherwise a caller could believe it is tenant-scoped while holding
    // platform reach — the merge this rule exists to prevent, spelled as a
    // plausible typo.
    expect(() => connectionKey(CREDENTIAL_PROFILE.ADMIN_PLATFORM, 'acme')).toThrow(
      /not tenant-scoped/,
    )
  })

  it('refuses a profile it does not know', () => {
    expect(() => connectionKey('everything')).toThrow(/Unknown credential profile/)
  })
})

describe('connection lifecycle', () => {
  it('closes one profile without touching the others', async () => {
    const { registry: r } = registry()
    const acme = await r.acquire(CREDENTIAL_PROFILE.SEAFREIGHT_TENANT, 'acme')
    await r.acquire(CREDENTIAL_PROFILE.ADMIN_PLATFORM)

    await r.close(CREDENTIAL_PROFILE.SEAFREIGHT_TENANT, 'acme')

    expect(acme.close).toHaveBeenCalledTimes(1)
    expect(r.has(CREDENTIAL_PROFILE.ADMIN_PLATFORM)).toBe(true)
  })

  it('closes everything on teardown', async () => {
    const { registry: r } = registry()
    const a = await r.acquire(CREDENTIAL_PROFILE.ADMIN_PLATFORM)
    const b = await r.acquire(CREDENTIAL_PROFILE.OPERATOR_TENANT, 'acme')

    await r.closeAll()

    expect(a.close).toHaveBeenCalled()
    expect(b.close).toHaveBeenCalled()
    expect(r.keys).toEqual([])
  })

  it('does not cache a failed connect', async () => {
    let attempt = 0
    const r = createConnectionRegistry({
      connect: async () => {
        attempt += 1
        if (attempt === 1) throw new Error('no route to host')
        return { close: vi.fn() }
      },
    })

    await expect(r.acquire(CREDENTIAL_PROFILE.ADMIN_PLATFORM)).rejects.toThrow('no route to host')
    await expect(r.acquire(CREDENTIAL_PROFILE.ADMIN_PLATFORM)).resolves.toBeTruthy()
  })
})
