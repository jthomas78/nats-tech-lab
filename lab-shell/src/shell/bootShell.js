/*
  Boot: discovery -> validation -> indexing (BR-AS01, AS04, AS08, AS13).

  This is the only place the pieces meet, and it is deliberately a function
  rather than a singleton — a spec boots a shell per test, and the Phase 1b
  example plugin's failure switches need to boot one with a broken registry on
  demand.

  Plugins are indexed in curated document order. With no registry, the
  native Home and Plugins frame remains usable without loading any plugin.

  Nothing here loads code. A booted shell has a complete nav tree, a complete
  route table and zero remote chunks fetched — that is BR-AS08, and it is
  assertable precisely because loading lives in the loader and is never
  reached from this path.
*/

import { reactive, ref } from 'vue'

import { createContributionRegistry } from './contributions/contributionRegistry.js'
import { declareShellExtensionPoints } from './extensions/extensionPoints.js'
import { validateManifest } from './registry/manifestSchema.js'
import { PLUGIN_STATUS, PluginStatusRecord } from './registry/pluginStatus.js'
import { decideRead } from './registry/readPolicy.js'
import { RemoteAllowlist } from './registry/remoteAllowlist.js'

/**
 * @param {object} options
 * @param {{fetchRegistry(): Promise<object>}} [options.registryClient] optional fixture read; the host discovers after paint
 * @param {{can(permission: string|null): boolean}} options.permissions
 * @param {import('./extensions/extensionPoints.js').ExtensionPointRegistry} [options.extensionPoints]
 */
export async function bootShell({
  registryClient,
  permissions,
  extensionPoints = declareShellExtensionPoints(),
}) {
  /* Reactive for the same reason the contribution arrays are (decision 47):
     App.vue resolves a breadcrumb through `shell.statuses.get(id)`, and the
     Plugins screen counts from `inventory`, which iterates this map. A plain
     Map registers no dependency, so a plugin admitted into a running shell
     stayed invisible to both. The records inside were already reactive; the
     container holding them was not. */
  const statuses = reactive(new Map())
  const allowlist = new RemoteAllowlist()
  const plugins = []
  /* The same admitted manifests, keyed for lookup. Maintained here rather
     than rebuilt by the host, because a second index built from `plugins`
     after every read is a second thing that can be stale — and was: the host
     kept a Map under the same name `plugins`, so a component could not tell
     from the name whether it held the array or the index (decision 47's
     mistake, one level up). One collection, two accessors. */
  const byId = new Map()
  const registryError = ref(null)
  /* Reactive, because these three are on screen (the footer's revision, the
     banner's degraded note) and BR-AS19 lets them move while the shell runs. */
  const registry = reactive({ revision: null, fetchedAt: null, degraded: false, heldRevision: null })
  /* Changes the shell may not apply to itself: a withdrawn entry or a moved
     remote (decision 25). Offered here, applied only by a reload — there is
     no transition out of `active`, so tearing a mounted plugin down is not a
     move the state machine has. */
  const pendingReload = reactive([])

  const admit = (raw) => {
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
    byId.set(result.plugin.id, result.plugin)
  }

  const contributions = createContributionRegistry({ extensionPoints, permissions })

  /*
    One path for the first read and every later one (BR-AS19). Boot is not a
    special case with its own rules — it is simply the read where everything
    is an addition, which is why the live path gets exercised on every boot
    rather than only when a registry happens to change.

    Never throws, and never tears anything down: additions are indexed,
    everything else is *offered*.
  */
  const raiseReload = (changes) => {
    for (const change of changes) {
      if (!pendingReload.some((p) => p.id === change.id && p.reason === change.reason)) {
        pendingReload.push(change)
      }
    }
  }

  /* Raise, then retract what this read no longer supports. Only a read the
     service vouched for may retract — see the reasoning at the call site —
     which is why the degraded branch calls raiseReload alone. */
  const syncPendingReload = (reloadRequired) => {
    raiseReload(reloadRequired)
    for (let i = pendingReload.length - 1; i >= 0; i--) {
      const held = pendingReload[i]
      if (!reloadRequired.some((c) => c.id === held.id && c.reason === held.reason)) {
        pendingReload.splice(i, 1)
      }
    }
  }

  const applyWithdrawals = (changes) => {
    for (const change of changes) contributions.withdraw(change.id, statuses)
    return changes
  }

  /*
    Decide, then do. Every rule about what a read MEANS — degraded, 304,
    failed, a document — lives in `decideRead`, which is pure and specced on
    its own; what is left here is the doing, and it is deliberately dull:
    install the state the policy returned, index what it says is new, offer
    what it says needs a reload, apply what it says was taken away.

    The one thing this function still decides is ORDER, because order is an
    effect: routes are measured before admission so only the new ones are
    handed back.
  */
  const applyRegistry = (discovery) => {
    const decision = decideRead(discovery, {
      current: registry,
      running: plugins,
      isWithdrawn: (id) => contributions.isWithdrawn(id),
    })

    registryError.value = decision.error
    Object.assign(registry, decision.registry)

    let addedRoutes = []
    if (decision.added.length > 0 || decision.outcome === 'document') {
      const before = new Set(contributions.routes.map((route) => route.qualifiedId))
      for (const manifest of decision.added) admit(manifest)
      contributions.index(plugins, statuses)
      /* Only the routes this read placed. The caller adds them to a router
         that already holds the rest, so handing back all of them would
         re-register every route on every change. */
      addedRoutes = contributions.routes.filter((route) => !before.has(route.qualifiedId))
    }

    /* Sync retracts an offer this read no longer supports; raise cannot.
       Which one a read earns is the policy's call, not this function's — see
       `retract` in readPolicy.js. Retraction is not a nicety: an operator who
       disabled an entry and re-enabled it seconds later left every running
       shell insisting a plugin it was still rendering had been withdrawn.
       Nothing here applies anything — the offer is withdrawn, never the
       plugin (decision 25 / BR-AS19). */
    if (decision.retract) syncPendingReload(decision.reloadRequired)
    else raiseReload(decision.reloadRequired)

    /* Applied, not offered — the one catalogue change other than an addition
       that a running shell may act on (BR-AS56). Taking UI away cannot leave
       two versions of one plugin in one page, which is what every reload
       offer above exists to prevent. */
    applyWithdrawals(decision.withdrawn)
    for (const change of decision.restored) contributions.restore(change.id, statuses)

    return {
      added: decision.added,
      withdrawn: decision.withdrawn,
      restored: decision.restored,
      addedRoutes,
      reloadRequired: decision.reloadRequired,
    }
  }

  // Live discovery starts after the native frame paints. An injected client
  // supports isolated/offline fixtures through the same admission path.
  if (registryClient) applyRegistry(await registryClient.fetchRegistry())

  return {
    plugins,
    /* The one lookup every renderer needs: contribution -> the manifest that
       supplied it. Null for an id the shell never admitted, which is a real
       answer (an incompatible plugin has a status but no manifest) and not a
       missing-collaborator crash. */
    manifestFor: (id) => byId.get(id) ?? null,
    statuses,
    contributions,
    extensionPoints,
    allowlist,
    registry,
    get registryError() {
      return registryError.value
    },
    /* Offered, never applied (decision 25 / BR-AS19). */
    pendingReload,
    applyRegistry,
    /* Everything the Plugins screen renders, in one shape (see the Plugins
       artboard): a row per plugin with its status and reason, plus the
       contribution-level refusals that a plugin-level status cannot express. */
    get inventory() {
      const manifestOf = (id) => byId.get(id) ?? null
      return [...statuses.values()].map((record) => ({
        id: record.id,
        name: record.name,
        version: manifestOf(record.id)?.version ?? null,
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
