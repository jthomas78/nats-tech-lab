/* Build/registry parity — the acceptance check phase 17 named first.

   Every other navigation spec calls `contributionRegistry.index()` with
   already-admitted manifests, so it proves the MERGE is right and says
   nothing about how the manifests arrived. This file closes that: one set of
   raw manifests, carried to the registry over a NATS subject and to build
   mode over a fetched catalogue file, and the merged tree and the clash list
   compared at the end.

   It matters because the two sources differ in the one way that could bite.
   The registry hands back entries in the order an operator curated them; the
   build catalogue hands them back in the order a directory scan found them.
   If merging ever depended on arrival order, a plugin admitted in one source
   would sit in a different band — or lose a label fight it won in the other —
   and a lab would behave differently depending on how it was deployed. So the
   fixtures below are fed to the two clients in DELIBERATELY opposite orders,
   and the assertion is that the answer does not move (D17-3).

   `degraded` is where the two legitimately differ, and that is not parity's
   business: it describes the SERVICE, and build mode has no service. */
import { describe, expect, it } from 'vitest'

import { createPermissionEvaluator } from '../auth/permissions.js'
import { declareShellExtensionPoints } from '../extensions/extensionPoints.js'
import { validateManifest } from '../registry/manifestSchema.js'
import { createBuildCatalogueClient } from '../registry/buildCatalogueClient.js'
import { createRegistryTransport } from '../registry/registryTransport.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { createContributionRegistry } from './contributionRegistry.js'

/* A plugin's raw manifest, as a publisher writes it. Both sources carry this
   same object; neither is allowed to reshape it (BR-AS78). */
const manifest = (id, entries) => ({
  id,
  name: id,
  version: '1.0.0',
  schemaVersion: REGISTRY_SCHEMA_VERSION,
  shellApiVersion: SHELL_API_VERSION,
  routePrefix: id,
  remote: { kind: 'federated', url: `/plugins/${id}/remoteEntry.js`, name: id.replace(/-/g, '_'), module: 'plugin' },
  contributions: entries.flatMap((entry, index) => [
    { kind: 'route', id: `r${index}`, path: `/${id}/r${index}`, title: `R${index}`, component: 'default' },
    {
      kind: 'navigation',
      id: `n${index}`,
      label: entry.label,
      route: `r${index}`,
      ...(entry.group === undefined ? {} : { group: entry.group }),
      ...(entry.order === undefined ? {} : { order: entry.order }),
    },
  ]),
})

/* The worked example at the head of phase 17, plus the two clashes the phase
   promises to mark. One fixture set, so parity is asserted on a tree that has
   something to get wrong. */
const FIXTURES = [
  manifest('demo-04', [
    { label: 'Lesson 1', group: { id: 'jetstream', label: 'JetStream' }, order: 10 },
    { label: 'Lesson 2', group: { id: 'jetstream', label: 'JetStream' }, order: 20 },
  ]),
  manifest('demo-05', [
    /* Same band, a DIFFERENT name for it — clash case one. */
    { label: 'Lesson 3', group: { id: 'jetstream', label: 'JETSTREAM' }, order: 30 },
    { label: '3 NATS Cluster', group: { id: 'topology', label: 'Topology' }, order: 10 },
  ]),
  manifest('demo-06', [
    /* A different band wearing the same visible name — clash case two. */
    { label: 'Routes', group: { id: 'shapes', label: 'Topology' }, order: 10 },
  ]),
]

const indexed = (raws) => {
  const registry = createContributionRegistry({
    extensionPoints: declareShellExtensionPoints(),
    permissions: createPermissionEvaluator({ permissions: ['*'] }),
  })
  const plugins = raws.map((raw) => {
    const result = validateManifest(raw)
    if (!result.ok) throw new Error(`fixture is invalid: ${result.message}`)
    return result.plugin
  })
  return registry.index(plugins)
}

/* Curated by an operator, delivered over the read subject. */
const fromRegistry = async (raws) => {
  const transport = createRegistryTransport({
    request: async () => ({ ok: true, unchanged: false, revision: 7, entries: raws, degraded: false }),
  })
  const read = await transport.fetchRegistry()
  expect(read.ok).toBe(true)
  return read.plugins
}

/* Found by a directory scan, written into a file the shell's own build emits,
   fetched from the shell's own origin. */
const fromBuild = async (raws) => {
  const client = createBuildCatalogueClient({
    fetch: async () => ({
      ok: true,
      status: 200,
      json: async () => ({ schemaVersion: REGISTRY_SCHEMA_VERSION, revision: 'abc123', plugins: raws }),
    }),
  })
  const read = await client.fetchRegistry()
  expect(read.ok).toBe(true)
  return read.plugins
}

const shape = (tree) => tree.map((g) => [g.id, g.label, g.items.map((i) => [i.label, i.pluginId, i.to?.name ?? null])])
const reversed = [...FIXTURES].reverse()

describe('BR-AS86, D17-3 — the same manifests merge the same way from either source', () => {
  it('draws the same grouped tree, in the same order, from the registry and from build', async () => {
    const viaRegistry = indexed(await fromRegistry(FIXTURES))
    const viaBuild = indexed(await fromBuild(reversed))

    expect(shape(viaBuild.navigationTree)).toEqual(shape(viaRegistry.navigationTree))
  })

  it('reports the same clashes, with the same participants, from either source', async () => {
    const viaRegistry = indexed(await fromRegistry(FIXTURES))
    const viaBuild = indexed(await fromBuild(reversed))

    expect(viaBuild.navigationClashes).toEqual(viaRegistry.navigationClashes)
  })

  it('refuses nothing in one source that the other admitted', async () => {
    const viaRegistry = indexed(await fromRegistry(FIXTURES))
    const viaBuild = indexed(await fromBuild(reversed))

    expect(viaBuild.refusals).toEqual(viaRegistry.refusals)
  })

  /* The parity above is worth nothing if the tree it compares is empty or
     trivial. This is the tree BOTH sources produced, read out once, so a
     future edit that quietly drops a band fails here rather than passing
     parity by matching nothing against nothing.

     Band order is alphabetical by group ID, which is the last step of
     BR-AS86's cascade and the reason parity holds at all: neither source's
     arrival order can reach it. `shapes` therefore sits between `jetstream`
     and `topology`, however the fixtures were listed. */
  it('is comparing a real tree — bands, items and order', async () => {
    const { navigationTree, navigationClashes } = indexed(await fromRegistry(FIXTURES))

    expect(shape(navigationTree).map(([id, , items]) => [id, items.map(([label]) => label)])).toEqual([
      ['jetstream', ['Lesson 1', 'Lesson 2', 'Lesson 3']],
      ['shapes', ['Routes']],
      ['topology', ['3 NATS Cluster']],
    ])
    expect(navigationClashes.length).toBeGreaterThan(0)
  })
})
