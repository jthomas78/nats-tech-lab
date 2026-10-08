// The one shared controller of the playground page (PLAYGROUND-PLAN.md,
// Part 2). A composable, not a store: one call in MetaLeaderRoute, and one
// `reactive` state handed down.
//
// What it does, and what it never does:
//
// - It polls GET /state?after=<seq> every 500 ms and merges the history.
// - A failed poll marks the SERVICE unreachable. It says nothing about the
//   rig: the last snapshot is kept, labelled with its age, and never shown as
//   current (rule 3: missing or stale means unknown).
// - It sends commands. Each button is one POST with a JSON body; the service
//   answers 202 with a command ID, or a refusal. Pending entries come from
//   the service's own `pending` list, never from a guess here.
// - It never computes the meta summary, an arrow or a phase. It renders the
//   service's.
// - It holds the two client settings, `via` and `timeoutS`, and sends them in
//   each command body. The service keeps no settings of its own.
// - Retry same ID sends `retryOf` with the message ID the service recorded in
//   its ledger. Publish new never does.
import { reactive } from 'vue'

import * as config from './config.js'

const HISTORY_KEPT = 600
const REFUSALS_KEPT = 20

export function usePlayground({
  fetchImpl = (...a) => globalThis.fetch(...a),
  pollMs = config.POLL_MS,
  clock = () => Date.now(),
} = {}) {
  const state = reactive({
    // null until the first poll answers or fails.
    service: { reachable: null, lastOkAt: null, error: '' },
    snap: null,
    history: [],
    historySeq: 0,
    session: '',
    settings: { via: 'auto', timeoutS: 5 },
    // POSTs the service refused before any command began (409, 400, 404…).
    // They are not commands, so they are not in the service's history.
    refusals: [],
    // Local clock, ticked by the poll timer, for pending timers and ages.
    now: clock(),
    // Server time minus local time, measured on each answer.
    skewMs: 0,
  })

  let timer = null
  let inFlight = false

  function base() {
    return config.COMMAND_API
  }

  async function poll() {
    if (inFlight) return
    inFlight = true
    try {
      const after = state.historySeq
      const res = await fetchImpl(`${base()}/state?after=${after}`)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      merge(await res.json(), after)
      state.service.reachable = true
      state.service.lastOkAt = clock()
      state.service.error = ''
    } catch (err) {
      state.service.reachable = false
      state.service.error = String(err?.message ?? err)
    } finally {
      inFlight = false
      state.now = clock()
    }
  }

  function merge(s, after) {
    const { history = [], historySeq = 0, ...rest } = s
    state.snap = rest
    // A new session, or a sequence that went backwards, is a restarted
    // service. Its history starts again; ours must too. If we asked it with
    // an old `after`, its first events were skipped: the next poll asks
    // again from the start.
    if (s.session !== state.session || historySeq < state.historySeq) {
      state.history = []
      state.historySeq = 0
      state.session = s.session
      if (after > 0) return
    }
    const fresh = history.filter((e) => e.seq > state.historySeq)
    if (fresh.length) {
      const merged = state.history.concat(fresh)
      state.history = merged.length > HISTORY_KEPT ? merged.slice(-HISTORY_KEPT) : merged
    }
    state.historySeq = Math.max(state.historySeq, historySeq)
    const at = Date.parse(s.at)
    if (!Number.isNaN(at)) state.skewMs = at - clock()
  }

  async function send(path, body = {}) {
    let res
    try {
      res = await fetchImpl(`${base()}${path}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
    } catch (err) {
      refuse(path, 'not sent: the control service did not answer', String(err?.message ?? err))
      return null
    }
    let data = null
    try {
      data = await res.json()
    } catch {
      data = null
    }
    if (res.status !== 202) {
      refuse(path, data?.message ?? `HTTP ${res.status}`, data?.error ?? '')
      return null
    }
    // Ask at once, so the pending entry shows without waiting a tick.
    poll()
    return data
  }

  function refuse(path, message, error) {
    state.refusals.unshift({ at: clock(), path, message, error })
    if (state.refusals.length > REFUSALS_KEPT) state.refusals.length = REFUSALS_KEPT
  }

  const via = () => state.settings.via

  const actions = {
    startRig: () => send('/rig/start'),
    attach: () => send('/rig/attach'),
    stopRig: () => send('/rig/stop'),
    restore: () => send('/restore'),
    freeze: (c) => send(`/clusters/${c}/freeze`),
    resume: (c) => send(`/clusters/${c}/resume`),
    requestLeadership: (c) => send(`/clusters/${c}/leadership`, { via: via() }),
    publish: (c) => send(`/clusters/${c}/publish`, { via: via(), timeoutS: state.settings.timeoutS }),
    retry: (c, msgId) =>
      send(`/clusters/${c}/publish`, { via: via(), timeoutS: state.settings.timeoutS, retryOf: msgId }),
    verify: (c) => send(`/clusters/${c}/verify`, { via: via() }),
    probe: () => send('/meta/probe', { via: via() }),
    // The two client settings. Not commands: nothing is sent.
    setVia: (v) => {
      state.settings.via = v
    },
    setTimeoutS: (s) => {
      state.settings.timeoutS = s
    },
  }

  function start() {
    if (timer) return
    poll()
    timer = setInterval(poll, pollMs)
  }

  function stop() {
    clearInterval(timer)
    timer = null
  }

  return { state, actions, poll, start, stop, send }
}

// ---- read helpers: pure functions of the service's snapshot ----

/** The pending command of this kind (and cluster, when given), or undefined. */
export function pendingOf(snap, kind, cluster) {
  return (snap?.pending ?? []).find((p) => p.kind === kind && (cluster === undefined || p.cluster === cluster))
}

/** Server time now, in ms, from the local clock and the measured skew. */
export function serverNow(state) {
  return state.now + state.skewMs
}

/** Seconds since an RFC 3339 time, as of the page's server-time clock. */
export function secsSince(state, iso) {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return null
  return Math.max(0, (serverNow(state) - t) / 1000)
}

/** hh:mm:ss in local time. */
export function hms(iso) {
  const d = typeof iso === 'number' ? new Date(iso) : new Date(Date.parse(iso))
  if (Number.isNaN(d.getTime())) return '—'
  return d.toTimeString().slice(0, 8)
}

export const nsToS = (ns) => (ns ?? 0) / 1e9

/** The cluster a server belongs to: `t-za-2` → `za`. */
export function clusterOf(server) {
  const m = /^t-(za|arb|au)-\d$/.exec(server ?? '')
  return m ? m[1] : ''
}

/**
 * Which command kind each command ID is, read from its action event. The
 * service writes "<kind>" or "<kind> <cluster>" as the action text.
 */
export function commandKinds(history) {
  const out = {}
  for (const e of history) {
    if (e.kind !== 'action' || !e.cmd) continue
    const [kind, cluster = ''] = e.text.split(' ')
    out[e.cmd] = { kind, cluster, at: e.at }
  }
  return out
}
