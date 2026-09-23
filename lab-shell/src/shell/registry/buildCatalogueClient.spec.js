import { describe, expect, it, vi } from 'vitest'

import {
  CATALOGUE_MALFORMED,
  CATALOGUE_MISSING,
  CATALOGUE_UNREACHABLE,
  CATALOGUE_UNREADABLE,
  createBuildCatalogueClient,
} from './buildCatalogueClient.js'
import { BUILD_CATALOGUE_PATH } from './buildCatalogueLocation.js'
import { createRegistryTransport } from './registryTransport.js'
import { decideRead } from './readPolicy.js'

const PLUGIN = {
  id: 'jetstream-cqrs',
  name: 'JetStream CQRS',
  schemaVersion: 1,
  shellApiVersion: 1,
  remote: { kind: 'federated', url: '/remoteEntry.js', module: 'plugin' },
  contributions: [{ kind: 'route', id: 'main', path: '/jetstream-cqrs', title: 'CQRS' }],
}

const served = (document, { ok = true, status = 200 } = {}) =>
  vi.fn(async () => ({ ok, status, json: async () => document }))

const clientFor = (fetch, now = () => '2026-09-23T00:00:00.000Z') =>
  createBuildCatalogueClient({ fetch, now })

const emptyRegistry = { revision: null, heldRevision: null, fetchedAt: null, degraded: false }

describe('the build catalogue client', () => {
  it('reads the generated document from the shell\'s own origin', async () => {
    const fetch = served({ schemaVersion: 1, revision: 'abc', degraded: false, plugins: [] })
    await clientFor(fetch).fetchRegistry({ heldRevision: null })
    expect(fetch).toHaveBeenCalledWith(BUILD_CATALOGUE_PATH, { cache: 'no-store' })
  })

  /* Decision 4. `decideRead` must stay pure: it never learns that sources
     exist, which is only true if both clients hand it the same object. */
  it('returns the registry transport\'s exact result shape', async () => {
    const document = { schemaVersion: 1, revision: '7', degraded: false, plugins: [PLUGIN] }
    const build = await clientFor(served(document)).fetchRegistry({ heldRevision: null })

    const transport = createRegistryTransport({
      request: async () => ({ ok: true, schemaVersion: 1, revision: 7, degraded: false, entries: [PLUGIN] }),
      now: () => '2026-09-23T00:00:00.000Z',
    })
    const registry = await transport.fetchRegistry({ heldRevision: null })

    expect(Object.keys(build).sort()).toEqual(Object.keys(registry).sort())

    /* Every key carries the same kind of value EXCEPT the revision pair. The
       phase's boot table makes that difference explicit: a registry revision
       is monotonic and comes from Postgres, a build revision is a hash of the
       generated document. `decideRead` only ever copies a revision through and
       compares it for equality, so the difference costs it nothing — and
       `validateRegistryDocument` accepts a string or a number by design. */
    for (const key of Object.keys(registry)) {
      if (key === 'revision' || key === 'heldRevision') continue
      expect(typeof build[key], key).toBe(typeof registry[key])
    }
    expect(typeof build.revision).toBe('string')
    expect(typeof registry.revision).toBe('number')
  })

  it('never reports degraded, because there is no service to be half-available', async () => {
    const document = { schemaVersion: 1, revision: 'r1', degraded: true, plugins: [PLUGIN] }
    const result = await clientFor(served(document)).fetchRegistry({})
    expect(result.degraded).toBe(false)
  })

  /* The distinction decision 4 added at the gate: an empty lab and a broken
     build both end in an empty screen and must not look alike. */
  describe('an empty catalogue and a broken one', () => {
    it('reads zero entries as a successful empty read', async () => {
      const document = { schemaVersion: 1, revision: 'e', degraded: false, plugins: [] }
      const result = await clientFor(served(document)).fetchRegistry({})
      expect(result.ok).toBe(true)
      expect(result.plugins).toEqual([])
      expect(result.code).toBeUndefined()
    })

    it('reads a missing catalogue as a failed read carrying a code', async () => {
      const result = await clientFor(served(null, { ok: false, status: 404 })).fetchRegistry({})
      expect(result).toEqual({ ok: false, code: CATALOGUE_MISSING })
    })

    /* A shell deployed without its catalogue and a generator that could not
       produce one are both failures, but they are fixed in different places,
       so they carry different codes. */
    it('separates a catalogue it could not generate from one that is absent', async () => {
      const result = await clientFor(served(null, { ok: false, status: 500 })).fetchRegistry({})
      expect(result).toEqual({ ok: false, code: CATALOGUE_UNREADABLE })
    })

    it('reads an unparseable catalogue as a failed read carrying a code', async () => {
      const fetch = vi.fn(async () => ({ ok: true, json: async () => { throw new SyntaxError('bad') } }))
      const result = await clientFor(fetch).fetchRegistry({})
      expect(result).toEqual({ ok: false, code: CATALOGUE_MALFORMED })
    })

    it('reads a document of the wrong shape as malformed, not as empty', async () => {
      const result = await clientFor(served({ schemaVersion: 1, plugins: 'nope' })).fetchRegistry({})
      expect(result).toEqual({ ok: false, code: CATALOGUE_MALFORMED })
    })

    it('reports an unreachable catalogue separately from a missing one', async () => {
      const fetch = vi.fn(async () => { throw new TypeError('network') })
      const result = await clientFor(fetch).fetchRegistry({})
      expect(result).toEqual({ ok: false, code: CATALOGUE_UNREACHABLE })
    })

    /* The acceptance wording is "the two render differently". That is a claim
       about what reaches the screen, so it is asserted through the real
       policy rather than on the client's own keys. */
    it('renders the two differently once decideRead has seen them', async () => {
      const empty = await clientFor(served({ schemaVersion: 1, revision: 'e', plugins: [] })).fetchRegistry({})
      const broken = await clientFor(served(null, { ok: false, status: 404 })).fetchRegistry({})

      const emptyRead = decideRead(empty, { current: emptyRegistry })
      const brokenRead = decideRead(broken, { current: emptyRegistry })

      expect(emptyRead.outcome).toBe('document')
      expect(emptyRead.error).toBeNull()
      expect(brokenRead.outcome).toBe('failed')
      expect(brokenRead.error.code).toBe(CATALOGUE_MISSING)
    })
  })

  describe('conditional reads', () => {
    it('reports unchanged when the held revision matches', async () => {
      const document = { schemaVersion: 1, revision: 'same', degraded: false, plugins: [PLUGIN] }
      const result = await clientFor(served(document)).fetchRegistry({ heldRevision: 'same' })
      expect(result.unchanged).toBe(true)
      expect(result.plugins).toBeUndefined()
    })

    it('returns the document when the held revision has moved on', async () => {
      const document = { schemaVersion: 1, revision: 'new', degraded: false, plugins: [PLUGIN] }
      const result = await clientFor(served(document)).fetchRegistry({ heldRevision: 'old' })
      expect(result.unchanged).toBe(false)
      expect(result.plugins).toEqual([PLUGIN])
      expect(result.heldRevision).toBe('new')
    })

    it('treats a held revision it cannot use as no held revision', async () => {
      const document = { schemaVersion: 1, revision: 'r', degraded: false, plugins: [PLUGIN] }
      for (const held of [0, '', undefined, {}]) {
        const result = await clientFor(served(document)).fetchRegistry({ heldRevision: held })
        expect(result.unchanged, String(held)).toBe(false)
      }
    })
  })
})
