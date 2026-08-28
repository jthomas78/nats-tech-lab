/*
  Shell-owned permission evaluation (BR-AS05).

  Three things this module deliberately is not:

  1. It is not an authorization decision. Every permission here decides whether
     the *shell* draws something. The service behind the contribution still
     authorizes every call it receives, and it is the only thing that can:
     these claims arrive in the browser, where the user owns the runtime. A
     contribution that is unreachable in the nav is not a contribution that is
     unreachable, and the specs assert that difference rather than assuming it.
  2. It is not a token verifier. Verification is accounts-service's, on a
     transport the browser cannot forge. The shell reads an already-verified
     token's payload to decide what to draw, which is why `fromClaims` takes a
     decoded object and there is no signature check anywhere in this file.
  3. It is not per-plugin. A plugin never evaluates its own permissions —
     that would put the check inside the thing being checked. The shell holds
     the evaluator and consults it at three points: nav visibility, route
     guard, and extension render.

  The route guard is the load-bearing one. Nav visibility is a courtesy; the
  guard is what makes a pasted URL behave the same as a hidden menu item.
*/

/* A permission is dot-segmented, with '*' matching one segment or, as a
   trailing segment, the rest. Deliberately the same shape as the NATS subject
   wildcards used across this repo, so an operator reading a grant does not
   have to hold two matching models in their head. */
const SEGMENT = /^[a-z0-9*]+(?:-[a-z0-9]+)*$/

export function createPermissionEvaluator(claims) {
  const granted = normalizeGrants(claims?.permissions)

  return {
    /* The viewer's identity, exposed so panels can label themselves. Never
       used for a decision — decisions go through can(). */
    subject: typeof claims?.sub === 'string' ? claims.sub : null,
    tenant: typeof claims?.tenant === 'string' ? claims.tenant : null,
    context: typeof claims?.context === 'string' ? claims.context : null,

    /**
     * @param {string|null} permission the contribution's declared requirement;
     *   null means the contribution requires none, which is a real answer and
     *   not a missing one.
     */
    can(permission) {
      if (permission === null || permission === undefined) return true
      if (typeof permission !== 'string' || permission === '') return false
      return granted.some((grant) => grantMatches(grant, permission.split('.')))
    },

    /** For the Plugins screen's "why is this hidden" column. */
    get grants() {
      return granted.map((g) => g.join('.'))
    },
  }
}

/* No claims at all is an anonymous viewer, not an error: the shell still boots
   and still renders its unrestricted built-ins (BR-AS04). */
export const ANONYMOUS = createPermissionEvaluator(null)

function normalizeGrants(permissions) {
  if (!Array.isArray(permissions)) return []
  return permissions
    .filter((p) => typeof p === 'string' && p !== '')
    .map((p) => p.split('.'))
    .filter((segments) => segments.every((s) => SEGMENT.test(s)))
}

function grantMatches(grant, required) {
  for (let i = 0; i < grant.length; i += 1) {
    /* A trailing '*' covers everything below it: 'example-plugin.*' grants
       'example-plugin.vessels.read'. A '*' in any other position covers
       exactly one segment. */
    if (grant[i] === '*' && i === grant.length - 1) return true
    if (i >= required.length) return false
    if (grant[i] !== '*' && grant[i] !== required[i]) return false
  }
  return grant.length === required.length
}
