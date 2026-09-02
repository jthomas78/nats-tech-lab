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
import { diffRegistry, RELOAD_REASON } from './registry/registryDiff.js'
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

  /* The tombstones in a document, against what the shell is running.
     Deliberately NOT diffRegistry: that function reads an absent id as a
     removal, which is right for a document the service vouched for and wrong
     for a stale one. Here the only signal taken is the presence of a
     tombstone — never the absence of an entry. */
  const revocationsIn = (entries = []) => {
    const running = new Set(plugins.map((p) => p.id))
    return (entries ?? [])
      .filter((e) => e?.withheld === true && running.has(e.id))
      .map((e) => ({
        id: e.id,
        name: plugins.find((p) => p.id === e.id)?.name ?? e.id,
        reason: RELOAD_REASON.REVOKED,
        forced: true,
      }))
  }

  /* The withdrawal markers in a document, against what the shell is running
     (BR-AS54). Read on the same terms as a tombstone: only the PRESENCE of a
     marker is taken, never the absence of an entry, because a filtered,
     degraded or failed read is also an absence. */
  const withdrawalsIn = (entries = []) =>
    (entries ?? [])
      .filter((e) => e?.withdrawn === true && e?.withheld !== true)
      .filter((e) => plugins.some((p) => p.id === e.id))
      .map((e) => ({ id: e.id, name: plugins.find((p) => p.id === e.id)?.name ?? e.id }))

  const applyWithdrawals = (changes) => {
    for (const change of changes) contributions.withdraw(change.id, statuses)
    return changes
  }

  const applyRegistry = (discovery) => {
    if (!discovery?.ok) {
      /* Recorded, not thrown. The native shell frame remains usable, and the
         Plugins screen shows why the remote list is empty. */
      registryError.value = { code: discovery?.code ?? 'registry-malformed', message: discovery?.message ?? '' }
      /* Same rule as the degraded branch (decision 48): a read the shell
         could not complete leaves it unable to say what the server would
         honour, so the next one asks unconditionally rather than betting the
         recovery on a token from before the outage. */
      registry.heldRevision = null
      return { added: [], addedRoutes: [], reloadRequired: [], withdrawn: [], restored: [] }
    }

    registryError.value = null
    registry.fetchedAt = discovery.fetchedAt ?? registry.fetchedAt
    /* Cleared on ANY successful read, a 304 included, and therefore before
       the `unchanged` guard below rather than after it (decision 48). A 304
       is positive evidence the service is answering; leaving the flag set
       through one made degraded a one-way door, because recovery at the same
       revision is exactly what answers 304. */
    registry.degraded = discovery.degraded === true
    /* BR-AS22: an empty document that says it is degraded is not the same
       claim as an empty registry, and the native frame remains usable either
       way. A degraded read is therefore never read as "everything was
       withdrawn" — diffing it would offer a reload for every remote plugin
       the shell is running, on the strength of a document the service already
       said it could not vouch for. */
    if (registry.degraded) {
      /* And the conditional token goes with it. A degraded response carries
         no revision it stands behind, so keeping the pre-outage one would have
         the shell ask
         "anything newer than the revision I hold?" and be told no — for a
         document the service never served it. The next read is unconditional
         and the answer is a real document. */
      registry.heldRevision = null
      /* One thing IS taken from a degraded document: a tombstone (BR-AS49).
         The reasoning above is that a stale document cannot be trusted to
         say what exists — but it can be trusted to say what was withdrawn,
         because cache writes are monotonic (BR-AS51) and withdrawal is the
         safe direction to be wrong in. A revocation that arrived just before
         Postgres went down must not wait out the outage. */
      /* Raised, never synced. A degraded document is not evidence that
         anything was taken back (decision 48), so an offer standing from a
         healthy read must survive the outage. */
      const withheld = revocationsIn(discovery.plugins)
      raiseReload(withheld)
      /* A withdrawal is taken from a degraded document for the same reason a
         tombstone is: the shell cannot trust a stale document to say what
         exists, but it can trust it to say what was taken away, and away is
         the safe direction to be wrong in. No RETURN is ever taken from one —
         putting a plugin back needs a document the service vouched for. */
      return {
        added: [],
        addedRoutes: [],
        reloadRequired: withheld,
        withdrawn: applyWithdrawals(withdrawalsIn(discovery.plugins)),
        restored: [],
      }
    }

    /* Kept beside the revision so the watcher can pick the conditional read
       up from wherever the shell left off — including a boot read it did not
       make itself. */
    registry.heldRevision = discovery.heldRevision ?? registry.heldRevision
    /* A 304 carries no document. Nothing to diff, nothing to place — and
       deliberately no clearing of what is already on screen. */
    if (discovery.unchanged) {
      return { added: [], addedRoutes: [], reloadRequired: [], withdrawn: [], restored: [] }
    }

    registry.revision = discovery.revision ?? null

    const { added, reloadRequired, withdrawn, restored } = diffRegistry(
      plugins,
      discovery.plugins,
      { isWithdrawn: (id) => contributions.isWithdrawn(id) },
    )
    const before = new Set(contributions.routes.map((route) => route.qualifiedId))
    for (const manifest of added) admit(manifest)
    contributions.index(plugins, statuses)

    /* Synced with this read, not accumulated across reads. `diffRegistry` is a
       pure comparison of the document against the manifests the shell is
       running, and the running set only ever grows — so a change that still
       holds is produced again by every later read, and one this read did not
       produce is one the document no longer supports.

       Retraction is what that buys, and it is not a nicety: an operator who
       disabled an entry and re-enabled it seconds later left every running
       shell insisting a plugin it was still rendering had been withdrawn.
       Nothing here applies anything — the offer is withdrawn, never the
       plugin (decision 25 / BR-AS19). */
    syncPendingReload(reloadRequired)

    /* Applied, not offered — the one catalogue change other than an addition
       that a running shell may act on (BR-AS56). Taking UI away cannot leave
       two versions of one plugin in one page, which is what every reload
       offer above exists to prevent. */
    applyWithdrawals(withdrawn)
    for (const change of restored) contributions.restore(change.id, statuses)

    return {
      added,
      withdrawn,
      restored,
      /* Only the routes this read placed. The caller adds them to a router
         that already holds the rest, so handing back all of them would
         re-register every route on every change. */
      addedRoutes: contributions.routes.filter((route) => !before.has(route.qualifiedId)),
      reloadRequired,
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
