/*
  The clash channel (BR-AS87, decision D17-5, amendment A2).

  A clash is NOT a refusal, and the two must never share a collection. A
  refusal explains a contribution that was not placed; a clash explains two
  contributions that WERE both placed and that a reader will find confusing.
  Putting "dropped" and "kept but marked" in one list makes the Plugins screen
  lie about what the shell is running, which is why `refusals` is left exactly
  as it is and this is a channel of its own.

  Three kinds, and no more — amendment A2 settled the other three cases as
  inapplicable or unchanged:

  - `group-label-conflict`   two plugins identify one band and disagree about
                             its name. The band still displays one name
                             (BR-AS86); this says whose name lost.
  - `duplicate-group-label`  two DIFFERENT bands display the same name, so the
                             reader sees one word twice in the rail.
  - `duplicate-item-label`   two entries in one band read the same and go to
                             different places.

  Everything here is derived from the tree, so it inherits the tree's purity:
  the same manifests give the same clashes, in the same order, whatever order
  the plugins arrived in.
*/

/* A band the shell owns takes its name from the shell, so no plugin is at
   fault for it. It still takes part in a duplicate-name clash, because the
   reader sees the repeat either way. */
const SHELL = Object.freeze({ pluginId: null, qualifiedId: null })

export function detectNavigationClashes(tree) {
  const clashes = []

  for (const group of tree) {
    /* One band, two names asked for. The shell-owned case is not a conflict:
       nobody competes with the shell for a name it owns. */
    if (!group.shellOwned && group.labelClaims.length > 1) {
      const [winner, ...losers] = group.labelClaims
      clashes.push(
        clash({
          kind: 'group-label-conflict',
          groupId: group.id,
          label: group.label,
          participants: group.labelClaims.map((c) => ({
            pluginId: c.pluginId,
            qualifiedId: c.qualifiedId,
            label: c.label,
          })),
          message: `Navigation group ${group.id} is shown as ${quoted(winner.label)} from ${winner.pluginId}; ${losers
            .map((l) => `${l.pluginId} asked for ${quoted(l.label)}`)
            .join(', ')}`,
        }),
      )
    }

    /* Two entries in one band that read the same and go somewhere different.
       The same label at the same destination is a duplicate, not a clash —
       the reader is not misled by it. */
    const byLabel = new Map()
    for (const item of group.items) {
      const seen = byLabel.get(item.label) ?? []
      seen.push(item)
      byLabel.set(item.label, seen)
    }
    for (const [label, items] of byLabel) {
      const routes = new Set(items.map((i) => i.routeQualifiedId))
      if (routes.size < 2) continue
      clashes.push(
        clash({
          kind: 'duplicate-item-label',
          groupId: group.id,
          label,
          participants: items.map((i) => ({
            pluginId: i.pluginId,
            qualifiedId: i.qualifiedId,
            label: i.label,
            route: i.routeQualifiedId,
          })),
          message: `Navigation group ${group.id} shows ${quoted(label)} twice, going to ${[...routes].join(' and ')}`,
        }),
      )
    }
  }

  /* Two bands, one name. Read across the whole tree, after the per-band pass,
     so the order of the list stays a function of the tree's own order. */
  const byDisplayed = new Map()
  for (const group of tree) {
    const seen = byDisplayed.get(group.label) ?? []
    seen.push(group)
    byDisplayed.set(group.label, seen)
  }
  for (const [label, groups] of byDisplayed) {
    if (groups.length < 2) continue
    clashes.push(
      clash({
        kind: 'duplicate-group-label',
        groupId: groups[0].id,
        groupIds: groups.map((g) => g.id),
        label,
        participants: groups.map((g) => ({
          ...(g.labelClaims[0] ?? SHELL),
          label,
          groupId: g.id,
        })),
        message: `Navigation groups ${groups.map((g) => g.id).join(' and ')} are both shown as ${quoted(label)}`,
      }),
    )
  }

  return Object.freeze(clashes)
}

/* Every record carries the distinct plugins involved, so a caller that only
   wants to know WHO — the Plugins screen, the nav mark — never has to walk
   the participants itself. The shell is not a plugin and is not listed. */
function clash(record) {
  const pluginIds = [...new Set(record.participants.map((p) => p.pluginId).filter(Boolean))].sort()
  return Object.freeze({
    ...record,
    groupIds: Object.freeze(record.groupIds ?? [record.groupId]),
    participants: Object.freeze(record.participants.map((p) => Object.freeze(p))),
    pluginIds: Object.freeze(pluginIds),
  })
}

const quoted = (value) => `"${value}"`
