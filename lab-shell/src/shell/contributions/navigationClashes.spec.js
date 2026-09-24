import { describe, expect, it } from 'vitest'

import { createPermissionEvaluator } from '../auth/permissions.js'
import { declareShellExtensionPoints } from '../extensions/extensionPoints.js'
import { validateManifest } from '../registry/manifestSchema.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { createContributionRegistry } from './contributionRegistry.js'

const plugin = (id, contributions) => {
  const result = validateManifest({
    id,
    name: id,
    schemaVersion: REGISTRY_SCHEMA_VERSION,
    shellApiVersion: SHELL_API_VERSION,
    remote: { kind: 'federated', url: `http://localhost:7110/${id}.js`, module: './plugin' },
    contributions,
  })
  if (!result.ok) throw new Error(`fixture is invalid: ${result.message}`)
  return result.plugin
}

/* Each entry gets its own route, so nothing here is refused for want of one
   (BR-AS12) and every clash below is genuinely about two PLACED entries. */
const navPlugin = (id, entries) =>
  plugin(
    id,
    entries.flatMap((entry, index) => {
      const routeId = entry.routeId ?? `r${index}`
      return [
        ...(entry.routeId ? [] : [{ kind: 'route', id: routeId, path: `/${id}/${routeId}`, title: routeId }]),
        {
          kind: 'navigation',
          id: `n${index}`,
          label: entry.label,
          route: routeId,
          ...(entry.group === undefined ? {} : { group: entry.group }),
          ...(entry.order === undefined ? {} : { order: entry.order }),
        },
      ]
    }),
  )

const build = () =>
  createContributionRegistry({
    extensionPoints: declareShellExtensionPoints(),
    permissions: createPermissionEvaluator({ permissions: ['*'] }),
  })

const clashesOf = (...plugins) => build().index(plugins).navigationClashes
const kinds = (clashes) => clashes.map((c) => c.kind)

describe('BR-AS87 — same group id, conflicting label, is a clash', () => {
  const registry = () =>
    build().index([
      navPlugin('zulu', [{ label: 'Z', group: { id: 'ops', label: 'Zulu Ops' } }]),
      navPlugin('alpha', [{ label: 'A', group: { id: 'ops', label: 'Alpha Ops' } }]),
    ])

  it('reports one clash naming the kind and the band', () => {
    const [clash] = registry().navigationClashes

    expect(clash.kind).toBe('group-label-conflict')
    expect(clash.groupId).toBe('ops')
  })

  it('names BOTH owning plugins', () => {
    expect(registry().navigationClashes[0].pluginIds).toEqual(['alpha', 'zulu'])
  })

  it('says which name is shown and which was asked for', () => {
    const [clash] = registry().navigationClashes

    expect(clash.label).toBe('Alpha Ops')
    expect(clash.message).toContain('shown as "Alpha Ops"')
    expect(clash.message).toContain('zulu asked for "Zulu Ops"')
  })

  it('keeps both entries placed, because a clash is not a refusal', () => {
    const r = registry()

    expect(r.navigation.map((n) => n.label).sort()).toEqual(['A', 'Z'])
    expect(r.refusals).toEqual([])
  })

  it('is silent when the two plugins agree on the name', () => {
    expect(
      clashesOf(
        navPlugin('zulu', [{ label: 'Z', group: { id: 'ops', label: 'Operations' } }]),
        navPlugin('alpha', [{ label: 'A', group: { id: 'ops', label: 'Operations' } }]),
      ),
    ).toEqual([])
  })

  it('does not blame a plugin for a band the shell names', () => {
    expect(
      clashesOf(navPlugin('alpha', [{ label: 'A', group: { id: 'features', label: 'Alpha Says' } }])),
    ).toEqual([])
  })
})

describe('BR-AS87 — different group ids showing the same name is a clash', () => {
  const clash = () =>
    clashesOf(
      navPlugin('alpha', [{ label: 'A', group: { id: 'sea', label: 'Operations' } }]),
      navPlugin('bravo', [{ label: 'B', group: { id: 'road', label: 'Operations' } }]),
    )[0]

  it('reports the kind and every band involved', () => {
    expect(clash().kind).toBe('duplicate-group-label')
    expect(clash().groupIds).toEqual(['road', 'sea'])
  })

  it('names both owning plugins and the repeated name', () => {
    expect(clash().pluginIds).toEqual(['alpha', 'bravo'])
    expect(clash().label).toBe('Operations')
  })

  it('reports a band that collides with one the shell owns', () => {
    const found = clashesOf(
      navPlugin('alpha', [{ label: 'A' }]),
      navPlugin('bravo', [{ label: 'B', group: { id: 'extras', label: 'Features' } }]),
    )

    expect(kinds(found)).toEqual(['duplicate-group-label'])
    expect(found[0].pluginIds).toEqual(['bravo'])
  })

  /* The rail draws a band the tree does not hold — the shell's own Home and
     Plugins band. It is host-owned and unplaceable (BR-AS07), but a reader
     who sees `Shell` twice is confused by it either way. */
  it('reports a band that collides with one the RAIL draws outside the tree', () => {
    const found = clashesOf(navPlugin('alpha', [{ label: 'A', group: { id: 'extras', label: 'Shell' } }]))

    expect(kinds(found)).toEqual(['duplicate-group-label'])
    expect(found[0].groupIds).toEqual(['shell', 'extras'])
    expect(found[0].pluginIds).toEqual(['alpha'])
  })

  it('does not report the reserved band on its own, because the rail always draws it', () => {
    expect(clashesOf(navPlugin('alpha', [{ label: 'A' }]))).toEqual([])
  })

  it('is silent when two bands read differently', () => {
    expect(
      clashesOf(
        navPlugin('alpha', [{ label: 'A', group: { id: 'sea', label: 'Sea' } }]),
        navPlugin('bravo', [{ label: 'B', group: { id: 'road', label: 'Road' } }]),
      ),
    ).toEqual([])
  })
})

describe('BR-AS87 — the same child name going to two places is a clash', () => {
  it('reports the kind, the band and both entries', () => {
    const [clash] = clashesOf(
      navPlugin('alpha', [{ label: 'Rates', group: 'ops' }]),
      navPlugin('bravo', [{ label: 'Rates', group: 'ops' }]),
    )

    expect(clash.kind).toBe('duplicate-item-label')
    expect(clash.groupId).toBe('ops')
    expect(clash.pluginIds).toEqual(['alpha', 'bravo'])
    expect(clash.participants.map((p) => p.route)).toEqual(['alpha/r0', 'bravo/r0'])
  })

  it('is silent when the same name goes to the same place', () => {
    const p = plugin('alpha', [
      { kind: 'route', id: 'rates', path: '/alpha/rates', title: 'Rates' },
      { kind: 'navigation', id: 'n0', label: 'Rates', route: 'rates', group: 'ops' },
      { kind: 'navigation', id: 'n1', label: 'Rates', route: 'rates', group: 'ops' },
    ])

    expect(clashesOf(p)).toEqual([])
  })

  it('is silent when the same name sits in two different bands', () => {
    expect(
      clashesOf(
        navPlugin('alpha', [{ label: 'Rates', group: { id: 'sea', label: 'Sea' } }]),
        navPlugin('bravo', [{ label: 'Rates', group: { id: 'road', label: 'Road' } }]),
      ),
    ).toEqual([])
  })
})

describe('BR-AS87 — the three cases amendment A2 settled as NOT clashes', () => {
  it('merges two plugins that name one band — the feature, not a fault', () => {
    const r = build().index([
      navPlugin('alpha', [{ label: 'A', group: { id: 'ops', label: 'Operations' } }]),
      navPlugin('bravo', [{ label: 'B', group: { id: 'ops', label: 'Operations' } }]),
    ])

    expect(r.navigationTree).toHaveLength(1)
    expect(r.navigationClashes).toEqual([])
  })

  it('cannot see a group order conflict, because a plugin declares no group order', () => {
    const r = build().index([
      navPlugin('alpha', [{ label: 'A', group: { id: 'ops', label: 'Operations', order: 99 } }]),
      navPlugin('bravo', [{ label: 'B', group: { id: 'ops', label: 'Operations', order: 1 } }]),
    ])

    expect(r.navigationTree.map((g) => g.id)).toEqual(['ops'])
    expect(r.navigationClashes).toEqual([])
  })

  it('treats the same local id in two plugins as valid, because identity is qualified', () => {
    const r = build().index([
      navPlugin('alpha', [{ label: 'A', group: 'ops' }]),
      navPlugin('bravo', [{ label: 'B', group: 'ops' }]),
    ])

    expect(r.navigation.map((n) => n.qualifiedId)).toEqual(['alpha/n0', 'bravo/n0'])
    expect(r.navigationClashes).toEqual([])
  })

  it('leaves an unresolved route a refusal, and reports no clash for it', () => {
    const r = build().index([
      plugin('alpha', [
        { kind: 'route', id: 'rates', path: '/alpha/rates', title: 'Rates' },
        { kind: 'navigation', id: 'n0', label: 'Rates', route: 'rates', group: 'ops' },
        { kind: 'navigation', id: 'n1', label: 'Rates', route: 'nowhere', group: 'ops' },
      ]),
    ])

    expect(r.refusals.map((f) => f.code)).toEqual(['unresolved-route'])
    expect(r.navigationClashes).toEqual([])
  })

  it('takes a withdrawn plugin out of the clash list, and puts it back on return', () => {
    const r = build().index([
      navPlugin('zulu', [{ label: 'Z', group: { id: 'ops', label: 'Zulu Ops' } }]),
      navPlugin('alpha', [{ label: 'A', group: { id: 'ops', label: 'Alpha Ops' } }]),
    ])
    expect(kinds(r.navigationClashes)).toEqual(['group-label-conflict'])

    r.withdraw('zulu')
    expect(r.navigationClashes).toEqual([])

    r.restore('zulu')
    expect(kinds(r.navigationClashes)).toEqual(['group-label-conflict'])
  })
})

describe('BR-AS87 — the clash list is as pure as the tree it reads', () => {
  const three = () => [
    navPlugin('zulu', [{ label: 'Rates', group: { id: 'ops', label: 'Zulu Ops' } }]),
    navPlugin('alpha', [{ label: 'Rates', group: { id: 'ops', label: 'Alpha Ops' } }]),
    navPlugin('bravo', [{ label: 'B', group: { id: 'extras', label: 'Alpha Ops' } }]),
  ]
  const said = (clashes) => clashes.map((c) => c.message)

  it('reports the same clashes whatever order the plugins were indexed in', () => {
    const forwards = clashesOf(...three())
    const backwards = clashesOf(...three().reverse())

    expect(said(backwards)).toEqual(said(forwards))
  })

  it('reports all three kinds at once without dropping one', () => {
    expect(kinds(clashesOf(...three())).sort()).toEqual([
      'duplicate-group-label',
      'duplicate-item-label',
      'group-label-conflict',
    ])
  })

  it('is kept apart from refusals, which stay empty', () => {
    expect(build().index(three()).refusals).toEqual([])
  })
})
