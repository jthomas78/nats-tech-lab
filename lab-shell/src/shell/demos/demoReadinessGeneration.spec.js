/* The generated half of demo readiness (BR-AS79, task 16e).

   Three outputs, one scan: the shell-owned demo catalogue, the development
   proxy map, and the hosted nginx snippet. These specs hold the properties
   that make the arrangement safe rather than merely working — that the
   catalogue publishes no upstream, that neither route can be widened into a
   tunnel, and that a readiness declaration admits nothing.

   The code lives under `tools/` because it runs in Node; the spec lives here
   because this is the shell's only Vitest runner. */
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import {
  demoReadiness,
  demoReadinessProxy,
  readinessNginxConf,
} from '../../../tools/buildCatalogue/demoReadiness.js'
import {
  demoCatalogueDocument,
  scanDemoManifests,
} from '../../../tools/buildCatalogue/scanDemos.js'
import { validateRegistryDocument } from '../registry/manifestSchema.js'

let repoRoot

const manifestFor = (id) => ({
  id,
  name: id,
  schemaVersion: 1,
  shellApiVersion: 1,
  remote: { kind: 'federated', url: '/remoteEntry.js', module: 'plugin' },
  contributions: [{ kind: 'route', id: 'main', path: `/${id}`, title: id }],
})

const readyMetadata = {
  readiness: {
    path: '/readyz',
    devPort: 20402,
    hostedUpstream: 'http://demo04-cqrs:20402',
    timeoutMs: 3000,
  },
  runCommand: 'docker compose up -d',
}

/** A demo with a manifest, and optionally the sibling metadata file. */
function demo(name, { manifest, metadata } = {}) {
  const dir = join(repoRoot, 'demos', name, 'frontend', 'public')
  mkdirSync(dir, { recursive: true })
  if (manifest !== undefined) {
    writeFileSync(join(dir, 'manifest.json'), JSON.stringify(manifest))
  }
  if (metadata !== undefined) {
    writeFileSync(join(dir, 'demo.json'), JSON.stringify(metadata))
  }
}

beforeEach(() => {
  repoRoot = mkdtempSync(join(tmpdir(), 'demo-readiness-'))
})
afterEach(() => {
  rmSync(repoRoot, { recursive: true, force: true })
})

const catalogueFor = (root = repoRoot) =>
  demoCatalogueDocument(scanDemoManifests({ repoRoot: root }).entries)

describe('the readiness declaration in the scan', () => {
  it('is optional — a demo that declares none is simply absent', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('cqrs') })
    expect(scanDemoManifests({ repoRoot }).entries[0].readiness).toBeNull()
    expect(catalogueFor().demos).toEqual([])
  })

  it('is normalised once, not at each of its four readers', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('cqrs'), metadata: readyMetadata })
    expect(scanDemoManifests({ repoRoot }).entries[0].readiness).toEqual({
      path: '/readyz',
      devPort: 20402,
      hostedUpstream: 'http://demo04-cqrs:20402',
      timeoutMs: 3000,
    })
  })

  /* "The demo is not set up to be checked" and "the demo cannot be reached"
     are different answers. A half-written declaration must become the first,
     never a probe against port NaN that then reports the second. */
  it('reads a half-written declaration as no declaration at all', () => {
    demo('a', { manifest: manifestFor('a'), metadata: { readiness: { path: '/readyz' } } })
    demo('b', { manifest: manifestFor('b'), metadata: { readiness: { devPort: 1 } } })
    demo('c', { manifest: manifestFor('c'), metadata: { readiness: { path: 'readyz', devPort: 1 } } })
    for (const entry of scanDemoManifests({ repoRoot }).entries) {
      expect(entry.readiness).toBeNull()
    }
  })

  it('lets a demo ask for longer than the default, but not for forever', () => {
    const withTimeout = (ms) => ({ readiness: { ...readyMetadata.readiness, timeoutMs: ms } })
    demo('a', { manifest: manifestFor('a'), metadata: withTimeout(9000) })
    demo('b', { manifest: manifestFor('b'), metadata: withTimeout(600000) })
    demo('c', { manifest: manifestFor('c'), metadata: withTimeout(1) })
    const [a, b, c] = scanDemoManifests({ repoRoot }).entries
    expect(a.readiness.timeoutMs).toBe(9000)
    expect(b.readiness.timeoutMs).toBe(15000)
    expect(c.readiness.timeoutMs).toBe(250)
  })
})

describe('the shell-owned demo catalogue', () => {
  it('associates a demo with a plugin by two stable identifiers', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('cqrs'), metadata: readyMetadata })
    const [entry] = catalogueFor().demos
    expect(entry.demo).toBe('04-jetstream-cqrs')
    expect(entry.pluginId).toBe('cqrs')
  })

  /* R-1. A plugin found by `registry` and the same plugin found by `build`
     match the same entry, so neither identifier may encode a source. */
  it('names no plugin source anywhere in the document', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('cqrs'), metadata: readyMetadata })
    const body = JSON.stringify(catalogueFor())
    expect(body).not.toContain('registry')
    expect(body).not.toContain('build')
  })

  /* F-3. A page that knew the port could call it directly, which is the
     cross-origin exception the whole arrangement exists to avoid. */
  it('publishes no port and no upstream — only the shell\'s own route', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('cqrs'), metadata: readyMetadata })
    const body = JSON.stringify(catalogueFor())
    expect(body).not.toContain('20402')
    expect(body).not.toContain('demo04-cqrs')
    expect(body).not.toContain('/readyz')
    expect(catalogueFor().demos[0].readiness.url).toBe('/demo-readiness/04-jetstream-cqrs')
  })

  it('carries the local run command, leaving who sees it to the deployment', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('cqrs'), metadata: readyMetadata })
    expect(catalogueFor().demos[0].runCommand).toBe('docker compose up -d')
  })

  it('carries a null run command when the demo declared none', () => {
    demo('a', { manifest: manifestFor('a'), metadata: { readiness: readyMetadata.readiness } })
    expect(catalogueFor().demos[0].runCommand).toBeNull()
  })

  it('changes its revision when a demo changes, and only then', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('cqrs'), metadata: readyMetadata })
    const first = catalogueFor().revision
    expect(catalogueFor().revision).toBe(first)
    demo('02-multi-region', { manifest: manifestFor('region'), metadata: readyMetadata })
    expect(catalogueFor().revision).not.toBe(first)
  })

  it('is an empty catalogue, not a fault, when the lab has no demos', () => {
    expect(catalogueFor().demos).toEqual([])
  })
})

/* BR-AS79's hardest line: readiness metadata carries no authority. */
describe('what a readiness declaration cannot do', () => {
  it('cannot admit a plugin — a demo with no manifest stays invisible', () => {
    demo('05-nothing', { metadata: readyMetadata })
    expect(catalogueFor().demos).toEqual([])
    expect(demoReadinessProxy({ repoRoot })).toEqual({})
  })

  it('cannot supply or override a remote.url', () => {
    demo('04-jetstream-cqrs', {
      manifest: manifestFor('cqrs'),
      metadata: { ...readyMetadata, remote: { url: 'http://evil.example/remoteEntry.js' } },
    })
    const { plugins } = scanDemoManifests({ repoRoot })
    expect(plugins[0].remote.url).toBe('/remoteEntry.js')
    expect(JSON.stringify(catalogueFor())).not.toContain('evil.example')
  })

  /* F-1: prove it, do not assume it. The registry's drift check hashes this
     file, so a byte added here would be a false drift report in the other
     plugin source. */
  it('leaves manifest.json byte-unchanged', () => {
    const manifest = manifestFor('cqrs')
    const raw = JSON.stringify(manifest)
    demo('04-jetstream-cqrs', { manifest, metadata: readyMetadata })
    const { plugins } = scanDemoManifests({ repoRoot })
    expect(JSON.stringify(plugins[0])).toBe(raw)
    expect(validateRegistryDocument({
      schemaVersion: 1, revision: 'r', degraded: false, plugins,
    }).ok).toBe(true)
  })
})

describe('the development proxy', () => {
  it('forwards the shell\'s route to the demo\'s own path', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('cqrs'), metadata: readyMetadata })
    const proxy = demoReadinessProxy({ repoRoot })
    const key = '^/demo-readiness/04-jetstream-cqrs$'
    expect(Object.keys(proxy)).toEqual([key])
    expect(proxy[key].target).toBe('http://localhost:20402')
    expect(proxy[key].rewrite()).toBe('/readyz')
  })

  /* A prefix key would turn one readiness route into a general tunnel to a
     backend the page is not allowed to call. */
  it('matches one exact route, which a path suffix cannot widen', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('cqrs'), metadata: readyMetadata })
    const [key] = Object.keys(demoReadinessProxy({ repoRoot }))
    const pattern = new RegExp(key)
    expect(pattern.test('/demo-readiness/04-jetstream-cqrs')).toBe(true)
    expect(pattern.test('/demo-readiness/04-jetstream-cqrs/commands')).toBe(false)
    expect(pattern.test('/demo-readiness/04-jetstream-cqrs?x=1')).toBe(false)
  })

  /* BR-AS77: the asset prefix is files, this is a call. R-2 records that the
     two mappings are separate for that reason. */
  it('never places a readiness route under the plugin asset prefix', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('cqrs'), metadata: readyMetadata })
    for (const key of Object.keys(demoReadinessProxy({ repoRoot }))) {
      expect(key).not.toContain('/plugins/')
    }
  })

  it('skips a demo that declared no readiness', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('cqrs') })
    expect(demoReadinessProxy({ repoRoot })).toEqual({})
  })
})

describe('the hosted nginx snippet', () => {
  it('writes one exact-match location per demo, pointing at its upstream', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('cqrs'), metadata: readyMetadata })
    const conf = readinessNginxConf({ repoRoot })
    expect(conf).toContain('location = /demo-readiness/04-jetstream-cqrs {')
    expect(conf).toContain('set $demo_readiness_04_jetstream_cqrs "http://demo04-cqrs:20402/readyz";')
    expect(conf).toContain('proxy_pass $demo_readiness_04_jetstream_cqrs;')
  })

  /* The whole shell must not refuse to start because one demo is stopped.
     nginx resolves a LITERAL proxy_pass host once, at startup, and exits when
     it cannot — which it did: `nginx -t` failed with "host not found in
     upstream demo04-cqrs" on an image whose demo container was simply not
     running. A variable defers the lookup to the request, so an absent demo
     is a 502 the shell reports as one demo that cannot be reached (BR-AS79),
     and every other demo is still served. */
  it('defers the upstream lookup to the request, so a stopped demo cannot stop the shell', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('cqrs'), metadata: readyMetadata })
    const conf = readinessNginxConf({ repoRoot })
    expect(conf).toContain('resolver 127.0.0.11')
    expect(conf).not.toMatch(/proxy_pass https?:\/\//)
  })

  /* An unproxied probe answering 200 with a page of HTML would read as a demo
     that is up. Both environments answer 404 instead. */
  it('closes the prefix with a 404, so nothing falls through to the SPA', () => {
    demo('04-jetstream-cqrs', { manifest: manifestFor('cqrs'), metadata: readyMetadata })
    expect(readinessNginxConf({ repoRoot })).toContain('location /demo-readiness/ {\n    return 404;\n}')
  })

  /* nginx.conf includes it unconditionally, and a missing include is a
     container that will not start. */
  it('is still a valid file when no demo declared readiness', () => {
    const conf = readinessNginxConf({ repoRoot })
    expect(conf).toContain('No demo declared a hosted readiness upstream.')
    expect(conf).toContain('return 404;')
  })

  it('omits a demo that declared only a dev port', () => {
    demo('a', {
      manifest: manifestFor('a'),
      metadata: { readiness: { path: '/readyz', devPort: 1234 } },
    })
    expect(readinessNginxConf({ repoRoot })).not.toContain('location = /demo-readiness/a')
  })
})

/* The dev server's middleware ORDER, which is the one thing about this plugin
   that no unit of the generators can show. It cost a real bug: added in the
   wrong position, the prefix 404 answered every readiness call itself and the
   demo was never reached; moved behind the proxy, the SPA fallback answered
   an unknown path with 200 and a page of HTML, which reads as a demo that is
   up. Only one position is correct, and these specs hold it. */
describe('the development middleware', () => {
  /* A Connect-like stub. `pre` is what the plugin registered in the hook body
     (in FRONT of Vite's proxy and SPA fallback); `post` is what it returned
     (BEHIND both). */
  function serve(root) {
    const pre = []
    const server = { middlewares: { use: (fn) => pre.push(fn) }, watcher: { add: () => {} } }
    const post = demoReadiness({ repoRoot: root }).configureServer(server)
    return { pre, post }
  }

  const answer = (pre, url) => {
    const res = { statusCode: 200, setHeader: () => {}, end(body) { this.body = body } }
    let passed = false
    pre[0]({ url }, res, () => { passed = true })
    return { passed, status: res.statusCode, body: res.body }
  }

  beforeEach(() => demo('04-jetstream-cqrs', { manifest: manifestFor('demo-04'), metadata: readyMetadata }))

  it('registers in front of Vite\'s own stack, not behind it', () => {
    const { pre, post } = serve(repoRoot)
    expect(pre).toHaveLength(1)
    /* Returning a function would put it behind the SPA fallback. */
    expect(post).toBeUndefined()
  })

  /* In front, it must then keep its hands off the paths the proxy owns. */
  it('passes a demo\'s own readiness route through to the proxy', () => {
    const { pre } = serve(repoRoot)
    expect(answer(pre, '/demo-readiness/04-jetstream-cqrs').passed).toBe(true)
  })

  it('answers an unknown readiness path itself, and never lets it reach the SPA', () => {
    const { pre } = serve(repoRoot)
    const out = answer(pre, '/demo-readiness/nope')
    expect(out.passed).toBe(false)
    expect(out.status).toBe(404)
  })

  /* The exact-match rule, at the one place it can be worked around: the proxy
     keys are anchored, so a query string misses them. */
  it('does not treat a suffixed or queried route as the readiness route', () => {
    const { pre } = serve(repoRoot)
    expect(answer(pre, '/demo-readiness/04-jetstream-cqrs?x=1').status).toBe(404)
    expect(answer(pre, '/demo-readiness/04-jetstream-cqrs/more').status).toBe(404)
  })

  it('serves the demo catalogue, which is not proxied anywhere', () => {
    const { pre } = serve(repoRoot)
    const out = answer(pre, '/demo-catalogue.json')
    expect(out.passed).toBe(false)
    expect(JSON.parse(out.body).demos[0].demo).toBe('04-jetstream-cqrs')
  })

  it('leaves every other request alone', () => {
    const { pre } = serve(repoRoot)
    expect(answer(pre, '/plugins/demo-04/remoteEntry.js').passed).toBe(true)
  })
})
