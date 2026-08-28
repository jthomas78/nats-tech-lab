import { describe, expect, it, vi } from 'vitest'

import { REGISTRY_SCHEMA_VERSION } from '../versions.js'
import { validateRegistryDocument } from './manifestSchema.js'
import { createRegistryClient, REGISTRY_ENDPOINT } from './registryClient.js'

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

describe('Phase 2c — rules whose implementation does not exist yet', () => {
  // Registered, not skipped-with-a-body: there is nothing to assert against
  // until 2c lands, and a spec asserting absent behaviour would be red for the
  // whole of 2a and 2b.

  it.todo('BR-AS19 — a conditional read sends If-None-Match and can report 304 unchanged')
  it.todo('BR-AS19 — a re-read fires on visibilitychange (hidden -> visible) and on a slow interval')
  it.todo('BR-AS19 — a removed entry offers a reload and leaves an active plugin rendering')
  it.todo('BR-AS22 — a degraded:true document renders built-ins and is distinguishable from an empty registry')
  it.todo('decision 26 — indexing an added entry twice does not duplicate its contributions')
})
