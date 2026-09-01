/*
  Routing for a plugin the publisher withdrew (BR-AS57).

  The two rules pull in opposite directions, which is why they are two
  functions rather than one:

  * The OCCUPANT is not moved. A redirect would throw away the URL they are
    standing on, and the shell has nothing better to put them at. The view is
    replaced in place, by the caller, using `isWithdrawnRoute` below.
  * Nobody NEW may enter. A nav click, a pasted deep link or a back button
    aimed at a withdrawn route is refused by the guard, because there is no
    longer a plugin behind it.

  The route RECORD stays registered either way. Removing it would make the
  path resolve to not-found, and an unchanged return would then have to
  re-register a route the router had already accepted once — with a live
  navigation in flight. Refusing at the guard is one place, checked every
  time, and the return is a change to a set rather than to the route table.
*/

/** Is this route (resolved or current) a withdrawn plugin's? */
export function isWithdrawnRoute(contributions, route) {
  const pluginId = route?.meta?.pluginId
  if (!pluginId) return false
  return contributions.isWithdrawn(pluginId) === true
}

export function installWithdrawalGuard({ router, contributions }) {
  /* `false`, not a redirect to home or not-found: refusing leaves the person
     exactly where they are, which is the same promise the occupant gets. A
     redirect would additionally rewrite the history entry they came from. */
  return router.beforeEach((to) => !isWithdrawnRoute(contributions, to))
}
