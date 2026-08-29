import { describe, expect, it } from 'vitest'

import { diffRegistry, RELOAD_REASON } from './registryDiff.js'

/*
  Decision 26's split, which is the load-bearing half of BR-AS19: an addition
  may be indexed live because indexing runs no plugin code; a removal or a
  moved remote may not, because `active` has no exit transition.
*/

const federated = (id, overrides = {}) => ({
  id,
  name: id,
  remote: { kind: 'federated', url: `http://localhost:7110/${id}.js`, module: './plugin' },
  ...overrides,
})

describe('diffRegistry', () => {
  it('reports an entry the shell has never seen as an addition', () => {
    const result = diffRegistry([federated('a')], [federated('a'), federated('b')])
    expect(result.added.map((p) => p.id)).toEqual(['b'])
    expect(result.reloadRequired).toEqual([])
  })

  it('reports a withdrawn entry as needing a reload, never as something to apply (BR-AS19)', () => {
    const result = diffRegistry([federated('a'), federated('b')], [federated('a')])
    expect(result.added).toEqual([])
    expect(result.reloadRequired).toEqual([{ id: 'b', name: 'b', reason: RELOAD_REASON.REMOVED }])
  })

  it('treats a moved remote URL as a reload, not as an addition', () => {
    // Placing it live would leave two versions of one plugin in one page: the
    // container already registered under the old URL keeps answering.
    const moved = federated('a', { remote: { kind: 'federated', url: 'http://localhost:7110/a-2.js', module: './plugin' } })
    const result = diffRegistry([federated('a')], [moved])
    expect(result.added).toEqual([])
    expect(result.reloadRequired[0].reason).toBe(RELOAD_REASON.REMOTE_CHANGED)
  })

  it('treats a re-exposed module as a moved remote', () => {
    const moved = federated('a', { remote: { kind: 'federated', url: 'http://localhost:7110/a.js', module: './other' } })
    expect(diffRegistry([federated('a')], [moved]).reloadRequired[0].reason).toBe(
      RELOAD_REASON.REMOTE_CHANGED,
    )
  })

  it('does not read a built-in as removed when it is absent from the document', () => {
    // Built-ins ship in the shell's own bundle and are deliberately never
    // curated, so every document "omits" them.
    const builtin = { id: 'demo-catalog', name: 'Demo Catalog', remote: { kind: 'builtin', module: 'demo-catalog' } }
    expect(diffRegistry([builtin], []).reloadRequired).toEqual([])
  })

  it('reports no change at all when the same document comes back', () => {
    const held = [federated('a'), federated('b')]
    expect(diffRegistry(held, [federated('a'), federated('b')])).toEqual({ added: [], reloadRequired: [] })
  })
})
