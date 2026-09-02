/*
  What a read means, asserted without booting a shell (BR-AS19, AS22, AS49,
  AS54; decisions 25 and 48).

  Every rule here used to be reachable only through `bootShell` — admit a
  manifest, index it, read back reactive state — so a spec about "what a
  degraded read may retract" had to go through the contribution registry, the
  status map and the allowlist to ask a question about none of them. These call
  one function and read its answer.
*/
import { describe, expect, it } from 'vitest'

import { decideRead, revocationsIn, withdrawalsIn } from './readPolicy.js'
import { RELOAD_REASON } from './registryDiff.js'

const held = { revision: 7, fetchedAt: 1_000, degraded: false, heldRevision: 7 }
const running = [
  { id: 'alpha', name: 'Alpha', schemaVersion: 1, shellApiVersion: 1, contributions: [] },
  { id: 'beta', name: 'Beta', schemaVersion: 1, shellApiVersion: 1, contributions: [] },
]
const decide = (discovery, current = held) => decideRead(discovery, { current, running })

describe('BR-AS19 — a read the shell could not complete', () => {
  it('records the cause rather than throwing', () => {
    const d = decide({ ok: false, code: 'registry-unreachable', message: 'no responders' })
    expect(d.outcome).toBe('failed')
    expect(d.error).toEqual({ code: 'registry-unreachable', message: 'no responders' })
  })

  it('names a cause even when the transport supplied none', () => {
    expect(decide(null).error.code).toBe('registry-malformed')
    expect(decide(undefined).error.code).toBe('registry-malformed')
  })

  it('drops the conditional token so the next read is unconditional', () => {
    expect(decide({ ok: false }).registry.heldRevision).toBeNull()
  })

  it('is not evidence about the catalogue, so nothing else moves', () => {
    const d = decide({ ok: false })
    expect(d.registry.revision).toBe(7)
    expect(d.registry.degraded).toBe(false)
    expect(d.registry.fetchedAt).toBe(1_000)
    expect(d).toMatchObject({ added: [], reloadRequired: [], withdrawn: [], restored: [] })
  })
})

describe('BR-AS22 / decision 48 — a degraded read', () => {
  const degraded = { ok: true, degraded: true, plugins: [], fetchedAt: 2_000 }

  it('is never read as "everything was withdrawn"', () => {
    const d = decide(degraded)
    expect(d.outcome).toBe('degraded')
    expect(d.reloadRequired).toEqual([])
    expect(d.withdrawn).toEqual([])
    expect(d.added).toEqual([])
  })

  it('drops the conditional token, because no revision was vouched for', () => {
    expect(decide(degraded).registry.heldRevision).toBeNull()
  })

  it('keeps the revision already on screen', () => {
    expect(decide(degraded).registry.revision).toBe(7)
  })

  it('may not retract a reload offer standing from a healthy read', () => {
    expect(decide(degraded).retract).toBe(false)
  })

  it('still carries a tombstone through (BR-AS49)', () => {
    const d = decide({ ...degraded, plugins: [{ id: 'alpha', withheld: true }] })
    expect(d.reloadRequired).toEqual([
      { id: 'alpha', name: 'Alpha', reason: RELOAD_REASON.REVOKED, forced: true },
    ])
  })

  it('still carries a withdrawal through (BR-AS54)', () => {
    const d = decide({ ...degraded, plugins: [{ id: 'beta', withdrawn: true }] })
    expect(d.withdrawn).toEqual([{ id: 'beta', name: 'Beta' }])
  })

  it('never carries a return through — that needs a vouched-for document', () => {
    const d = decideRead({ ...degraded, plugins: [{ id: 'beta' }] }, {
      current: held, running, isWithdrawn: () => true,
    })
    expect(d.restored).toEqual([])
  })
})

describe('decision 48 — degraded is a state the shell leaves', () => {
  const inOutage = { ...held, degraded: true, heldRevision: null }

  it('clears on a 304, which is the very answer a recovery at the same revision gives', () => {
    const d = decide({ ok: true, unchanged: true, heldRevision: 7, fetchedAt: 3_000 }, inOutage)
    expect(d.outcome).toBe('unchanged')
    expect(d.registry.degraded).toBe(false)
    expect(d.registry.heldRevision).toBe(7)
  })

  it('clears on a document', () => {
    const d = decide({ ok: true, revision: 8, plugins: [], fetchedAt: 3_000 }, inOutage)
    expect(d.registry.degraded).toBe(false)
  })
})

describe('BR-AS19 — a 304 carries no document', () => {
  it('changes nothing that is on screen', () => {
    const d = decide({ ok: true, unchanged: true, heldRevision: 7 })
    expect(d).toMatchObject({ added: [], reloadRequired: [], withdrawn: [], restored: [] })
    expect(d.registry.revision).toBe(7)
    expect(d.retract).toBe(false)
  })

  it('keeps the token it already holds when the answer names none', () => {
    expect(decide({ ok: true, unchanged: true }).registry.heldRevision).toBe(7)
  })
})

describe('BR-AS19 — a document the service vouched for', () => {
  const doc = {
    ok: true,
    revision: 9,
    heldRevision: 9,
    fetchedAt: 4_000,
    plugins: [...running, { id: 'gamma', name: 'Gamma', schemaVersion: 1, shellApiVersion: 1, contributions: [] }],
  }

  it('installs the revision, the stamp and the token together', () => {
    const d = decide(doc)
    expect(d.outcome).toBe('document')
    expect(d.registry).toEqual({ revision: 9, fetchedAt: 4_000, degraded: false, heldRevision: 9 })
  })

  it('names what is new', () => {
    expect(decide(doc).added.map((p) => p.id)).toEqual(['gamma'])
  })

  it('is the only read that may retract a standing offer', () => {
    expect(decide(doc).retract).toBe(true)
  })

  it('reads a revision it was not given as no revision at all', () => {
    expect(decide({ ok: true, plugins: [] }).registry.revision).toBeNull()
  })
})

describe('BR-AS49 / AS54 — presence is read, absence never is', () => {
  it('takes no marker from an id the shell is not running', () => {
    expect(revocationsIn([{ id: 'ghost', withheld: true }], running)).toEqual([])
    expect(withdrawalsIn([{ id: 'ghost', withdrawn: true }], running)).toEqual([])
  })

  it('reads a tombstone as a revocation, not merely a withdrawal', () => {
    const entries = [{ id: 'alpha', withheld: true, withdrawn: true }]
    expect(revocationsIn(entries, running)).toHaveLength(1)
    expect(withdrawalsIn(entries, running)).toEqual([])
  })

  it('falls back to the id when the shell holds no name', () => {
    expect(revocationsIn([{ id: 'alpha', withheld: true }], [{ id: 'alpha' }])[0].name).toBe('alpha')
  })

  it('treats a missing plugin list as no markers, never as a removal', () => {
    expect(revocationsIn(undefined, running)).toEqual([])
    expect(withdrawalsIn(undefined, running)).toEqual([])
  })
})
