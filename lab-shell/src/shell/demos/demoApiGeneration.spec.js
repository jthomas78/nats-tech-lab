/* A demo's own command surface, served same-origin (BR-AS82, task 16j).

   Two outputs, one scan: the development proxy map and the hosted nginx
   snippet. These specs hold the property that makes the arrangement safe
   rather than merely working — that the shell forwards ONLY the routes a demo
   named out loud, so the route that fixed demo 04's buttons did not also
   become a general tunnel to its backend.

   The code lives under `tools/` because it runs in Node; the spec lives here
   because this is the shell's only Vitest runner. */
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { apiNginxConf, demoApiProxy } from '../../../tools/buildCatalogue/demoApi.js'
import { scanDemoManifests } from '../../../tools/buildCatalogue/scanDemos.js'
import { DEMO_API_PREFIX, DEMO_READINESS_PREFIX, demoApiPath } from './demoCatalogueLocation.js'

let repoRoot

const manifestFor = (id) => ({
  id,
  name: id,
  schemaVersion: 1,
  shellApiVersion: 1,
  remote: { kind: 'federated', url: '/remoteEntry.js', module: 'plugin' },
  contributions: [{ kind: 'route', id: 'main', path: `/${id}`, title: id }],
})

const apiMetadata = {
  api: {
    devPort: 20402,
    hostedUpstream: 'http://demo04-cqrs:20402',
    routes: ['/pool', '/pool/run', '/commands/'],
  },
}

function demo(name, { manifest, metadata } = {}) {
  const dir = join(repoRoot, 'demos', name, 'frontend', 'public')
  mkdirSync(dir, { recursive: true })
  if (manifest !== undefined) writeFileSync(join(dir, 'manifest.json'), JSON.stringify(manifest))
  if (metadata !== undefined) writeFileSync(join(dir, 'demo.json'), JSON.stringify(metadata))
}

beforeEach(() => {
  repoRoot = mkdtempSync(join(tmpdir(), 'demo-api-'))
})
afterEach(() => {
  rmSync(repoRoot, { recursive: true, force: true })
})

/** Does any generated proxy key match this shell-origin path? */
const routed = (url) => Object.keys(demoApiProxy({ repoRoot }))
  .some((key) => new RegExp(key).test(url))

describe('the api declaration in the scan', () => {
  it('is optional — a demo that declares none is simply absent', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('demo-04') })
    expect(scanDemoManifests({ repoRoot }).entries[0].api).toBeNull()
    expect(demoApiProxy({ repoRoot })).toEqual({})
  })

  it('is normalised once, with its routes deduplicated and ordered', () => {
    demo('04-jetstream-cqrs', {
      manifest: manifestFor('demo-04'),
      metadata: { api: { ...apiMetadata.api, routes: ['/pool/run', '/pool', '/pool'] } },
    })
    expect(scanDemoManifests({ repoRoot }).entries[0].api).toEqual({
      devPort: 20402,
      hostedUpstream: 'http://demo04-cqrs:20402',
      routes: ['/pool', '/pool/run'],
    })
  })

  /* A port and no routes is the tunnel this rule exists to refuse. It must
     read as no declaration, never as "forward everything". */
  it('reads a declaration with no routes as no declaration at all', () => {
    demo('04-jetstream-cqrs', {
      manifest: manifestFor('demo-04'),
      metadata: { api: { devPort: 20402, hostedUpstream: 'http://x:1', routes: [] } },
    })
    expect(scanDemoManifests({ repoRoot }).entries[0].api).toBeNull()
  })

  it.each([
    ['a relative path, which names nothing here', 'pool'],
    ['an origin wearing a path’s clothes', '//evil.example/pool'],
    ['a climb out of the demo’s own surface', '/../secret'],
    ['a query, which belongs to the caller', '/pool?drain=1'],
  ])('drops %s', (_why, route) => {
    demo('04-jetstream-cqrs', {
      manifest: manifestFor('demo-04'),
      metadata: { api: { devPort: 20402, routes: [route, '/pool'] } },
    })
    expect(scanDemoManifests({ repoRoot }).entries[0].api.routes).toEqual(['/pool'])
  })

  it('needs somewhere to forward to, not just a list of paths', () => {
    demo('04-jetstream-cqrs', {
      manifest: manifestFor('demo-04'),
      metadata: { api: { routes: ['/pool'] } },
    })
    expect(scanDemoManifests({ repoRoot }).entries[0].api).toBeNull()
  })

  /* The whole point of the sibling file (BR-AS78). */
  it('cannot admit a plugin or supply a remote.url', () => {
    demo('04-jetstream-cqrs', {
      manifest: manifestFor('demo-04'),
      metadata: { ...apiMetadata, id: 'other', remote: { url: 'https://evil.example/x' } },
    })
    const [entry] = scanDemoManifests({ repoRoot }).entries
    expect(entry.id).toBe('demo-04')
    expect(entry.manifest.remote.url).toBe('/remoteEntry.js')
  })
})

describe('the development proxy', () => {
  beforeEach(() => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('demo-04'), metadata: apiMetadata })
  })

  it('makes one entry per declared route, never one per demo', () => {
    expect(Object.keys(demoApiProxy({ repoRoot }))).toHaveLength(3)
  })

  it('forwards every named route', () => {
    for (const route of ['/pool', '/pool/run', '/commands/travel']) {
      expect(routed(`${demoApiPath('04-jetstream-cqrs')}${route}`)).toBe(true)
    }
  })

  /* The single property this rule rests on. `/readyz` is the sharpest case:
     the demo's backend really does serve it, and the demo really does declare
     it — under `readiness`, for the shell to call. It is deliberately NOT in
     `api.routes`, and so it must not be reachable here. */
  it.each([
    ['an unnamed route the backend does serve', '/readyz'],
    ['an unnamed route it does not', '/debug/pprof'],
    ['a suffix on a named exact route', '/pool/run/extra'],
    ['the bare base', ''],
    ['a climb', '/pool/../readyz'],
  ])('refuses %s', (_why, suffix) => {
    expect(routed(`${demoApiPath('04-jetstream-cqrs')}${suffix}`)).toBe(false)
  })

  it('serves a trailing-slash route as a prefix, the way serve.go registers it', () => {
    expect(routed(`${demoApiPath('04-jetstream-cqrs')}/commands/register`)).toBe(true)
    expect(routed(`${demoApiPath('04-jetstream-cqrs')}/commands/`)).toBe(true)
  })

  it('carries a query through, because a query is the caller’s', () => {
    expect(routed(`${demoApiPath('04-jetstream-cqrs')}/pool?x=1`)).toBe(true)
  })

  it('strips only the shell’s own base, leaving the demo’s path alone', () => {
    const [entry] = Object.values(demoApiProxy({ repoRoot }))
    expect(entry.target).toBe('http://localhost:20402')
    expect(entry.rewrite(`${demoApiPath('04-jetstream-cqrs')}/pool/run?x=1`)).toBe('/pool/run?x=1')
  })

  /* No demo backend was given a CORS grant for this, and none needs one: the
     browser calls the shell's origin and the shell calls the demo. A proxy
     that rewrote the Host header would be a different arrangement. */
  it('does not change the origin it presents upstream', () => {
    for (const entry of Object.values(demoApiProxy({ repoRoot }))) {
      expect(entry.changeOrigin).toBe(false)
    }
  })

  it('is never under the plugin asset prefix or the readiness prefix', () => {
    for (const key of Object.keys(demoApiProxy({ repoRoot }))) {
      expect(key).not.toContain('/plugins/')
      expect(key).not.toContain(DEMO_READINESS_PREFIX)
      expect(key.startsWith(`^${DEMO_API_PREFIX}/`)).toBe(true)
    }
  })

  it('falls back to the hosted upstream when no dev port is declared', () => {
    rmSync(repoRoot, { recursive: true, force: true })
    mkdirSync(repoRoot, { recursive: true })
    demo('04-jetstream-cqrs', {
      manifest: manifestFor('demo-04'),
      metadata: { api: { hostedUpstream: 'http://demo04-cqrs:20402', routes: ['/pool'] } },
    })
    expect(Object.values(demoApiProxy({ repoRoot }))[0].target).toBe('http://demo04-cqrs:20402')
  })
})

describe('the hosted nginx snippet', () => {
  it('is always emitted, so an unconditional include cannot break the container', () => {
    const conf = apiNginxConf({ repoRoot })
    expect(conf).toContain('No demo declared a hosted API upstream')
    expect(conf).toContain(`location ${DEMO_API_PREFIX}/ {`)
    expect(conf).toContain('return 404;')
  })

  it('writes one location per named route, exact unless the demo asked for a prefix', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('demo-04'), metadata: apiMetadata })
    const conf = apiNginxConf({ repoRoot })
    expect(conf).toContain('location = /demo-api/04-jetstream-cqrs/pool {')
    expect(conf).toContain('location = /demo-api/04-jetstream-cqrs/pool/run {')
    expect(conf).toContain('location /demo-api/04-jetstream-cqrs/commands/ {')
    expect(conf).not.toContain('/readyz')
  })

  /* nginx resolves a literal proxy_pass host once, at startup, and refuses to
     start when it cannot. A stopped demo must not take the whole shell down. */
  it('passes each upstream through a variable with a resolver', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('demo-04'), metadata: apiMetadata })
    const conf = apiNginxConf({ repoRoot })
    expect(conf).toContain('resolver 127.0.0.11 ipv6=off valid=10s;')
    expect(conf).toContain('set $demo_api_04_jetstream_cqrs "http://demo04-cqrs:20402";')
    expect(conf).toContain('proxy_pass $demo_api_04_jetstream_cqrs;')
  })

  it('closes the prefix with 404 so nothing falls through to the SPA', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('demo-04'), metadata: apiMetadata })
    const conf = apiNginxConf({ repoRoot })
    expect(conf.trimEnd().endsWith('}')).toBe(true)
    expect(conf).toContain(`location ${DEMO_API_PREFIX}/ {\n    return 404;\n}`)
  })

  it('leaves out a demo with no hosted upstream, rather than inventing one', () => {
    demo('04-jetstream-cqrs', {
      manifest: manifestFor('demo-04'),
      metadata: { api: { devPort: 20402, routes: ['/pool'] } },
    })
    expect(apiNginxConf({ repoRoot })).toContain('No demo declared a hosted API upstream')
  })
})
