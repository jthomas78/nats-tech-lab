/* Finding 5 (task 16k). The dev server watched the two discovery inputs and
   then did nothing with the change.

   `server.watcher.add(file)` tells chokidar to REPORT a file. Vite turns a
   report into an HMR message by looking the file up in its module graph, and
   these JSON files are read with `fs.readFileSync` from outside the Vite root
   — they are in no module's import chain, so the lookup finds nothing and
   Vite correctly does nothing. The menu stayed stale until a manual refresh.

   The spec lives here because this is the app shell's only Vitest runner:
   vite.config.js `test.include` covers spec files under `src/` and nothing
   else. Same reason as buildCatalogueScan.spec.js next door. */
import { EventEmitter } from 'node:events'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { buildCatalogue } from '../../../tools/buildCatalogue/vitePlugin.js'
import { BUILD_CATALOGUE_PATH } from './buildCatalogueLocation.js'

let repoRoot

const manifestFor = (id) => ({
  id,
  name: id,
  schemaVersion: 1,
  shellApiVersion: 1,
  remote: { kind: 'federated', url: `/plugins/${id}/remoteEntry.js`, module: 'plugin' },
  contributions: [{ kind: 'route', id: 'main', path: `/${id}`, title: id }],
})

function demo(name, id) {
  const dir = join(repoRoot, 'demos', name, 'frontend', 'public')
  mkdirSync(dir, { recursive: true })
  writeFileSync(join(dir, 'manifest.json'), JSON.stringify(manifestFor(id)))
  return join(dir, 'manifest.json')
}

/** A dev server with just the parts this plugin touches. */
function fakeServer() {
  const watcher = new EventEmitter()
  watcher.add = vi.fn()
  const middlewares = []
  return {
    watcher,
    hot: { send: vi.fn() },
    middlewares: { use: (fn) => middlewares.push(fn) },
    config: { logger: { error: vi.fn() } },
    /** Drive the catalogue route the way a browser would. */
    async read() {
      const res = { setHeader: vi.fn(), end: vi.fn(), statusCode: 0 }
      for (const fn of middlewares) fn({ url: BUILD_CATALOGUE_PATH }, res, () => {})
      return res
    },
  }
}

let server

beforeEach(() => {
  repoRoot = mkdtempSync(join(tmpdir(), 'catalogue-reload-'))
  server = fakeServer()
  buildCatalogue({ repoRoot }).configureServer(server)
})

afterEach(() => {
  rmSync(repoRoot, { recursive: true, force: true })
})

const reloads = () => server.hot.send.mock.calls.filter(([m]) => m?.type === 'full-reload')

describe('a manifest edit while the dev server runs', () => {
  it('reloads the page, rather than only being watched', async () => {
    const manifest = demo('04-jetstream-cqrs', 'demo-04')
    await server.read()
    expect(server.watcher.add).toHaveBeenCalledWith(manifest)

    server.watcher.emit('change', manifest)
    expect(reloads()).toEqual([[{ type: 'full-reload', path: '*' }]])
  })

  /* A full reload, not an HMR update. Catalogue membership decides the nav
     tree and the route table, and both are built once at boot — there is no
     module to swap. */
  it('asks for a full reload, not a module swap', async () => {
    const manifest = demo('04-jetstream-cqrs', 'demo-04')
    await server.read()
    server.watcher.emit('change', manifest)
    expect(server.hot.send).toHaveBeenCalledWith({ type: 'full-reload', path: '*' })
  })

  it('reloads for the demo metadata file too', async () => {
    const dir = join(repoRoot, 'demos', '04-jetstream-cqrs', 'frontend', 'public')
    demo('04-jetstream-cqrs', 'demo-04')
    const metadata = join(dir, 'demo.json')
    writeFileSync(metadata, JSON.stringify({ demo: '04-jetstream-cqrs' }))
    await server.read()

    server.watcher.emit('change', metadata)
    expect(reloads().length).toBe(1)
  })

  /* A demo ADDED while the server runs was never read, so it cannot be in the
     watched set. It is recognised by its tail instead. */
  it('reloads for a demo that appears after the last read', async () => {
    await server.read()
    const manifest = demo('05-new-demo', 'demo-05')

    server.watcher.emit('add', manifest)
    expect(reloads().length).toBe(1)
  })

  it('reloads when a demo is removed', async () => {
    const manifest = demo('04-jetstream-cqrs', 'demo-04')
    await server.read()
    server.watcher.emit('unlink', manifest)
    expect(reloads().length).toBe(1)
  })

  /* Every other file change in the repo already has Vite's own handling. A
     reload on each one would throw away the editor's state for nothing. */
  it('stays quiet for a file that is not a discovery input', async () => {
    demo('04-jetstream-cqrs', 'demo-04')
    await server.read()

    server.watcher.emit('change', join(repoRoot, 'demos', '04-jetstream-cqrs', 'README.md'))
    server.watcher.emit('change', join(repoRoot, 'lab-shell', 'src', 'main.js'))
    expect(reloads()).toEqual([])
  })

  /* `server.hot` is Vite 6+; `server.ws` is the older name. The plugin must
     not pin a minor version. */
  it('falls back to the older ws channel', async () => {
    const older = fakeServer()
    older.ws = older.hot
    delete older.hot
    buildCatalogue({ repoRoot }).configureServer(older)
    const manifest = demo('04-jetstream-cqrs', 'demo-04')
    await older.read()

    older.watcher.emit('change', manifest)
    expect(older.ws.send).toHaveBeenCalledWith({ type: 'full-reload', path: '*' })
  })
})
