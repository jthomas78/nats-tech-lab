/* Where the shell-owned demo catalogue lives, and where a readiness probe is
   answered (BR-AS79, task 16e).

   Two public paths, and they are deliberately not one:

   - `/demo-catalogue.json` is a FILE. It is generated from the same scan that
     produces the plugin catalogue and the asset proxy (BR-AS81), and it is
     served in BOTH plugin sources — readiness is independent of how a plugin
     was discovered (R-1).
   - `/demo-readiness/<demo>` is a CALL. BR-AS79 keeps it off the
     `/plugins/<id>/…` asset prefix, because one is a dynamic request to a
     demo's backend and the other is a static file. R-2 records that the two
     proxy mappings are therefore plural; both still derive from one scan.

   Neither path names a plugin source, and neither encodes one. Moving a demo
   between `build` and `registry` changes nothing here, which is what BR-AS78
   always claimed and R-1 made true of readiness as well.
*/

/** Served by the dev server and emitted by the build. */
export const DEMO_CATALOGUE_PATH = '/demo-catalogue.json'

/** The same document's name inside `dist/`. */
export const DEMO_CATALOGUE_ASSET = 'demo-catalogue.json'

/** The readiness route prefix. Not under `/plugins` — BR-AS79 (F-3). */
export const DEMO_READINESS_PREFIX = '/demo-readiness'

/**
 * The shell-origin readiness route for one demo.
 *
 * @param {string} demo The stable demo identifier — the demo's directory
 *   name. It is not the plugin id and not a source, so a demo keeps its
 *   route whichever way its plugin was discovered.
 */
export function demoReadinessPath(demo) {
  return `${DEMO_READINESS_PREFIX}/${demo}`
}

/* The demo API route prefix. Not under `/plugins` and not under
   `/demo-readiness` — BR-AS82.

   It is a third public path because it is a third kind of thing. `/plugins`
   is files. `/demo-readiness/<demo>` is ONE call, exact-matched on purpose so
   it can never be widened. This prefix carries a demo's own command surface:
   several routes, declared by the demo, each one named before it is served.

   The same stable demo identifier as readiness, for the same reason: a demo
   keeps its route whichever source discovered its plugin. */
export const DEMO_API_PREFIX = '/demo-api'

/**
 * The shell-origin API route base for one demo.
 *
 * @param {string} demo The demo's directory name — the same stable identifier
 *   `demoReadinessPath` uses.
 */
export function demoApiPath(demo) {
  return `${DEMO_API_PREFIX}/${demo}`
}
