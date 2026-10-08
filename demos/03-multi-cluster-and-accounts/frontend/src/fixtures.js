// Fixture factories for the specs: a /state answer in the shape the service
// sends (playground/state.go), with every part overridable. Test-only.
import { reactive } from 'vue'

export const AT = '2026-10-06T10:00:10.000Z'

const SERVERS = ['za', 'arb', 'au'].flatMap((c) => [1, 2, 3].map((n) => ({ server: `t-${c}-${n}`, cluster: c })))

export function serverState(over = {}) {
  return {
    process: 'running',
    processAgeNs: 100_000_000,
    reading: { server: over.server, kind: 'ok', leader: 't-arb-1', term: 4, state: 'follower', why: '' },
    readingAgeNs: 300_000_000,
    fresh: true,
    attemptFailed: false,
    metaSize: 9,
    ...over,
  }
}

export function arrow(from, to, over = {}) {
  return {
    from,
    to,
    state: 'listed',
    listed: 3,
    fresh: 3,
    tag: `${from} → ${to} 3/3`,
    servers: [1, 2, 3].map((n) => ({ server: `t-${from}-${n}`, status: 'listed', ageNs: 1e8, never: false })),
    ...over,
  }
}

export function snapshot(over = {}) {
  return {
    at: AT,
    session: 's1',
    pollEveryNs: 500_000_000,
    rig: {
      status: 'ready',
      owner: 'owned',
      servers: SERVERS.map((s, i) => ({ server: s.server, pid: 1000 + i, start: 'x', verified: true, reason: '' })),
    },
    servers: SERVERS.map((s) => serverState({ ...s, reading: { server: s.server, kind: 'ok', leader: 't-arb-1', term: 4, state: s.server === 't-arb-1' ? 'leader' : 'follower', why: '' } })),
    summary: {
      at: AT,
      state: 'agreed',
      text: 'agreed',
      leader: 't-arb-1',
      term: 4,
      views: [{ leader: 't-arb-1', term: 4, servers: SERVERS.map((s) => s.server) }],
      readings: { fresh: 9, invalid: 0, noAnswer: 0, stale: 0, neverAnswered: 0 },
      processes: { running: 9, stopped: 0, gone: 0, unknown: 0 },
      majority: 5,
      last: null,
    },
    arrows: [
      arrow('arb', 'za'), arrow('za', 'arb'), arrow('arb', 'au'),
      arrow('au', 'arb'), arrow('za', 'au'), arrow('au', 'za'),
    ],
    clusters: ['za', 'arb', 'au'].map((c) => ({ cluster: c, wanted: false, phase: 'off', stopped: 0, running: 3, sinceNs: 0, pending: false })),
    transitions: { closed: [] },
    pending: [],
    ledger: { za: [], arb: [], au: [] },
    history: [],
    historySeq: 0,
    ...over,
  }
}

/** A page state as usePlayground holds it, for mounting one component. */
export function pageState(snapOver = {}, over = {}) {
  const { history = [], historySeq = 0, ...snap } = snapshot(snapOver)
  return reactive({
    service: { reachable: true, lastOkAt: Date.parse(AT), error: '' },
    snap,
    history,
    historySeq,
    session: snap.session,
    settings: { via: 'auto', timeoutS: 5 },
    refusals: [],
    now: Date.parse(AT),
    skewMs: 0,
    ...over,
  })
}

/** Every action as a spy, so a spec can see which command a button sends. */
export function spyActions(vi) {
  const names = ['startRig', 'attach', 'stopRig', 'restore', 'freeze', 'resume', 'requestLeadership', 'publish', 'retry', 'verify', 'probe', 'setVia', 'setTimeoutS']
  return Object.fromEntries(names.map((n) => [n, vi.fn()]))
}

export function event(seq, kind, text, over = {}) {
  return { seq, at: `2026-10-06T10:00:${String(seq).padStart(2, '0')}.000Z`, kind, text, ...over }
}
