/*
  Boot: discovery -> validation -> indexing (BR-AS01, AS04, AS08, AS13).

  This is the only place the pieces meet, and it is deliberately a function
  rather than a singleton — a spec boots a shell per test, and the Phase 1b
  example plugin's failure switches need to boot one with a broken registry on
  demand.

  The ordering is the contract. Built-in plugins are indexed first and
  unconditionally: they are in the shell's own bundle, so they are available
  whether or not accounts-service answers, and a shell that renders nothing
  when its registry is down would fail BR-AS04 at the first hop. Remote
  plugins are then appended in the order the operator curated them, which is
  what makes cross-plugin ordering reproducible.

  Nothing here loads code. A booted shell has a complete nav tree, a complete
  route table and zero remote chunks fetched — that is BR-AS08, and it is
  assertable precisely because loading lives in the loader and is never
  reached from this path.
*/

import { reactive } from 'vue'

import { createContributionRegistry } from './contributions/contributionRegistry.js'
import { declareShellExtensionPoints } from './extensions/extensionPoints.js'
import { validateManifest } from './registry/manifestSchema.js'
import { PLUGIN_STATUS, PluginStatusRecord } from './registry/pluginStatus.js'
import { RemoteAllowlist } from './registry/registryClient.js'

/**
 * @param {object} options
 * @param {{fetchRegistry(): Promise<object>}} options.registryClient
 * @param {object[]} options.builtins manifests for plugins the shell bundles
 * @param {{can(permission: string|null): boolean}} options.permissions
 * @param {import('./extensions/extensionPoints.js').ExtensionPointRegistry} [options.extensionPoints]
 */
export async function bootShell({
  registryClient,
  builtins = [],
  permissions,
  extensionPoints = declareShellExtensionPoints(),
}) {
  const statuses = new Map()
  const allowlist = new RemoteAllowlist()
  const plugins = []
  let registryError = null

  const admit = (raw, { builtin }) => {
    const id = typeof raw?.id === 'string' ? raw.id : '<unnamed>'
    /* Reactive, because the Plugins screen is a live inventory: a plugin that
       fails on first use transitions long after boot, and a table that still
       said `available` next to a visibly broken feature would be worse than
       no table. The proxy tracks the record's own mutations, so the state
       machine itself stays a plain class with no framework in it. */
    const record = reactive(new PluginStatusRecord(id, { name: raw?.name ?? id }))
    /* A duplicate plugin id would give two plugins one status record and one
       nav namespace. The first wins; the second is reported rather than
       quietly shadowing it. */
    if (statuses.has(id)) {
      record.transition(PLUGIN_STATUS.INCOMPATIBLE, {
        code: 'duplicate-plugin-id',
        message: `A plugin with id ${id} is already registered`,
      })
      return
    }
    statuses.set(id, record)

    const result = validateManifest(raw)
    if (!result.ok) {
      record.transition(PLUGIN_STATUS.INCOMPATIBLE, {
        code: result.code,
        message: result.message,
      })
      return
    }
    if (builtin && result.plugin.remote.kind !== 'builtin') {
      record.transition(PLUGIN_STATUS.INCOMPATIBLE, {
        code: 'builtin-must-be-bundled',
        message: `Built-in plugin ${id} declares a ${result.plugin.remote.kind} remote`,
      })
      return
    }
    /* Regions the plugin owns are opened before anything is indexed, so a
       contribution can target `demo-catalog/details-sidebar/v1` whether or
       not the catalog itself has been loaded. A collision is the plugin's
       fault and only the second declarer pays for it. */
    for (const point of result.plugin.extensionPoints) {
      if (extensionPoints.has(point.id)) {
        record.transition(PLUGIN_STATUS.INCOMPATIBLE, {
          code: 'duplicate-extension-point',
          message: `Extension point ${point.id} is already declared`,
        })
        return
      }
    }
    for (const point of result.plugin.extensionPoints) extensionPoints.declare(point)

    allowlist.add(result.plugin)
    plugins.push(result.plugin)
  }

  for (const manifest of builtins) admit(manifest, { builtin: true })

  const discovery = await registryClient.fetchRegistry()
  /* What the shell read, so the Plugins screen and the footer can name the
     registry revision on screen. Never the endpoint URL (BR-AS04). */
  let registry = { revision: null, fetchedAt: null }
  if (discovery.ok) {
    registry = { revision: discovery.revision ?? null, fetchedAt: discovery.fetchedAt ?? null }
    for (const manifest of discovery.plugins) admit(manifest, { builtin: false })
  } else {
    /* Recorded, not thrown. The shell continues with its built-ins, and the
       Plugins screen shows why the remote list is empty. */
    registryError = { code: discovery.code, message: discovery.message }
  }

  const contributions = createContributionRegistry({ extensionPoints, permissions }).index(
    plugins,
    statuses,
  )

  return {
    plugins,
    statuses,
    contributions,
    extensionPoints,
    allowlist,
    registry,
    registryError,
    /* Everything the Plugins screen renders, in one shape (see the Plugins
       artboard): a row per plugin with its status and reason, plus the
       contribution-level refusals that a plugin-level status cannot express. */
    get inventory() {
      const manifestOf = (id) => plugins.find((plugin) => plugin.id === id) ?? null
      return [...statuses.values()].map((record) => ({
        id: record.id,
        name: record.name,
        version: manifestOf(record.id)?.version ?? null,
        builtin: manifestOf(record.id)?.remote?.kind === 'builtin',
        shellApiVersion: manifestOf(record.id)?.shellApiVersion ?? null,
        /* Placed contributions, not declared ones: a contribution the index
           refused is in `refusals`, and listing it here would tell the
           operator the plugin contributed something it does not. */
        contributionKinds: contributions.all
          .filter((c) => c.pluginId === record.id)
          .map((c) => c.kind),
        status: record.status,
        reasonCode: record.reasonCode,
        reason: record.reason,
        refusals: contributions.refusals.filter((r) => r.pluginId === record.id),
      }))
    },
  }
}

/**
 * Compose the booted shell with its runtime collaborators (loader, router)
 * into the one object the app provides.
 *
 * A spread would be wrong here, and silently so: `inventory` is a getter, and
 * `{...shell}` evaluates it once and copies the resulting array. The Plugins
 * screen would then render the inventory as it stood at boot forever — every
 * plugin `available`, including one the user just watched fail. Prototype
 * delegation keeps the getter live.
 */
export function withRuntime(shell, extras) {
  return Object.assign(Object.create(shell), extras)
}
