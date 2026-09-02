/*
  Connection state keyed by credential profile (BR-AS10).

  The apps due for migration hold several distinct NATS credentials between
  them, and they must stay distinct. The tempting simplification when they
  share a shell — one connection, broadest permissions — would dissolve a
  boundary the NATS server enforces today into one the frontend merely intends,
  and tenancy in this platform is the account boundary (CLAUDE.md).

  So there is no "current connection" here. Every consumer names the profile it
  wants, and a tenant-scoped profile also names its tenant; the key is the pair.
  That is deliberately the opposite shape from the bug this design has to avoid:
  demos/01-dictionary/frontend/shared/refdata/useRefdataLabels.js keeps a
  module-global mutable `transport`, so whichever tenant connected last is the
  tenant every caller reads — a cross-tenant leak with no error and no symptom.
  A module-global cannot be keyed, so the fix is structural: state lives in a
  registry that has no default.

  **Which profiles exist is the caller's to say, not this file's.** It used to
  name five as a frozen set while exactly one could be dialled, which meant the
  rule above was only ever specced against four profiles nothing could reach,
  and the host had to reject them again at the point of connecting. A profile
  arrives when the credential that backs it does. The migration map — the
  operator and SeaFreight profiles and which of them are tenant-scoped — is
  documented in ARCHITECTURE-APP-SHELL.md, which is where a plan belongs.
*/

/* The shell's own PLATFORM credential: registry read and notify only
   (BR-AS27). The one profile that can be dialled today, so the one id the
   shell names. */
export const SHELL_PLATFORM = 'shell-platform'

/**
 * @param {object} options
 * @param {Record<string, {tenantScoped?: boolean}>} options.profiles the
 *   credential profiles that exist, and which of them are scoped to a tenant.
 * @param {(descriptor: {profile: string, tenant: string|null, key: string}) => Promise<object>} options.connect
 *   injected: the specs dial nothing, and the host owns how a credential is minted.
 */
export function createConnectionRegistry({ profiles, connect }) {
  const declared = new Map(Object.entries(profiles ?? {}))
  const connections = new Map()

  const keyFor = (profile, tenant = null) => {
    const spec = declared.get(profile)
    if (!spec) {
      throw new Error(`Unknown credential profile ${JSON.stringify(profile)}`)
    }
    if (spec.tenantScoped) {
      if (typeof tenant !== 'string' || tenant === '') {
        throw new Error(`Credential profile ${profile} is tenant-scoped and needs a tenant`)
      }
      return `${profile}#${tenant}`
    }
    /* A tenant passed to a platform profile is a category error, and the one it
       would produce silently is exactly the merge this rule forbids: a caller
       believing it is scoped to a tenant while holding platform-wide reach. */
    if (tenant !== null && tenant !== undefined) {
      throw new Error(`Credential profile ${profile} is not tenant-scoped; got tenant ${tenant}`)
    }
    return profile
  }

  return {
    keyFor,

    /** Shared per key; one connection per profile-and-tenant, never per caller. */
    acquire(profile, tenant = null) {
      const key = keyFor(profile, tenant)
      if (!connections.has(key)) {
        connections.set(
          key,
          Promise.resolve(connect({ profile, tenant: tenant ?? null, key })).catch((error) => {
            /* Do not cache a failed connect — the next caller should retry
               rather than inherit a permanently rejected promise. */
            connections.delete(key)
            throw error
          }),
        )
      }
      return connections.get(key)
    },

    has(profile, tenant = null) {
      return connections.has(keyFor(profile, tenant))
    },

    get keys() {
      return [...connections.keys()]
    },

    async close(profile, tenant = null) {
      const key = keyFor(profile, tenant)
      const pending = connections.get(key)
      if (!pending) return
      connections.delete(key)
      const connection = await pending.catch(() => null)
      await connection?.close?.()
    },

    async closeAll() {
      const pending = [...connections.values()]
      connections.clear()
      for (const p of pending) {
        const connection = await p.catch(() => null)
        await connection?.close?.()
      }
    },
  }
}
