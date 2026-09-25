import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { validateManifest } from './manifestSchema.js'
import { RESERVED_ROUTE_PREFIXES } from './reservedPrefixes.js'

/*
  BR-AS12 — a plugin may not claim a segment the shell owns.

  The list is checked three ways. The schema refuses each entry with its own
  cause. The list is re-derived from the files that actually own the paths —
  the shell's route table, nginx and the dev proxy — so a new shell route or
  a new hosting location that is not reserved fails here. And the registry's
  copy in Go is parsed and held equal, so the two doors cannot drift.
*/

/* Vitest runs from lab-shell/. */
const read = (rel) => readFileSync(resolve(process.cwd(), rel), 'utf8')
const RESERVED = Object.keys(RESERVED_ROUTE_PREFIXES).sort()
const firstSegment = (path) => /^\/([a-z0-9-]+)/.exec(path)?.[1] ?? null

const manifest = ({ id = 'fleet-ops', routePrefix } = {}) => {
  const prefix = routePrefix ?? id
  return {
    id,
    name: 'Fleet Ops',
    schemaVersion: REGISTRY_SCHEMA_VERSION,
    shellApiVersion: SHELL_API_VERSION,
    ...(routePrefix === undefined ? {} : { routePrefix }),
    remote: { kind: 'federated', url: 'http://localhost:7111/remoteEntry.js', module: './plugin' },
    contributions: [
      { kind: 'route', id: 'vessels', path: `/${prefix}/vessels`, title: 'Vessels' },
    ],
  }
}

describe('the schema refuses a reserved prefix', () => {
  it.each(RESERVED)('refuses %s, naming who owns it', (prefix) => {
    const result = validateManifest(manifest({ routePrefix: prefix }))

    expect(result.ok).toBe(false)
    expect(result.code).toBe('reserved-route-prefix')
    expect(result.message).toContain(RESERVED_ROUTE_PREFIXES[prefix])
  })

  it('holds a prefix that defaults to the id to the same rule', () => {
    const result = validateManifest(manifest({ id: 'api' }))

    expect(result.code).toBe('reserved-route-prefix')
  })

  it('admits a segment that only starts like a reserved one', () => {
    expect(validateManifest(manifest({ routePrefix: 'plugins-archive' })).ok).toBe(true)
    expect(validateManifest(manifest({ routePrefix: 'api-docs' })).ok).toBe(true)
  })

  it('admits every prefix a plugin in this repo declares today', () => {
    for (const prefix of ['demos', 'demo-04', 'example', 'example-slow']) {
      expect(validateManifest(manifest({ routePrefix: prefix })).ok).toBe(true)
    }
  })
})

describe('the list is the one the shell\'s own files imply', () => {
  const shellRouteSegments = [...read('src/main.js').matchAll(/\bpath:\s*'([^']+)'/g)]
    .map((m) => firstSegment(m[1]))
    .filter(Boolean)

  const nginx = read('nginx.conf')
  const nginxSegments = [...nginx.matchAll(/^\s*location\s+(?:=\s*)?(\S+)\s*\{/gm)]
    .map((m) => firstSegment(m[1]))
    .filter(Boolean)

  const vite = read('vite.config.js')
  const proxyBlock = vite.slice(vite.indexOf('proxy: {'))
  const proxySegments = [...proxyBlock.matchAll(/^\s*'\^?(\/[^']*)'\s*:\s*\{/gm)]
    .map((m) => firstSegment(m[1]))
    .filter(Boolean)

  it('reserves every static first segment of a shell route', () => {
    expect(shellRouteSegments).toEqual(expect.arrayContaining(['plugins', 'lab-demos']))
    for (const segment of shellRouteSegments) expect(RESERVED).toContain(segment)
  })

  it('reserves every path nginx answers before the SPA', () => {
    expect(nginxSegments.length).toBeGreaterThan(0)
    for (const segment of nginxSegments) expect(RESERVED).toContain(segment)
  })

  it('reserves every path the dev proxy answers', () => {
    expect(proxySegments).toEqual(expect.arrayContaining(['api', 'nats']))
    for (const segment of proxySegments) expect(RESERVED).toContain(segment)
  })

  it('reserves the build output directory, which vite leaves at its default', () => {
    expect(vite).not.toMatch(/assetsDir/)
    expect(RESERVED).toContain('assets')
  })

  it('reserves nothing that no source owns', () => {
    const owned = new Set([
      ...shellRouteSegments,
      ...nginxSegments,
      ...proxySegments,
      // Emitted into the generated nginx snippets; the list imports them.
      'demo-api',
      'demo-readiness',
      'assets',
    ])
    for (const segment of RESERVED) expect(owned).toContain(segment)
  })
})

describe('the registry refuses the same list', () => {
  const go = read('../demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain/admissible.go')
  const literal = /var reservedRoutePrefixes = map\[string\]string\{([\s\S]*?)\n\}/.exec(go)?.[1] ?? ''
  const goList = Object.fromEntries(
    [...literal.matchAll(/^\s*"([^"]+)":\s*"([^"]*)",/gm)].map((m) => [m[1], m[2]]),
  )

  it('finds the Go literal', () => {
    expect(Object.keys(goList).length).toBeGreaterThan(0)
  })

  it('holds the same prefixes, each with the same owner', () => {
    expect(goList).toEqual({ ...RESERVED_ROUTE_PREFIXES })
  })
})
