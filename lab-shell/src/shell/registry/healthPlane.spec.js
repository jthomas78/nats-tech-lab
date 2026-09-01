/*
  BR-AS64 / BR-AS65 — health in the browser.

  The health plane is decoration. Everything here follows from that one word:
  it is read on its own subject, it never blocks the boot, a failure to read
  it leaves the catalogue and every loaded plugin exactly as they were, and
  the worst it may ever do to a screen is say "unknown".

  The other half is honesty about age. A reading the shell fetched once and
  kept is a fact about a moment that has passed, so it ages into `stale` on
  its own — without a new read, without a hint, and without anything
  re-rendering to make it happen. Unknown and stale are different words on
  purpose: "we never looked" and "this was true once" send an operator to
  different places.
*/

import { describe, expect, it, vi } from 'vitest'

import {
  HEALTH_FRESHNESS_MS,
  HEALTH_NOTIFY_SUBJECT,
  HEALTH_READ_SUBJECT,
  HEALTH_STATE,
  createHealthPlane,
  createHealthTransport,
} from './healthPlane.js'

const signal = (state, extra = {}) => ({ state, ...extra })

// Lets every queued microtask settle. The plane chains promises rather than
// awaiting, so a fixed number of ticks would be a guess about its internals.
const settle = () => new Promise((resolve) => setTimeout(resolve, 0))

const reply = (plugins, asOf = 1_000) => ({ ok: true, asOf, plugins })

describe('BR-AS65 — the health read is its own subject and its own answer', () => {
  it('names the read subject and the hint, and they are not the catalogue’s', () => {
    expect(HEALTH_READ_SUBJECT).toBe('api._platform.registry.frontend-plugins.health.v1')
    expect(HEALTH_NOTIFY_SUBJECT).toBe('notify._platform.registry.frontend-plugins.health')
    expect(HEALTH_READ_SUBJECT).not.toBe('api._platform.registry.frontend-plugins.read.v1')
  })

  it('asks with no arguments — there is no conditional read of an observation', async () => {
    // A held revision would imply health has one. It does not: every answer
    // is the current reading, and "unchanged" is not a thing an observation
    // can be.
    const request = vi.fn().mockResolvedValue(reply({}))
    const transport = createHealthTransport({ request })

    await transport.fetchHealth()

    expect(request).toHaveBeenCalledWith(HEALTH_READ_SUBJECT, {})
  })

  it('returns each plugin’s two signals separately', async () => {
    const transport = createHealthTransport({
      request: vi.fn().mockResolvedValue(reply({
        'fleet-ops': { frontend: signal('healthy'), backend: signal('unavailable', { cause: 'not-ready' }) },
      })),
    })

    const result = await transport.fetchHealth()

    expect(result.ok).toBe(true)
    expect(result.plugins['fleet-ops'].frontend.state).toBe(HEALTH_STATE.HEALTHY)
    expect(result.plugins['fleet-ops'].backend.state).toBe(HEALTH_STATE.UNAVAILABLE)
    expect(result.plugins['fleet-ops'].backend.cause).toBe('not-ready')
  })

  it('never throws — an unreachable health plane is a code, like the catalogue’s', async () => {
    const transport = createHealthTransport({ request: vi.fn().mockRejectedValue(new Error('boom')) })

    await expect(transport.fetchHealth()).resolves.toEqual(expect.objectContaining({ ok: false, code: 'health-unreachable' }))
  })

  it('refuses a malformed answer whole rather than half-believing it', async () => {
    const transport = createHealthTransport({ request: vi.fn().mockResolvedValue({ ok: true, plugins: 'not-a-map' }) })

    const result = await transport.fetchHealth()

    expect(result).toEqual(expect.objectContaining({ ok: false, code: 'health-malformed' }))
  })

  it('drops a signal it does not recognise instead of showing an invented word', async () => {
    // The state vocabulary is closed. A word from outside it is either an
    // older shell reading a newer registry or something wrong; both are
    // "unknown", which is a state the UI already knows how to draw.
    const transport = createHealthTransport({
      request: vi.fn().mockResolvedValue(reply({
        'fleet-ops': { frontend: signal('on fire'), backend: signal('healthy') },
      })),
    })

    const result = await transport.fetchHealth()

    expect(result.plugins['fleet-ops'].frontend.state).toBe(HEALTH_STATE.UNKNOWN)
    expect(result.plugins['fleet-ops'].backend.state).toBe(HEALTH_STATE.HEALTHY)
  })

  it('drops a cause that is not a plain short word', async () => {
    // A cause reaches the browser from a service that reached a host. It is
    // one word from a closed list, and anything else — a URL, a stack, a
    // sentence — is dropped rather than rendered (BR-AS60).
    const transport = createHealthTransport({
      request: vi.fn().mockResolvedValue(reply({
        'fleet-ops': { frontend: signal('unavailable', { cause: 'dial tcp 10.0.0.5:5432: refused' }), backend: signal('healthy') },
      })),
    })

    const result = await transport.fetchHealth()

    expect(result.plugins['fleet-ops'].frontend.state).toBe(HEALTH_STATE.UNAVAILABLE)
    expect(result.plugins['fleet-ops'].frontend.cause).toBe('')
  })
})

describe('BR-AS64 — a reading ages, and the shell says so', () => {
  const planeWith = (transport, clock) => createHealthPlane({
    transport,
    subscribe: () => ({ unsubscribe: () => {} }),
    now: () => clock.value,
  })

  it('starts as unknown, which is not the same as stale', async () => {
    const clock = { value: 0 }
    const plane = planeWith({ fetchHealth: vi.fn().mockResolvedValue(reply({})) }, clock)

    const before = plane.signalsFor('fleet-ops')

    expect(before.frontend.state).toBe(HEALTH_STATE.UNKNOWN)
    expect(before.backend.state).toBe(HEALTH_STATE.UNKNOWN)
  })

  it('shows a fresh reading as it came', async () => {
    const clock = { value: 0 }
    const plane = planeWith({
      fetchHealth: vi.fn().mockResolvedValue(reply({ 'fleet-ops': { frontend: signal('healthy'), backend: signal('healthy') } })),
    }, clock)

    await plane.refresh()
    clock.value = HEALTH_FRESHNESS_MS - 1

    expect(plane.signalsFor('fleet-ops').frontend.state).toBe(HEALTH_STATE.HEALTHY)
  })

  it('ages into stale at the boundary, with no new read and no re-render', async () => {
    const clock = { value: 0 }
    const plane = planeWith({
      fetchHealth: vi.fn().mockResolvedValue(reply({ 'fleet-ops': { frontend: signal('healthy'), backend: signal('healthy') } })),
    }, clock)

    await plane.refresh()
    clock.value = HEALTH_FRESHNESS_MS

    // Age is applied when the value is READ, so nothing has to wake up to
    // make a stale reading stop claiming to be current.
    expect(plane.signalsFor('fleet-ops').frontend.state).toBe(HEALTH_STATE.STALE)
  })

  it('leaves a configuration answer alone as it ages', async () => {
    // "Not configured" and "not applicable" are facts about the deployment,
    // not observations, so they do not go stale — there is nothing in them
    // that could have changed while nobody looked.
    const clock = { value: 0 }
    const plane = planeWith({
      fetchHealth: vi.fn().mockResolvedValue(reply({
        'fleet-ops': { frontend: signal('not configured'), backend: signal('not applicable') },
      })),
    }, clock)

    await plane.refresh()
    clock.value = HEALTH_FRESHNESS_MS * 10

    expect(plane.signalsFor('fleet-ops').frontend.state).toBe(HEALTH_STATE.NOT_CONFIGURED)
    expect(plane.signalsFor('fleet-ops').backend.state).toBe(HEALTH_STATE.NOT_APPLICABLE)
  })

  it('keeps the last reading through a failed read, and lets it age', async () => {
    // Losing the health plane says nothing about any plugin. Blanking to
    // unknown would throw away the last thing actually observed; keeping it
    // forever would lie. It ages, which is both.
    const clock = { value: 0 }
    const fetchHealth = vi.fn()
      .mockResolvedValueOnce(reply({ 'fleet-ops': { frontend: signal('unavailable'), backend: signal('healthy') } }))
      .mockResolvedValue({ ok: false, code: 'health-unreachable' })
    const plane = planeWith({ fetchHealth }, clock)

    await plane.refresh()
    clock.value = 1
    await plane.refresh()

    expect(plane.signalsFor('fleet-ops').frontend.state).toBe(HEALTH_STATE.UNAVAILABLE)

    clock.value = HEALTH_FRESHNESS_MS + 1
    expect(plane.signalsFor('fleet-ops').frontend.state).toBe(HEALTH_STATE.STALE)
  })

  it('ages each plugin on its own clock', async () => {
    const clock = { value: 0 }
    const fetchHealth = vi.fn()
      .mockResolvedValueOnce(reply({
        'fleet-ops': { frontend: signal('healthy'), backend: signal('healthy') },
        pricing: { frontend: signal('healthy'), backend: signal('healthy') },
      }))
      .mockResolvedValue(reply({ pricing: { frontend: signal('healthy'), backend: signal('healthy') } }))
    const plane = planeWith({ fetchHealth }, clock)

    await plane.refresh()
    clock.value = HEALTH_FRESHNESS_MS - 1
    await plane.refresh() // only pricing came back this time

    clock.value = HEALTH_FRESHNESS_MS + 1
    expect(plane.signalsFor('fleet-ops').frontend.state).toBe(HEALTH_STATE.STALE)
    expect(plane.signalsFor('pricing').frontend.state).toBe(HEALTH_STATE.HEALTHY)
  })

  it('ignores an answer that arrives out of order', async () => {
    // Two reads in flight can land backwards. The older one must not
    // overwrite the newer, or a recovered plugin flickers back to broken.
    const clock = { value: 0 }
    const plane = planeWith({
      fetchHealth: vi.fn()
        .mockResolvedValueOnce(reply({ 'fleet-ops': { frontend: signal('healthy'), backend: signal('healthy') } }, 2_000))
        .mockResolvedValueOnce(reply({ 'fleet-ops': { frontend: signal('unavailable'), backend: signal('healthy') } }, 1_000)),
    }, clock)

    await plane.refresh()
    await plane.refresh()

    expect(plane.signalsFor('fleet-ops').frontend.state).toBe(HEALTH_STATE.HEALTHY)
  })
})

describe('BR-AS65 — health never reaches into the catalogue', () => {
  it('re-reads on a hint, and the hint carries nothing to install', async () => {
    let handler = null
    const fetchHealth = vi.fn().mockResolvedValue(reply({}))
    const plane = createHealthPlane({
      transport: { fetchHealth },
      subscribe: (subject, fn) => {
        expect(subject).toBe(HEALTH_NOTIFY_SUBJECT)
        handler = fn
        return { unsubscribe: () => {} }
      },
      now: () => 0,
    })

    plane.start()
    await settle()
    const afterStart = fetchHealth.mock.calls.length
    // Whatever a hint claims to carry is ignored: the read is what is
    // authoritative, exactly as the catalogue's notification works.
    handler({ plugins: { 'fleet-ops': { frontend: { state: 'healthy' } } } })
    await settle()

    expect(fetchHealth.mock.calls.length).toBeGreaterThan(afterStart)
  })

  it('recovers from a first read that lost the race with the connection', async () => {
    // Found live: the shell started the plane in the same tick it started the
    // session, so the first read could be answered by a connection that was
    // not up yet. The plane otherwise reads on start, on a hint and after a
    // reconnect — with no hint arriving and no reconnect to speak of, every
    // signal stayed `unknown` forever with nothing left to wake it. A later
    // refresh has to be enough on its own.
    const fetchHealth = vi.fn()
      .mockResolvedValueOnce({ ok: false, code: 'health-unreachable' })
      .mockResolvedValue(reply({ 'fleet-ops': { frontend: { state: 'healthy' }, backend: { state: 'healthy' } } }))
    const plane = createHealthPlane({
      transport: { fetchHealth },
      subscribe: () => ({ unsubscribe: () => {} }),
      now: () => 0,
    })

    plane.start()
    await settle()
    expect(plane.signalsFor('fleet-ops').frontend.state).toBe(HEALTH_STATE.UNKNOWN)

    await plane.refresh()
    await settle()

    expect(plane.signalsFor('fleet-ops').frontend.state).toBe(HEALTH_STATE.HEALTHY)
  })

  it('reads once on start, and again after a reconnect gap', async () => {
    const fetchHealth = vi.fn().mockResolvedValue(reply({}))
    const plane = createHealthPlane({
      transport: { fetchHealth },
      subscribe: () => ({ unsubscribe: () => {} }),
      now: () => 0,
    })

    plane.start()
    await settle()
    await plane.onReconnect()
    await settle()

    // A gap in the subscription is a gap in the hints, and a hint that never
    // arrived is indistinguishable from nothing happening. Re-read.
    expect(fetchHealth.mock.calls.length).toBeGreaterThanOrEqual(2)
  })

  it('stops asking once stopped', async () => {
    let handler = null
    const fetchHealth = vi.fn().mockResolvedValue(reply({}))
    const plane = createHealthPlane({
      transport: { fetchHealth },
      subscribe: (_subject, fn) => { handler = fn; return { unsubscribe: () => {} } },
      now: () => 0,
    })

    plane.start()
    await settle()
    plane.stop()
    const after = fetchHealth.mock.calls.length
    handler({})
    await settle()

    expect(fetchHealth.mock.calls.length).toBe(after)
  })

  it('answers unknown for a plugin it has never heard of', () => {
    const plane = createHealthPlane({
      transport: { fetchHealth: vi.fn() },
      subscribe: () => ({ unsubscribe: () => {} }),
      now: () => 0,
    })

    expect(plane.signalsFor('never-seen').frontend.state).toBe(HEALTH_STATE.UNKNOWN)
  })
})
