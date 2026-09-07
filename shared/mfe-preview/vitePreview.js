/*
  The plugin-side half of the shared contribution preview harness.

  Every micro-frontend plugin in `lab-shell/plugins/` is its own app on its own
  port, and until now the only way to look at one of its contributions was to
  boot the shell and navigate to wherever the shell decided to place it. This
  Vite plugin adds one route — `/__preview` — to a plugin's OWN dev server,
  which mounts every contribution the plugin declares, on its own, with the
  house theme applied.

  Two properties are deliberate:

  - **`apply: 'serve'`.** The harness exists only in `vite dev`. It contributes
    nothing to `vite build`, so `remoteEntry.js` and the federated container are
    byte-for-byte what they were: the preview cannot quietly become part of the
    thing it previews, and BR-AS03/BR-AS15's independence claims are untouched.
  - **Nothing lives in the plugin.** The page, the styling, the fake shell API
    and the fixtures logic are all in `shared/mfe-preview/`. A plugin adopts the
    harness with one line in its `vite.config.js`; a fix here reaches all of
    them at once.

  Usage:

      import { previewHarness } from '../../../shared/mfe-preview/vitePreview.js'
      export default defineConfig({ plugins: [vue(), federation({...}), previewHarness()] })
*/

import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const harnessDir = path.dirname(fileURLToPath(import.meta.url))
/* `shared/` — handed to server.fs.allow because the harness's client module and
   the theme it imports both live outside the plugin's Vite root, and Vite
   refuses to serve a file outside the allow list even when a module in the
   graph asked for it. */
const sharedDir = path.dirname(harnessDir)
const clientFile = path.join(harnessDir, 'client.js')

/* Vite's dev URL for a file outside the project root. On POSIX the absolute
   path already starts with `/`; a Windows path (`C:/x`) needs one adding. */
function fsUrl(absolute) {
  const posix = absolute.replace(/\\/g, '/')
  return `/@fs${posix.startsWith('/') ? '' : '/'}${posix}`
}

function escapeHtml(value) {
  return String(value).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c])
}

function page(options) {
  /* The options blob is read by the client rather than baked into it, so the
     same client module serves every plugin unmodified. `<` is escaped because
     a `</script>` inside JSON would close this tag early. */
  const blob = JSON.stringify(options).replace(/</g, '\\u003c')
  return `<!doctype html>
<html lang="en" class="p-dark">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>${escapeHtml(options.title)} — contributions</title>
  </head>
  <body>
    <div id="mfe-preview"></div>
    <script type="application/json" id="mfe-preview-options">${blob}</script>
    <script type="module">
      import { mountPreview } from '${fsUrl(clientFile)}'
      mountPreview()
    </script>
  </body>
</html>
`
}

/*
  Which optional packages this plugin has, read from its own package.json.

  The client uses these to decide what to import. It has to be decided HERE,
  on the server, because Vite resolves the imports of a module when it
  transforms it — even a dynamic `import('primevue/config')` that never runs
  fails the transform and throws a full-screen error overlay in a plugin that
  has no PrimeVue. Not importing the module at all is the only quiet answer.
*/
function declaredDeps(root) {
  let pkg = {}
  try {
    pkg = JSON.parse(fs.readFileSync(path.join(root, 'package.json'), 'utf8'))
  } catch {
    /* No package.json is not an error here — it only means no optionals. */
  }
  const declared = { ...pkg.dependencies, ...pkg.devDependencies }
  return {
    primevue: 'primevue' in declared,
    primevueThemes: '@primevue/themes' in declared,
  }
}

/**
 * @param {object}  [options]
 * @param {string}  [options.route]     Path the harness answers on. Default `/__preview`.
 * @param {string}  [options.entry]     The plugin's federated entry module. Default `/src/plugin.js`.
 * @param {string}  [options.manifest]  Registry manifest URL. Default `/manifest.json` (i.e. `public/manifest.json`).
 * @param {string}  [options.fixtures]  Optional per-plugin sample props. Default `/preview.fixtures.js`, ignored when absent.
 * @param {string}  [options.title]     Heading for the page. Defaults to the plugin directory name.
 */
export function previewHarness(options = {}) {
  const route = `/${(options.route ?? '/__preview').replace(/^\/|\/$/g, '')}`
  const entry = options.entry ?? '/src/plugin.js'
  const manifest = options.manifest ?? '/manifest.json'
  const fixtures = options.fixtures ?? '/preview.fixtures.js'
  let root = process.cwd()

  return {
    name: 'mfe-preview:harness',
    apply: 'serve',

    config(userConfig) {
      /* The plugin's own root has to be listed alongside shared/: naming
         `allow` at all replaces Vite's default entry, and dropping the root
         makes the dev server refuse to serve the plugin's own source. */
      const rootDir = path.resolve(userConfig.root ?? process.cwd())
      return {
        server: { fs: { allow: [rootDir, sharedDir] } },
        /* The harness's own modules live outside the plugin's root, so a bare
           `vue` inside them resolves up the tree to the repository's root
           node_modules — a SECOND Vue copy beside the plugin's own. Two Vue
           instances in one page is exactly the failure the federation config
           declares `vue` a singleton to avoid. `dedupe` pins these to the
           copy at the Vite root, which is the plugin. */
        resolve: { dedupe: ['vue', 'primevue', '@primeuix/styled'] },
      }
    },

    configResolved(config) {
      root = config.root
    },

    configureServer(server) {
      /* Installed ahead of Vite's own middlewares, so the SPA-fallback that
         would otherwise answer this URL with the plugin's placeholder
         index.html never sees it. */
      server.middlewares.use(async (req, res, next) => {
        const url = (req.url ?? '').split('?')[0].replace(/\/$/, '') || '/'
        if (url !== route) return next()

        /* Resolved per request, not at startup: a fixtures file added while the
           server is running should work without restarting it. */
        const fixturesFile = path.join(root, fixtures.replace(/^\//, ''))
        try {
          const html = await server.transformIndexHtml(url, page({
            title: options.title ?? path.basename(root),
            entry,
            manifest,
            fixtures: fs.existsSync(fixturesFile) ? fixtures : null,
            deps: declaredDeps(root),
          }))
          res.statusCode = 200
          res.setHeader('Content-Type', 'text/html; charset=utf-8')
          res.setHeader('Cache-Control', 'no-store')
          res.end(html)
        } catch (error) {
          next(error)
        }
      })
    },
  }
}

export default previewHarness
