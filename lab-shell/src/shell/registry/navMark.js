/*
  The one mark a nav item can carry (BR-AS04, BR-AS60, BR-AS89).

  A nav item has room for exactly one dot, and three independent signals want
  it: the plugin's own load status (failed, incompatible), the health of what
  it depends on, and a navigation clash it takes part in. The rule is
  precedence, not merging — a failure is the bigger news and keeps the dot to
  itself, because two marks in one corner of the eye compete rather than
  inform.

  That rule used to be written twice in App.vue: once as an early return in a
  helper, and again as the ORDER of a `v-if` / `v-else-if` pair in the
  template. Both had to agree, and only one of them was checkable. Here it is
  one function that returns one mark, and the template renders whatever it
  gets.

  A clash ranks LAST of the three (amendment A2). Load and health are runtime
  faults the reader is looking at now; a clash is a configuration fault, seen
  at index time and fixed by an operator. Because a clash can therefore LOSE
  the dot, it never lives in the dot alone: `description` names every signal
  that applies, so the clash survives losing the colour and is reachable by a
  screen reader. `title` stays what the dot means, so a hover is not made to
  read out a fault the colour is not showing.

  Nothing in here reads the shell. It takes a status, a health reading and a
  list of clashes and returns what to draw — so the same rule is available to
  any surface that grows a plugin list, without any of them re-deriving the
  precedence.
*/

import { healthAttention } from './healthText.js'
import { attentionTone } from './statusRollup.js'

/**
 * @param {object} input
 * @param {string|null} [input.status] the plugin's load status, if it has one
 * @param {object|null} [input.health] `{frontend, backend}` signals, if any
 * @param {{kind: string, message: string}[]} [input.clashes] the navigation
 *   clashes this ENTRY takes part in — already narrowed to the entry by the
 *   caller, because whether a clash names an entry is a question about the
 *   tree and not about precedence.
 * @returns {{tone: string, title: string, description: string}|null} null
 *   means draw nothing — a plugin that loaded, depends on nothing that is
 *   down and clashes with nobody gets a clean nav item, because a dot that is
 *   always there stops being a signal.
 */
export function navMark({ status = null, health = null, clashes = [] } = {}) {
  /* Load status first, and it is exclusive: a plugin that failed is not also
     told that its API is slow. */
  const statusTone = attentionTone(status)
  const healthTone = healthAttention(health)
  const clashText = describeClashes(clashes)

  const mark = statusTone
    ? { tone: statusTone, title: status }
    : healthTone
      ? { tone: healthTone, title: 'a dependency is unavailable' }
      : clashText
        ? { tone: CLASH_TONE, title: clashText }
        : null

  if (!mark) return null

  /* The clash is added to the description only when it is not already the
     title — so the text never says the same thing twice, and never goes
     missing because something louder took the colour. */
  return {
    ...mark,
    description: clashText && clashText !== mark.title
      ? `${mark.title}; ${clashText}`
      : mark.title,
  }
}

/* A clash is a configuration fault, not a failure: `warn`, never `err`. */
const CLASH_TONE = 'warn'

/**
 * @param {{message: string}[]} clashes
 * @returns {string|null}
 */
function describeClashes(clashes) {
  if (!clashes?.length) return null
  return clashes.map((clash) => clash.message).join('; ')
}
