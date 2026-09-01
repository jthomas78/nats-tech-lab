import { describe, expect, it } from 'vitest'

import { validateManifest } from './manifestSchema.js'
import { diffRegistry, RELOAD_REASON } from './registryDiff.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'

/*
  The class-change and disable SEQUENCES (BR-AS52–54), which are the last
  half of 5a and could not be written until 5b's withdrawal existed.

  The class is not a behaviour the shell implements — it is a behaviour the
  REGISTRY implements, and the shell sees only the two different documents it
  produces. That is the whole point of the split, so these specs are written
  against the two documents rather than against a class field:

    disabling a STATIC entry   -> the entry leaves the document, and an
                                  absence is a reload OFFER (BR-AS53): the
                                  contributions keep running meanwhile.
    disabling a DYNAMIC entry  -> the entry is served as a withdrawal MARKER,
                                  and a marker is APPLIED (BR-AS54).

  A shell that inferred the class itself would have to trust a metadata field
  to decide whether to take a running plugin off screen; here the two cases
  are simply two different reads.
*/

const raw = (id, overrides = {}) => ({
  id,
  name: id,
  schemaVersion: REGISTRY_SCHEMA_VERSION,
  shellApiVersion: SHELL_API_VERSION,
  remote: { kind: 'federated', url: `http://localhost:7110/${id}.js`, module: './plugin' },
  contributions: [{ kind: 'route', id: 'home', path: `/${id}/home`, title: 'Home' }],
  ...overrides,
})

const held = (id, overrides = {}) => {
  const result = validateManifest(raw(id, overrides))
  if (!result.ok) throw new Error(`fixture is invalid: ${result.message}`)
  return result.plugin
}

const marker = (id) => ({ id, withdrawn: true })

describe('BR-AS53 — disabling a static plugin is offered, never applied', () => {
  it('reports the absence as a reload offer and takes nothing away', () => {
    const changes = diffRegistry([held('fleet-ops', { lifecycle: 'static' })], [])

    expect(changes.reloadRequired).toEqual([
      { id: 'fleet-ops', name: 'fleet-ops', reason: RELOAD_REASON.REMOVED },
    ])
    expect(changes.withdrawn).toEqual([])
  })

  it('leaves the offer unforced, so the shell asks rather than reloads', () => {
    const [offer] = diffRegistry([held('fleet-ops', { lifecycle: 'static' })], []).reloadRequired

    expect(offer.forced).toBeUndefined()
  })

  it('stops offering once the operator puts the same entry back', () => {
    const current = [held('fleet-ops', { lifecycle: 'static' })]

    expect(diffRegistry(current, [raw('fleet-ops', { lifecycle: 'static' })])).toEqual({
      added: [],
      reloadRequired: [],
      withdrawn: [],
      restored: [],
    })
  })
})

describe('BR-AS54 — disabling a dynamic plugin is applied', () => {
  it('reports the marker as a withdrawal and not as a removal', () => {
    const changes = diffRegistry([held('fleet-ops', { lifecycle: 'dynamic' })], [marker('fleet-ops')])

    expect(changes.withdrawn).toEqual([{ id: 'fleet-ops', name: 'fleet-ops' }])
    expect(changes.reloadRequired).toEqual([])
  })

  it('restores it on the re-enable, because the entry comes back unchanged', () => {
    const current = [held('fleet-ops', { lifecycle: 'dynamic' })]
    const changes = diffRegistry(current, [raw('fleet-ops', { lifecycle: 'dynamic' })], {
      isWithdrawn: (id) => id === 'fleet-ops',
    })

    expect(changes.restored).toEqual([{ id: 'fleet-ops', name: 'fleet-ops' }])
    expect(changes.reloadRequired).toEqual([])
  })

  it('offers a reload instead when the entry that comes back is not the one that went away', () => {
    // A return is a return only on equality (BR-AS59). Anything else would
    // put a second version of one plugin into one page.
    const current = [held('fleet-ops', { lifecycle: 'dynamic' })]
    const changes = diffRegistry(
      current,
      [raw('fleet-ops', { lifecycle: 'dynamic', version: '2.0.0' })],
      { isWithdrawn: (id) => id === 'fleet-ops' },
    )

    expect(changes.restored).toEqual([])
    expect(changes.reloadRequired).toEqual([
      { id: 'fleet-ops', name: 'fleet-ops', reason: RELOAD_REASON.CHANGED },
    ])
  })
})

describe('BR-AS52 — an operator changing the class is an edit like any other', () => {
  it('offers a reload when the class changes under a running plugin', () => {
    const changes = diffRegistry(
      [held('fleet-ops', { lifecycle: 'static' })],
      [raw('fleet-ops', { lifecycle: 'dynamic' })],
    )

    expect(changes.reloadRequired).toEqual([
      { id: 'fleet-ops', name: 'fleet-ops', reason: RELOAD_REASON.CHANGED },
    ])
    expect(changes.withdrawn).toEqual([])
  })

  it('withdraws — it does not offer twice — when the class change and the disable arrive together', () => {
    // The operator edits static to dynamic and disables it before the shell
    // reads again. Only the marker arrives, so there is one piece of news and
    // not two: the shell takes the UI away and never offers the edit it can
    // no longer make.
    const changes = diffRegistry([held('fleet-ops', { lifecycle: 'static' })], [marker('fleet-ops')])

    expect(changes.withdrawn).toEqual([{ id: 'fleet-ops', name: 'fleet-ops' }])
    expect(changes.reloadRequired).toEqual([])
    expect(changes.added).toEqual([])
  })

  it('drops the class the other way too — a dynamic plugin made static then disabled just goes absent', () => {
    const changes = diffRegistry([held('fleet-ops', { lifecycle: 'dynamic' })], [])

    expect(changes.reloadRequired.map((r) => r.reason)).toEqual([RELOAD_REASON.REMOVED])
    expect(changes.withdrawn).toEqual([])
  })

  it('lets a revocation outrank the class entirely', () => {
    // BR-AS49: a withheld key is a security event, so a dynamic entry that
    // would otherwise be withdrawn is a FORCED reload instead.
    const changes = diffRegistry(
      [held('fleet-ops', { lifecycle: 'dynamic' })],
      [{ id: 'fleet-ops', withheld: true, withdrawn: true }],
    )

    expect(changes.withdrawn).toEqual([])
    expect(changes.reloadRequired[0]).toMatchObject({
      reason: RELOAD_REASON.REVOKED,
      forced: true,
    })
  })
})
