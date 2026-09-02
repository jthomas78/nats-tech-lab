/*
  BR-AS10 — credential profiles never merge.

  The profiles are declared by the spec rather than imported, because that is
  how the host declares them: the rule is about keying, and it has to hold for
  whatever set of credentials the platform can actually mint. The set used to
  be frozen in the module and only one member of it could be dialled, so these
  specs proved the rule against four profiles nothing could reach.
*/
import { describe, expect, it, vi } from 'vitest'

import { createConnectionRegistry, SHELL_PLATFORM } from './connectionRegistry.js'

/* A stand-in for the migration's own set: two PLATFORM profiles sharing an
   account with different grants, and two tenant-scoped ones. */
const PROFILES = {
  [SHELL_PLATFORM]: {},
  'admin-platform': {},
  'operator-refdata-platform': {},
  'operator-tenant': { tenantScoped: true },
  'seafreight-tenant': { tenantScoped: true },
}

const registry = (profiles = PROFILES) => {
  const connect = vi.fn(async ({ key }) => ({ key, close: vi.fn() }))
  return { registry: createConnectionRegistry({ profiles, connect }), connect }
}

describe('BR-AS10 — declared credential profiles never merge', () => {
  it('gives each profile its own connection', async () => {
    const { registry: r } = registry()

    const connections = await Promise.all([
      r.acquire('admin-platform'),
      r.acquire('operator-refdata-platform'),
      r.acquire('operator-tenant', 'acme'),
      r.acquire('seafreight-tenant', 'acme'),
    ])

    expect(new Set(connections.map((c) => c.key)).size).toBe(4)
  })

  it('does not merge two PLATFORM profiles, though they share an account', () => {
    // Same account, different grants. Sharing the connection would hand the
    // refdata-admin token the Admin UI's cross-account reach.
    const { registry: r } = registry()
    expect(r.keyFor('admin-platform')).not.toBe(r.keyFor('operator-refdata-platform'))
  })

  it('keeps the shell profile apart from every operator profile', () => {
    // BR-AS27: the shell's credential reads the registry and nothing else, and
    // it is not tenant-scoped.
    const { registry: r } = registry()
    expect(r.keyFor(SHELL_PLATFORM)).not.toBe(r.keyFor('admin-platform'))
    expect(r.keyFor(SHELL_PLATFORM)).not.toBe(r.keyFor('operator-refdata-platform'))
    expect(() => r.keyFor(SHELL_PLATFORM, 'acme')).toThrow(/not tenant-scoped/)
  })

  it('keys a tenant-scoped profile by its tenant', async () => {
    const { registry: r, connect } = registry()

    const acme = await r.acquire('seafreight-tenant', 'acme')
    const globex = await r.acquire('seafreight-tenant', 'globex')

    expect(acme.key).not.toBe(globex.key)
    expect(connect).toHaveBeenCalledTimes(2)
  })

  it('shares one connection between callers naming the same profile and tenant', async () => {
    const { registry: r, connect } = registry()

    const [a, b] = await Promise.all([
      r.acquire('operator-tenant', 'acme'),
      r.acquire('operator-tenant', 'acme'),
    ])

    expect(a).toBe(b)
    expect(connect).toHaveBeenCalledTimes(1)
  })

  it('has no current connection to switch, so a tenant switch cannot leak', async () => {
    // The useRefdataLabels.js shape: a module-global `transport` that the last
    // connect overwrites. A registry with no default cannot express it — the
    // first tenant's connection is still addressable after the second exists.
    const { registry: r } = registry()

    const acme = await r.acquire('operator-tenant', 'acme')
    await r.acquire('operator-tenant', 'globex')

    expect(await r.acquire('operator-tenant', 'acme')).toBe(acme)
    expect(r.keys).toEqual(['operator-tenant#acme', 'operator-tenant#globex'])
  })

  it('refuses a tenant-scoped profile with no tenant', () => {
    const { registry: r } = registry()
    expect(() => r.keyFor('seafreight-tenant')).toThrow(/needs a tenant/)
  })

  it('refuses a tenant on a platform profile', () => {
    // Otherwise a caller could believe it is tenant-scoped while holding
    // platform reach — the merge this rule exists to prevent, spelled as a
    // plausible typo.
    const { registry: r } = registry()
    expect(() => r.keyFor('admin-platform', 'acme')).toThrow(/not tenant-scoped/)
  })

  it('refuses a profile nobody declared, and connects nothing for it', async () => {
    // The only guard: the host no longer rejects an undialable profile a
    // second time, so a credential that does not exist fails in one place.
    const { registry: r, connect } = registry({ [SHELL_PLATFORM]: {} })
    expect(() => r.acquire('admin-platform')).toThrow(/Unknown credential profile/)
    expect(() => r.keyFor('everything')).toThrow(/Unknown credential profile/)
    expect(connect).not.toHaveBeenCalled()
  })
})

describe('connection lifecycle', () => {
  it('closes one profile without touching the others', async () => {
    const { registry: r } = registry()
    const acme = await r.acquire('seafreight-tenant', 'acme')
    await r.acquire('admin-platform')

    await r.close('seafreight-tenant', 'acme')

    expect(acme.close).toHaveBeenCalledTimes(1)
    expect(r.has('admin-platform')).toBe(true)
  })

  it('closes everything on teardown', async () => {
    const { registry: r } = registry()
    const a = await r.acquire('admin-platform')
    const b = await r.acquire('operator-tenant', 'acme')

    await r.closeAll()

    expect(a.close).toHaveBeenCalled()
    expect(b.close).toHaveBeenCalled()
    expect(r.keys).toEqual([])
  })

  it('does not cache a failed connect', async () => {
    let attempt = 0
    const r = createConnectionRegistry({
      profiles: { 'admin-platform': {} },
      connect: async () => {
        attempt += 1
        if (attempt === 1) throw new Error('no route to host')
        return { close: vi.fn() }
      },
    })

    await expect(r.acquire('admin-platform')).rejects.toThrow('no route to host')
    await expect(r.acquire('admin-platform')).resolves.toBeTruthy()
  })
})
