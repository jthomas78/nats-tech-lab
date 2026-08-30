/*
  Connection state keyed by credential profile (BR-AS10).

  The three apps due for migration hold four distinct NATS credentials between
  them, and they must stay four. The tempting simplification when they share a
  shell — one connection, broadest permissions — would dissolve a boundary the
  NATS server enforces today into one the frontend merely intends, and tenancy
  in this platform is the account boundary (CLAUDE.md).

  So there is no "current connection" here. Every consumer names the profile it
  wants, and a tenant-scoped profile also names its tenant; the key is the pair.
  That is deliberately the opposite shape from the bug this design has to avoid:
  demos/01-dictionary/frontend/shared/refdata/useRefdataLabels.js keeps a
  module-global mutable `transport`, so whichever tenant connected last is the
  tenant every caller reads — a cross-tenant leak with no error and no symptom.
  A module-global cannot be keyed, so the fix is structural: state lives in a
  registry that has no default.
*/

/* The four profiles the migration has to preserve. Ids are opaque keys, not
   credential filenames — the shell asks accounts-service for connect info per
   profile and never holds a .creds file. */
export const CREDENTIAL_PROFILE = Object.freeze({
  /* Shell-owned PLATFORM connection: registry read and notify only. */
  SHELL_PLATFORM: 'shell-platform',
  /* Admin UI, PLATFORM account — cross-account diagnostics. */
  ADMIN_PLATFORM: 'admin-platform',
  /* Tech Lab Operator's refdata-admin token, also PLATFORM: reference data is
     platform-scoped, and its context lives in the subject, not the account. */
  OPERATOR_REFDATA_PLATFORM: 'operator-refdata-platform',
  /* Tech Lab Operator acting inside one tenant, for Organizations. */
  OPERATOR_TENANT: 'operator-tenant',
  /* SeaFreight, inside one tenant. */
  SEAFREIGHT_TENANT: 'seafreight-tenant',
})

const TENANT_SCOPED = new Set([
  CREDENTIAL_PROFILE.OPERATOR_TENANT,
  CREDENTIAL_PROFILE.SEAFREIGHT_TENANT,
])

export function isTenantScoped(profile) {
  return TENANT_SCOPED.has(profile)
}

export function connectionKey(profile, tenant = null) {
  if (!Object.values(CREDENTIAL_PROFILE).includes(profile)) {
    throw new Error(`Unknown credential profile ${JSON.stringify(profile)}`)
  }
  if (isTenantScoped(profile)) {
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

/**
 * @param {object} options
 * @param {(descriptor: {profile: string, tenant: string|null, key: string}) => Promise<object>} options.connect
 *   injected: Phase 1a has no NATS client in the shell, and the specs need none.
 */
export function createConnectionRegistry({ connect }) {
  const connections = new Map()

  return {
    /** Shared per key; one connection per profile-and-tenant, never per caller. */
    acquire(profile, tenant = null) {
      const key = connectionKey(profile, tenant)
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
      return connections.has(connectionKey(profile, tenant))
    },

    get keys() {
      return [...connections.keys()]
    },

    async close(profile, tenant = null) {
      const key = connectionKey(profile, tenant)
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
