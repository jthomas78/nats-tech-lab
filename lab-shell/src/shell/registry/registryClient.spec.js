import { describe, expect, it, vi } from 'vitest'

import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { validateManifest } from './manifestSchema.js'
import { createRegistryClient, REGISTRY_ENDPOINT } from './registryClient.js'
import { RemoteAllowlist } from './remoteAllowlist.js'

const jsonResponse = (body, status = 200) => ({
  ok: status >= 200 && status < 300,
  status,
  json: async () => body,
})

const manifest = (overrides = {}) => ({
  id: 'example-plugin',
  name: 'Example Plugin',
  schemaVersion: REGISTRY_SCHEMA_VERSION,
  shellApiVersion: SHELL_API_VERSION,
  remote: { kind: 'federated', url: 'http://localhost:7110/remoteEntry.js', module: './plugin' },
  contributions: [{ kind: 'route', id: 'vessels', path: '/example-plugin/vessels', title: 'Vessels' }],
  ...overrides,
})

describe('BR-AS01 — curated discovery', () => {
  it('reads the registry from accounts-service, not from the plugins themselves', async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse({ schemaVersion: REGISTRY_SCHEMA_VERSION, plugins: [manifest()] }),
    )
    const client = createRegistryClient({ fetch: fetchImpl })

    const result = await client.fetchRegistry()

    expect(fetchImpl).toHaveBeenCalledTimes(1)
    expect(fetchImpl.mock.calls[0][0]).toBe(REGISTRY_ENDPOINT)
    expect(result.plugins).toHaveLength(1)
  })

  it('allows only remote URLs that came from the curated document', () => {
    const allowlist = new RemoteAllowlist()
    allowlist.add(validateManifest(manifest()).plugin)

    expect(allowlist.allows('http://localhost:7110/remoteEntry.js')).toBe(true)
    expect(allowlist.allows('http://evil.example/remoteEntry.js')).toBe(false)
  })

  it('does not allow a URL from a manifest that failed validation', () => {
    // The rejected manifest names a perfectly reachable remote. Being
    // reachable is not being curated.
    const rejected = validateManifest(manifest({ schemaVersion: 99 }))
    const allowlist = new RemoteAllowlist()
    allowlist.add(rejected.plugin)

    expect(rejected.ok).toBe(false)
    expect(allowlist.size).toBe(0)
  })


  it('sends the viewer credentials and refuses a shared cache', async () => {
    // The document is per-viewer (BR-AS05), so a cached copy would leak one
    // account's curated plugin list to another.
    const fetchImpl = vi.fn(async () =>
      jsonResponse({ schemaVersion: REGISTRY_SCHEMA_VERSION, plugins: [] }),
    )

    await createRegistryClient({ fetch: fetchImpl }).fetchRegistry()

    expect(fetchImpl.mock.calls[0][1]).toMatchObject({
      credentials: 'same-origin',
      cache: 'no-store',
    })
  })
})

describe('BR-AS04 — a registry failure is a degraded shell, not a broken one', () => {
  it('returns a result rather than throwing when the endpoint is unreachable', async () => {
    const fetchImpl = vi.fn(async () => {
      throw new TypeError('Failed to fetch')
    })

    const result = await createRegistryClient({ fetch: fetchImpl }).fetchRegistry()

    expect(result.ok).toBe(false)
    expect(result.code).toBe('registry-unreachable')
  })

  it('reports an HTTP failure with its status', async () => {
    const result = await createRegistryClient({
      fetch: async () => jsonResponse({}, 503),
    }).fetchRegistry()

    expect(result.code).toBe('registry-http-503')
  })

  it('reports a non-JSON body without throwing', async () => {
    const result = await createRegistryClient({
      fetch: async () => ({
        ok: true,
        status: 200,
        json: async () => {
          throw new SyntaxError('Unexpected token <')
        },
      }),
    }).fetchRegistry()

    expect(result.code).toBe('registry-malformed')
  })

  it('gives up on a hung registry rather than hanging the boot', async () => {
    const fetchImpl = vi.fn((_url, { signal }) =>
      new Promise((_resolve, reject) => {
        signal.addEventListener('abort', () => {
          const error = new Error('aborted')
          error.name = 'AbortError'
          reject(error)
        })
      }),
    )

    const result = await createRegistryClient({ fetch: fetchImpl, timeoutMs: 5 }).fetchRegistry()

    expect(result.code).toBe('registry-timeout')
  })

  it('rejects a document whose schema version it does not understand', async () => {
    const result = await createRegistryClient({
      fetch: async () => jsonResponse({ schemaVersion: 99, plugins: [manifest()] }),
    }).fetchRegistry()

    expect(result.code).toBe('unsupported-schema-version')
  })
})
