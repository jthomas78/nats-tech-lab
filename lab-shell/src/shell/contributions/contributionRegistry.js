/*
  The contribution registry — where validated metadata becomes placed UI
  (BR-AS02, BR-AS06, BR-AS07).

  Indexing happens once per boot, over metadata only. Nothing here loads,
  imports or executes plugin code; a fully indexed shell has a complete nav
  tree, a complete route table and every extension slot accounted for, with
  zero remote chunks fetched. That gap is what BR-AS08 asserts, and keeping
  this module free of the loader is what makes it true rather than incidental.

  Every refusal is recorded rather than thrown. One plugin's bad contribution
  must not cost another plugin its nav entry (BR-AS04), and — less obviously —
  it must not cost the *same* plugin its other contributions: a plugin whose
  footer item targets a full region still gets its route. The Plugins screen
  reads `refusals` to explain what is missing and why.

  The collections below are reactive, and that is decision 47 rather than a
  convenience. Every getter here hands back a copy — `[...routes]` — so a
  reader's `computed(() => shell.contributions.navigation)` evaluated once
  over a plain array and registered ZERO dependencies: a plugin added to a
  running shell got its route record and never appeared in the nav. Tracking
  belongs at the source, because the copy is the whole point of the getter and
  no future reader should have to know the rule. The placement RULES stay
  framework-free — what is reactive is the container the rules write into,
  not the deciding.
*/

import { reactive } from 'vue'

import { PLUGIN_STATUS } from '../registry/pluginStatus.js'
import { decidePlacements } from './placementPolicy.js'

export function createContributionRegistry({ extensionPoints, permissions }) {
  const routes = reactive([])
  const navigation = reactive([])
  const extensions = reactive(new Map()) // point id -> contribution[]
  const shellControls = reactive([])
  const footerItems = reactive([])
  const refusals = reactive([])
  const byQualifiedId = reactive(new Map())
  /* Deliberately NOT reactive: bookkeeping the index reads to decide, never
     state a reader can see. Proxying it would cost every lookup and track a
     dependency nothing renders. */
  const routePrefixOwners = new Map() // prefix -> plugin id
  /* Indexing is no longer a once-per-boot event: BR-AS19 lets an entry added
     to the registry be placed into a running shell (decision 26). The arrays
     above append, so re-indexing a plugin already placed would duplicate its
     nav entry and its route. This is what makes index() incremental rather
     than idempotent-by-luck. */
  const indexedPluginIds = new Set()
  /* The validated manifest of every plugin this registry has placed, kept so
     a return can be re-placed from the same definition the shell is already
     running (BR-AS59). A return whose definition differs never reaches here —
     that is a reload, decided before the call. */
  const placedPlugins = new Map()
  const withdrawnPluginIds = reactive(new Set())
  /* Placements that are nobody's fault: a contribution aimed at a slot whose
     OWNER is withdrawn (BR-AS58). Held rather than refused, because the
     contributor is still running and the placement is owed back — once — when
     the slot returns. */
  const suspended = []

  const refuse = (contribution, code, message) => {
    refusals.push({
      qualifiedId: contribution.qualifiedId,
      pluginId: contribution.pluginId,
      kind: contribution.kind,
      code,
      message,
    })
  }

  const ownerOf = (pointId) => String(pointId ?? '').split('/')[0]

  /* Order is a property of the whole shell, not of one plugin, so it is
     settled after any batch of placement — an indexing pass or a return. */
  const resort = () => {
    routes.sort(byOrder)
    navigation.sort(byOrder)
    shellControls.sort(byOrder)
    footerItems.sort(byOrder)
    for (const [pointId, list] of extensions) extensions.set(pointId, [...list].sort(byOrder))
  }

  /* The one path that puts a contribution into a slot, used by an indexing
     pass and by a return alike, so capacity is re-checked the same way both
     times. Deciding is `placementPolicy`'s job — this only writes. */
  const fill = (contribution, pointId) => {
    const placed = extensions.get(pointId) ?? []
    placed.push(contribution)
    extensions.set(pointId, placed)
  }

  const hold = (pointId, contribution) => {
    if (suspended.some((s) => s.contribution.qualifiedId === contribution.qualifiedId)) return
    suspended.push({ pointId, contribution })
  }

  /*
    Decide, then do (BR-AS02, BR-AS06).

    `decidePlacements` answers the questions below and hands back a plan; this
    walks the plan and writes. Nothing here branches on a rule — if a
    placement rule ever needs changing, `placementPolicy.js` is the only file
    that moves.

    `reindex` is how a return re-decides honestly (BR-AS59). A restore has to
    get past the "already ruled on" guard, and it used to do that by deleting
    the plugin from `indexedPluginIds` first — briefly telling the registry a
    plugin it had placed was unknown. Now it says so.
  */
  const place = (plugins, statuses, { reindex = false } = {}) => {
    const plan = decidePlacements(plugins, {
      alreadyIndexed: (pluginId) => !reindex && indexedPluginIds.has(pluginId),
      permits: (permission) => permissions.can(permission),
      prefixOwner: (prefix) => routePrefixOwners.get(prefix),
      occupancy: (pointId) => (extensions.get(pointId) ?? []).length,
      accepts: (pointId, info) => extensionPoints.accepts(pointId, info),
      ownerWithdrawn: (owner) => withdrawnPluginIds.has(owner),
      routePlaced: (qualifiedId) => routes.some((route) => route.qualifiedId === qualifiedId),
    })

    for (const plugin of plan.indexed) {
      indexedPluginIds.add(plugin.id)
      placedPlugins.set(plugin.id, plugin)
    }
    for (const { prefix, pluginId } of plan.prefixClaims) routePrefixOwners.set(prefix, pluginId)

    const dropped = new Set(plan.dropNavigation)
    for (const { op, contribution, pointId } of plan.actions) {
      switch (op) {
        case 'route':
          routes.push(contribution)
          break
        case 'navigation':
          /* A nav entry naming a route that is not placed is never shown at
             all, rather than shown and then removed. */
          if (!dropped.has(contribution.qualifiedId)) navigation.push(contribution)
          break
        case 'extension':
          fill(contribution, pointId)
          break
        case 'shell-control':
          fill(contribution, pointId)
          shellControls.push(contribution)
          break
        case 'shell-footer':
          fill(contribution, pointId)
          footerItems.push(contribution)
          break
        default:
          break
      }
    }

    for (const { pointId, contribution } of plan.suspensions) hold(pointId, contribution)
    for (const { contribution, code, message } of plan.refusals) refuse(contribution, code, message)
    for (const contribution of plan.known) byQualifiedId.set(contribution.qualifiedId, contribution)

    /* Cross-plugin order is settled here, once. Within one order value the
       tiebreak is plugin id then declaration index — stable, and independent
       of the order plugins happened to arrive from the registry (BR-AS06). */
    resort()

    for (const { pluginId, to, code, message } of plan.statuses) {
      statuses?.get(pluginId)?.transition(to, { code, message })
    }
  }

  return {
    /**
     * @param {object[]} plugins validated, normalized manifests
     * @param {Map<string, import('../registry/pluginStatus.js').PluginStatusRecord>} [statuses]
     *   updated in place when supplied, so the caller does not have to
     *   re-derive which plugins ended up placed.
     */
    index(plugins, statuses = null) {
      place(plugins, statuses)
      return this
    },

    /*
      The publisher said this plugin is gone (BR-AS54, BR-AS56).

      Everything it contributed comes off screen; nothing else moves. The
      manifest stays in `placedPlugins` and the id stays in
      `indexedPluginIds`, which is what makes a later re-index — or an import
      finishing late — unable to put it back. Its module is not touched: the
      shell promises to stop showing a plugin, not to unload JavaScript.
    */
    withdraw(pluginId, statuses = null) {
      if (!indexedPluginIds.has(pluginId)) return false
      if (withdrawnPluginIds.has(pluginId)) return false
      withdrawnPluginIds.add(pluginId)

      const mine = (c) => c.pluginId === pluginId
      dropFrom(routes, mine)
      dropFrom(navigation, mine)
      dropFrom(shellControls, mine)
      dropFrom(footerItems, mine)
      for (const [pointId, list] of extensions) {
        extensions.set(pointId, list.filter((c) => !mine(c)))
      }
      /* Dropped, not kept: a return is a fresh placement decision, and a
         refusal held over from before would be recorded twice and would
         describe a placement nobody attempted this time. */
      dropFrom(refusals, (r) => r.pluginId === pluginId)

      /* And the slots this plugin OWNED go with it (BR-AS58). Everything
         placed into them belongs to plugins that are still running, so the
         placements are suspended and the contributors are left alone. */
      for (const [pointId, list] of extensions) {
        if (ownerOf(pointId) !== pluginId) continue
        for (const contribution of list) suspended.push({ pointId, contribution })
        extensions.set(pointId, [])
      }
      dropFrom(shellControls, (c) => ownerOf(c.region) === pluginId)
      for (const [qualifiedId, contribution] of byQualifiedId) {
        if (mine(contribution)) byQualifiedId.delete(qualifiedId)
      }

      statuses?.get(pluginId)?.withdraw()
      return true
    },

    /*
      It came back, unchanged and authorized (BR-AS59). Placement runs again
      from the same manifest rather than replaying the old decision, because
      the session's claims may have changed while it was away — a contribution
      the viewer may no longer see must not return. If nothing of it can be
      placed, it stays withdrawn: a return that restores nothing is not a
      return.
    */
    restore(pluginId, statuses = null) {
      if (!withdrawnPluginIds.has(pluginId)) return false
      const plugin = placedPlugins.get(pluginId)
      if (!plugin) return false

      withdrawnPluginIds.delete(pluginId)
      /* Deliberately without `statuses`: the record is withdrawn, and the
         status it goes back to is the one it left, not the `available` a
         first indexing would assign. */
      place([plugin], null, { reindex: true })

      if (!this.all.some((c) => c.pluginId === pluginId)) {
        withdrawnPluginIds.add(pluginId)
        return false
      }

      /* The slots it owns are open again, so the placements waiting on them
         go back — once, and only for contributors that are themselves still
         running. Capacity is re-checked by going through the same placement
         path, so a slot that shrank does not overfill on the way back. */
      for (let i = suspended.length - 1; i >= 0; i -= 1) {
        const { pointId, contribution } = suspended[i]
        if (ownerOf(pointId) !== pluginId) continue
        if (withdrawnPluginIds.has(contribution.pluginId)) continue
        const verdict = extensionPoints.accepts(pointId, {
          placedCount: (extensions.get(pointId) ?? []).length,
        })
        if (!verdict.ok) {
          refuse(contribution, verdict.code, verdict.message)
          suspended.splice(i, 1)
          continue
        }
        suspended.splice(i, 1)
        fill(contribution, pointId)
        if (contribution.kind === 'shell-control') shellControls.push(contribution)
      }
      resort()
      statuses?.get(pluginId)?.restore()
      return true
    },

    isWithdrawn(pluginId) {
      return withdrawnPluginIds.has(pluginId)
    },

    get routes() {
      return [...routes]
    },
    get navigation() {
      return [...navigation]
    },
    get shellFooter() {
      return [...footerItems]
    },
    /* Every contribution that was actually placed, in one list — the Plugins
       screen counts per plugin from this, so the count can never claim a
       contribution the index refused. */
    get all() {
      const refused = new Set(refusals.map((r) => r.qualifiedId))
      return [...byQualifiedId.values()].filter((c) => !refused.has(c.qualifiedId))
    },
    get refusals() {
      return [...refusals]
    },

    extensionsFor(pointId) {
      return [...(extensions.get(pointId) ?? [])]
    },

    /* Route-scoped topbar controls (BR-AS07): a control with no `routes` is
       unscoped and always shown; one with routes appears only under them.
       Prefix matching, so a control declared for '/fleet-ops' covers its
       detail routes without listing each. */
    shellControlsFor(path) {
      return shellControls.filter(
        (control) =>
          control.routes.length === 0 ||
          control.routes.some((prefix) => path === prefix || path.startsWith(`${prefix}/`)),
      )
    },

    get(qualifiedId) {
      return byQualifiedId.get(qualifiedId) ?? null
    },
  }
}

/* In place, back to front: these arrays are reactive and readers hold the
   containers, so a rebuilt array would silently stop updating the UI. */
function dropFrom(list, matches) {
  for (let i = list.length - 1; i >= 0; i -= 1) {
    if (matches(list[i])) list.splice(i, 1)
  }
}

function byOrder(a, b) {
  return (
    a.order - b.order ||
    a.pluginId.localeCompare(b.pluginId) ||
    a.declarationIndex - b.declarationIndex
  )
}
