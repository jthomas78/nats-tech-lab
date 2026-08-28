/*
  Which step of the load a cause code belongs to.

  The panel shows stage and cause as two lines because they answer different
  questions: the stage says how far the shell got before it stopped, the cause
  says what stopped it. Both are shell-authored constants — never the caught
  error's message, which for a federation failure quotes the remote's URL back
  at the user (BR-AS04).
*/

const STAGES = Object.freeze({
  'unsupported-schema-version': 'manifest validation',
  'unsupported-shell-api-version': 'manifest validation',
  'invalid-id': 'manifest validation',
  malformed: 'manifest validation',
  'remote-not-curated': 'curation check',
  'no-loader-adapter': 'loader selection',
  'chunk-load-failed': 'remote entry fetch',
  'malformed-module': 'module resolution',
  'activate-threw': 'plugin activation',
  'render-threw': 'contribution render',
})

/**
 * @param {string|null} code a cause code from a plugin status record
 * @returns {string} a stage label; 'unknown' for a code with no mapping, since
 *   an unmapped code is a shell gap and must not print as an empty line
 */
export function failureStage(code) {
  return STAGES[code] ?? 'unknown'
}
