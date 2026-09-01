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

  const refuse = (contribution, code, message) => {
    refusals.push({
      qualifiedId: contribution.qualifiedId,
      pluginId: contribution.pluginId,
      kind: contribution.kind,
      code,
      message,
    })
  }

  const placeExtension = (contribution, pointId) => {
    const placed = extensions.get(pointId) ?? []
    const verdict = extensionPoints.accepts(pointId, { placedCount: placed.length })
    if (!verdict.ok) {
      refuse(contribution, verdict.code, verdict.message)
      return false
    }
    placed.push(contribution)
    extensions.set(pointId, placed)
    return true
  }

  return {
    /**
     * @param {object[]} plugins validated, normalized manifests
     * @param {Map<string, import('../registry/pluginStatus.js').PluginStatusRecord>} [statuses]
     *   updated in place when supplied, so the caller does not have to
     *   re-derive which plugins ended up placed.
     */
    index(plugins, statuses = null) {
      for (const plugin of plugins) {
        /* Seen before — including one that was disabled or refused, whose
           outcome was already recorded, and one that is withdrawn, whose
           contributions must not come back through a re-index (BR-AS56). A
           second pass must not re-record it. */
        if (indexedPluginIds.has(plugin.id)) continue
        indexedPluginIds.add(plugin.id)
        placedPlugins.set(plugin.id, plugin)

        if (!plugin.enabled) {
          statuses?.get(plugin.id)?.transition(PLUGIN_STATUS.DISABLED, {
            code: 'operator-disabled',
            message: `${plugin.name} is switched off in the plugin registry`,
          })
          continue
        }

        /* Two plugins claiming one route prefix is unresolvable: the URL
           would name both. The first claim wins — deterministically, since
           plugins are indexed in registry order — and the second plugin keeps
           everything of its own that is not a route. */
        const prefixOwner = routePrefixOwners.get(plugin.routePrefix)
        const prefixTaken = prefixOwner !== undefined && prefixOwner !== plugin.id
        if (!prefixTaken) routePrefixOwners.set(plugin.routePrefix, plugin.id)

        let placedAny = false
        for (const contribution of plugin.contributions) {
          /* Permission is checked before placement, not at render: a
             contribution the viewer may not see must not occupy capacity at
             an extension point that someone else could have used. */
          if (!permissions.can(contribution.permission)) {
            refuse(
              contribution,
              'permission-denied',
              `Requires ${contribution.permission}, which this session's claims do not grant`,
            )
            continue
          }

          switch (contribution.kind) {
            case 'route':
              if (prefixTaken) {
                refuse(
                  contribution,
                  'route-prefix-conflict',
                  `Route prefix /${plugin.routePrefix} is already claimed by ${prefixOwner}`,
                )
                break
              }
              routes.push(contribution)
              placedAny = true
              break
            case 'navigation':
              navigation.push(contribution)
              placedAny = true
              break
            case 'extension':
              placedAny = placeExtension(contribution, contribution.target) || placedAny
              break
            case 'shell-control':
              if (placeExtension(contribution, contribution.region)) {
                shellControls.push(contribution)
                placedAny = true
              }
              break
            case 'shell-footer':
              if (placeExtension(contribution, 'shell/footer/v1')) {
                footerItems.push(contribution)
                placedAny = true
              }
              break
            default:
              refuse(contribution, 'unknown-contribution-kind', `Unhandled kind ${contribution.kind}`)
          }
          byQualifiedId.set(contribution.qualifiedId, contribution)
        }

        const record = statuses?.get(plugin.id)
        if (!record) continue
        if (placedAny) {
          record.transition(PLUGIN_STATUS.AVAILABLE)
        } else {
          /* Nothing of this plugin is reachable in this session. That is the
             same observable outcome as an operator switch-off, so it gets the
             same status with a different reason — the Plugins screen shows
             one word and one explanation, never a contradiction. */
          record.transition(PLUGIN_STATUS.DISABLED, {
            code: 'no-permitted-contributions',
            message: `No contribution of ${plugin.name} is available to this session`,
          })
        }
      }

      /* Cross-plugin order is settled here, once. Within one order value the
         tiebreak is plugin id then declaration index — stable, and independent
         of the order plugins happened to arrive from the registry (BR-AS06). */
      routes.sort(byOrder)
      navigation.sort(byOrder)
      shellControls.sort(byOrder)
      footerItems.sort(byOrder)
      for (const [pointId, list] of extensions) extensions.set(pointId, [...list].sort(byOrder))

      /* Nav entries are resolved after every route is indexed, so a nav entry
         may name a route declared later in the manifest. A nav entry naming a
         route that does not exist is caught here rather than on click. */
      for (let i = navigation.length - 1; i >= 0; i -= 1) {
        const entry = navigation[i]
        if (!routes.some((route) => route.qualifiedId === entry.routeQualifiedId)) {
          refuse(
            entry,
            'unresolved-route',
            `Navigation ${entry.qualifiedId} names route ${entry.routeQualifiedId}, which is not placed`,
          )
          navigation.splice(i, 1)
        }
      }

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
      indexedPluginIds.delete(pluginId)
      /* Deliberately without `statuses`: the record is withdrawn, and the
         status it goes back to is the one it left, not the `available` a
         first indexing would assign. */
      this.index([plugin])

      if (!this.all.some((c) => c.pluginId === pluginId)) {
        withdrawnPluginIds.add(pluginId)
        return false
      }
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
