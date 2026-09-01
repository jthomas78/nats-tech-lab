/*
  How a health signal is said, in the nav and in the inventory (BR-AS60).

  Two rules do most of the work here.

  First, health never outranks load status. A plugin that failed to load is
  the bigger news, and a health dot on top of a failure dot would be two
  marks competing for the same corner of the eye. Health decorates a plugin
  the shell otherwise believes is fine.

  Second, the two signals stay separate. A plugin whose UI is served fine
  while its API is down is exactly the case an operator needs to see, and one
  merged verdict would hide it — so the inventory shows two cells, and only
  the nav dot, which has room for one mark, takes the worse of the two.
*/

import { HEALTH_STATE } from './healthPlane.js'

/* `unavailable` is `warn`, not `bad`: the plugin has not failed — something
   it depends on is not answering, and it may be answering again in five
   seconds. `stale` and `unknown` are the same quiet `off` as a disabled row,
   because "we cannot currently tell" is not a problem to act on. */
export const HEALTH_TONE = Object.freeze({
  [HEALTH_STATE.HEALTHY]: 'ok',
  [HEALTH_STATE.UNAVAILABLE]: 'warn',
  [HEALTH_STATE.STALE]: 'off',
  [HEALTH_STATE.UNKNOWN]: 'off',
  [HEALTH_STATE.NOT_CONFIGURED]: 'off',
  [HEALTH_STATE.NOT_APPLICABLE]: 'off',
})

export function healthTone(state) {
  return HEALTH_TONE[state] ?? 'off'
}

/**
 * The words for one signal. The cause is appended only when there is one, and
 * a cause has already been filtered to a single safe word by the transport —
 * nothing that could name a host reaches this function.
 */
export function healthLabel(signal) {
  if (!signal || !signal.state) return HEALTH_STATE.UNKNOWN
  return signal.cause ? `${signal.state} (${signal.cause})` : signal.state
}

/**
 * When the reading was taken, for the inventory's detail column. Empty when
 * nothing has been observed — an absent time is honest, and "never" would
 * read as a fact about the plugin rather than about the platform.
 */
export function healthCheckedAt(signal) {
  if (!signal?.lastCheckAt) return ''
  const at = new Date(signal.lastCheckAt)
  return Number.isNaN(at.getTime()) ? '' : at.toLocaleTimeString()
}

/**
 * The nav's single mark. Null means draw nothing: a healthy plugin, a plugin
 * with nothing configured to watch, and a plugin nobody has looked at yet all
 * get a clean nav item, because a dot that is always there stops being a
 * signal.
 */
export function healthAttention(signals) {
  const states = [signals?.frontend?.state, signals?.backend?.state]
  if (states.includes(HEALTH_STATE.UNAVAILABLE)) return 'warn'
  return null
}
