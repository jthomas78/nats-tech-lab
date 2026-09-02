/*
  The one mark a nav item can carry (BR-AS04, BR-AS60).

  A nav item has room for exactly one dot, and two independent signals want
  it: the plugin's own load status (failed, incompatible) and the health of
  what it depends on. The rule is precedence, not merging — a failure is the
  bigger news and keeps the dot to itself, because two marks in one corner of
  the eye compete rather than inform.

  That rule used to be written twice in App.vue: once as an early return in a
  helper, and again as the ORDER of a `v-if` / `v-else-if` pair in the
  template. Both had to agree, and only one of them was checkable. Here it is
  one function that returns one mark, and the template renders whatever it
  gets.

  Nothing in here reads the shell. It takes a status and a health reading and
  returns what to draw — so the same rule is available to any surface that
  grows a plugin list, without any of them re-deriving the precedence.
*/

import { healthAttention } from './healthText.js'
import { attentionTone } from './statusRollup.js'

/**
 * @param {object} input
 * @param {string|null} [input.status] the plugin's load status, if it has one
 * @param {object|null} [input.health] `{frontend, backend}` signals, if any
 * @returns {{tone: string, title: string}|null} null means draw nothing — a
 *   plugin that loaded and depends on nothing that is down gets a clean nav
 *   item, because a dot that is always there stops being a signal.
 */
export function navMark({ status = null, health = null } = {}) {
  /* Load status first, and it is exclusive: a plugin that failed is not also
     told that its API is slow. */
  const tone = attentionTone(status)
  if (tone) return { tone, title: status }

  const healthTone = healthAttention(health)
  if (healthTone) return { tone: healthTone, title: 'a dependency is unavailable' }

  return null
}
