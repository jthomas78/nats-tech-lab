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

/* The contract as a QUESTION, for the build-mode gate (task 16k).

   BR-AS75 says every `remote.url` must resolve same-origin with the shell,
   and BR-AS77 says a build-mode plugin lives under `/plugins/<id>/…`. Until
   16k both were properties the GENERATOR happened to produce: the scan copies
   a demo's `manifest.json` through untouched, so a manifest naming
   `https://outside.example/remoteEntry.js` travelled into the catalogue and
   nothing downstream looked. `RemoteAllowlist` is a different gate — it holds
   the URLs the catalogue named, so it cannot judge them.

   So the contract is asked here, once, and `buildCatalogueClient.js` asks it
   before a plugin is admitted.

   Four refusals, and each is a shape that would otherwise leave the origin:

   - a scheme (`https:`, and `javascript:` with it),
   - a protocol-relative authority (`//outside.example/x.js`), which carries no
     scheme and is still another origin,
   - anything not rooted at `/`, because a relative URL resolves against
     whatever page is open,
   - a path that climbs or is normalised out of the plugin's own directory.

   The climb is answered by resolving against a base that cannot be the real
   one, then reading the result back. `/plugins/demo-04/../../evil.js`
   normalises to `/evil.js`, which is no longer under the prefix, and the
   WHATWG parser folds `\` into `/` before we see it — so encoded and
   backslash spellings are answered by the same two lines rather than by a
   list of spellings to remember. */
const NOWHERE = 'https://plugin-asset-path.invalid'

/**
 * Is this the public path of a build-mode plugin's own asset?
 *
 * @param {string} id The plugin's own id, from its manifest.
 * @param {string} url The `remote.url` the catalogue named.
 * @returns {boolean} True only for `/plugins/<id>/<something>`.
 */
export function isPluginAssetUrl(id, url) {
  if (typeof id !== 'string' || id === '') return false
  if (typeof url !== 'string' || !url.startsWith('/') || url.startsWith('//')) return false
  let resolved
  try {
    resolved = new URL(url, NOWHERE)
  } catch {
    return false
  }
  if (resolved.origin !== NOWHERE) return false
  /* Once more, decoded. A server decodes the path before it routes it, so
     `..%2f..%2fevil.js` is a climb spelled in percent-encoding and the first
     pass reads it as an ordinary file name. Resolving the decoded form puts
     the two spellings back on the same footing. */
  let decoded
  try {
    decoded = new URL(decodeURIComponent(resolved.pathname), NOWHERE)
  } catch {
    return false
  }
  if (decoded.origin !== NOWHERE) return false
  const base = pluginAssetBase(id)
  return decoded.pathname.startsWith(base) && decoded.pathname.length > base.length
}
