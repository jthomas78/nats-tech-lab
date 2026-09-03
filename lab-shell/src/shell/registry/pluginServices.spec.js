import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it, vi } from 'vitest'

import { bootShell } from '../bootShell.js'
import { createPluginLoader } from '../loader/pluginLoader.js'

const root = resolve(import.meta.dirname, '../../..')
const compose = readFileSync(resolve(root, '../demos/01-dictionary/docker-compose.yml'), 'utf8')
const preload = JSON.parse(readFileSync(resolve(root, '../demos/01-dictionary/registry.json'), 'utf8'))
const packages = ['example-plugin', 'example-plugin-slow', 'example-plugin-activate-throws', 'example-plugin-incompatible']
const read = (name, path) => readFileSync(resolve(root, 'plugins', name, path), 'utf8')
/* 13e moved the five examples out of the preload file and behind publisher
   sidecars, so each plugin's own `public/manifest.json` is now the only place
   its entry lives. That is the file its sidecar signs and announces, which
   makes it the right thing for these specs to load the shell from — the same
   bytes the running lab admits, not a second copy of them. */
const manifest = (name) => JSON.parse(read(name, 'public/manifest.json'))
const serviceBlock = (service) => {
  const lines = compose.split('\n')
  const start = lines.findIndex((line) => line === `  ${service}:`)
  const end = lines.findIndex((line, index) => index > start && /^ {2}\S/.test(line))
  return lines.slice(start, end < 0 ? undefined : end).join('\n')
}
const publicOrigin = (name) => {
  const service = name === 'example-plugin-unreachable' ? `${name}-announcer` : `${name}-frontend`
  return serviceBlock(service).match(/PLUGIN_PUBLIC_ORIGIN:\s*(\S+)/)[1]
}
const announcedEntry = (name) => {
  const value = manifest(name)
  return { ...value, remote: { ...value.remote, url: new URL(value.remote.url, publicOrigin(name)).href } }
}
const entryOf = (name) =>
  name === 'demo-catalog' ? preload.plugins.find((p) => p.id === name) : manifest(name)
const modules = {
  'example-plugin': () => import('../../../plugins/example-plugin/src/plugin.js'),
  'example-plugin-slow': () => import('../../../plugins/example-plugin-slow/src/plugin.js'),
  'example-plugin-activate-throws': () => import('../../../plugins/example-plugin-activate-throws/src/plugin.js'),
  'example-plugin-incompatible': () => import('../../../plugins/example-plugin-incompatible/src/plugin.js'),
}

// Real entry modules, with only HTTP replaced: failures cross the same loader
// and status boundary while remaining independent of Docker being available.
async function loadServices(stopped) {
  const entries = [...packages, 'example-plugin-unreachable'].map(announcedEntry)
  const shell = await bootShell({
    registryClient: { fetchRegistry: async () => ({ ok: true, revision: '1', plugins: entries }) },
    permissions: { can: () => true },
  })
  const load = vi.fn(async (remote) => {
    const entry = entries.find((p) => p.remote.url === remote.url && p.remote.name === remote.name)
    if (entry.id === stopped || entry.id === 'example-plugin-unreachable') throw new Error('remote unavailable')
    return modules[entry.id]()
  })
  const loader = createPluginLoader({ allowlist: shell.allowlist, statuses: shell.statuses, adapters: { federated: { load } } })
  await Promise.allSettled(shell.plugins.map((p) => loader.load(p)))
  return { states: Object.fromEntries([...shell.statuses].map(([id, record]) => [id, record.status])), load }
}

const expected = {
  'example-plugin': 'active',
  'example-plugin-slow': 'active',
  'example-plugin-activate-throws': 'failed',
  'example-plugin-incompatible': 'incompatible',
  'example-plugin-unreachable': 'failed',
}

describe('BR-AS03 / decisions 87, 93–95 — independently built plugin services', () => {
  it.each([...packages, 'demo-catalog'])('%s serves its own manifest and owns its build inputs', (name) => {
    const entry = entryOf(name)
    /* `enabled` and `approvedBackendServices` are the operator's, so they
       appear in the seed file and never in the bytes a plugin signs: a plugin
       declares what it depends on, and only an operator says whether the
       registry may probe it (BR-AS62, BR-AS70). */
    const { enabled: _enabled, approvedBackendServices: _approved, ...expected } = entry
    expect(manifest(name)).toEqual(expected)
    expect(Array.isArray(manifest(name).backendServices)).toBe(true)
    const preview = JSON.parse(read(name, 'package.json')).scripts.preview
    const port = preview.match(/--port (\d+)/)?.[1]
    if (port) {
      const origin = name === 'demo-catalog' ? entry.remote.url : publicOrigin(name)
      expect(port).toBe(new URL(origin).port)
    }
    for (const path of ['Dockerfile', 'vite.config.js', 'package.json', 'package-lock.json']) {
      expect(existsSync(resolve(root, 'plugins', name, path))).toBe(true)
    }
    // Every plugin, the catalogue included, is served by the same shared Go
    // host since Phase 15c. demo-catalog used to be the exception and ran
    // nginx; once health became something a plugin publishes over NATS, the
    // exception would have been the one plugin with no health at all. No
    // plugin carries an nginx.conf any more — the four that still did were
    // dead files no Dockerfile referenced, and a dead config that still looks
    // authoritative is how a deleted contract quietly comes back.
    expect(read(name, 'Dockerfile')).toContain('FROM mfe-plugin-host')
    expect(read(name, 'Dockerfile')).toMatch(/COPY --from=build \/\S+\/dist \/srv/)
    expect(existsSync(resolve(root, 'plugins', name, 'nginx.conf'))).toBe(false)
    if (name === 'demo-catalog') {
      expect(read(name, 'Dockerfile')).toContain(`lab-shell/plugins/${name}/public`)
    } else {
      expect(entry.remote.url.startsWith('/')).toBe(true)
    }
  })
  it('keeps only one view in each variant and all six in the main example', () => {
    for (const name of packages) {
      const views = readdirSync(resolve(root, 'plugins', name, 'src/views')).filter((f) => f.endsWith('.vue'))
      expect(views).toHaveLength(name === 'example-plugin' ? 6 : 1)
    }
  })
  it('makes every plugin state its backend dependencies rather than leave them unsaid', () => {
    /* The old deployment map's failure mode: a plugin nobody remembered to add
       read `not configured` forever, and that was indistinguishable from a
       plugin nobody had got round to. Declaring is now the plugin's own job,
       so an empty list is a real answer and a missing one is a bug here. */
    for (const name of [...packages, 'example-plugin-unreachable', 'demo-catalog']) {
      expect(Array.isArray(manifest(name).backendServices)).toBe(true)
    }
    expect(manifest('demo-catalog').backendServices).toEqual(['refdata-service'])
    expect(manifest('example-plugin').backendServices).toEqual([])
  })
  it('has only one operator seed file', () => {
    expect(existsSync(resolve(root, 'plugins/example-plugin/registry.dev.json'))).toBe(false)
  })
})

describe('BR-AS04 / BR-AS13 — independently failing remotes', () => {
  it('preserves all five fixture statuses and never fetches the incompatible remote', async () => {
    const { states, load } = await loadServices()
    expect(states).toEqual(expected)
    expect(load.mock.calls.some(([r]) => r.name === 'example_plugin_incompatible')).toBe(false)
  }, 15000)
  it('a stopped healthy service fails only that plugin', async () => {
    const { states } = await loadServices('example-plugin')
    expect(states).toEqual({ ...expected, 'example-plugin': 'failed' })
  }, 15000)
})
