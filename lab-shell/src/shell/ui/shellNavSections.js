/*
  The rail, as data (BR-AS89).

  Phase 17's whole point is that the rail is ONE tree: the shell's own two
  screens, then every plugin's navigation merged into shell-placed bands
  (BR-AS86). This turns that tree into the `sections` array `NavList` takes,
  and it does nothing else — no sorting, no merging, no clash detection. Those
  happened upstream, in `navigationTree.js` and `navigationClashes.js`, and
  re-deciding any of them here would give the rail an order the Plugins screen
  does not agree with.

  It is a plain function rather than a computed inside the component so the
  shape can be asserted without mounting anything, and so the SHELL band's
  fixed content has one definition instead of a template's worth.
*/

import { navMark } from '../registry/navMark.js'
import { iconClass } from './iconClass.js'

/* The shell's own screens. Not contributions, and deliberately not reachable
   by one — BR-AS07 keeps the rail host-owned, and this band is the part of it
   no plugin can touch. */
export const SHELL_SECTION_ID = 'shell'
export const SHELL_SECTION_LABEL = 'Shell'

/**
 * Which clashes name this entry.
 *
 * Membership is by `qualifiedId`, never by plugin: a plugin with four entries
 * in a band and one clashing label should mark the one entry that clashed.
 * Every clash record already carries the qualified ids of its participants,
 * so this asks the record rather than guessing from the band.
 *
 * @param {readonly object[]} clashes
 * @param {{qualifiedId: string}} entry
 * @returns {object[]}
 */
export function clashesFor(clashes, entry) {
  return clashes.filter((clash) =>
    clash.participants.some((participant) => participant.qualifiedId === entry.qualifiedId),
  )
}

/**
 * @param {readonly object[]} tree the merged navigation tree (BR-AS86)
 * @param {object} context
 * @param {readonly object[]} [context.clashes] the registry's clash list
 * @param {(pluginId: string) => string|null} [context.statusOf]
 * @param {(pluginId: string) => object|null} [context.healthOf]
 * @param {number} [context.inventoryCount] how many plugins the shell knows
 * @param {{count: number, tone: string, label: string}|null} [context.attention]
 * @returns {object[]} `NavList` sections, shell band first
 */
export function shellNavSections(tree, {
  clashes = [],
  statusOf = () => null,
  healthOf = () => null,
  inventoryCount = 0,
  attention = null,
} = {}) {
  /* The badge is the inventory size, so the number and the screen it leads to
     can never disagree. It gives way to the dot rather than sitting beside it
     — BR-AS60 allows one mark, and a count next to a warning reads as part of
     the warning. */
  const flagged = Boolean(attention?.count)

  const shell = {
    id: SHELL_SECTION_ID,
    eyebrow: SHELL_SECTION_LABEL,
    items: [
      {
        key: 'shell/home',
        label: 'Home',
        icon: iconClass('pi pi-home'),
        to: '/',
      },
      {
        key: 'shell/plugins',
        label: 'Plugins',
        icon: iconClass('pi pi-box'),
        to: '/plugins',
        badge: flagged ? null : inventoryCount,
        mark: flagged ? { tone: attention.tone, title: attention.label, description: attention.label } : null,
      },
    ],
  }

  const bands = tree.map((group) => ({
    id: group.id,
    eyebrow: group.label,
    items: group.items.map((entry) => ({
      key: entry.qualifiedId,
      label: entry.label,
      icon: iconClass(entry.icon),
      /* By NAME, never by path — BR-AS12. The route's own record owns where
         it lives, and the rail asks the router for it. */
      to: { name: entry.routeQualifiedId },
      /* One dot, whichever signal earned it: this plugin's own failure, a
         dependency of it that is not answering, or a clash it takes part in.
         A sibling's failure never dots this entry — that isolation is the
         whole claim the shell makes (BR-AS04). */
      mark: navMark({
        status: statusOf(entry.pluginId),
        health: healthOf(entry.pluginId),
        clashes: clashesFor(clashes, entry),
      }),
    })),
  }))

  /* A band with nothing in it is not rendered. Withdrawal empties a band
     before it removes it, and a lone eyebrow over blank space is a dead
     heading (BR-AS56). */
  return [shell, ...bands.filter((band) => band.items.length > 0)]
}
