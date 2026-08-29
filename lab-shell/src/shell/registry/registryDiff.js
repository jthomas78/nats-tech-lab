/*
  What changed between the registry a running shell is holding and the one it
  just re-read (BR-AS19, decisions 25 and 26).

  The split this module exists to make is the whole of decision 26:

  * An ADDITION can be placed live. Indexing reads metadata and fetches no
    remote code (BR-AS08), so a new entry costs a nav item and a route record
    and nothing has run.
  * A REMOVAL or a changed remote cannot. The status machine has no transition
    out of `active`, so a plugin whose entry disappeared while its components
    are mounted has no legal state to move to — and a remote whose URL moved
    under an already-loaded container would be two versions of one plugin in
    one page. Both are offered as a reload and never applied.

  Pure: no shell, no router, no clock. Everything it returns is a description,
  which is what lets the caller decide whether to place it or merely offer it.
*/

/** A change nobody may apply without a reload, with the reason to say so. */
export const RELOAD_REASON = {
  REMOVED: 'entry-removed',
  REMOTE_CHANGED: 'remote-changed',
}

/**
 * @param {object[]} current validated manifests the shell is holding
 * @param {object[]} next raw manifests from the document just read
 * @returns {{added: object[], reloadRequired: {id: string, name: string, reason: string}[]}}
 */
export function diffRegistry(current = [], next = []) {
  const held = new Map(current.map((plugin) => [plugin.id, plugin]))
  const arrived = new Map()
  const added = []
  const reloadRequired = []

  for (const manifest of next) {
    const id = typeof manifest?.id === 'string' ? manifest.id : null
    /* An entry with no id cannot be diffed against anything. It is not dropped
       silently — it goes through as an addition, where the shell's existing
       validation rejects it and records why, the same as at boot. */
    if (id === null) {
      added.push(manifest)
      continue
    }
    arrived.set(id, manifest)

    const before = held.get(id)
    if (!before) {
      added.push(manifest)
      continue
    }
    if (remoteOf(before) !== remoteOf(manifest)) {
      reloadRequired.push({ id, name: nameOf(before), reason: RELOAD_REASON.REMOTE_CHANGED })
    }
  }

  for (const [id, plugin] of held) {
    /* Built-ins are not curated and never appear in the document (decision
       30), so their absence is not a removal — it is the normal case. */
    if (plugin?.remote?.kind === 'builtin') continue
    if (!arrived.has(id)) {
      reloadRequired.push({ id, name: nameOf(plugin), reason: RELOAD_REASON.REMOVED })
    }
  }

  return { added, reloadRequired }
}

/* Module as well as URL: a container re-exposing a different module is the
   same substitution problem by another route. */
function remoteOf(plugin) {
  return `${plugin?.remote?.kind ?? ''}|${plugin?.remote?.url ?? ''}|${plugin?.remote?.module ?? ''}`
}

function nameOf(plugin) {
  return typeof plugin?.name === 'string' ? plugin.name : plugin?.id
}
