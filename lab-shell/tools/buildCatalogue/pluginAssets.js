/* One public path layout for plugin assets, served two ways (BR-AS77,
   decision 6, task 16c).

   The contract is `/plugins/<id>/…` on the shell's own origin. This file is
   the two implementations of it, and they derive from the SAME scan that
   produces the catalogue — that sameness is the rule, not a convenience.
   Nothing here restates a demo, a port or an id by hand.

   Development, two cases, in this order:

   - The demo declares a dev-server port in its sibling `demo.json`. The shell
     proxies the whole prefix to that port, with `ws: true` so hot module
     replacement upgrades through it as well. The demo's own build must be
     compiled with `base: pluginAssetBase(id)`, which is what puts the plugin's
     lazy chunks, CSS, fonts and images under the prefix too — the entry alone
     is not enough, and BR-AS77 says so explicitly.
   - The demo declares no port. Its built `dist/` is served from disk under the
     same prefix. A plugin is then still reachable at exactly the public path
     it will have when hosted, which is what makes the two environments one
     layout rather than two.

   Build: each plugin's built output is copied into the shell's served tree at
   `dist/plugins/<id>/`. A demo that opted in and has not been built FAILS the
   build. BR-AS77: "a hosted deployment must ship each plugin's built assets at
   those paths; emitting the catalogue alone is not sufficient." A build that
   emitted a catalogue naming a plugin whose files are absent would ship that
   exact insufficiency, and it would surface as a 404 in the browser long after
   the build said `ok`.

   What is deliberately NOT here: any dynamic route. A readiness probe is a
   call to a demo's backend, not a file, and BR-AS79 keeps it off this prefix
   (F-3). The proxy map is plural for that reason; this is one of the two.
*/
import { cpSync, existsSync, readFileSync, statSync } from 'node:fs'
import { join, normalize, resolve, sep } from 'node:path'

import { PLUGIN_SOURCE_BUILD, readPluginSource } from '../../src/shell/pluginSource.js'
import { PLUGIN_ASSET_PREFIX, pluginAssetBase } from '../../src/shell/registry/pluginAssetPath.js'
import { scanDemoManifests } from './scanDemos.js'

/* Enough to serve a Vite build. Kept small and explicit rather than pulling a
   mime dependency in for a development-only path. */
const CONTENT_TYPES = {
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.html': 'text/html; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.webp': 'image/webp',
  '.ico': 'image/x-icon',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
  '.ttf': 'font/ttf',
  '.eot': 'application/vnd.ms-fontobject',
  '.map': 'application/json; charset=utf-8',
}

function contentTypeFor(path) {
  const dot = path.lastIndexOf('.')
  return (dot === -1 ? null : CONTENT_TYPES[path.slice(dot).toLowerCase()])
    ?? 'application/octet-stream'
}

/**
 * The development proxy entries, derived from the scan.
 *
 * @returns {Record<string, object>} A Vite `server.proxy` map. Only demos that
 *   declared a dev-server port appear; the rest are served from disk instead.
 */
export function pluginAssetProxy({ repoRoot, fs } = {}) {
  const { entries } = scanDemoManifests({ repoRoot, fs })
  const proxy = {}
  for (const entry of entries) {
    if (entry.id === null || entry.devPort === null) continue
    /* No trailing slash: Vite prefix-matches this key, so `/plugins/<id>`
       covers `/plugins/<id>/assets/x.js` and the bare entry alike. */
    const prefix = `${PLUGIN_ASSET_PREFIX}/${entry.id}`
    proxy[prefix] = {
      target: `http://localhost:${entry.devPort}`,
      /* The demo's dev server is compiled with the prefix as its `base`, so
         the path it expects is the path we received. Nothing is rewritten:
         rewriting here would put the plugin's own chunk URLs and the proxy
         out of step, and the chunks are written by its compiler, not by us. */
      ws: true,
      changeOrigin: false,
    }
  }
  return proxy
}

/* Refuse a path that climbs out of the plugin's own directory. The prefix is
   the security boundary as well as the layout. */
function resolveWithin(root, relativePath) {
  const full = resolve(root, `.${normalize(`/${relativePath}`)}`)
  if (full !== root && !full.startsWith(root + sep)) return null
  return full
}

/**
 * The Vite plugin. Serves the prefix in development, fills it on build.
 *
 * @param {object} [options]
 * @param {string} [options.repoRoot] Defaults to the parent of the Vite root.
 */
export function pluginAssets(options = {}) {
  let repoRoot = options.repoRoot ?? null
  let outDir = null
  let building = false

  const scan = () => scanDemoManifests({ repoRoot })

  return {
    name: 'plugin-assets:layout',

    config() {
      /* `config` runs before `configResolved`, so the root is resolved here
         from the option or from this file's own location. The proxy map is a
         static part of Vite's server config: a plugin ADDED while the dev
         server runs needs a restart, which is the same rebuild boundary
         decision 5 already puts on catalogue membership. */
      const root = options.repoRoot ?? resolve(import.meta.dirname, '..', '..', '..')
      return { server: { proxy: pluginAssetProxy({ repoRoot: root }) } }
    },

    configResolved(config) {
      if (repoRoot === null) repoRoot = resolve(config.root, '..')
      outDir = resolve(config.root, config.build.outDir)
      /* `closeBundle` also fires when the DEV server shuts down, and in serve
         mode Vite points `build.outDir` at a placeholder it never creates
         (`dummy-non-existing-folder`). Without this guard, stopping the dev
         server silently wrote a tree of plugin assets into the repository —
         which BR-AS76 forbids for the catalogue and is no more welcome here.
         Caught by stopping the dev server during the 16c verification pass. */
      /* …and only when this build will actually SERVE the prefix. BR-AS77 is
         a rule about `plugin-source: build`: a registry-source shell resolves
         every remote from the curated registry and never answers
         `/plugins/<id>/…` itself, so demanding a demo's built output would
         fail a build that had no use for it. The readiness scan above is not
         gated — that one IS required in both sources (BR-AS79). */
      building = config.command === 'build'
        && readPluginSource(config.env ?? process.env) === PLUGIN_SOURCE_BUILD
    },

    configureServer(server) {
      /* Served from disk for the demos that declared no dev port. Registered
         after the proxy, which Vite installs first, so a demo with a port is
         never answered from a stale `dist/`. */
      server.middlewares.use((req, res, next) => {
        const path = (req.url ?? '').split('?')[0]
        if (!path.startsWith(`${PLUGIN_ASSET_PREFIX}/`)) return next()
        const rest = path.slice(PLUGIN_ASSET_PREFIX.length + 1)
        const slash = rest.indexOf('/')
        if (slash === -1) return next()
        const id = rest.slice(0, slash)
        const entry = scan().entries.find((candidate) => candidate.id === id)
        if (!entry || entry.devPort !== null) return next()
        const file = resolveWithin(entry.dist, rest.slice(slash + 1))
        if (file === null || !existsSync(file) || !statSync(file).isFile()) {
          /* A 404, never the shell's index.html. A missing plugin asset that
             answers with an HTML page fails as a syntax error inside the
             module loader, which reads as a broken plugin rather than a
             missing file. */
          res.statusCode = 404
          res.setHeader('Content-Type', 'text/plain; charset=utf-8')
          res.end(`No such plugin asset: ${path}`)
          return undefined
        }
        res.statusCode = 200
        res.setHeader('Content-Type', contentTypeFor(file))
        res.setHeader('Cache-Control', 'no-store')
        res.end(readFileSync(file))
        return undefined
      })
    },

    /* After the bundle is written, so the copy lands beside it rather than
       being overwritten by Vite's own emptyOutDir. */
    closeBundle() {
      if (!building || outDir === null) return
      const missing = []
      for (const entry of scan().entries) {
        if (entry.id === null) continue
        if (!existsSync(entry.dist)) {
          missing.push(`${entry.demo} (expected ${entry.dist})`)
          continue
        }
        cpSync(entry.dist, join(outDir, 'plugins', entry.id), { recursive: true })
      }
      if (missing.length) {
        throw new Error(
          'plugin-assets: these demos are in the catalogue but have no built output, '
          + `so the shell would ship a catalogue naming files it does not serve (BR-AS77): ${missing.join(', ')}`,
        )
      }
    },
  }
}

export { PLUGIN_ASSET_PREFIX, pluginAssetBase }
