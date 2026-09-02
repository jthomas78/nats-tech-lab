/*
  What a registry read MEANS, as a pure function (BR-AS19, AS22, AS49, AS54;
  decisions 25 and 48).

  This is the hardest reasoning in the shell — a read can be a failure, a
  degraded answer, a 304 or a document, and each one licenses a different set
  of moves — and it used to live inside `bootShell.applyRegistry`, interleaved
  with the mutations it implies. That made every one of the rules below
  assertable only by booting a shell, indexing manifests and reading back
  reactive state, so a spec about "what a degraded read may retract" had to go
  through three collaborators that have nothing to do with the question.

  So the decision is separated from the effects. Nothing here mutates,
  subscribes or renders: it takes the read, the registry state the shell holds
  and the manifests it is running, and returns what the shell should do. The
  caller does it. The seam is the whole point — one side is policy and is pure,
  the other side is effect and is trivial.

  Never throws. A read the shell could not complete is one of the four
  outcomes, not an exception, because the native frame stays usable either way.
*/

import { diffRegistry, RELOAD_REASON } from './registryDiff.js'

/**
 * The tombstones in a document, against what the shell is running (BR-AS49).
 *
 * Deliberately NOT diffRegistry: that reads an absent id as a removal, which
 * is right for a document the service vouched for and wrong for a stale one.
 * Only the PRESENCE of a tombstone is taken here, never the absence of an
 * entry.
 */
export function revocationsIn(entries, running) {
  const held = new Map(running.map((p) => [p.id, p]))
  return (entries ?? [])
    .filter((e) => e?.withheld === true && held.has(e.id))
    .map((e) => ({
      id: e.id,
      name: held.get(e.id)?.name ?? e.id,
      reason: RELOAD_REASON.REVOKED,
      forced: true,
    }))
}

/**
 * The withdrawal markers in a document, against what the shell is running
 * (BR-AS54). Read on the same terms as a tombstone: presence only.
 */
export function withdrawalsIn(entries, running) {
  const held = new Map(running.map((p) => [p.id, p]))
  return (entries ?? [])
    .filter((e) => e?.withdrawn === true && e?.withheld !== true)
    .filter((e) => held.has(e.id))
    .map((e) => ({ id: e.id, name: held.get(e.id)?.name ?? e.id }))
}

const NOTHING = { added: [], reloadRequired: [], withdrawn: [], restored: [] }

/**
 * @param {object|null} discovery the transport's answer
 * @param {object} options
 * @param {{revision: number|null, fetchedAt: number|null, degraded: boolean, heldRevision: number|null}} options.current
 *   the registry state the shell holds. Returned back, whole, as `registry` —
 *   so the caller installs one object rather than deciding per field which of
 *   these a given outcome leaves alone.
 * @param {object[]} [options.running] the manifests the shell has admitted
 * @param {(id: string) => boolean} [options.isWithdrawn] whether the shell is
 *   currently holding this plugin withdrawn
 * @returns {{outcome: string, error: object|null, registry: object, retract: boolean,
 *            added: object[], reloadRequired: object[], withdrawn: object[], restored: object[]}}
 */
export function decideRead(discovery, { current, running = [], isWithdrawn = () => false }) {
  if (!discovery?.ok) {
    return {
      outcome: 'failed',
      /* Recorded, not thrown. The native shell frame remains usable and the
         Plugins screen shows why the remote list is empty. */
      error: { code: discovery?.code ?? 'registry-malformed', message: discovery?.message ?? '' },
      /* Same rule as the degraded branch (decision 48): a read the shell could
         not complete leaves it unable to say what the server would honour, so
         the next one asks unconditionally rather than betting the recovery on
         a token from before the outage. Nothing else moves — a failed read is
         not evidence about the catalogue, so it is not evidence that the
         registry became degraded either. */
      registry: { ...current, heldRevision: null },
      retract: false,
      ...NOTHING,
    }
  }

  /* Cleared on ANY successful read, a 304 included, and therefore before the
     `unchanged` guard rather than after it (decision 48). A 304 is positive
     evidence the service is answering; leaving the flag set through one made
     degraded a one-way door, because recovery at the same revision is exactly
     what answers 304. */
  const base = {
    ...current,
    fetchedAt: discovery.fetchedAt ?? current.fetchedAt,
    degraded: discovery.degraded === true,
  }

  /* BR-AS22: an empty document that says it is degraded is not the same claim
     as an empty registry. A degraded read is never read as "everything was
     withdrawn" — diffing it would offer a reload for every remote plugin the
     shell is running, on the strength of a document the service already said
     it could not vouch for. */
  if (base.degraded) {
    return {
      outcome: 'degraded',
      error: null,
      /* And the conditional token goes with it. A degraded response carries no
         revision it stands behind, so keeping the pre-outage one would have the
         shell ask "anything newer than the revision I hold?" and be told no —
         for a document the service never served it. */
      registry: { ...base, heldRevision: null },
      /* Raised, never synced. A degraded document is not evidence that anything
         was taken back (decision 48), so an offer standing from a healthy read
         must survive the outage. */
      retract: false,
      added: [],
      /* One thing IS taken from a degraded document: a tombstone (BR-AS49). A
         stale document cannot be trusted to say what exists, but it can be
         trusted to say what was withdrawn, because cache writes are monotonic
         (BR-AS51) and withdrawal is the safe direction to be wrong in. A
         revocation that arrived just before Postgres went down must not wait
         out the outage. */
      reloadRequired: revocationsIn(discovery.plugins, running),
      /* A withdrawal is taken for the same reason. No RETURN is ever taken from
         a degraded document — putting a plugin back needs one the service
         vouched for. */
      withdrawn: withdrawalsIn(discovery.plugins, running),
      restored: [],
    }
  }

  /* Kept beside the revision so the watcher can pick the conditional read up
     from wherever the shell left off — including a boot read it did not make
     itself. */
  const held = { ...base, heldRevision: discovery.heldRevision ?? base.heldRevision }

  /* A 304 carries no document. Nothing to diff, nothing to place — and
     deliberately no clearing of what is already on screen. */
  if (discovery.unchanged) {
    return { outcome: 'unchanged', error: null, registry: held, retract: false, ...NOTHING }
  }

  const diff = diffRegistry(running, discovery.plugins, { isWithdrawn })
  return {
    outcome: 'document',
    error: null,
    registry: { ...held, revision: discovery.revision ?? null },
    /* Only a read the service vouched for may retract a standing offer. See
       `syncPendingReload` at the call site for what retraction buys. */
    retract: true,
    ...diff,
  }
}
