/*
  Who is reading this deployment (BR-AS79, F-5).

  A demo's `runCommand` is information about the demo and is always true. It
  is only USEFUL to somebody sitting at the repository with a terminal open.
  Show it to a visitor on a hosted URL and it reads as an instruction they
  cannot follow, which turns "the demo is off" into "you did something wrong".

  So the audience is DECLARED by the deployment, never inferred. In particular
  it is not `import.meta.env.DEV`: development, preview and a production build
  can each be shown to either audience, and the words must not change because
  somebody ran a different npm script. That is the whole of F-5.

  Default is `visitor`, the quieter of the two. A deployment that declares
  nothing shows no run instructions — silence is the safe answer, because the
  cost of hiding a command from an operator is one lookup, and the cost of
  showing it to a visitor is a page that blames them.

  An unknown value FAILS BOOT rather than falling back, for the same reason
  `plugin-source` does: a typo that silently picks the other audience is the
  one outcome this rule exists to prevent.
*/

export const AUDIENCE_VISITOR = 'visitor'
export const AUDIENCE_OPERATOR = 'operator'

const KNOWN = [AUDIENCE_VISITOR, AUDIENCE_OPERATOR]

/** The environment variable that declares the audience. Nothing else may. */
export const AUDIENCE_VAR = 'VITE_SHELL_AUDIENCE'

/* Pure and injectable, so a spec can assert every value without the memo. */
export function readAudience(env) {
  const declared = env?.[AUDIENCE_VAR]
  if (declared === undefined || declared === null) return AUDIENCE_VISITOR
  const value = String(declared).trim()
  if (value === '') return AUDIENCE_VISITOR
  if (!KNOWN.includes(value)) {
    throw new Error(
      `${AUDIENCE_VAR} must be one of ${KNOWN.join(', ')}; got ${JSON.stringify(declared)}`,
    )
  }
  return value
}

let resolved = null

/**
 * The one answer, for this shell, for its whole life.
 * @returns {'visitor'|'operator'}
 */
export function audience(env = import.meta.env) {
  if (resolved === null) resolved = readAudience(env)
  return resolved
}

/** True when this deployment may show a demo's local run command. */
export function showsRunCommand(env = import.meta.env) {
  return audience(env) === AUDIENCE_OPERATOR
}

/* Specs only — see the note on resetPluginSourceForTests. */
export function resetAudienceForTests() {
  resolved = null
}
