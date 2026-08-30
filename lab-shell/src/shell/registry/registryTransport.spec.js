/*
  BR-AS27 / decision 58 — the same read, over NATS request/reply.

  Phase 4 changes the transport and nothing else. `bootShell.applyRegistry`
  keeps its contract, so this module's job is to answer in exactly the shape
  the REST client answered in: `{ok, unchanged, etag, revision, plugins,
  degraded}` — including the parts that only existed because HTTP had them.
  Decision 58 says that vocabulary is re-implemented in the payload
  deliberately: a conditional read is a read that names the revision it
  holds, and "unchanged" is an answer, not an absence.

  Never throws. Every failure is an `{ok: false, code}` the shell records
  (BR-AS22), because the shell renders its built-ins either way.
*/

import { describe, expect, it, vi } from 'vitest'

import { createRegistryTransport, SHELL_READ_SUBJECT } from './registryTransport.js'

const manifest = (id) => ({
  id,
  name: id,
  version: '1.0.0',
  shellApiVersion: '1.x',
  remote: { kind: 'federated', url: 'http://localhost:7110/remoteEntry.js', module: './plugin' },
  contributions: [],
  extensionPoints: [],
})

const transportWith = (reply) =>
  createRegistryTransport({ request: vi.fn().mockResolvedValue(reply) })

describe('BR-AS27 — the shell reads the registry over one subject', () => {
  it('names the read subject and nothing else', () => {
    expect(SHELL_READ_SUBJECT).toBe('api._platform.registry.frontend-plugins.read.v1')
  })

  it('sends the revision it holds, so an unchanged registry costs no document', async () => {
    const request = vi.fn().mockResolvedValue({ ok: true, unchanged: true, revision: 12 })
    const transport = createRegistryTransport({ request })

    const result = await transport.fetchRegistry({ etag: '"12"' })

    expect(request).toHaveBeenCalledWith(SHELL_READ_SUBJECT, { heldRevision: 12 })
    expect(result.ok).toBe(true)
    expect(result.unchanged).toBe(true)
    /* Held, not cleared: the caller's `unchanged` guard returns before it
       touches anything, and losing the token here would make the next read
       unconditional for no reason. */
    expect(result.etag).toBe('"12"')
  })

  it('asks unconditionally when it holds nothing', async () => {
    const request = vi.fn().mockResolvedValue({ ok: true, unchanged: false, revision: 1, entries: [] })
    const transport = createRegistryTransport({ request })

    await transport.fetchRegistry({})

    expect(request).toHaveBeenCalledWith(SHELL_READ_SUBJECT, { heldRevision: null })
  })

  it('answers with the document and its revision when the registry moved', async () => {
    const transport = transportWith({
      ok: true,
      unchanged: false,
      revision: 13,
      schemaVersion: 1,
      entries: [manifest('fleet-ops')],
    })

    const result = await transport.fetchRegistry({ etag: '"12"' })

    expect(result.ok).toBe(true)
    expect(result.unchanged).toBe(false)
    expect(result.revision).toBe(13)
    expect(result.plugins.map((p) => p.id)).toEqual(['fleet-ops'])
    /* The revision IS the conditional token — decision 58 keeps the ETag
       spelling so `applyRegistry` and the watcher need no second vocabulary. */
    expect(result.etag).toBe('"13"')
  })

  /* BR-AS22, and the half of decision 48 that lives on the read side: a
     degraded answer is an answer. It must not arrive as `unchanged` (which
     would leave the shell believing its catalog was confirmed) and it must
     not arrive as an error (which would look like an unreachable service). */
  it('reports a degraded registry as a successful, degraded read carrying no token', async () => {
    const transport = transportWith({ ok: true, unchanged: false, degraded: true, revision: 0, entries: [] })

    const result = await transport.fetchRegistry({ etag: '"12"' })

    expect(result.ok).toBe(true)
    expect(result.unchanged).toBe(false)
    expect(result.degraded).toBe(true)
    expect(result.etag).toBe(null)
  })

  it('reports a timed-out read as a failure the shell records, not a throw', async () => {
    const transport = createRegistryTransport({
      request: () => Promise.reject(Object.assign(new Error('timeout'), { code: '503' })),
    })

    const result = await transport.fetchRegistry({})

    expect(result.ok).toBe(false)
    expect(result.code).toBe('registry-timeout')
  })

  it('reports an unparsable answer as malformed rather than indexing it', async () => {
    const transport = transportWith({ ok: true, unchanged: false, revision: 3, entries: 'not-a-list' })

    const result = await transport.fetchRegistry({})

    expect(result.ok).toBe(false)
    expect(result.code).toBe('registry-malformed')
  })

  /* BR-AS04: the shell renders this code. A transport error that carried the
     server's own text would put a host and port on screen. */
  it('names no address in what it hands back', async () => {
    const transport = createRegistryTransport({
      request: () => Promise.reject(new Error('nats: no responders available on 127.0.0.1:4222')),
    })

    const result = await transport.fetchRegistry({})

    const rendered = JSON.stringify(result)
    expect(rendered).not.toContain('4222')
    expect(rendered).not.toContain('127.0.0.1')
  })

  /* BR-AS21 and BR-AS24 restated as a transport claim: this module can ask
     one question. There is no verb here that writes, and no verb that
     removes — an added one fails this test rather than review. */
  it('exposes a read and nothing that writes', () => {
    const transport = transportWith({ ok: true, unchanged: true, revision: 1 })
    expect(Object.keys(transport)).toEqual(['fetchRegistry'])
  })
})
