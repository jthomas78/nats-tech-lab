/* The one public path layout for a build-mode plugin's static assets
   (BR-AS77, decision 6).

   The contract is a sentence: build-mode plugins load under
   `/plugins/<id>/…` on the shell's OWN origin. Development proxies and hosted
   asset placement implement that same layout, so a manifest's `remote.url` is
   the same relative string in both, and `federatedAdapter.js` resolves it
   against the shell document with no mode branch.

   The prefix covers the WHOLE plugin — the entry module, its lazy chunks, its
   CSS, its fonts and its images — not the entry alone. That is why a plugin's
   own build must set Vite's `base` to `pluginAssetBase(id)`: the entry can be
   fetched by an absolute path, but the chunks it imports are written by the
   plugin's compiler, and only `base` makes those relative to the prefix too.

   Deliberately NOT the home of anything dynamic. A readiness probe is a call
   to a demo's backend, not a file, and BR-AS79 gives it routes of its own
   OUTSIDE this prefix. The catalogue is likewise outside it, at
   `/plugin-catalogue.json` (see buildCatalogueLocation.js).

   No imports, on purpose: the Node-side tooling and the browser both read
   these, and neither should drag the other's dependencies along. */

/** The prefix, with no trailing slash, as a proxy key or an nginx location. */
export const PLUGIN_ASSET_PREFIX = '/plugins'

/** The public base a plugin's own build must be compiled with. */
export function pluginAssetBase(id) {
  return `${PLUGIN_ASSET_PREFIX}/${id}/`
}

/** The public path of a plugin's federation entry, for a manifest's `remote.url`. */
export function pluginEntryPath(id, entry = 'remoteEntry.js') {
  return `${pluginAssetBase(id)}${entry}`
}
