/* Hosted plugin assets, proxied on the shell's own origin (BR-AS77, task 16l).

   The failure these specs hold shut: a `plugin-source: registry` image
   compiles no plugin and copies no plugin, so `/plugins/<id>/…` answered 404
   for every enabled demo although development worked. Development is the
   proxied case and hid it.

   The code lives under `tools/` because it runs in Node; the spec lives here
   because this is the shell's only Vitest runner. */
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { assetsNginxConf } from '../../../tools/buildCatalogue/demoAssets.js'
import { scanDemoManifests } from '../../../tools/buildCatalogue/scanDemos.js'
import { pluginAssetBase } from '../registry/pluginAssetPath.js'

let repoRoot

const manifestFor = (id) => ({
  id,
  name: id,
  schemaVersion: 1,
  shellApiVersion: 1,
  remote: { kind: 'federated', url: `/plugins/${id}/remoteEntry.js`, module: 'plugin' },
  contributions: [{ kind: 'route', id: 'main', path: `/${id}`, title: id }],
})

const assetsMetadata = { assets: { hostedUpstream: 'http://demo04-frontend:80' } }

function demo(name, { manifest, metadata } = {}) {
  const dir = join(repoRoot, 'demos', name, 'frontend', 'public')
  mkdirSync(dir, { recursive: true })
  if (manifest !== undefined) writeFileSync(join(dir, 'manifest.json'), JSON.stringify(manifest))
  if (metadata !== undefined) writeFileSync(join(dir, 'demo.json'), JSON.stringify(metadata))
}

const conf = (env) => assetsNginxConf({ repoRoot, env })

beforeEach(() => {
  repoRoot = mkdtempSync(join(tmpdir(), 'demo-assets-'))
})
afterEach(() => {
  rmSync(repoRoot, { recursive: true, force: true })
})

describe('the assets declaration in the scan', () => {
  it('is optional — a demo that declares none is simply absent', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('demo-04') })
    expect(scanDemoManifests({ repoRoot }).entries[0].assets).toBeNull()
    expect(conf()).not.toContain('proxy_pass')
  })

  it('is normalised to one upstream', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('demo-04'), metadata: assetsMetadata })
    expect(scanDemoManifests({ repoRoot }).entries[0].assets)
      .toEqual({ hostedUpstream: 'http://demo04-frontend:80' })
  })

  /* An upstream is an ORIGIN. A path here would be a second, hidden rewrite
     of the public layout — the demo's own compiler already decided what the
     chunk URLs say, and a rewrite would put the two out of step. */
  it('refuses anything that is not a bare origin', () => {
    const refused = [
      'http://demo04-frontend:80/plugins/demo-04/', // a path
      'http://demo04-frontend:80/',                 // even a bare slash
      '//demo04-frontend',                          // not a URL this can name
      'demo04-frontend:80',
      'ftp://demo04-frontend',
      'http://demo04-frontend:80?x=1',
      '',
      42,
    ]
    for (const hostedUpstream of refused) {
      rmSync(repoRoot, { recursive: true, force: true })
      repoRoot = mkdtempSync(join(tmpdir(), 'demo-assets-'))
      demo('04-jetstream-cqrs', { manifest: manifestFor('demo-04'), metadata: { assets: { hostedUpstream } } })
      expect(scanDemoManifests({ repoRoot }).entries[0].assets, String(hostedUpstream)).toBeNull()
    }
  })
})

describe('the hosted nginx snippet', () => {
  beforeEach(() => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('demo-04'), metadata: assetsMetadata })
  })

  /* The reported failure, stated as the fix. Without this location the
     packaged registry shell answers 404 for the remote entry it just told the
     browser to load. */
  it('serves the plugin prefix from the demo\'s own image', () => {
    expect(conf()).toContain(`location ${pluginAssetBase('demo-04')} {`)
    expect(conf()).toContain('proxy_pass $demo_assets_04_jetstream_cqrs$uri$is_args$args;')
  })

  /* BR-AS77: the prefix covers the WHOLE plugin. A prefix location with the
     URI passed through unchanged is what makes that true for a lazy chunk and
     a font as well as for the entry — none of which the shell can name, since
     the plugin's own compiler writes them. */
  it('is a prefix, not an exact match, and rewrites nothing', () => {
    expect(conf()).not.toContain(`location = ${pluginAssetBase('demo-04')}`)
    expect(conf()).not.toContain('rewrite')
  })

  /* nginx resolves a literal proxy_pass host once, at startup, and refuses to
     start when it cannot. A stopped demo would then take the whole lab down
     instead of being one demo that will not mount. */
  it('defers the lookup so a stopped demo cannot stop the shell', () => {
    expect(conf()).toContain('resolver 127.0.0.11 ipv6=off valid=10s;')
    expect(conf()).toContain('set $demo_assets_04_jetstream_cqrs "http://demo04-frontend:80";')
  })

  it('never intercepts the upstream status, so a missing asset stays a 404', () => {
    expect(conf()).not.toContain('proxy_intercept_errors on')
    expect(conf()).not.toContain('index.html')
    expect(conf()).not.toContain('try_files')
  })

  /* Build mode packages the files into the shell's own tree. Two rules on one
     prefix is one too many, and the proxy would win — nginx takes the longest
     prefix, and `/plugins/<id>/` is longer than `/plugins/`. */
  it('emits nothing at all for a build-mode shell', () => {
    const built = conf({ VITE_PLUGIN_SOURCE: 'build' })
    expect(built).not.toContain('proxy_pass')
    /* Every line is a comment. The word "location" appears in one of them,
       pointing at the rule in nginx.conf that does serve these files. */
    expect(built.split('\n').filter((line) => line.trim() !== '' && !line.startsWith('#'))).toEqual([])
    expect(built).toContain('plugin-source: build')
  })

  it('treats an unset source as registry, which is BR-AS75\'s default', () => {
    expect(conf()).toEqual(conf({}))
    expect(conf()).toEqual(conf({ VITE_PLUGIN_SOURCE: 'registry' }))
  })

  it('fails the build on a source it does not know, rather than guessing', () => {
    expect(() => conf({ VITE_PLUGIN_SOURCE: 'buidl' })).toThrow(/VITE_PLUGIN_SOURCE/)
  })

  /* `nginx.conf` includes this file unconditionally, and a missing include is
     a container that will not start. */
  it('is still a file when no demo declared anything', () => {
    rmSync(repoRoot, { recursive: true, force: true })
    repoRoot = mkdtempSync(join(tmpdir(), 'demo-assets-'))
    expect(conf()).toContain('No demo declared a hosted asset upstream')
  })

  it('names one location per demo, derived and never hand-written', () => {
    demo('05-other', {
      manifest: manifestFor('demo-05'),
      metadata: { assets: { hostedUpstream: 'http://demo05-frontend:80' } },
    })
    const rendered = conf()
    expect(rendered).toContain(`location ${pluginAssetBase('demo-05')} {`)
    expect(rendered.match(/proxy_pass/g)).toHaveLength(2)
  })
})
