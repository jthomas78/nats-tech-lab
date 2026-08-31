/*
  What changed between the registry a running shell is holding and the one it
  just re-read (BR-AS19, decisions 25, 26 and 46).

  The split this module exists to make is the whole of decision 26:

  * An ADDITION can be placed live. Indexing reads metadata and fetches no
    remote code (BR-AS08), so a new entry costs a nav item and a route record
    and nothing has run.
  * EVERY OTHER DIFFERENCE cannot. The status machine has no transition out of
    `active`, so a plugin whose entry disappeared while its components are
    mounted has no legal state to move to — and a remote whose URL moved under
    an already-loaded container would be two versions of one plugin in one
    page. Both are offered as a reload and never applied.
  * ONE EXCEPTION, and it is not a catalogue change: an entry whose publisher
    key was revoked arrives as a tombstone, and decision 100 makes that
    outrank the offer. It is still reported here as a reload — this module
    describes and never acts — but flagged `forced`, and the shell's one
    reload path takes it rather than offering it.

  Decision 46 is why the second bullet says "every other difference" rather
  than naming a taxonomy. The write path is `ON CONFLICT DO UPDATE SET
  enabled, entry`, which replaces the *whole* entry: label, order, routePrefix,
  permission, version and the contribution list are all mutable, and a diff
  that only compared the remote saw none of it. The failure that produced was
  worse than staleness — a transaction editing A and adding B applied only B,
  leaving the shell holding a catalog that existed at no revision at all.

  So the comparison is deep equality over the VALIDATED manifest, on both
  sides. That normalization is not decoration: the shell holds validated
  manifests and the document carries raw ones, and the two forms differ in
  defaulted fields (`description`, `version`, `enabled`) and in the
  qualified ids the validator derives. Comparing the forms directly would
  report every plugin as edited on every read.

  Pure: no shell, no router, no clock. Everything it returns is a description,
  which is what lets the caller decide whether to place it or merely offer it.
*/

import { validateManifest } from './manifestSchema.js'

/** A change nobody may apply without a reload, with the reason to say so. */
export const RELOAD_REASON = {
  REMOVED: 'entry-removed',
  REMOTE_CHANGED: 'remote-changed',
  /* Kept distinct from REMOTE_CHANGED although both are "the entry is not
     what the shell is running". A moved remote and a renamed nav item are the
     same refusal but not the same news, and the banner names them
     differently. */
  CHANGED: 'entry-changed',
  /* Not a catalogue change at all — a security event (decision 100, BR-AS49).
     The publisher key that signed this entry was revoked, and the service is
     serving a tombstone in its place: the id, marked withheld, and nothing
     loadable. Distinct from REMOVED because the shell's answer is different:
     every other reason here is OFFERED and this one is TAKEN. */
  REVOKED: 'entry-revoked',
}

/* A tombstone is not a manifest and must never be validated as one. It has no
   remote by design, so putting it through `admit()` would record a rejection
   for a plugin nobody tried to add and bury the revocation in the noise. */
function isTombstone(manifest) {
  return manifest?.withheld === true
}

/**
 * @param {object[]} current validated manifests the shell is holding
 * @param {object[]} next raw manifests from the document just read
 * @param {object} [options]
 * @param {(raw: object) => object|null} [options.normalize] raw manifest to
 *   the validated form, or null when it does not validate. Injected so a spec
 *   can diff fixtures without building whole manifests; production always
 *   uses the validator the shell itself admits with, because a diff that
 *   normalized differently from `admit()` would disagree with the shell about
 *   what is on screen.
 * @returns {{added: object[], reloadRequired: {id: string, name: string, reason: string}[]}}
 */
export function diffRegistry(current = [], next = [], { normalize = validated } = {}) {
  const held = new Map(current.map((plugin) => [plugin.id, plugin]))
  const arrived = new Set()
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
    arrived.add(id)

    /* Checked before everything else. A tombstone for a plugin the shell is
       running is the one change it may not merely offer; a tombstone for one
       it never held is simply news it does not need, and is dropped rather
       than added. Either way it never reaches the validator. */
    if (isTombstone(manifest)) {
      const running = held.get(id)
      if (running) {
        reloadRequired.push({
          id,
          name: nameOf(running),
          reason: RELOAD_REASON.REVOKED,
          /* The flag, not the reason string, is what the banner branches on:
             a later forced reason must not have to be added in two places. */
          forced: true,
        })
      }
      continue
    }

    const before = held.get(id)
    if (!before) {
      /* The RAW manifest, deliberately: `admit()` validates it again and
         records the real reason for a rejection. Handing over a normalized
         one would mean a malformed entry either vanished here or arrived
         with the failure already swallowed. */
      added.push(manifest)
      continue
    }

    const after = normalize(manifest)
    /* An entry that no longer validates is a change like any other. The shell
       cannot un-place what it already placed, so this is a reload offer and
       not a silent downgrade of a plugin the user is looking at. */
    if (after === null) {
      reloadRequired.push({ id, name: nameOf(before), reason: RELOAD_REASON.CHANGED })
      continue
    }
    if (remoteOf(before) !== remoteOf(after)) {
      reloadRequired.push({ id, name: nameOf(before), reason: RELOAD_REASON.REMOTE_CHANGED })
      continue
    }
    if (!deepEqual(before, after)) {
      reloadRequired.push({ id, name: nameOf(before), reason: RELOAD_REASON.CHANGED })
    }
  }

  for (const [id, plugin] of held) {
    // All plugins are curated; an omitted entry requires a reload.
    if (!arrived.has(id)) {
      reloadRequired.push({ id, name: nameOf(plugin), reason: RELOAD_REASON.REMOVED })
    }
  }

  return { added, reloadRequired }
}

function validated(raw) {
  const result = validateManifest(raw)
  return result.ok ? result.plugin : null
}

/* Module as well as URL: a container re-exposing a different module is the
   same substitution problem by another route. Checked before the deep
   comparison purely so the reason is the specific one. */
function remoteOf(plugin) {
  return `${plugin?.remote?.kind ?? ''}|${plugin?.remote?.url ?? ''}|${plugin?.remote?.module ?? ''}`
}

function nameOf(plugin) {
  return typeof plugin?.name === 'string' ? plugin.name : plugin?.id
}

/* Written out rather than JSON.stringify'd. Both sides come from the
   validator so their key order does match today, but that is an accident of
   one function's literal, and a diff that silently starts reporting every
   plugin as edited is the loudest possible failure for the quietest possible
   cause. */
function deepEqual(a, b) {
  if (a === b) return true
  if (a === null || b === null || typeof a !== 'object' || typeof b !== 'object') return false
  if (Array.isArray(a) !== Array.isArray(b)) return false
  if (Array.isArray(a)) {
    return a.length === b.length && a.every((item, i) => deepEqual(item, b[i]))
  }
  const keys = Object.keys(a)
  if (keys.length !== Object.keys(b).length) return false
  return keys.every((key) => Object.hasOwn(b, key) && deepEqual(a[key], b[key]))
}
