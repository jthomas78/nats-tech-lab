import { describe, expect, it, vi } from 'vitest'

import { bootShell } from '../bootShell.js'
import { createPermissionEvaluator } from '../auth/permissions.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { validateRegistryDocument } from './manifestSchema.js'
import { createRegistryClient, REGISTRY_ENDPOINT } from './registryClient.js'
import { createChangeSubscription } from './changeSubscription.js'

/*
  Phase 2 turns the curated registry into service state (Postgres source of
  truth, KV write-through cache, a real monotonic revision). The phase claims
  independence from Phases 10-12 on the grounds that the SHELL's read contract
  does not change — decision 27 says that claim is "confirmed, not assumed".

  This file is where it is confirmed. These are characterization specs: they
  pin the read-side facts Phase 2a must not break, and they pass against the
  code as it stands today. The rules whose implementation does not exist yet
  (BR-AS19's re-read, BR-AS22's degraded document) are registered here as
  todos so the derivation is visible without turning the suite red.
*/

const doc = (overrides = {}) => ({
  schemaVersion: REGISTRY_SCHEMA_VERSION,
  plugins: [],
  ...overrides,
})

describe('decision 27 — revision may become a monotonic integer with no shell change', () => {
  it('accepts a hand-typed string revision (what the seeded file serves today)', () => {
    expect(validateRegistryDocument(doc({ revision: 'dev-1b' })).revision).toBe('dev-1b')
  })

  it('accepts a server-assigned integer revision and stringifies it', () => {
    // The whole of decision 27 rests on this line. A validator that demanded a
    // string would make the store change a breaking change for every shell.
    const result = validateRegistryDocument(doc({ revision: 47 }))
    expect(result.ok).toBe(true)
    expect(result.revision).toBe('47')
  })

  it('keeps revision 0 as "0" rather than dropping it', () => {
    // Decision 30 reserves revision 0 for the degraded response, so it can
    // never collide with a real revision (which starts at 1). A truthiness
    // check here would erase the one value that carries that meaning.
    expect(validateRegistryDocument(doc({ revision: 0 })).revision).toBe('0')
  })

  it('treats a missing revision as absent, not as an error', () => {
    // An older service that omits it still serves — the shell displays the
    // revision and does nothing else with it.
    const result = validateRegistryDocument(doc())
    expect(result.ok).toBe(true)
    expect(result.revision).toBeNull()
  })

  it('rejects the whole document when the schema version moves, revision or not', () => {
    const result = validateRegistryDocument(doc({ schemaVersion: 999, revision: 47 }))
    expect(result.ok).toBe(false)
    expect(result.code).toBe('unsupported-schema-version')
  })
})

describe('decision 34 — the endpoint path is named in exactly one place', () => {
  it('is the client default, so no call site hardcodes it', async () => {
    // Decision 34 moves the path to /api/platform/registry/frontend-plugins in
    // Phase 2a. That is a one-line change only while this stays true.
    const fetchImpl = vi.fn(async () => ({
      ok: true,
      status: 200,
      json: async () => doc({ revision: 1 }),
    }))

    const client = createRegistryClient({ fetch: fetchImpl })

    expect(client.endpoint).toBe(REGISTRY_ENDPOINT)
    await client.fetchRegistry()
    expect(fetchImpl.mock.calls[0][0]).toBe(REGISTRY_ENDPOINT)
  })

  it('sits under the /api/platform prefix each app rewrites in its proxy', () => {
    // The prefix is what makes the move a proxy rule rather than a client
    // rewrite; a path outside it would not reach the service in dev at all.
    expect(REGISTRY_ENDPOINT.startsWith('/api/platform/')).toBe(true)
  })
})

const permissions = createPermissionEvaluator({ permissions: ['*'] })

const manifest = (id, overrides = {}) => ({
  id,
  name: id,
  schemaVersion: REGISTRY_SCHEMA_VERSION,
  shellApiVersion: SHELL_API_VERSION,
  remote: { kind: 'federated', url: `http://localhost:7110/${id}.js`, module: './plugin' },
  contributions: [
    { kind: 'route', id: 'home', path: `/${id}`, title: id },
    { kind: 'navigation', id: 'nav', label: id, route: 'home' },
  ],
  ...overrides,
})

describe('Phase 2c — the shell notices a change', () => {
  it('BR-AS19 — a conditional read sends If-None-Match and can report 304 unchanged', async () => {
    const fetchImpl = vi.fn(async () => ({ ok: true, status: 304, headers: { get: () => null } }))
    const client = createRegistryClient({ fetch: fetchImpl })

    const result = await client.fetchRegistry({ etag: '"47"' })

    expect(fetchImpl.mock.calls[0][1].headers['If-None-Match']).toBe('"47"')
    // Unchanged is a fact the SERVICE states. A shell that inferred it by
    // comparing documents would re-diff, and re-offer, on every quiet read.
    expect(result).toMatchObject({ ok: true, unchanged: true, etag: '"47"' })
  })

  it('BR-AS19 — Phase 4 replaces focus/interval triggers with push and reconnect', async () => {
    let deliver
    const read = vi.fn(async () => ({ ok: true, unchanged: true }))
    const subscription = createChangeSubscription({
      subscribe: (_subject, handler) => { deliver = handler; return { unsubscribe() {} } },
      read,
      currentRevision: () => 47,
    })
    subscription.start()
    deliver({ revision: 48 })
    await new Promise((resolve) => setTimeout(resolve, 0))
    await subscription.onReconnect()
    expect(read.mock.calls.map(([opts]) => opts)).toEqual([
      { unconditional: false, reason: 'notify' },
      { unconditional: true, reason: 'reconnect' },
    ])
    subscription.stop()
  })

  it('BR-AS19 — a removed entry offers a reload and leaves an active plugin rendering', async () => {
    const shell = await bootShell({
      registryClient: { fetchRegistry: vi.fn(async () => ({ ok: true, revision: '1', plugins: [manifest('fleet-ops')] })) },
      permissions,
    })

    shell.applyRegistry({ ok: true, revision: '2', plugins: [] })

    // The offer is the whole of decision 25: `active` has no exit transition,
    // so the route the user may be standing on is still placed.
    expect(shell.pendingReload).toEqual([
      { id: 'fleet-ops', name: 'fleet-ops', reason: 'entry-removed' },
    ])
    expect(shell.contributions.routes.map((r) => r.path)).toContain('/fleet-ops')
    expect(shell.registry.revision).toBe('2')
  })

  it('BR-AS22 — a degraded:true document preserves running plugins and differs from an empty registry', async () => {
    const shell = await bootShell({
      registryClient: { fetchRegistry: vi.fn(async () => ({ ok: true, revision: '3', plugins: [manifest('fleet-ops')] })) },
      permissions,
    })

    shell.applyRegistry({ ok: true, revision: '0', degraded: true, plugins: [] })

    expect(shell.registry.degraded).toBe(true)
    expect(shell.contributions.routes.map((r) => r.path)).toContain('/fleet-ops')
    // Not read as "everything was withdrawn": the service already said it
    // could not vouch for this document, so it is no basis for an offer.
    expect(shell.pendingReload).toHaveLength(0)
  })

  it('decision 26 — indexing an added entry twice does not duplicate its contributions', async () => {
    const shell = await bootShell({
      registryClient: { fetchRegistry: vi.fn(async () => ({ ok: true, revision: '1', plugins: [] })) },
      permissions,
    })

    const added = shell.applyRegistry({ ok: true, revision: '2', plugins: [manifest('fleet-ops')] })
    shell.applyRegistry({ ok: true, revision: '3', plugins: [manifest('fleet-ops')] })

    expect(added.addedRoutes.map((r) => r.path)).toEqual(['/fleet-ops'])
    expect(shell.contributions.routes).toHaveLength(1)
    expect(shell.contributions.navigation).toHaveLength(1)
  })
})
