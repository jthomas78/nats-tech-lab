import { describe, expect, it } from 'vitest'

import { validateManifest } from './manifestSchema.js'
import { diffRegistry, RELOAD_REASON } from './registryDiff.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'

/*
  A withdrawal is the one catalogue change other than an addition that a
  running shell may apply (BR-AS54, BR-AS56): it takes UI away, which is
  always safe, unlike an edit that would leave two versions of one plugin in
  one page. The marker is what carries it — absence never does, because a
  filtered, degraded or failed read is also an absence.
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

describe('BR-AS56 — a withdrawal marker is reported as a withdrawal', () => {
  it('reports a running plugin as withdrawn, not as removed or changed', () => {
    const changes = diffRegistry([held('fleet-ops')], [marker('fleet-ops')])

    expect(changes.withdrawn).toEqual([{ id: 'fleet-ops', name: 'fleet-ops' }])
    expect(changes.reloadRequired).toEqual([])
    expect(changes.added).toEqual([])
  })

  it('ignores a marker for a plugin the shell never held', () => {
    const changes = diffRegistry([held('fleet-ops')], [raw('fleet-ops'), marker('pricing')])

    expect(changes.withdrawn).toEqual([])
    expect(changes.added).toEqual([])
    expect(changes.reloadRequired).toEqual([])
  })

  it('leaves siblings in the same document untouched', () => {
    const changes = diffRegistry(
      [held('fleet-ops'), held('pricing')],
      [marker('fleet-ops'), raw('pricing')],
    )

    expect(changes.withdrawn.map((w) => w.id)).toEqual(['fleet-ops'])
    expect(changes.reloadRequired).toEqual([])
  })

  it('reports the same withdrawal on every later read, so the caller can be idempotent', () => {
    const document = [marker('fleet-ops')]
    const current = [held('fleet-ops')]

    expect(diffRegistry(current, document).withdrawn).toHaveLength(1)
    expect(diffRegistry(current, document).withdrawn).toHaveLength(1)
  })

  it('lets a revocation outrank a withdrawal, because one is a security event', () => {
    const changes = diffRegistry(
      [held('fleet-ops')],
      [{ id: 'fleet-ops', withheld: true, withdrawn: true }],
    )

    expect(changes.withdrawn).toEqual([])
    expect(changes.reloadRequired[0]).toMatchObject({
      reason: RELOAD_REASON.REVOKED,
      forced: true,
    })
  })
})

describe('BR-AS59 — a return is only a return when nothing changed', () => {
  const withdrawnIds = (...ids) => ({ isWithdrawn: (id) => ids.includes(id) })

  it('reports an unchanged withdrawn plugin as restored', () => {
    const changes = diffRegistry([held('fleet-ops')], [raw('fleet-ops')], withdrawnIds('fleet-ops'))

    expect(changes.restored).toEqual([{ id: 'fleet-ops', name: 'fleet-ops' }])
    expect(changes.reloadRequired).toEqual([])
  })

  it('offers a reload when the remote moved while it was away', () => {
    const moved = raw('fleet-ops')
    moved.remote = { ...moved.remote, url: 'http://localhost:7110/other.js' }

    const changes = diffRegistry([held('fleet-ops')], [moved], withdrawnIds('fleet-ops'))

    expect(changes.restored).toEqual([])
    expect(changes.reloadRequired[0].reason).toBe(RELOAD_REASON.REMOTE_CHANGED)
  })

  it('offers a reload when the definition changed while it was away', () => {
    const relabelled = raw('fleet-ops', { name: 'Fleet Ops v2' })

    const changes = diffRegistry([held('fleet-ops')], [relabelled], withdrawnIds('fleet-ops'))

    expect(changes.restored).toEqual([])
    expect(changes.reloadRequired[0].reason).toBe(RELOAD_REASON.CHANGED)
  })

  it('says nothing about a plugin that was never withdrawn', () => {
    const changes = diffRegistry([held('fleet-ops')], [raw('fleet-ops')])

    expect(changes.restored).toEqual([])
    expect(changes.withdrawn).toEqual([])
  })
})
