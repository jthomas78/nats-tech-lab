/* The `build` catalogue, generated and served — never bundled (BR-AS76).

   The 2026-08-28 working assumption rejected "static JSON inside the shell's
   bundle" because it makes BR-AS03 only nearly true: adding a plugin would
   still mean redeploying the shell. That objection does not apply to this
   plugin and the distinction is load-bearing. The document is emitted as a
   SEPARATE static asset beside the bundle, and the host's own JavaScript still
   declares no remotes, names no plugin and imports no manifest. Adding a
   build-sourced plugin therefore leaves every hashed host chunk untouched —
   which `tools/hostBundleFingerprint.mjs` proves, and which is the reason that
   tool excludes this one asset from its digest.

   Structure follows the repo's only other custom Vite plugin,
   `shared/mfe-preview/vitePreview.js`: a `namespace:role` name, an options
   object with documented defaults, the scan resolved per request rather than
   once at start-up, and the long why-comment at the top.

   Two halves:

   - `configureServer` serves the document from memory at BUILD_CATALOGUE_PATH
     and re-scans on every request, so a manifest added or edited while the dev
     server runs is picked up on the next read with no restart. The watcher is
     told about each manifest so an edit also triggers a browser reload.
   - `generateBundle` emits the same document into `dist/`. Changing catalogue
     membership therefore needs a rebuild, which is decision 2's trust anchor
     holding rather than a limitation to work around — a catalogue the shell's
     build did not produce would be a runtime admission path.
*/
import { resolve, sep } from 'node:path'

import { BUILD_CATALOGUE_ASSET, BUILD_CATALOGUE_PATH } from '../../src/shell/registry/buildCatalogueLocation.js'
import { DEMO_METADATA_PATH, generateCatalogue, MANIFEST_PATH } from './scanDemos.js'

/* Is this file one of the two the scan opts in on?
   Matched by its tail, so a demo ADDED while the dev server runs reloads too
   — its manifest was never read, so it cannot be in the watched set. */
function isDiscoveryInput(file) {
  return file.endsWith(`${sep}${MANIFEST_PATH}`) || file.endsWith(`${sep}${DEMO_METADATA_PATH}`)
}

/**
 * @param {object} [options]
 * @param {string} [options.repoRoot] Absolute path to the repository root.
 *   Defaults to the parent of the Vite root, which is where `lab-shell/` sits.
 */
export function buildCatalogue(options = {}) {
  let repoRoot = options.repoRoot ?? null

  const render = () => generateCatalogue({ repoRoot })

  return {
    name: 'build-catalogue:generate',

    configResolved(config) {
      if (repoRoot === null) repoRoot = resolve(config.root, '..')
    },

    configureServer(server) {
      /* Watching is not reloading (task 16k).

         `server.watcher.add(file)` below tells chokidar to report the file.
         It does NOT reload the page: Vite turns a file change into an HMR
         message by looking the file up in its module graph, and these two
         JSON files are read with `fs.readFileSync` from outside the Vite root
         — they are in no module's import chain, so the lookup finds nothing
         and Vite correctly does nothing. The catalogue is re-scanned on every
         REQUEST, so the new document was always one manual refresh away; the
         menu simply never asked.

         So the reload is sent explicitly. A full reload rather than an HMR
         update, because catalogue membership decides the nav tree and the
         route table, which are built once at boot — there is no module to
         swap. `server.hot` is Vite 6+; `server.ws` is the older name, kept so
         this plugin does not pin a minor version. */
      const watched = new Set()
      const reload = (file) => {
        if (!watched.has(file) && !isDiscoveryInput(file)) return
        ;(server.hot ?? server.ws)?.send({ type: 'full-reload', path: '*' })
      }
      for (const event of ['add', 'change', 'unlink']) server.watcher.on(event, reload)

      /* Ahead of Vite's own middlewares, like vitePreview.js, so the path is
         ours before the static handler can answer it from `public/`. */
      server.middlewares.use((req, res, next) => {
        const path = (req.url ?? '').split('?')[0]
        if (path !== BUILD_CATALOGUE_PATH) return next()
        let rendered
        try {
          rendered = render()
        } catch (error) {
          /* A manifest that will not parse is a FAILED read carrying a code,
             not an empty catalogue (decision 4). The two must not look alike,
             so this must not fall through to an empty document. */
          server.config.logger.error(`[build-catalogue] ${error.message}`)
          res.statusCode = 500
          res.setHeader('Content-Type', 'application/json; charset=utf-8')
          res.end(JSON.stringify({ error: error.name, message: error.message }))
          return undefined
        }
        /* Watch what we read. An edit to a manifest is then an ordinary
           file change, and the page reloads without a restart. */
        for (const file of rendered.files) {
          server.watcher.add(file)
          watched.add(file)
        }
        res.statusCode = 200
        res.setHeader('Content-Type', 'application/json; charset=utf-8')
        res.setHeader('Cache-Control', 'no-store')
        res.end(JSON.stringify(rendered.document, null, 2))
        return undefined
      })
    },

    generateBundle() {
      const { document } = render()
      this.emitFile({
        type: 'asset',
        fileName: BUILD_CATALOGUE_ASSET,
        source: `${JSON.stringify(document, null, 2)}\n`,
      })
    },
  }
}
