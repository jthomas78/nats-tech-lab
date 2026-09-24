/*
  One navigation tree (BR-AS86).

  A plugin says which band its entry belongs to; the shell says where that
  band sits and what it is called. This module is the second half of that
  sentence. It takes the flat list of placed navigation contributions and
  projects it into groups — and it is a PURE function of that list, which is
  the whole point. Two shells given the same manifests must draw the same
  rail, whatever order the plugins were indexed in, whatever order they came
  back from a withdrawal in, and whichever catalogue source they were read
  from. Nothing here reads arrival order, and nothing here may start to.

  It also does not touch the flat list. `contributionRegistry` keeps placing,
  refusing and sorting exactly as before; this is a view over the result.
*/

import { byClaim, byOrder } from './contributionOrder.js'

/*
  The shell-owned group order table — step 1 of BR-AS86's cascade.

  A group named here is placed by the shell, at the position it holds in this
  array, under the label written here. A plugin cannot buy a place in it and
  cannot rename one of its bands; that is BR-AS83 read from the shell's side.

  `Features` is the home of every entry that declares no group at all, and it
  keeps the name the rail has always shown it under. This phase renames no
  band a reader is already used to.
*/
export const SHELL_GROUP_ORDER = Object.freeze([Object.freeze({ id: 'features', label: 'Features' })])

/* Step 4: an entry with no group needs a defined home, not a band invented
   on its behalf. */
export const UNGROUPED_GROUP_ID = 'features'

/*
  The bands the RAIL draws that are not in the tree at all.

  The shell's own band — Home and Plugins — is host-owned and deliberately not
  placeable, so it is not in `SHELL_GROUP_ORDER` and a plugin cannot name it
  (BR-AS07, BR-AS89). But a reader who sees `Shell` twice in the rail is
  confused by it exactly as much as by any other repeated band name, so the
  label is named here and handed to clash detection. Being reserved is about
  the WORD, not about placement: nothing here lets a plugin into the band.
*/
export const RESERVED_RAIL_LABELS = Object.freeze(['Shell'])

export function buildNavigationTree(entries, { groupOrder = SHELL_GROUP_ORDER } = {}) {
  const table = new Map()
  groupOrder.forEach((group, index) => table.set(group.id, { label: group.label, index }))

  const groups = new Map()
  for (const entry of entries) {
    const id = entry.group?.id ?? UNGROUPED_GROUP_ID
    let node = groups.get(id)
    if (!node) {
      node = { id, claims: [], items: [] }
      groups.set(id, node)
    }
    node.items.push(entry)
    /* An ungrouped entry asks for no name, so it makes no claim on one. */
    if (entry.group) {
      node.claims.push({
        pluginId: entry.pluginId,
        declarationIndex: entry.declarationIndex,
        qualifiedId: entry.qualifiedId,
        label: entry.group.label,
      })
    }
  }

  const placed = [...groups.values()].map((node) => {
    const owned = table.get(node.id) ?? null
    /* Step 3, within the band. */
    const items = [...node.items].sort(byOrder)
    /* Amendment A2: one claimant per distinct label, the first by the claim
       cascade, so the list is stable and says something a diagnostic can read
       out. The losing label is kept, never dropped. */
    const claims = [...node.claims].sort(byClaim)
    const distinct = []
    for (const claim of claims) {
      if (distinct.some((seen) => seen.label === claim.label)) continue
      distinct.push(Object.freeze(claim))
    }
    return Object.freeze({
      id: node.id,
      /* The shell's label wins for a band the shell owns; otherwise the first
         claimant by the cascade names it. A group always carries a label —
         the shorthand form makes one out of the id — so the final fallback is
         reached only by a caller that hand-built an entry. */
      label: owned?.label ?? distinct[0]?.label ?? node.id,
      shellOwned: owned !== null,
      /* Every label anybody asked for, in cascade order. Exactly one is
         displayed; task 17d reads the rest to name the clash. */
      labelClaims: Object.freeze(distinct),
      items: Object.freeze(items),
      tableIndex: owned ? owned.index : -1,
    })
  })

  /* Steps 1 and 2: the table's own order first, then every other band by id.
     NOT by first-seen — first-seen is arrival order, which is the thing this
     whole module exists to stop mattering. */
  placed.sort((a, b) => {
    if (a.shellOwned && b.shellOwned) return a.tableIndex - b.tableIndex
    if (a.shellOwned !== b.shellOwned) return a.shellOwned ? -1 : 1
    return a.id.localeCompare(b.id)
  })

  return Object.freeze(placed)
}
