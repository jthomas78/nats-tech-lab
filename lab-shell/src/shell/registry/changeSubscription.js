import { createHintedReader } from './hintedReader.js'

export const NOTIFY_SUBJECT = 'notify._platform.registry.frontend-plugins.changed'

const revisionNumber = (value) => {
  if (typeof value !== 'number' && !(typeof value === 'string' && /^\d+$/.test(value))) return null
  const n = Number(value)
  return Number.isSafeInteger(n) && n >= 0 ? n : null
}

/*
  Every read the shell makes of the catalogue, and the whole of when to make
  one (BR-AS28, AS29; decisions 55-58).

  When to read is not a catalogue question and no longer lives here — it is
  `hintedReader`, shared with the health plane. What is left is the part only
  the catalogue knows: its hints carry a revision, and a revision is enough to
  answer two questions a bare hint cannot.

    - A hint for a revision the shell has already read is not news. It is
      dropped before it can cost a read.
    - A hint held during a read whose document came back at or past that
      revision is already answered. No trailing read follows it.

  Both shortcuts fail safe. An unparseable or absent revision poisons the
  comparison to `null`, and a null token is never stale and never covered — so
  the shell reads again rather than assuming it is current.
*/
export function createChangeSubscription({ subscribe, read, currentRevision }) {
  return createHintedReader({
    subject: NOTIFY_SUBJECT,
    subscribe,
    read,
    hints: {
      of: (message) => revisionNumber(message?.revision),
      /* Already overtaken by a read that has landed. */
      stale: (revision) => {
        const held = revisionNumber(currentRevision())
        return revision !== null && held !== null && revision <= held
      },
      /* The read that just finished vouched for this revision or a later one.
         Falls back to what the shell holds, because a 304 carries no revision
         of its own and the held one is what it confirmed. */
      covered: (revision, result) => {
        const received = revisionNumber(result?.revision) ?? revisionNumber(currentRevision())
        return revision !== null && received !== null && revision <= received
      },
      /* The later of two held hints — unless either is unknown, in which case
         the merged hint is unknown too and a read is guaranteed. */
      merge: (a, b) => (a === null || b === null ? null : Math.max(a, b)),
    },
  })
}
