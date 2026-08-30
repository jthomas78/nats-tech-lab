/*
  Route contributions -> vue-router records (BR-AS12).

  Two properties this module exists to keep true:

  * A plugin route is addressable. It has a real path, so history, refresh and
    a pasted deep link all work, and the path names its owning plugin without
    a lookup. Navigating to it is what triggers the load — the route table is
    complete long before any plugin code is fetched (BR-AS08).

  * A route the user may not see is not in the table at all. The contribution
    registry has already dropped it on permission, so a direct hit on its URL
    falls through to the not-found record rather than to a guard that has to
    remember to check. There is no second place to forget the check, because
    there is no second check.

  The component is resolved through the loader, so a failed load is a status
  the shell can render (BR-AS04) rather than an unhandled rejection: the record
  falls back to the supplied error component and the plugin sits at `failed`.
*/

/**
 * @param {object} options
 * @param {{routes: object[]}} options.contributions
 * @param {{load(plugin: object): Promise<object>}} options.loader
 * @param {Map<string, object>} options.plugins by plugin id
 * @param {any} [options.errorComponent] rendered when the remote will not load
 */
export function createShellRoutes({
  contributions,
  loader,
  plugins,
  errorComponent = null,
  /* Which route contributions to build. Boot passes them all; a live addition
     passes only the ones that read placed, because the router already holds
     the rest (BR-AS19, decision 26). */
  routes = contributions.routes,
}) {
  return routes.map((route) => ({
    path: route.path,
    /* The qualified id is already globally unique (BR-AS06), so it is the
       route name too — a nav entry resolves to a name, never to a hand-built
       path string. */
    name: route.qualifiedId,
    props: true,
    meta: {
      pluginId: route.pluginId,
      title: route.title,
      contributionId: route.qualifiedId,
    },
    component: () => resolveRouteComponent({ route, loader, plugins, errorComponent }),
  }))
}

// Discovery now happens after the router has painted. Re-resolve an initial
// catch-all only when this read supplied its real route; a cold deep link
// must not remain a 404 after its plugin arrives (BR-AS12).
export async function installShellRoutes({ router, ...options }) {
  for (const route of createShellRoutes(options)) router.addRoute(route)
  const current = router.currentRoute.value
  if (current.name === 'not-found' && router.resolve(current.fullPath).name !== 'not-found') {
    await router.replace(current.fullPath)
  }
}

/**
 * Load the plugin behind one route contribution and hand back the component it
 * named. Never rejects: an unresolvable route renders the error component so
 * the shell frame survives (BR-AS04).
 */
export async function resolveRouteComponent({ route, loader, plugins, errorComponent = null }) {
  const plugin = plugins.get(route.pluginId)
  if (!plugin) return errorComponent

  /* `loader.load` rejects — it does not return a result object — because the
     status record it just wrote is the report; the rejection only tells the
     caller not to render. Here that means the error component. */
  let module
  try {
    module = await loader.load(plugin)
  } catch {
    return errorComponent
  }

  /* The manifest promised a component this module does not export. Same class
     of failure as a chunk that will not load: a broken plugin, reported, not a
     crashed shell. */
  return module?.components?.[route.component] ?? errorComponent
}
