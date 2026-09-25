/*
  BR-AS89 — the rail is the merged tree, as data.

  These run against `shellNavSections` rather than against a mounted rail: the
  shape is what `NavList` is handed, and asserting it here means a change to
  the rail's ORDER or its MARKS is caught without a DOM.
*/
import { describe, expect, it } from 'vitest'

import { createPermissionEvaluator } from '../auth/permissions.js'
import { createContributionRegistry } from '../contributions/contributionRegistry.js'
import { declareShellExtensionPoints } from '../extensions/extensionPoints.js'
import { validateManifest } from '../registry/manifestSchema.js'
import { PLUGIN_STATUS } from '../registry/pluginStatus.js'
import { HEALTH_STATE } from '../registry/healthPlane.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { clashesFor, SHELL_SECTION_ID, shellNavSections } from './shellNavSections.js'

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

const navPlugin = (id, entries) =>
  plugin(
    id,
    entries.flatMap((entry, index) => [
      { kind: 'route', id: `r${index}`, path: `/${id}/r${index}`, title: `r${index}` },
      {
        kind: 'navigation',
        id: `n${index}`,
        label: entry.label,
        route: `r${index}`,
        ...(entry.icon === undefined ? {} : { icon: entry.icon }),
        ...(entry.group === undefined ? {} : { group: entry.group }),
        ...(entry.order === undefined ? {} : { order: entry.order }),
      },
    ]),
  )

const registry = (...plugins) =>
  createContributionRegistry({
    extensionPoints: declareShellExtensionPoints(),
    permissions: createPermissionEvaluator({ permissions: ['*'] }),
  }).index(plugins)

const sectionsOf = (indexed, context = {}) =>
  shellNavSections(indexed.navigationTree, { clashes: indexed.navigationClashes, ...context })

const shape = (sections) =>
  sections.map((section) => [section.id, section.items.map((item) => item.label)])

describe('BR-AS89 — the shell band', () => {
  it('comes first, and no plugin can reach it', () => {
    const [first] = sectionsOf(registry(navPlugin('demo', [{ label: 'One' }])))

    expect(first.id).toBe(SHELL_SECTION_ID)
    expect(first.eyebrow).toBe('Shell')
    expect(first.items.map((item) => item.label)).toEqual(['Home', 'Plugins'])
  })

  it('is still there when no plugin contributed anything', () => {
    expect(shape(sectionsOf(registry()))).toEqual([['shell', ['Home', 'Plugins']]])
  })

  it('links the shell screens by PATH, not by route name', () => {
    const [shell] = sectionsOf(registry())
    const [home, plugins] = shell.items

    expect(home.to).toBe('/')
    expect(plugins.to).toBe('/plugins')
  })

  it('badges Plugins with the inventory size', () => {
    const [shell] = sectionsOf(registry(), { inventoryCount: 4 })

    expect(shell.items[1].badge).toBe(4)
    expect(shell.items[1].mark).toBeNull()
  })

  it('gives the badge up for the dot when something needs attention', () => {
    const attention = { count: 2, tone: 'err', label: '2 need attention' }
    const [shell] = sectionsOf(registry(), { inventoryCount: 4, attention })

    expect(shell.items[1].badge).toBeNull()
    expect(shell.items[1].mark).toEqual({
      tone: 'err',
      title: '2 need attention',
      description: '2 need attention',
    })
  })
})

describe('BR-AS89 — the plugin bands are the merged tree', () => {
  it('puts ungrouped entries in Features, under the shell band', () => {
    const indexed = registry(navPlugin('demo', [{ label: 'One' }, { label: 'Two' }]))

    expect(shape(sectionsOf(indexed))).toEqual([
      ['shell', ['Home', 'Plugins']],
      ['features', ['One', 'Two']],
    ])
  })

  it('renders the worked example of the phase — two plugins, one band each way', () => {
    const indexed = registry(
      navPlugin('p1', [
        { label: 'Lesson 1', group: { id: 'jetstream', label: 'JETSTREAM' }, order: 10 },
        { label: 'Lesson 2', group: { id: 'jetstream', label: 'JETSTREAM' }, order: 20 },
      ]),
      navPlugin('p2', [
        { label: 'Lesson 3', group: { id: 'jetstream', label: 'JETSTREAM' }, order: 30 },
        { label: '3 NATS Cluster', group: { id: 'topology', label: 'TOPOLOGY' }, order: 10 },
      ]),
    )

    expect(shape(sectionsOf(indexed))).toEqual([
      ['shell', ['Home', 'Plugins']],
      ['jetstream', ['Lesson 1', 'Lesson 2', 'Lesson 3']],
      ['topology', ['3 NATS Cluster']],
    ])
  })

  it('names a route, never a path (BR-AS12)', () => {
    const indexed = registry(navPlugin('demo', [{ label: 'One' }]))
    const [item] = sectionsOf(indexed)[1].items

    expect(item.to).toEqual({ name: 'demo/r0' })
    expect(item.key).toBe('demo/n0')
  })

  it('turns an icon CLASS into something NavList can render', () => {
    const indexed = registry(navPlugin('demo', [{ label: 'One', icon: 'pi pi-gauge' }]))
    const [item] = sectionsOf(indexed)[1].items

    expect(item.icon).toBeTruthy()
    expect(item.icon.render).toBeTypeOf('function')
  })

  it('leaves the icon out when the entry named none', () => {
    const indexed = registry(navPlugin('demo', [{ label: 'One' }]))

    expect(sectionsOf(indexed)[1].items[0].icon).toBeNull()
  })

  it('renders no band with nothing in it (BR-AS56)', () => {
    const indexed = registry(navPlugin('demo', [{ label: 'One' }]))
    indexed.withdraw('demo')

    expect(shape(sectionsOf(indexed))).toEqual([['shell', ['Home', 'Plugins']]])
  })

  it('brings the band back when the plugin is restored', () => {
    const indexed = registry(navPlugin('demo', [{ label: 'One' }]))
    indexed.withdraw('demo')
    indexed.restore('demo')

    expect(shape(sectionsOf(indexed))).toEqual([
      ['shell', ['Home', 'Plugins']],
      ['features', ['One']],
    ])
  })

  /* Phase 17's acceptance check, the case a one-plugin band cannot show: a
     MERGED band loses one of its two owners. The band must survive with the
     survivor's items still in it — emptying it would take a working plugin's
     navigation away because a different plugin went (task 17i). */
  const merged = () => registry(
    navPlugin('alpha', [{ label: 'Vessels', group: { id: 'ops', label: 'Operations' }, order: 10 }]),
    navPlugin('bravo', [{ label: 'Berths', group: { id: 'ops', label: 'Operations' }, order: 20 }]),
  )

  it('keeps a merged band when only one of its two plugins withdraws', () => {
    const indexed = merged()
    indexed.withdraw('bravo')

    expect(shape(sectionsOf(indexed))).toEqual([
      ['shell', ['Home', 'Plugins']],
      ['ops', ['Vessels']],
    ])
  })

  it('takes the band away only when the LAST of its owners goes', () => {
    const indexed = merged()
    indexed.withdraw('bravo')
    indexed.withdraw('alpha')

    expect(shape(sectionsOf(indexed))).toEqual([['shell', ['Home', 'Plugins']]])
  })

  it('leaves no dead link behind: the withdrawn plugin\'s item is gone, not disabled', () => {
    const indexed = merged()
    indexed.withdraw('bravo')
    const [, band] = sectionsOf(indexed)

    expect(band.items.map((item) => item.label)).not.toContain('Berths')
    expect(band.items.every((item) => item.to)).toBe(true)
  })

  it('restores the withdrawn owner back into the band it shares', () => {
    const indexed = merged()
    indexed.withdraw('bravo')
    indexed.restore('bravo')

    expect(shape(sectionsOf(indexed))).toEqual([
      ['shell', ['Home', 'Plugins']],
      ['ops', ['Vessels', 'Berths']],
    ])
  })

  /* The band's NAME is a claim, and the claim cascade picks it from the
     plugins still standing. A band that kept a withdrawn plugin's label would
     be naming something no longer there. */
  it('re-picks the band label from the owners that remain', () => {
    const indexed = registry(
      navPlugin('alpha', [{ label: 'Vessels', group: { id: 'ops', label: 'Operations' } }]),
      navPlugin('bravo', [{ label: 'Berths', group: { id: 'ops', label: 'OPS' } }]),
    )
    expect(sectionsOf(indexed)[1].eyebrow).toBe('Operations')

    indexed.withdraw('alpha')

    expect(sectionsOf(indexed)[1].eyebrow).toBe('OPS')
  })
})

describe('BR-AS89 — an entry stays lit on its own detail pages', () => {
  /* The rail before phase 17 matched the path AND everything under it. The
     router matches route records, which are siblings here, so the shell
     keeps deciding this itself (review, 2026-09-24). */
  const lit = (currentPath) => {
    const indexed = registry(navPlugin('demo', [{ label: 'One' }, { label: 'Two' }]))
    const sections = sectionsOf(indexed, {
      currentPath,
      pathOf: (to) => `/demo/${to.name.split('/')[1]}`,
    })
    return sections[1].items.filter((item) => item.active).map((item) => item.label)
  }

  it('lights the entry you are standing on', () => {
    expect(lit('/demo/r0')).toEqual(['One'])
  })

  it('stays lit on a page UNDER the entry', () => {
    expect(lit('/demo/r0/lesson-3')).toEqual(['One'])
  })

  it('lights nothing on an unrelated page', () => {
    expect(lit('/plugins')).toEqual([])
  })

  it('does not light a mere prefix of another path', () => {
    expect(lit('/demo/r00')).toEqual([])
  })

  it('leaves it to the router when the caller gave no path', () => {
    const indexed = registry(navPlugin('demo', [{ label: 'One' }]))

    expect(sectionsOf(indexed)[1].items[0].active).toBeNull()
  })

  it('leaves the shell band to the router, which is exact', () => {
    const [shell] = sectionsOf(registry(), { currentPath: '/plugins/demo' })

    expect(shell.items.every((item) => item.active === undefined)).toBe(true)
  })
})

describe('BR-AS89 — a mark is this plugin`s own, never a sibling`s', () => {
  const two = () => registry(
    navPlugin('good', [{ label: 'Good' }]),
    navPlugin('bad', [{ label: 'Bad' }]),
  )

  it('dots only the entry whose plugin failed (BR-AS04)', () => {
    const items = sectionsOf(two(), {
      statusOf: (id) => (id === 'bad' ? PLUGIN_STATUS.FAILED : PLUGIN_STATUS.ACTIVE),
    })[1].items
    const marks = Object.fromEntries(items.map((item) => [item.label, item.mark?.tone ?? null]))

    expect(marks).toEqual({ Bad: 'err', Good: null })
  })

  it('dots only the entry whose dependency is down', () => {
    const down = { frontend: { state: HEALTH_STATE.UNAVAILABLE }, backend: { state: HEALTH_STATE.HEALTHY } }
    const items = sectionsOf(two(), {
      statusOf: () => PLUGIN_STATUS.ACTIVE,
      healthOf: (id) => (id === 'bad' ? down : null),
    })[1].items

    expect(items.find((item) => item.label === 'Bad').mark.tone).toBe('warn')
    expect(items.find((item) => item.label === 'Good').mark).toBeNull()
  })
})

describe('BR-AS89 — a clash marks BOTH entries that caused it', () => {
  const conflicting = () => registry(
    navPlugin('zulu', [{ label: 'Z', group: { id: 'ops', label: 'Zulu Ops' } }]),
    navPlugin('alpha', [{ label: 'A', group: { id: 'ops', label: 'Alpha Ops' } }]),
  )

  it('marks each claiming entry', () => {
    const [band] = sectionsOf(conflicting()).slice(1)

    expect(band.items.map((item) => item.mark?.tone)).toEqual(['warn', 'warn'])
  })

  it('puts the diagnostic in the description, not in the colour alone', () => {
    const [band] = sectionsOf(conflicting()).slice(1)

    for (const item of band.items) {
      expect(item.mark.description).toContain('Navigation group ops')
    }
  })

  it('marks the two entries of a duplicate CHILD label, and nobody else', () => {
    const indexed = registry(
      navPlugin('one', [{ label: 'Same' }, { label: 'Other' }]),
      navPlugin('two', [{ label: 'Same' }]),
    )
    const items = sectionsOf(indexed)[1].items
    const marked = items.filter((item) => item.mark).map((item) => item.label)

    expect(marked).toEqual(['Same', 'Same'])
    expect(items.find((item) => item.label === 'Other').mark).toBeNull()
  })

  it('marks a plugin that AGREED with a label that lost', () => {
    /* A and B both say JETSTREAM, C says Streams. All three take part: B
       loses the same name A did, and an unmarked B is a silent rail. */
    const indexed = registry(
      navPlugin('alpha', [{ label: 'A', group: { id: 'ops', label: 'JETSTREAM' } }]),
      navPlugin('bravo', [{ label: 'B', group: { id: 'ops', label: 'JETSTREAM' } }]),
      navPlugin('charlie', [{ label: 'C', group: { id: 'ops', label: 'Streams' } }]),
    )
    const items = sectionsOf(indexed)[1].items

    expect(items.map((item) => item.mark?.tone)).toEqual(['warn', 'warn', 'warn'])
    for (const item of items) {
      expect(item.mark.description).toContain('Navigation group ops')
    }
  })

  it('leaves an agreed label unmarked', () => {
    const indexed = registry(
      navPlugin('one', [{ label: 'A', group: { id: 'ops', label: 'Ops' } }]),
      navPlugin('two', [{ label: 'B', group: { id: 'ops', label: 'Ops' } }]),
    )

    expect(sectionsOf(indexed)[1].items.every((item) => item.mark === null)).toBe(true)
  })

  it('lets a failure take the dot, and keeps the clash in the description', () => {
    const band = sectionsOf(conflicting(), {
      statusOf: (id) => (id === 'zulu' ? PLUGIN_STATUS.FAILED : PLUGIN_STATUS.ACTIVE),
    })[1]
    const zulu = band.items.find((item) => item.label === 'Z')

    expect(zulu.mark.tone).toBe('err')
    expect(zulu.mark.description).toContain('Navigation group ops')
  })
})

describe('clashesFor', () => {
  const entry = { qualifiedId: 'demo/nav-1' }

  it('matches by qualified id, so one clashing label marks one entry', () => {
    const clashes = [
      { kind: 'a', participants: [{ qualifiedId: 'demo/nav-1' }, { qualifiedId: 'other/nav-1' }] },
      { kind: 'b', participants: [{ qualifiedId: 'demo/nav-2' }] },
    ]

    expect(clashesFor(clashes, entry).map((clash) => clash.kind)).toEqual(['a'])
  })

  it('ignores a participant that is the shell itself and has no entry', () => {
    const clashes = [{ kind: 'a', participants: [{ qualifiedId: null, pluginId: null }] }]

    expect(clashesFor(clashes, entry)).toEqual([])
  })

  it('returns nothing when there are no clashes at all', () => {
    expect(clashesFor([], entry)).toEqual([])
  })
})

/*
  Phase 17's acceptance check: a nav entry whose route was refused leaves a
  visible diagnostic and NO clickable dead link.

  Both halves in one place, because each on its own is a half-truth. Dropping
  the entry silently would be a screen that quietly loses navigation; keeping
  it would be a link to nowhere. The shell does neither: the item is gone from
  the rail and the reason is on the record (task 17i).
*/
describe('BR-AS87 — a nav entry pointing at a route nobody placed', () => {
  const dangling = () => plugin('alpha', [
    { kind: 'route', id: 'good', path: '/alpha/good', title: 'Good' },
    { kind: 'navigation', id: 'ok', label: 'Good', route: 'good' },
    /* No route with this id was ever declared. */
    { kind: 'navigation', id: 'broken', label: 'Ghost', route: 'missing' },
  ])

  it('draws no item for it, so there is nothing to click', () => {
    const indexed = registry(dangling())

    expect(shape(sectionsOf(indexed))).toEqual([
      ['shell', ['Home', 'Plugins']],
      ['features', ['Good']],
    ])
  })

  it('leaves every item that IS drawn with a real destination', () => {
    const indexed = registry(dangling())
    const items = sectionsOf(indexed).flatMap((section) => section.items)

    expect(items.every((item) => item.to || item.action)).toBe(true)
  })

  it('says so in the refusals, naming the entry and the cause', () => {
    const { refusals } = registry(dangling())
    const refused = refusals.find((r) => r.qualifiedId === 'alpha/broken')

    expect(refused).toBeTruthy()
    expect(refused.code).toBe('unresolved-route')
  })

  it('costs that entry and nothing else — the plugin is still placed', () => {
    const indexed = registry(dangling())

    expect(indexed.refusals).toHaveLength(1)
    expect(sectionsOf(indexed)[1].items[0].label).toBe('Good')
  })

  /* A refusal is not a clash. It must not be reported through the channel
     D17-5 built for things the shell DID place (BR-AS87). */
  it('reports no clash for it', () => {
    expect(registry(dangling()).navigationClashes).toEqual([])
  })
})

/*
  The drawing at the head of phase 17, as a spec.

      MFE plugin 1 contributes      MFE plugin 2 contributes       The shell renders
      - JETSTREAM                   - JETSTREAM                    - JETSTREAM
        - Lesson 1                    - Lesson 3                     - Lesson 1
        - Lesson 2                  - TOPOLOGY                       - Lesson 2
                                      - 3 NATS Cluster               - Lesson 3
                                                                   - TOPOLOGY
                                                                     - 3 NATS Cluster

  The phase's acceptance list asks for this exact picture, so here it is with
  nothing added and nothing left out (task 17i). The parity spec next door
  proves the same tree comes out of either catalogue source; this one proves
  the tree is the one that was drawn.
*/
describe('phase 17 — the two-plugin example renders exactly as drawn', () => {
  const jetstream = { id: 'jetstream', label: 'JetStream' }
  const topology = { id: 'topology', label: 'Topology' }

  const drawn = () => registry(
    navPlugin('plugin-one', [
      { label: 'Lesson 1', group: jetstream, order: 10 },
      { label: 'Lesson 2', group: jetstream, order: 20 },
    ]),
    navPlugin('plugin-two', [
      { label: 'Lesson 3', group: jetstream, order: 30 },
      { label: '3 NATS Cluster', group: topology, order: 10 },
    ]),
  )

  it('draws one JETSTREAM band holding all three lessons, then TOPOLOGY', () => {
    expect(shape(sectionsOf(drawn()))).toEqual([
      ['shell', ['Home', 'Plugins']],
      ['jetstream', ['Lesson 1', 'Lesson 2', 'Lesson 3']],
      ['topology', ['3 NATS Cluster']],
    ])
  })

  it('shows the band once, under the label both plugins agreed on', () => {
    const [, band] = sectionsOf(drawn())

    expect(band.eyebrow).toBe('JetStream')
  })

  it('keeps each item owned by the plugin that gave it', () => {
    const [, band] = sectionsOf(drawn())

    /* The item's key is its qualified id, so the owner is written into it
       (BR-AS06) — a merged band never loses track of who gave it what. */
    expect(band.items.map((item) => item.key))
      .toEqual(['plugin-one/n0', 'plugin-one/n1', 'plugin-two/n0'])
  })

  /* Nothing is wrong here, so nothing is marked. A mark on a clean merge
     would train the reader to ignore marks. */
  it('marks nothing, because the two plugins agree', () => {
    expect(drawn().navigationClashes).toEqual([])
  })
})
