/* How a demo's state is said (BR-AS79).

   Every sentence on this page is written HERE, by the shell. Nothing a demo's
   backend sends is ever shown: the probe reduces a demo's reply to a state, a
   cause word and a list of failing check names, and this file turns those into
   English. A demo cannot put words on a shell-owned panel.

   The one wording rule that matters, and the reason the file exists:

     `unknown` NEVER says the demo is stopped.

   We did not learn that. We learned that we could not ask. A sleeping laptop,
   a proxy that was never configured, a container still starting and a demo
   that is genuinely off all arrive here identically, and only one of them is
   fixed by starting the demo. Telling a reader "the demo is stopped" when we
   do not know sends them to fix the wrong thing.

   `unavailable` is the opposite case and may be specific, because the demo
   answered: it is running, and it named what it is missing.
*/
import { DEMO_STATE, PROBE_CAUSE } from './readinessProbe.js'

/* The same three tones the health signals use, so one dot means one thing
   across the shell. `unknown` is `off`, not `warn`: "we cannot currently
   tell" is not a problem to act on. */
export const DEMO_TONE = Object.freeze({
  [DEMO_STATE.AVAILABLE]: 'ok',
  [DEMO_STATE.UNAVAILABLE]: 'warn',
  [DEMO_STATE.UNKNOWN]: 'off',
})

export function demoTone(state) {
  return DEMO_TONE[state] ?? 'off'
}

/** The short word for a menu card or a nav row. */
export function demoStatusLabel(state) {
  if (state === DEMO_STATE.AVAILABLE) return 'Running'
  if (state === DEMO_STATE.UNAVAILABLE) return 'Not ready'
  return 'Unknown'
}

const CAUSE_DETAIL = Object.freeze({
  [PROBE_CAUSE.TIMEOUT]: 'The check took too long to answer.',
  [PROBE_CAUSE.UNREACHABLE]: 'The check could not be delivered.',
  [PROBE_CAUSE.NO_ROUTE]: 'This deployment has no check route for this demo.',
  [PROBE_CAUSE.UNREADABLE]: 'The answer did not make sense.',
})

/** The panel heading. */
export function demoHeadline(result) {
  if (result?.state === DEMO_STATE.AVAILABLE) return 'This demo is running.'
  if (result?.state === DEMO_STATE.UNAVAILABLE) return 'This demo is not ready.'
  /* Never "stopped". This is the whole rule. */
  return 'Cannot reach demo services.'
}

/**
 * The explaining sentence under the heading.
 *
 * For `unavailable` it names the failing checks, because the demo told us and
 * that is what a person acts on. The names come from the demo, so they are
 * shown as a LIST of identifiers, never spliced into a sentence the shell
 * wrote — a check called anything at all reads as a label, not as prose.
 */
export function demoDetail(result) {
  if (result?.state === DEMO_STATE.AVAILABLE) return 'Its services answered.'
  if (result?.state === DEMO_STATE.UNAVAILABLE) {
    const missing = (result.failing ?? []).filter((check) => check.code === 'missing')
    if (missing.length > 0) return 'The demo is running, but these are not set up yet:'
    if ((result.failing ?? []).length > 0) return 'The demo is running, but it could not check:'
    return 'The demo is running, but it reported that it is not ready.'
  }
  const detail = CAUSE_DETAIL[result?.cause] ?? CAUSE_DETAIL[PROBE_CAUSE.UNREACHABLE]
  return `${detail} The demo may be running, or it may be stopped — we could not tell.`
}

/** The failing check names, for the panel's list. Empty for every other state. */
export function demoFailingNames(result) {
  if (result?.state !== DEMO_STATE.UNAVAILABLE) return []
  return (result.failing ?? []).map((check) => check.name)
}

/**
 * When the reading was taken. Empty when nothing has been observed — an absent
 * time is honest, and "never" would read as a fact about the demo.
 */
export function demoCheckedAt(result) {
  if (!result?.checkedAt) return ''
  const at = new Date(result.checkedAt)
  return Number.isNaN(at.getTime()) ? '' : at.toLocaleTimeString()
}

/**
 * The run instruction, or null.
 *
 * Null for a visitor deployment, always — F-5. Null for a demo that is
 * running, because there is nothing to start.
 *
 * Offered for `unknown` as well as `unavailable`, and that is deliberate. On a
 * laptop a stopped demo refuses the connection, which is `unknown`, not
 * `unavailable` — so hiding the command there would hide it in exactly the
 * case an operator needs it. The PANEL still does not claim the demo is
 * stopped; it says it could not tell, and offers a command to try. Offering is
 * not asserting.
 */
export function demoRunInstruction(result, { runCommand, operator }) {
  if (!operator) return null
  if (!runCommand) return null
  if (result?.state === DEMO_STATE.AVAILABLE) return null
  return runCommand
}
