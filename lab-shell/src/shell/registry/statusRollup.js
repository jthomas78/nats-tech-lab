/*
  Plugin status, rolled up for the chrome (BR-AS04).

  The Plugins screen answers "what became of every plugin?" in a table. The
  frame has a harder job: it has to say the same thing in the corner of the
  eye, on a nav item the user is not looking at, without a table and without
  a word. That is what the dot and the attention count are for — a failure
  the user has not navigated to yet is still a failure they should be able to
  see from anywhere.

  It lives here rather than in App.vue because two surfaces need it (the nav
  dot and the topbar pill), and because "which statuses deserve attention" is
  a contract question, not a template detail.
*/

import { PLUGIN_STATUS } from './pluginStatus.js'

/* `failed` and `incompatible` are the two the user can act on: one may retry,
   the other needs an operator. `disabled` is deliberate and gets no dot —
   marking an operator's own decision as a problem would train people to
   ignore the dot. `loading` is transient and already has a skeleton saying
   so. */
export const ATTENTION_TONE = Object.freeze({
  [PLUGIN_STATUS.FAILED]: 'err',
  [PLUGIN_STATUS.INCOMPATIBLE]: 'warn',
})

export function attentionTone(status) {
  return ATTENTION_TONE[status] ?? null
}

export function needsAttention(status) {
  return attentionTone(status) !== null
}

/**
 * @param {Iterable<{id: string, status: string}>} statuses
 * @returns {{count: number, failed: number, incompatible: number, tone: string|null, label: string}}
 */
export function summarizeAttention(statuses) {
  let failed = 0
  let incompatible = 0
  for (const record of statuses) {
    if (record.status === PLUGIN_STATUS.FAILED) failed += 1
    else if (record.status === PLUGIN_STATUS.INCOMPATIBLE) incompatible += 1
  }
  const count = failed + incompatible

  /* One phrase, not a tally. "1 plugin failed" is what the Failed artboard
     shows when that is the whole story; a mixed set degrades to the neutral
     "N need attention" rather than inventing a sentence with two clauses in
     the topbar. */
  let label = ''
  if (failed > 0 && incompatible === 0) {
    label = failed === 1 ? '1 plugin failed' : `${failed} plugins failed`
  } else if (count > 0) {
    label = `${count} need attention`
  }

  return {
    count,
    failed,
    incompatible,
    tone: failed > 0 ? 'err' : count > 0 ? 'warn' : null,
    label,
  }
}
