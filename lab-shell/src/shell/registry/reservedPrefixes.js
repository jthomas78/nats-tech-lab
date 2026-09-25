/*
  First path segments a plugin may not claim as its route prefix (BR-AS12).

  A plugin's routes live under `/<routePrefix>/...`, and uniqueness is checked
  between plugins at index time. Nothing checked them against the SHELL. The
  schema admitted `plugins` and `lab-demos`, and the real router then did two
  different wrong things with them: the shell's own `/plugins` record won and
  the plugin's route was unreachable, while a plugin's static `/lab-demos/x`
  beat the shell's `/lab-demos/:demo` and took a shell page away. The hosting
  namespaces are worse, because the router never sees them: a deep link or a
  refresh under `/api/` or `/plugins/` is answered by nginx or the dev proxy,
  and never reaches the SPA at all.

  Two sources, each read from where it is defined rather than retyped:

  * the shell's own routes (`main.js`): a static first segment there is a
    shell page;
  * the hosting namespaces: the paths nginx and the dev proxy answer before
    the SPA does. Three are imported from the modules that emit them.

  `reservedPrefixes.spec.js` re-derives both from `main.js`, `nginx.conf`,
  `vite.config.js` and the snippet generators, and fails when this list and
  those files disagree. The registry refuses the same list at its write door
  (`reservedRoutePrefixes` in the mfe-registry-service's `admissible.go`), and
  the same spec holds the two lists equal.

  The value says who owns the segment. It goes into the refusal message, so an
  operator reading it knows what the prefix collided with.
*/
import { DEMO_API_PREFIX, DEMO_READINESS_PREFIX } from '../demos/demoCatalogueLocation.js'
import { PLUGIN_ASSET_PREFIX } from './pluginAssetPath.js'

const segment = (path) => path.replace(/^\//, '')

export const RESERVED_ROUTE_PREFIXES = Object.freeze({
  /* Shell routes. `/plugins` is also the asset prefix below; one entry. */
  [segment(PLUGIN_ASSET_PREFIX)]: 'shell route /plugins and plugin asset path /plugins/',
  'lab-demos': 'shell route /lab-demos/:demo',
  /* Hosting namespaces, answered before the SPA. */
  api: 'hosting namespace /api/',
  nats: 'hosting namespace /nats',
  [segment(DEMO_API_PREFIX)]: `hosting namespace ${DEMO_API_PREFIX}/`,
  [segment(DEMO_READINESS_PREFIX)]: `hosting namespace ${DEMO_READINESS_PREFIX}/`,
  /* Vite's build output directory (the default `assetsDir`). nginx serves a
     file that exists there ahead of the SPA, so a route under it would be
     shadowed by any chunk that happens to share its name. */
  assets: 'build output /assets/',
})

/** @returns {string|null} who owns `prefix`, or null when a plugin may take it */
export function reservedPrefixOwner(prefix) {
  return Object.hasOwn(RESERVED_ROUTE_PREFIXES, prefix) ? RESERVED_ROUTE_PREFIXES[prefix] : null
}
