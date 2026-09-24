/*
  The one cascade every placed contribution is ordered by (BR-AS06, BR-AS86).

  It lived inside `contributionRegistry.js` until the navigation tree needed
  it too. It is here, in a module of its own, rather than exported from the
  registry, because the tree imports the cascade and the registry imports the
  tree — a cycle, if the cascade stayed where it was born.

  Total by construction: `declarationIndex` is unique within a plugin and
  `pluginId` is unique within the shell, so no two contributions can tie. That
  totality is the property BR-AS86 rests on; without it the tree would depend
  on the arrival order the sort was supposed to remove.
*/
export function byOrder(a, b) {
  return (
    a.order - b.order ||
    a.pluginId.localeCompare(b.pluginId) ||
    a.declarationIndex - b.declarationIndex
  )
}

/* The label cascade of amendment A2: who gets to NAME a band, as opposed to
   who sits first inside it. `order` is deliberately absent — it is a request
   about position within a band, and a band's name is not a position. */
export function byClaim(a, b) {
  return a.pluginId.localeCompare(b.pluginId) || a.declarationIndex - b.declarationIndex
}
