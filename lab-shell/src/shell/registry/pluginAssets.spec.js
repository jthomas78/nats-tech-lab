/* BR-AS77's one public path layout, both implementations of it.

   Same arrangement as buildCatalogueScan.spec.js: the code runs in Node under
   `tools/`, and its spec sits here because this is the app shell's only Vitest
   runner. Real temporary directories rather than a mocked `fs`, because what
   is under test is a file layout.
*/
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { pluginAssetProxy, pluginAssets } from '../../../tools/buildCatalogue/pluginAssets.js'
import { PLUGIN_ASSET_PREFIX, pluginAssetBase, pluginEntryPath } from './pluginAssetPath.js'

let repoRoot

const manifestFor = (id) => ({
  id,
  name: id,
  schemaVersion: 1,
  shellApiVersion: 1,
  remote: { kind: 'federated', url: pluginEntryPath(id), module: 'plugin' },
  contributions: [{ kind: 'route', id: 'main', path: `/${id}`, title: id }],
})

/** Create a demo frontend that opted in, optionally with metadata and a dist. */
function demo(name, { id = name, port = null, dist = null } = {}) {
  const pub = join(repoRoot, 'demos', name, 'frontend', 'public')
  mkdirSync(pub, { recursive: true })
  writeFileSync(join(pub, 'manifest.json'), JSON.stringify(manifestFor(id)))
  if (port !== null) {
    writeFileSync(join(pub, 'demo.json'), JSON.stringify({ devServer: { port } }))
  }
  if (dist !== null) {
    const dir = join(repoRoot, 'demos', name, 'frontend', 'dist')
    for (const [file, body] of Object.entries(dist)) {
      const full = join(dir, file)
      mkdirSync(join(full, '..'), { recursive: true })
      writeFileSync(full, body)
    }
  }
}

beforeEach(() => {
  repoRoot = mkdtempSync(join(tmpdir(), 'plugin-assets-'))
})

afterEach(() => {
  rmSync(repoRoot, { recursive: true, force: true })
})

describe('the public path layout', () => {
  it('puts a plugin under /plugins/<id>/ on the shell origin', () => {
    expect(PLUGIN_ASSET_PREFIX).toBe('/plugins')
    expect(pluginAssetBase('demo-04')).toBe('/plugins/demo-04/')
    expect(pluginEntryPath('demo-04')).toBe('/plugins/demo-04/remoteEntry.js')
  })

  it('is relative, so it resolves against whatever origin serves the shell', () => {
    /* Decision 5, amended at the gate: the generator stamps no origin, so one
       build runs under any hostname. This is the property `federatedAdapter`
       relies on when it resolves the URL against the shell document. */
    expect(pluginEntryPath('demo-04').startsWith('/')).toBe(true)
    expect(pluginEntryPath('demo-04')).not.toMatch(/^\w+:/)
    expect(pluginEntryPath('demo-04')).not.toMatch(/^\/\//)
  })
})

describe('the development proxy', () => {
  it('derives an entry from the same scan as the catalogue', () => {
    demo('04-jetstream-cqrs', { id: 'demo-04', port: 20401 })
    expect(pluginAssetProxy({ repoRoot })).toEqual({
      '/plugins/demo-04': { target: 'http://localhost:20401', ws: true, changeOrigin: false },
    })
  })

  it('upgrades web sockets, so hot module replacement works through the prefix', () => {
    demo('04-jetstream-cqrs', { id: 'demo-04', port: 20401 })
    expect(pluginAssetProxy({ repoRoot })['/plugins/demo-04'].ws).toBe(true)
  })

  it('rewrites nothing, because the plugin compiles its own chunk URLs', () => {
    demo('04-jetstream-cqrs', { id: 'demo-04', port: 20401 })
    expect(pluginAssetProxy({ repoRoot })['/plugins/demo-04'].rewrite).toBeUndefined()
  })

  it('keys on the prefix without a trailing slash, so lazy chunks match too', () => {
    demo('04-jetstream-cqrs', { id: 'demo-04', port: 20401 })
    const key = Object.keys(pluginAssetProxy({ repoRoot }))[0]
    expect(key).toBe('/plugins/demo-04')
    // Vite prefix-matches, so every path below the key is covered.
    for (const path of ['/plugins/demo-04/remoteEntry.js', '/plugins/demo-04/assets/x.css',
      '/plugins/demo-04/assets/inter.woff2']) {
      expect(path.startsWith(key)).toBe(true)
    }
  })

  it('skips a demo that declared no port — it is served from disk instead', () => {
    demo('04-jetstream-cqrs', { id: 'demo-04' })
    expect(pluginAssetProxy({ repoRoot })).toEqual({})
  })

  it('skips a port that is not a usable number', () => {
    demo('04-jetstream-cqrs', { id: 'demo-04' })
    const pub = join(repoRoot, 'demos', '04-jetstream-cqrs', 'frontend', 'public')
    writeFileSync(join(pub, 'demo.json'), JSON.stringify({ devServer: { port: 'twenty' } }))
    expect(pluginAssetProxy({ repoRoot })).toEqual({})
  })

  it('has no entries at all when no demo opted in', () => {
    expect(pluginAssetProxy({ repoRoot })).toEqual({})
  })

  it('is contributed to Vite through the plugin config hook', () => {
    demo('04-jetstream-cqrs', { id: 'demo-04', port: 20401 })
    const contributed = pluginAssets({ repoRoot }).config()
    expect(contributed.server.proxy).toEqual(pluginAssetProxy({ repoRoot }))
  })
})

describe('the dev middleware for a demo with no dev server', () => {
  const serve = async (plugin, url) => {
    let handler
    plugin.configureServer({ middlewares: { use: (fn) => { handler = fn } } })
    const res = { statusCode: 0, headers: {}, body: null,
      setHeader(k, v) { this.headers[k.toLowerCase()] = v }, end(b) { this.body = b } }
    let passed = false
    handler({ url }, res, () => { passed = true })
    return { res, passed }
  }

  const ready = () => {
    const plugin = pluginAssets({ repoRoot })
    plugin.configResolved({
      root: join(repoRoot, 'lab-shell'),
      command: 'serve',
      build: { outDir: 'dummy-non-existing-folder' },
    })
    return plugin
  }

  it('serves a built asset at its public path', async () => {
    demo('04-jetstream-cqrs', { id: 'demo-04', dist: { 'remoteEntry.js': 'export const x = 1' } })
    const { res } = await serve(ready(), '/plugins/demo-04/remoteEntry.js')
    expect(res.statusCode).toBe(200)
    expect(String(res.body)).toBe('export const x = 1')
    expect(res.headers['content-type']).toBe('text/javascript; charset=utf-8')
  })

  it('covers the whole plugin, not the entry alone', async () => {
    demo('04-jetstream-cqrs', { id: 'demo-04', dist: {
      'remoteEntry.js': 'entry', 'assets/app.css': 'body{}', 'assets/inter.woff2': 'font',
      'assets/logo.svg': '<svg/>',
    } })
    const plugin = ready()
    for (const [path, type] of [
      ['/plugins/demo-04/assets/app.css', 'text/css; charset=utf-8'],
      ['/plugins/demo-04/assets/inter.woff2', 'font/woff2'],
      ['/plugins/demo-04/assets/logo.svg', 'image/svg+xml'],
    ]) {
      const { res } = await serve(plugin, path)
      expect(res.statusCode).toBe(200)
      expect(res.headers['content-type']).toBe(type)
    }
  })

  it('answers a missing asset with 404, never the shell page', async () => {
    demo('04-jetstream-cqrs', { id: 'demo-04', dist: { 'remoteEntry.js': 'entry' } })
    const { res } = await serve(ready(), '/plugins/demo-04/nope.js')
    expect(res.statusCode).toBe(404)
    expect(String(res.body)).not.toContain('<')
  })

  it('refuses a path that climbs out of the plugin directory', async () => {
    demo('04-jetstream-cqrs', { id: 'demo-04', dist: { 'remoteEntry.js': 'entry' } })
    const { res } = await serve(ready(), '/plugins/demo-04/../../../secret.txt')
    expect(res.statusCode).toBe(404)
  })

  it('leaves a demo that has its own dev server to the proxy', async () => {
    demo('04-jetstream-cqrs', { id: 'demo-04', port: 20401,
      dist: { 'remoteEntry.js': 'stale' } })
    const { passed, res } = await serve(ready(), '/plugins/demo-04/remoteEntry.js')
    expect(passed).toBe(true)
    expect(res.statusCode).toBe(0)
  })

  it('ignores every path outside the prefix', async () => {
    demo('04-jetstream-cqrs', { id: 'demo-04', dist: { 'remoteEntry.js': 'entry' } })
    for (const url of ['/', '/plugin-catalogue.json', '/plugins', '/pluginsx/a.js']) {
      const { passed } = await serve(ready(), url)
      expect(passed).toBe(true)
    }
  })
})

describe('the build-time collection', () => {
  const built = (outRoot) => {
    const plugin = pluginAssets({ repoRoot })
    plugin.configResolved({ root: outRoot, command: 'build', build: { outDir: 'dist' } })
    return plugin
  }

  it('copies every opted-in demo into the shell served tree', () => {
    const outRoot = join(repoRoot, 'lab-shell')
    demo('04-jetstream-cqrs', { id: 'demo-04', dist: {
      'remoteEntry.js': 'entry', 'assets/app.css': 'body{}',
    } })
    built(outRoot).closeBundle()
    const base = join(outRoot, 'dist', 'plugins', 'demo-04')
    expect(readFileSync(join(base, 'remoteEntry.js'), 'utf8')).toBe('entry')
    expect(readFileSync(join(base, 'assets', 'app.css'), 'utf8')).toBe('body{}')
  })

  it('places them at exactly the public path the manifest names', () => {
    const outRoot = join(repoRoot, 'lab-shell')
    demo('04-jetstream-cqrs', { id: 'demo-04', dist: { 'remoteEntry.js': 'entry' } })
    built(outRoot).closeBundle()
    const url = manifestFor('demo-04').remote.url // /plugins/demo-04/remoteEntry.js
    expect(existsSync(join(outRoot, 'dist', url.slice(1)))).toBe(true)
  })

  it('fails the build when a catalogued demo has no built output', () => {
    demo('04-jetstream-cqrs', { id: 'demo-04' })
    /* BR-AS77: emitting the catalogue alone is not sufficient. A build that
       shipped a catalogue naming files it does not serve would surface as a
       404 in a browser long after the build said ok. */
    expect(() => built(join(repoRoot, 'lab-shell')).closeBundle())
      .toThrow(/no built output/)
  })

  it('writes nothing when the dev server shuts down', () => {
    /* `closeBundle` fires on dev-server shutdown too, and in serve mode Vite
       points `build.outDir` at a placeholder inside the repository. Stopping
       the dev server once wrote a whole tree of plugin assets into
       `lab-shell/dummy-non-existing-folder/`. Nothing may be written back
       into the repository (BR-AS76). */
    const outRoot = join(repoRoot, 'lab-shell')
    demo('04-jetstream-cqrs', { id: 'demo-04', dist: { 'remoteEntry.js': 'entry' } })
    const plugin = pluginAssets({ repoRoot })
    plugin.configResolved({
      root: outRoot, command: 'serve', build: { outDir: 'dummy-non-existing-folder' },
    })
    plugin.closeBundle()
    expect(existsSync(join(outRoot, 'dummy-non-existing-folder'))).toBe(false)
  })

  it('does nothing when no demo opted in', () => {
    const outRoot = join(repoRoot, 'lab-shell')
    expect(() => built(outRoot).closeBundle()).not.toThrow()
    expect(existsSync(join(outRoot, 'dist', 'plugins'))).toBe(false)
  })
})
