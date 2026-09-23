/* The generator's scan. It lives under `tools/` because it runs in Node, but
   its spec lives here because this is the app shell's only Vitest runner —
   vite.config.js `test.include` covers spec files under `src/` and nothing
   else. */
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import {
  catalogueDocument,
  generateCatalogue,
  ManifestUnreadable,
  scanDemoManifests,
} from '../../../tools/buildCatalogue/scanDemos.js'
import { validateRegistryDocument } from './manifestSchema.js'

let repoRoot

const manifestFor = (id) => ({
  id,
  name: id,
  schemaVersion: 1,
  shellApiVersion: 1,
  remote: { kind: 'federated', url: '/remoteEntry.js', module: 'plugin' },
  contributions: [{ kind: 'route', id: 'main', path: `/${id}`, title: id }],
})

function demo(name, { manifest, raw } = {}) {
  const dir = join(repoRoot, 'demos', name, 'frontend', 'public')
  if (manifest === undefined && raw === undefined) {
    mkdirSync(join(repoRoot, 'demos', name, 'frontend'), { recursive: true })
    return
  }
  mkdirSync(dir, { recursive: true })
  writeFileSync(join(dir, 'manifest.json'), raw ?? JSON.stringify(manifest))
}

beforeEach(() => {
  repoRoot = mkdtempSync(join(tmpdir(), 'catalogue-scan-'))
})
afterEach(() => {
  rmSync(repoRoot, { recursive: true, force: true })
})

describe('the demo scan', () => {
  it('treats the manifest file as the opt-in', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('jetstream-cqrs') })
    demo('01-dictionary') // a frontend with no manifest is not a plugin
    const { plugins } = scanDemoManifests({ repoRoot })
    expect(plugins.map((p) => p.id)).toEqual(['jetstream-cqrs'])
  })

  it('finds nothing when no demo has opted in', () => {
    demo('01-dictionary')
    expect(scanDemoManifests({ repoRoot }).plugins).toEqual([])
  })

  it('finds nothing, rather than failing, when there are no demos at all', () => {
    expect(scanDemoManifests({ repoRoot }).plugins).toEqual([])
  })

  it('reports every manifest it read, so a dev server can watch them', () => {
    demo('02-multi-region', { manifest: manifestFor('multi-region') })
    demo('04-jetstream-cqrs', { manifest: manifestFor('jetstream-cqrs') })
    const { files } = scanDemoManifests({ repoRoot })
    expect(files).toHaveLength(2)
    for (const file of files) expect(file.endsWith('manifest.json')).toBe(true)
  })

  it('orders entries by demo directory, so two scans of one tree agree', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('jetstream-cqrs') })
    demo('02-multi-region', { manifest: manifestFor('multi-region') })
    expect(scanDemoManifests({ repoRoot }).plugins.map((p) => p.id))
      .toEqual(['multi-region', 'jetstream-cqrs'])
  })

  /* Decision 5, amended at the gate: the generator stamps no origin at all, so
     one build runs under any hostname and decision 2's same-origin property
     holds by construction rather than by configuration. */
  it('passes the manifest through untouched, leaving remote.url relative', () => {
    const manifest = manifestFor('jetstream-cqrs')
    demo('04-jetstream-cqrs', { manifest })
    expect(scanDemoManifests({ repoRoot }).plugins[0]).toEqual(manifest)
  })

  /* A plugin that vanished because somebody left a trailing comma is
     debuggable only by guessing. */
  it('refuses a manifest it cannot parse, rather than omitting it', () => {
    demo('04-jetstream-cqrs', { raw: '{ "id": "x", }' })
    expect(() => scanDemoManifests({ repoRoot })).toThrow(ManifestUnreadable)
  })
})

describe('the catalogue document', () => {
  it('is a document the shell\'s own validator accepts', () => {
    const document = catalogueDocument([manifestFor('jetstream-cqrs')])
    const validated = validateRegistryDocument(document)
    expect(validated.ok).toBe(true)
    expect(validated.plugins).toHaveLength(1)
  })

  it('is accepted when it holds zero plugins', () => {
    expect(validateRegistryDocument(catalogueDocument([])).ok).toBe(true)
  })

  it('never claims to be degraded', () => {
    expect(catalogueDocument([manifestFor('a')]).degraded).toBe(false)
  })

  it('changes its revision when membership changes, and only then', () => {
    const one = catalogueDocument([manifestFor('a')])
    const same = catalogueDocument([manifestFor('a')])
    const two = catalogueDocument([manifestFor('a'), manifestFor('b')])
    expect(one.revision).toBe(same.revision)
    expect(one.revision).not.toBe(two.revision)
  })

  it('changes its revision when a manifest changes', () => {
    const before = catalogueDocument([manifestFor('a')])
    const edited = { ...manifestFor('a'), name: 'Renamed' }
    expect(catalogueDocument([edited]).revision).not.toBe(before.revision)
  })
})

describe('generateCatalogue', () => {
  it('scans and renders in one step', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('jetstream-cqrs') })
    const { document, files } = generateCatalogue({ repoRoot })
    expect(document.plugins.map((p) => p.id)).toEqual(['jetstream-cqrs'])
    expect(files).toHaveLength(1)
  })
})

describe('the sibling demo metadata file', () => {
  const metadata = (name, body) => {
    writeFileSync(
      join(repoRoot, 'demos', name, 'frontend', 'public', 'demo.json'),
      typeof body === 'string' ? body : JSON.stringify(body),
    )
  }

  it('is optional — a demo without one is still a plugin', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('a') })
    const { entries } = scanDemoManifests({ repoRoot })
    expect(entries).toHaveLength(1)
    expect(entries[0].devPort).toBeNull()
    expect(entries[0].metadataFile).toBeNull()
  })

  it('carries the dev-server port the proxy needs', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('a') })
    metadata('04-jetstream-cqrs', { devServer: { port: 20401 } })
    expect(scanDemoManifests({ repoRoot }).entries[0].devPort).toBe(20401)
  })

  it('leaves manifest.json untouched, so BR-AS78 still holds', () => {
    /* The port is the one fact a manifest must never carry: the same file has
       to be byte-identical whether the plugin is found by `build` or by
       `registry`, and a dev port means nothing to a registry plugin. */
    demo('04-jetstream-cqrs', { manifest: manifestFor('a') })
    metadata('04-jetstream-cqrs', { devServer: { port: 20401 } })
    const { plugins } = scanDemoManifests({ repoRoot })
    expect(plugins[0]).toEqual(manifestFor('a'))
    expect(JSON.stringify(plugins[0])).not.toContain('20401')
  })

  it('is watched as well, so editing it is an ordinary file change', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('a') })
    metadata('04-jetstream-cqrs', { devServer: { port: 20401 } })
    const { files } = scanDemoManifests({ repoRoot })
    expect(files.some((f) => f.endsWith('demo.json'))).toBe(true)
  })

  it('is read only for a demo that already opted in', () => {
    demo('05-nothing')
    mkdirSync(join(repoRoot, 'demos', '05-nothing', 'frontend', 'public'), { recursive: true })
    metadata('05-nothing', { devServer: { port: 20501 } })
    expect(scanDemoManifests({ repoRoot }).entries).toEqual([])
  })

  it('refuses to parse silently — a broken one is fatal, like a manifest', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('a') })
    metadata('04-jetstream-cqrs', '{ "devServer": }')
    expect(() => scanDemoManifests({ repoRoot })).toThrow(ManifestUnreadable)
  })

  it('reports the demo directory and its built output', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('a') })
    const [entry] = scanDemoManifests({ repoRoot }).entries
    expect(entry.demo).toBe('04-jetstream-cqrs')
    expect(entry.id).toBe('a')
    expect(entry.dist).toBe(join(repoRoot, 'demos', '04-jetstream-cqrs', 'frontend', 'dist'))
  })
})
