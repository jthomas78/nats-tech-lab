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

  Task 16e adds one wrapper around the resolved component: the demo readiness
  gate (BR-AS79). It is applied HERE because this function is the single funnel
  every plugin route passes through, so the check cannot be forgotten on a
  route somebody adds later — the same argument as the permission note above.
  It is optional: with no demo store the resolver behaves exactly as it did.

  Task 17g adds the second kind of record this funnel emits: the bare-prefix
  redirect (D17-7, amendment A4). A plugin whose routes all sit under
  `/<prefix>/...` leaves `/<prefix>` itself pointing at nothing, and a reader
  who trims the URL lands on not-found. One route contribution may say
  `default: true`, and the shell then registers `/<prefix>` as a redirect to
  it. The shell hardcodes no demo: the prefix is the plugin's own declared
  `routePrefix` and the target is the plugin's own declared route, handled
  like every other contribution.
*/
import { withDemoGate } from '../demos/demoGate.js'

/**
 * @param {object} options
 * @param {{routes: object[]}} options.contributions
 * @param {{load(plugin: object): Promise<object>}} options.loader
 * @param {(id: string) => object|null} options.manifestFor the admitted manifest for a plugin id
 * @param {any} [options.errorComponent] rendered when the remote will not load
 */
export function createShellRoutes({
  contributions,
  loader,
  manifestFor,
  errorComponent = null,
  /* The demo readiness gate's state (BR-AS79). Optional: a shell built
     without one mounts every plugin unchecked, which is what every phase
     before 16e did. */
  demoStore = null,
  /* Which route contributions to build. Boot passes them all; a live addition
     passes only the ones that read placed, because the router already holds
     the rest (BR-AS19, decision 26). */
  routes = contributions.routes,
}) {
  return [
    ...routes.map((route) => routeRecord(route, { loader, manifestFor, errorComponent, demoStore })),
    ...defaultPrefixRedirects(routes, manifestFor),
  ]
}

function routeRecord(route, { loader, manifestFor, errorComponent, demoStore }) {
  return {
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
    component: () => resolveRouteComponent({ route, loader, manifestFor, errorComponent, demoStore }),
  }
}

/*
  `/<prefix>` -> the plugin's declared default route (D17-7, A4).

  Three things this deliberately does NOT do:

  * It does not look at any plugin id. The redirect exists because a manifest
    asked for it, and the shell would build the same record for any plugin
    that asked. No demo is named here, which is the constraint D17-7 was
    written for.
  * It does not check permission or refusal, because it cannot need to. Only
    an ADMITTED route contribution reaches this function — the registry has
    already dropped a route the reader may not see — so a default that was
    refused or not permitted produces no record at all and `/<prefix>` falls
    through to not-found, exactly as it does for a plugin that declared no
    default. That is the "no dead prefix" half of A4, by construction rather
    than by a check somebody has to remember.
  * It does not check withdrawal, and must not. A withdrawal is not a refusal
    (amendment A6): the records stay registered and `installWithdrawalGuard`
    refuses entry by `meta.pluginId`. This record carries the same
    `meta.pluginId`, so the existing guard covers it and the behaviour a
    withdrawal already had is unchanged.

  Readiness is not a special case either. The redirect lands on a real route
  record, whose component is wrapped by the demo gate, so a reader arriving at
  the bare prefix of a demo that is not running meets the same gate as a
  reader who typed the full path (BR-AS79). The redirect is a shorter way in,
  never a way round.
*/
function defaultPrefixRedirects(routes, manifestFor) {
  return routes.flatMap((route) => {
    if (route.default !== true) return []
    const prefix = manifestFor(route.pluginId)?.routePrefix
    if (!prefix) return []
    const path = `/${prefix}`
    /* The default already IS the bare prefix. Registering a redirect here
       would be a route that redirects to itself. */
    if (route.path === path) return []
    return [{
      path,
      /* A colon cannot appear in a qualified id — ids are kebab-case — so
         this name can never collide with a contribution's own. */
      name: `default-route:${route.pluginId}`,
      redirect: { name: route.qualifiedId },
      meta: { pluginId: route.pluginId, defaultFor: route.qualifiedId },
    }]
  })
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
export async function resolveRouteComponent({
  route,
  loader,
  manifestFor,
  errorComponent = null,
  demoStore = null,
}) {
  const plugin = manifestFor(route.pluginId)
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
  const component = module?.components?.[route.component] ?? null
  if (component === null) return errorComponent

  /* The gate wraps a component that LOADED. A plugin that would not load is a
     shell failure and is reported as one; asking whether its demo is running
     would answer a question nobody asked. */
  return withDemoGate({ component, pluginId: route.pluginId, demoStore })
}
