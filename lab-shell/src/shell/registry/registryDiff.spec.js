import { describe, expect, it } from 'vitest'

import { validateManifest } from './manifestSchema.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { diffRegistry, RELOAD_REASON } from './registryDiff.js'

/*
  Decision 26's split, which is the load-bearing half of BR-AS19: an addition
  may be indexed live because indexing runs no plugin code; anything else may
  not, because `active` has no exit transition.

  Decision 46 is what these fixtures are built to exercise. The manifests are
  COMPLETE and valid, because the diff now normalizes both sides through the
  same validator the shell admits with — a fixture that could not be validated
  would be testing the fallback rather than the rule. `held()` is what the
  shell is actually holding: the validated form, not the document's raw one.
*/

const raw = (id, overrides = {}) => ({
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

/** What a running shell holds — validated, as `admit()` left it. */
const held = (id, overrides = {}) => {
  const result = validateManifest(raw(id, overrides))
  if (!result.ok) throw new Error(`fixture ${id} does not validate: ${result.message}`)
  return result.plugin
}

describe('diffRegistry', () => {
  it('reports an entry the shell has never seen as an addition', () => {
    const result = diffRegistry([held('a')], [raw('a'), raw('b')])
    expect(result.added.map((p) => p.id)).toEqual(['b'])
    expect(result.reloadRequired).toEqual([])
  })

  it('hands an addition back in its raw form, so admit() records the real rejection', () => {
    // A normalized addition would arrive with the failure already swallowed,
    // and the Plugins screen would have nothing to explain.
    const malformed = { id: 'b', name: 'b' }
    expect(diffRegistry([held('a')], [raw('a'), malformed]).added).toEqual([malformed])
  })

  it('reports a withdrawn entry as needing a reload, never as something to apply (BR-AS19)', () => {
    const result = diffRegistry([held('a'), held('b')], [raw('a')])
    expect(result.added).toEqual([])
    expect(result.reloadRequired).toEqual([{ id: 'b', name: 'b', reason: RELOAD_REASON.REMOVED }])
  })

  it('treats a moved remote URL as a reload, not as an addition', () => {
    // Placing it live would leave two versions of one plugin in one page: the
    // container already registered under the old URL keeps answering.
    const moved = raw('a', { remote: { kind: 'federated', url: 'http://localhost:7110/a-2.js', module: './plugin' } })
    const result = diffRegistry([held('a')], [moved])
    expect(result.added).toEqual([])
    expect(result.reloadRequired[0].reason).toBe(RELOAD_REASON.REMOTE_CHANGED)
  })

  it('treats a re-exposed module as a moved remote', () => {
    const moved = raw('a', { remote: { kind: 'federated', url: 'http://localhost:7110/a.js', module: './other' } })
    expect(diffRegistry([held('a')], [moved]).reloadRequired[0].reason).toBe(
      RELOAD_REASON.REMOTE_CHANGED,
    )
  })

  it('offers a reload when the federated catalog is withdrawn', () => {
    const catalog = validateManifest(raw('demo-catalog')).plugin
    expect(diffRegistry([catalog], []).reloadRequired).toEqual([{ id: 'demo-catalog', name: 'demo-catalog', reason: RELOAD_REASON.REMOVED }])
  })

  it('reports no change at all when the same document comes back', () => {
    // The regression guard for decision 46: the shell holds VALIDATED
    // manifests and the document carries RAW ones. A diff that compared the
    // two forms directly would report every plugin as edited on every read —
    // a reload banner on a registry nobody touched.
    const current = [held('a'), held('b')]
    expect(diffRegistry(current, [raw('a'), raw('b')])).toEqual({ added: [], reloadRequired: [] })
  })
})

/*
  Decision 46. The write path is `ON CONFLICT DO UPDATE SET enabled, entry`,
  so the whole entry is replaceable — and the diff that only compared the
  remote saw none of it. Worse than staleness: a transaction editing A and
  adding B applied only B, leaving the shell holding a catalog that existed at
  no revision at all.
*/
describe('decision 46 — every difference that is not a new id is a reload offer', () => {
  const changed = (overrides) => diffRegistry([held('a')], [raw('a', overrides)]).reloadRequired

  it('reports an edited nav label', () => {
    const edited = raw('a', {
      contributions: [
        { kind: 'route', id: 'home', path: '/a', title: 'a' },
        { kind: 'navigation', id: 'nav', label: 'Fleet Operations', route: 'home' },
      ],
    })
    expect(diffRegistry([held('a')], [edited]).reloadRequired).toEqual([
      { id: 'a', name: 'a', reason: RELOAD_REASON.CHANGED },
    ])
  })

  it('reports an edited display name', () => {
    expect(changed({ name: 'Fleet Ops' })[0].reason).toBe(RELOAD_REASON.CHANGED)
  })

  it('reports a moved route prefix', () => {
    expect(changed({ routePrefix: 'fleet' })[0].reason).toBe(RELOAD_REASON.CHANGED)
  })

  it('reports a new version string', () => {
    expect(changed({ version: '2.0.0' })[0].reason).toBe(RELOAD_REASON.CHANGED)
  })

  it('reports a contribution added to an entry already on screen', () => {
    const grown = raw('a', {
      contributions: [
        { kind: 'route', id: 'home', path: '/a', title: 'a' },
        { kind: 'navigation', id: 'nav', label: 'a', route: 'home' },
        { kind: 'shell-footer', id: 'foot', label: 'a' },
      ],
    })
    expect(diffRegistry([held('a')], [grown]).reloadRequired[0].reason).toBe(RELOAD_REASON.CHANGED)
  })

  it('reports an entry that stopped validating, rather than dropping it silently', () => {
    // The shell cannot un-place what it already placed, so a now-malformed
    // entry is news for the banner and not a quiet downgrade of a plugin the
    // user is looking at.
    expect(diffRegistry([held('a')], [{ id: 'a', name: 'a' }]).reloadRequired).toEqual([
      { id: 'a', name: 'a', reason: RELOAD_REASON.CHANGED },
    ])
  })

  it('names a moved remote as a moved remote, not merely as an edit', () => {
    // Both refuse the same way; they are not the same news, and the banner
    // says different things about them.
    const moved = raw('a', {
      name: 'Fleet Ops',
      remote: { kind: 'federated', url: 'http://localhost:7110/a-2.js', module: './plugin' },
    })
    expect(diffRegistry([held('a')], [moved]).reloadRequired[0].reason).toBe(
      RELOAD_REASON.REMOTE_CHANGED,
    )
  })

  it('applies the addition and offers the edit when one transaction did both', () => {
    // The exact interleaving that produced a catalog belonging to no
    // revision: B is placeable, A is not, and the shell must do both things
    // rather than the first one it noticed.
    const result = diffRegistry([held('a')], [raw('a', { name: 'Fleet Ops' }), raw('b')])
    expect(result.added.map((p) => p.id)).toEqual(['b'])
    expect(result.reloadRequired).toEqual([{ id: 'a', name: 'a', reason: RELOAD_REASON.CHANGED }])
  })
})
