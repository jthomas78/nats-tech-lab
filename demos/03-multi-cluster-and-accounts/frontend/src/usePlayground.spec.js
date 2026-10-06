import { describe, expect, it, vi } from 'vitest'

import { event, snapshot } from './fixtures.js'
import { commandKinds, usePlayground } from './usePlayground.js'

const answer = (status, body) => Promise.resolve({ ok: status < 300, status, json: () => Promise.resolve(body) })

/** A fake fetch: /state answers come from a queue, POSTs answer 202. */
function fakeFetch(states, postStatus = 202, postBody = { id: 1 }) {
  const calls = []
  const impl = vi.fn((url, init) => {
    calls.push({ url, init })
    if (init?.method === 'POST') return answer(postStatus, postBody)
    const next = states.length > 1 ? states.shift() : states[0]
    return answer(200, next)
  })
  return { impl, calls }
}

describe('usePlayground, the poll', () => {
  it('merges new history and drops what it already holds', async () => {
    const { impl, calls } = fakeFetch([
      snapshot({ history: [event(1, 'action', 'start'), event(2, 'result', 'ok', { cmd: 1 })], historySeq: 2 }),
      snapshot({ history: [event(2, 'result', 'ok', { cmd: 1 }), event(3, 'observation', 'agreed')], historySeq: 3 }),
    ])
    const { state, poll } = usePlayground({ fetchImpl: impl })
    await poll()
    await poll()
    expect(state.history.map((e) => e.seq)).toEqual([1, 2, 3])
    expect(state.historySeq).toBe(3)
    expect(calls[1].url).toMatch(/\/state\?after=2$/)
  })

  it('starts the history again when the service session changes', async () => {
    const { impl, calls } = fakeFetch([
      snapshot({ history: [event(1, 'action', 'start'), event(2, 'action', 'freeze za')], historySeq: 2 }),
      // A restarted service, asked with after=2, skipped its own first event.
      snapshot({ session: 's2', history: [event(2, 'action', 'probe')], historySeq: 2 }),
      snapshot({ session: 's2', history: [event(1, 'action', 'attach'), event(2, 'action', 'probe')], historySeq: 2 }),
    ])
    const { state, poll } = usePlayground({ fetchImpl: impl })
    await poll()
    await poll()
    expect(state.history).toEqual([])
    expect(state.session).toBe('s2')
    await poll()
    expect(calls[2].url).toMatch(/after=0$/)
    expect(state.history.map((e) => e.text)).toEqual(['attach', 'probe'])
  })

  it('marks the service unreachable on a failed fetch, and keeps the last snapshot', async () => {
    let fail = false
    const impl = vi.fn(() => (fail ? Promise.reject(new Error('Failed to fetch')) : answer(200, snapshot())))
    const { state, poll } = usePlayground({ fetchImpl: impl })
    await poll()
    expect(state.service.reachable).toBe(true)
    fail = true
    await poll()
    expect(state.service.reachable).toBe(false)
    expect(state.snap.summary.state).toBe('agreed')
  })

  it('keeps the pending list the service sends, and invents none', async () => {
    const p = { id: 7, kind: 'publish', cluster: 'za', lane: 'za', via: 'auto', at: snapshot().at, limitNs: 5e9 }
    const { impl } = fakeFetch([snapshot({ pending: [p] })])
    const { state, poll } = usePlayground({ fetchImpl: impl })
    await poll()
    expect(state.snap.pending).toEqual([p])
  })
})

describe('usePlayground, the commands', () => {
  const bodyOf = (calls, path) => JSON.parse(calls.find((c) => c.url.endsWith(path) && c.init?.method === 'POST').init.body)

  it('sends via and timeoutS from the client settings', async () => {
    const { impl, calls } = fakeFetch([snapshot()])
    const { state, actions } = usePlayground({ fetchImpl: impl })
    state.settings.via = 'au'
    state.settings.timeoutS = 10
    await actions.publish('za')
    await actions.verify('za')
    await actions.requestLeadership('arb')
    await actions.probe()
    expect(bodyOf(calls, '/clusters/za/publish')).toEqual({ via: 'au', timeoutS: 10 })
    expect(bodyOf(calls, '/clusters/za/verify')).toEqual({ via: 'au' })
    expect(bodyOf(calls, '/clusters/arb/leadership')).toEqual({ via: 'au' })
    expect(bodyOf(calls, '/meta/probe')).toEqual({ via: 'au' })
  })

  it('sends retryOf on Retry same ID, and never on Publish new', async () => {
    const { impl, calls } = fakeFetch([snapshot()])
    const { actions } = usePlayground({ fetchImpl: impl })
    await actions.retry('za', 'PG_ZA_3')
    const retry = bodyOf(calls, '/clusters/za/publish')
    expect(retry.retryOf).toBe('PG_ZA_3')
    calls.length = 0
    await actions.publish('za')
    expect(bodyOf(calls, '/clusters/za/publish')).not.toHaveProperty('retryOf')
  })

  it('records a refusal with the service message, and a network failure as not sent', async () => {
    const { impl } = fakeFetch([snapshot()], 409, { error: 'busy', message: 'a publish to ODOMETER_ZA is pending' })
    const { state, actions } = usePlayground({ fetchImpl: impl })
    await actions.publish('za')
    expect(state.refusals[0]).toMatchObject({ path: '/clusters/za/publish', message: 'a publish to ODOMETER_ZA is pending', error: 'busy' })

    const down = usePlayground({ fetchImpl: () => Promise.reject(new Error('Failed to fetch')) })
    await down.actions.freeze('za')
    expect(down.state.refusals[0].message).toMatch(/^not sent/)
  })
})

describe('commandKinds', () => {
  it('reads each command kind and cluster from its action text', () => {
    const k = commandKinds([event(1, 'action', 'freeze za', { cmd: 4 }), event(2, 'action', 'probe', { cmd: 5 }), event(3, 'result', 'ok', { cmd: 4 })])
    expect(k[4]).toMatchObject({ kind: 'freeze', cluster: 'za' })
    expect(k[5]).toMatchObject({ kind: 'probe', cluster: '' })
  })
})
