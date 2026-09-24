import { describe, expect, it } from 'vitest'

import { createPermissionEvaluator } from '../auth/permissions.js'
import { declareShellExtensionPoints } from '../extensions/extensionPoints.js'
import { validateManifest } from '../registry/manifestSchema.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { createContributionRegistry } from './contributionRegistry.js'
import { SHELL_GROUP_ORDER, UNGROUPED_GROUP_ID, buildNavigationTree } from './navigationTree.js'

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

/* One route and one nav entry pointing at it, which is the smallest thing the
   placement rules will actually admit (BR-AS12). */
const navPlugin = (id, entries) =>
  plugin(
    id,
    entries.flatMap((entry, index) => [
      { kind: 'route', id: `r${index}`, path: `/${id}/r${index}`, title: `R${index}` },
      {
        kind: 'navigation',
        id: `n${index}`,
        label: entry.label,
        route: `r${index}`,
        ...(entry.group === undefined ? {} : { group: entry.group }),
        ...(entry.order === undefined ? {} : { order: entry.order }),
      },
    ]),
  )

const build = () =>
  createContributionRegistry({
    extensionPoints: declareShellExtensionPoints(),
    permissions: createPermissionEvaluator({ permissions: ['*'] }),
  })

const treeOf = (...plugins) => build().index(plugins).navigationTree
const shape = (tree) => tree.map((g) => [g.id, g.label, g.items.map((i) => i.label)])

describe('BR-AS86 — the navigation tree groups by group id', () => {
  it('puts two plugins that name the same group id into one band', () => {
    const tree = treeOf(
      navPlugin('alpha', [{ label: 'Vessels', group: { id: 'ops', label: 'Operations' } }]),
      navPlugin('bravo', [{ label: 'Berths', group: { id: 'ops', label: 'Operations' } }]),
    )

    expect(shape(tree)).toEqual([['ops', 'Operations', ['Vessels', 'Berths']]])
  })

  it('keeps two different group ids apart even when they share a label', () => {
    const tree = treeOf(
      navPlugin('alpha', [{ label: 'Vessels', group: { id: 'sea', label: 'Operations' } }]),
      navPlugin('bravo', [{ label: 'Trucks', group: { id: 'road', label: 'Operations' } }]),
    )

    expect(tree.map((g) => g.id)).toEqual(['road', 'sea'])
  })

  it('reads the string shorthand as a group whose id and label are the string', () => {
    const tree = treeOf(navPlugin('alpha', [{ label: 'Vessels', group: 'Reports' }]))

    expect(shape(tree)).toEqual([['Reports', 'Reports', ['Vessels']]])
  })
})

describe('BR-AS86 — an entry with no group lands in Features', () => {
  it('gives an ungrouped entry the shell-owned Features band', () => {
    const tree = treeOf(navPlugin('alpha', [{ label: 'Vessels' }]))

    expect(shape(tree)).toEqual([['features', 'Features', ['Vessels']]])
    expect(tree[0].shellOwned).toBe(true)
    expect(UNGROUPED_GROUP_ID).toBe('features')
  })

  it('does not rename Features, whatever a plugin calls that id', () => {
    const tree = treeOf(
      navPlugin('alpha', [{ label: 'Vessels' }]),
      navPlugin('bravo', [{ label: 'Berths', group: { id: 'features', label: 'Everything Else' } }]),
    )

    expect(shape(tree)).toEqual([['features', 'Features', ['Vessels', 'Berths']]])
  })

  it('makes no band at all when nothing is ungrouped', () => {
    const tree = treeOf(navPlugin('alpha', [{ label: 'Vessels', group: 'Reports' }]))

    expect(tree.map((g) => g.id)).toEqual(['Reports'])
  })
})

describe('BR-AS86 — the four-step order', () => {
  it('puts the shell-owned table first and every other band after it', () => {
    const tree = treeOf(
      navPlugin('alpha', [{ label: 'Admin', group: 'admin' }]),
      navPlugin('bravo', [{ label: 'Vessels' }]),
    )

    expect(tree.map((g) => g.id)).toEqual(['features', 'admin'])
  })

  it('orders unknown bands by group id, not by first seen', () => {
    const tree = treeOf(
      navPlugin('zulu', [{ label: 'Z', group: 'zebra' }]),
      navPlugin('alpha', [{ label: 'A', group: 'apple' }]),
      navPlugin('mike', [{ label: 'M', group: 'mango' }]),
    )

    expect(tree.map((g) => g.id)).toEqual(['apple', 'mango', 'zebra'])
  })

  it('orders inside a band by order, then plugin id, then declaration index', () => {
    const tree = treeOf(
      navPlugin('zulu', [
        { label: 'Z-first', group: 'ops', order: 10 },
        { label: 'Z-second', group: 'ops', order: 10 },
      ]),
      navPlugin('alpha', [
        { label: 'A-late', group: 'ops', order: 99 },
        { label: 'A-early', group: 'ops', order: 1 },
      ]),
    )

    expect(tree[0].items.map((i) => i.label)).toEqual(['A-early', 'Z-first', 'Z-second', 'A-late'])
  })
})

describe('BR-AS86 — the tree is a pure function of the manifests', () => {
  const alpha = () => navPlugin('alpha', [{ label: 'Vessels', group: 'ops' }])
  const bravo = () => navPlugin('bravo', [{ label: 'Berths', group: 'ops' }])
  const charlie = () => navPlugin('charlie', [{ label: 'Rates' }])

  it('draws the same tree whatever order the plugins were indexed in', () => {
    const forwards = treeOf(alpha(), bravo(), charlie())
    const backwards = treeOf(charlie(), bravo(), alpha())

    expect(shape(backwards)).toEqual(shape(forwards))
  })

  it('draws the same tree when the plugins arrive one indexing pass at a time', () => {
    const registry = build()
    registry.index([charlie()])
    registry.index([bravo()])
    registry.index([alpha()])

    expect(shape(registry.navigationTree)).toEqual(shape(treeOf(alpha(), bravo(), charlie())))
  })

  it('draws the same tree after a withdrawal and a restore', () => {
    const registry = build().index([alpha(), bravo(), charlie()])
    const before = shape(registry.navigationTree)

    registry.withdraw('alpha')
    expect(registry.navigationTree.flatMap((g) => g.items.map((i) => i.pluginId))).not.toContain(
      'alpha',
    )

    registry.restore('alpha')
    expect(shape(registry.navigationTree)).toEqual(before)
  })
})

describe('BR-AS86 — a clashing label is chosen, never merged', () => {
  const clash = () =>
    buildNavigationTree(
      build().index([
        navPlugin('zulu', [{ label: 'Z', group: { id: 'ops', label: 'Zulu Ops' } }]),
        navPlugin('alpha', [{ label: 'A', group: { id: 'ops', label: 'Alpha Ops' } }]),
      ]).navigation,
    )

  it('displays exactly one name, chosen by plugin id then declaration index', () => {
    expect(clash()[0].label).toBe('Alpha Ops')
  })

  it('places both entries, because a clash is not a refusal', () => {
    expect(clash()[0].items.map((i) => i.label)).toEqual(['A', 'Z'])
  })

  it('keeps the losing label, with who asked for it, in cascade order', () => {
    expect(clash()[0].labelClaims.map((c) => [c.pluginId, c.label])).toEqual([
      ['alpha', 'Alpha Ops'],
      ['zulu', 'Zulu Ops'],
    ])
  })

  it('records one claim per distinct label, not one per entry', () => {
    const tree = treeOf(
      navPlugin('alpha', [
        { label: 'One', group: { id: 'ops', label: 'Operations' } },
        { label: 'Two', group: { id: 'ops', label: 'Operations' } },
      ]),
    )

    expect(tree[0].labelClaims).toHaveLength(1)
  })

  it('lets a shell-owned band keep its own name whatever is claimed', () => {
    const tree = treeOf(
      navPlugin('alpha', [{ label: 'A', group: { id: 'features', label: 'Alpha Says' } }]),
    )

    expect(tree[0].label).toBe('Features')
    expect(tree[0].labelClaims.map((c) => c.label)).toEqual(['Alpha Says'])
  })

  it('makes no claim for an entry that asked for no group', () => {
    const tree = treeOf(navPlugin('alpha', [{ label: 'A' }]))

    expect(tree[0].labelClaims).toEqual([])
  })
})

describe('BR-AS86 — the projection changes nothing else', () => {
  it('leaves the flat navigation list exactly as it was', () => {
    const registry = build().index([
      navPlugin('zulu', [{ label: 'Z', group: 'ops' }]),
      navPlugin('alpha', [{ label: 'A' }]),
    ])
    const flat = registry.navigation.map((n) => n.qualifiedId)

    registry.navigationTree

    expect(registry.navigation.map((n) => n.qualifiedId)).toEqual(flat)
  })

  it('refuses nothing on the way through', () => {
    const registry = build().index([navPlugin('alpha', [{ label: 'A', group: 'ops' }])])

    expect(registry.navigationTree).toHaveLength(1)
    expect(registry.refusals).toEqual([])
  })

  it('names Features as the one band the shell owns', () => {
    expect(SHELL_GROUP_ORDER.map((g) => [g.id, g.label])).toEqual([['features', 'Features']])
  })
})
