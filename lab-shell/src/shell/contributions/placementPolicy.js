/*
  What a batch of plugins is owed, decided before anything is written
  (BR-AS02, BR-AS04, BR-AS06, BR-AS07, BR-AS58).

  This is the deciding half of indexing. It reads the manifests and a few
  questions about the shell as it stands — who owns a route prefix, how full a
  slot is, whose slots are away, what this session may see — and returns a
  plan: the placements to make, the refusals to record, the placements to hold,
  and the status each plugin ends up in. It writes nothing.

  It used to be one ~100-line pass that decided and wrote at the same time,
  into seven reactive containers and four plain ones. A rule could only be
  checked by indexing a whole registry and then reading the containers back,
  which meant every rule test also depended on Vue reactivity, on the order the
  containers happen to be filled, and on the containers still being empty. Now
  the rules are a pure function over plain data, and the registry's job is to
  apply a plan it did not compute.

  The plan is deliberately a flat, ordered list of actions rather than a set of
  grouped buckets: applying it must not be able to reorder the decisions, and
  capacity at an extension point depends on how many placements came before.
*/

import { PLUGIN_STATUS } from '../registry/pluginStatus.js'

/**
 * @typedef {object} PlacementContext
 * @property {(pluginId: string) => boolean} alreadyIndexed a plugin the shell
 *   has already ruled on — including one that was disabled, refused, or
 *   withdrawn — is skipped whole (BR-AS56).
 * @property {(permission: string|null) => boolean} permits the session's claims
 * @property {(prefix: string) => string|undefined} prefixOwner who holds a
 *   route prefix already, if anyone
 * @property {(pointId: string) => number} occupancy how many contributions the
 *   slot already holds
 * @property {(pointId: string, info: {placedCount: number}) => {ok: boolean, code?: string, message?: string}} accepts
 * @property {(pointId: string) => boolean} ownerWithdrawn the slot's OWNER is
 *   away, so placements into it are held rather than refused (BR-AS58)
 * @property {(qualifiedId: string) => boolean} routePlaced a route already in
 *   the shell, so a nav entry may name a route from an earlier pass
 */

/**
 * @param {object[]} plugins validated, normalized manifests, in registry order
 * @param {PlacementContext} context
 * @returns {{
 *   indexed: object[],
 *   prefixClaims: {prefix: string, pluginId: string}[],
 *   actions: {op: string, contribution: object, pointId?: string}[],
 *   suspensions: {pointId: string, contribution: object}[],
 *   refusals: {contribution: object, code: string, message: string}[],
 *   known: object[],
 *   dropNavigation: string[],
 *   statuses: {pluginId: string, to: string, code: string|null, message: string|null}[],
 * }}
 */
export function decidePlacements(plugins, context) {
  const plan = {
    indexed: [],
    prefixClaims: [],
    actions: [],
    suspensions: [],
    refusals: [],
    known: [],
    dropNavigation: [],
    statuses: [],
  }

  /* Local to this pass. Capacity and prefix ownership both move as the plan is
     built, and reading them back out of the shell would see the state before
     the plan is applied. */
  const filling = new Map() // pointId -> how many this plan has added
  const claimed = new Map() // prefix -> plugin id claimed within this plan
  const heldHere = new Set() // qualifiedIds already suspended in this plan
  const newRoutes = new Set()

  const refuse = (contribution, code, message) => {
    plan.refusals.push({ contribution, code, message })
  }

  const ownerOf = (pointId) => String(pointId ?? '').split('/')[0]

  /* Shared by extension, shell-control and shell-footer: all three land in a
     slot, and all three answer to the same capacity and suspension rules. */
  const intoSlot = (contribution, pointId) => {
    if (context.ownerWithdrawn(ownerOf(pointId))) {
      /* Held, not refused. The Plugins screen must not tell an operator this
         panel was rejected — the slot it targets simply is not there now. */
      if (!heldHere.has(contribution.qualifiedId)) {
        heldHere.add(contribution.qualifiedId)
        plan.suspensions.push({ pointId, contribution })
      }
      return false
    }
    const placedCount = context.occupancy(pointId) + (filling.get(pointId) ?? 0)
    const verdict = context.accepts(pointId, { placedCount })
    if (!verdict.ok) {
      refuse(contribution, verdict.code, verdict.message)
      return false
    }
    filling.set(pointId, (filling.get(pointId) ?? 0) + 1)
    return true
  }

  for (const plugin of plugins) {
    if (context.alreadyIndexed(plugin.id)) continue
    plan.indexed.push(plugin)

    if (!plugin.enabled) {
      plan.statuses.push({
        pluginId: plugin.id,
        to: PLUGIN_STATUS.DISABLED,
        code: 'operator-disabled',
        message: `${plugin.name} is switched off in the plugin registry`,
      })
      continue
    }

    /* Two plugins claiming one route prefix is unresolvable: the URL would
       name both. The first claim wins — deterministically, since plugins are
       decided in registry order — and the second keeps everything of its own
       that is not a route. */
    const owner = claimed.get(plugin.routePrefix) ?? context.prefixOwner(plugin.routePrefix)
    const prefixTaken = owner !== undefined && owner !== plugin.id
    if (!prefixTaken) {
      claimed.set(plugin.routePrefix, plugin.id)
      plan.prefixClaims.push({ prefix: plugin.routePrefix, pluginId: plugin.id })
    }

    let placedAny = false
    for (const contribution of plugin.contributions) {
      /* Permission is decided before placement, not at render: a contribution
         the viewer may not see must not occupy capacity at a slot someone else
         could have used. */
      if (!context.permits(contribution.permission)) {
        refuse(
          contribution,
          'permission-denied',
          `Requires ${contribution.permission}, which this session's claims do not grant`,
        )
        plan.known.push(contribution)
        continue
      }

      switch (contribution.kind) {
        case 'route':
          if (prefixTaken) {
            refuse(
              contribution,
              'route-prefix-conflict',
              `Route prefix /${plugin.routePrefix} is already claimed by ${owner}`,
            )
            break
          }
          plan.actions.push({ op: 'route', contribution })
          newRoutes.add(contribution.qualifiedId)
          placedAny = true
          break
        case 'navigation':
          plan.actions.push({ op: 'navigation', contribution })
          placedAny = true
          break
        case 'extension':
          if (intoSlot(contribution, contribution.target)) {
            plan.actions.push({ op: 'extension', contribution, pointId: contribution.target })
            placedAny = true
          }
          break
        case 'shell-control':
          if (intoSlot(contribution, contribution.region)) {
            plan.actions.push({ op: 'shell-control', contribution, pointId: contribution.region })
            placedAny = true
          }
          break
        case 'shell-footer':
          if (intoSlot(contribution, 'shell/footer/v1')) {
            plan.actions.push({ op: 'shell-footer', contribution, pointId: 'shell/footer/v1' })
            placedAny = true
          }
          break
        default:
          refuse(contribution, 'unknown-contribution-kind', `Unhandled kind ${contribution.kind}`)
      }
      plan.known.push(contribution)
    }

    plan.statuses.push(
      placedAny
        ? { pluginId: plugin.id, to: PLUGIN_STATUS.AVAILABLE, code: null, message: null }
        : /* Nothing of this plugin is reachable in this session. That is the
             same observable outcome as an operator switch-off, so it gets the
             same status with a different reason — the Plugins screen shows one
             word and one explanation, never a contradiction. */
          {
            pluginId: plugin.id,
            to: PLUGIN_STATUS.DISABLED,
            code: 'no-permitted-contributions',
            message: `No contribution of ${plugin.name} is available to this session`,
          },
    )
  }

  /* Nav entries are resolved after every route in the batch is decided, so a
     nav entry may name a route declared later in its own manifest, or one
     placed by an earlier pass. A nav entry naming a route that does not exist
     is caught here rather than on click.

     This runs after the statuses above on purpose: a plugin whose only visible
     contribution was an unresolved nav entry is still `available`, because the
     shell did place something for it and then took it back — telling the
     operator it is `disabled` would name the wrong cause. */
  for (const action of plan.actions) {
    if (action.op !== 'navigation') continue
    const entry = action.contribution
    if (newRoutes.has(entry.routeQualifiedId) || context.routePlaced(entry.routeQualifiedId)) continue
    refuse(
      entry,
      'unresolved-route',
      `Navigation ${entry.qualifiedId} names route ${entry.routeQualifiedId}, which is not placed`,
    )
    plan.dropNavigation.push(entry.qualifiedId)
  }

  return plan
}
