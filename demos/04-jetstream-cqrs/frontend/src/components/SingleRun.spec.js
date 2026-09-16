import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import SingleRun from './SingleRun.vue'
import * as api from '../pool/api.js'

// Lesson 02 · Live and Redelivery, run live (plan 04.9.8, decisions D2, D6, D8).
//
// One run, not a set. These two tabs ask one question each, so there is
// nothing to re-seed between and nothing to sequence. The set components own
// the other two tabs.
//
// D6 — Live is the ONLY tab that can be stopped. It runs open-ended; every
// other run finishes by itself. A Stop button on a run that ends on its own
// would suggest it might not.

const plan = { workers: 4, maxPending: 1000, ackWait: '30s', drain: false }

const ok = (over = {}) => ({
  kind: 'ok', workers: 4, maxPending: 1000, ackWait: '30s', killAt: 0,
  events: 10_000, acked: 10_000, dropped: 0, seconds: 8, rate: 1250,
  share: { workers: 4, busy: 4, idle: 0, acked: [] },
  ...over,
})

const at = (w, id) => w.find(`[data-testid="${id}"]`)
const make = (props = {}) => mount(SingleRun, { props: { label: 'Run the pool', plan, ...props } })

const workerRows = (acked, count = 4) =>
  new Map(Array.from({ length: count }, (_, i) => [
    String(i + 1),
    { worker: i + 1, status: 'working', acked: Math.floor(acked / count), dropped: 0 },
  ]))

beforeEach(() => vi.restoreAllMocks())

describe('one run, one press', () => {
  it('asks for exactly the plan it was given, and seeds nothing', async () => {
    vi.spyOn(api, 'runPool').mockResolvedValue(ok())
    const seed = vi.spyOn(api, 'seedPool')

    const w = make()
    await at(w, 'run-go').trigger('click')
    await flushPromises()

    expect(api.runPool).toHaveBeenCalledTimes(1)
    expect(api.runPool.mock.calls[0][0]).toEqual(plan)
    expect(seed).not.toHaveBeenCalled()
  })

  it('reports what the run did', async () => {
    vi.spyOn(api, 'runPool').mockResolvedValue(ok({ seconds: 8, acked: 9_990, dropped: 10 }))
    const w = make()
    await at(w, 'run-go').trigger('click')
    await flushPromises()

    const text = at(w, 'single-result').text()
    expect(text).toContain('8.0s')
    expect(text).toContain('9,990')
    expect(text).toContain('10')
  })

  // D8 — a refusal is an answer. Somebody else is measuring.
  it('says who is holding the shim when it is refused', async () => {
    vi.spyOn(api, 'runPool').mockResolvedValue({
      kind: 'busy', error: 'PoolRunning', message: 'a run is already in progress: 8 workers',
    })
    const w = make()
    await at(w, 'run-go').trigger('click')
    await flushPromises()
    expect(at(w, 'single-broken').text()).toContain('8 workers')
  })

  it('will not offer Run while somebody else holds the shim', () => {
    expect(at(make({ locked: true }), 'run-go').attributes('disabled')).toBeDefined()
  })

  it('prints the one command the press is equal to', () => {
    const lines = at(make(), 'run-cmd').findAll('code').map((c) => c.text())
    expect(lines).toEqual(['cqrs pool -workers 4 -max-pending 1000 -ack-wait 30s'])
  })

  // The Live run never ends by itself, so printing `-drain` under it would
  // describe a different run from the one the button makes.
  it('prints -drain only when the run actually drains', () => {
    const w = make({ plan: { ...plan, drain: true } })
    expect(at(w, 'run-cmd').text()).toContain('-drain')
  })
})

describe('Stop belongs to one tab only', () => {
  it('offers no Stop unless the tab asked for one', async () => {
    let release
    vi.spyOn(api, 'runPool').mockImplementation(() => new Promise((r) => { release = () => r(ok()) }))
    const w = make()
    await at(w, 'run-go').trigger('click')
    await flushPromises()
    expect(at(w, 'run-stop').exists()).toBe(false)
    release()
    await flushPromises()
  })

  it('offers Stop while a stoppable run is in flight, and not before', async () => {
    let release
    vi.spyOn(api, 'runPool').mockImplementation(() => new Promise((r) => { release = () => r(ok()) }))
    const w = make({ stoppable: true })
    expect(at(w, 'run-stop').exists()).toBe(false)

    await at(w, 'run-go').trigger('click')
    await flushPromises()
    expect(at(w, 'run-stop').exists()).toBe(true)

    release()
    await flushPromises()
  })

  it('stops the run through the shim, not by forgetting about it', async () => {
    let release
    vi.spyOn(api, 'runPool').mockImplementation(() => new Promise((r) => { release = () => r(ok()) }))
    vi.spyOn(api, 'stopPool').mockResolvedValue({ kind: 'ok', stopped: true, message: 'stopped' })

    const w = make({ stoppable: true })
    await at(w, 'run-go').trigger('click')
    await flushPromises()
    await at(w, 'run-stop').trigger('click')
    await flushPromises()

    expect(api.stopPool).toHaveBeenCalledTimes(1)
    release()
    await flushPromises()
  })
})

describe('the bar', () => {
  it('moves as the workers report', async () => {
    let release
    vi.spyOn(api, 'runPool').mockImplementation(() => new Promise((r) => { release = () => r(ok()) }))
    const w = make({ events: 10_000 })
    await at(w, 'run-go').trigger('click')
    await flushPromises()

    // One run in the set, so half the log acked is half the set.
    await w.setProps({ workers: workerRows(5_000) })
    await flushPromises()
    expect(at(w, 'run-bar').text()).toContain('50')

    release()
    await flushPromises()
  })
})
