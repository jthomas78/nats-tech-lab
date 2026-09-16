// The HTTP client for lesson 02's own log (plan 04.9.2, 04.9.3).
//
// Lesson 02 used to be a wall of recorded constants, because nothing on the
// page could start a pool. The shim can now, so this file is how the page
// asks: seed the log, drop the log, read what it holds, run a pool over it.
//
// Like rehydrate/api.js it checks no rule and decides nothing. It sends, and
// it hands back an outcome for every path including failure — `ok` or
// `broken`, never an exception. A screen that had to catch is a screen that
// will one day not catch.
//
// The reads on this page still come from NATS. The pool's PROGRESS is a KV
// watch on odometer-pool-workers, and 04.9 adds no transport for it (D3).
// What comes through here is the press: writes, and the one small GET that
// says what the log is before anybody presses anything.

import { COMMAND_API } from '../config.js'

// describePool turns one HTTP answer into what the screen draws.
//
// `bytes` is carried whether or not anything is seeded. A length never travels
// alone (user, 2026-09-16), and a shape that dropped the field when the count
// was zero would make the rule optional exactly when the reader is deciding
// whether to spend the disk.
export function describePool({ status, body = {} }) {
  if (status !== 200) {
    return {
      kind: 'broken',
      error: body.error ?? `HTTP ${status}`,
      message: body.message ?? 'the write side did not answer about the pool log',
    }
  }
  return {
    kind: 'ok',
    stream: String(body.stream ?? ''),
    subject: String(body.subject ?? ''),
    truthKv: String(body.truthKv ?? ''),
    exists: Boolean(body.exists),
    events: Number(body.events ?? 0),
    bytes: Number(body.bytes ?? 0),
    sizes: Array.isArray(body.sizes) ? body.sizes.map(Number) : [],
    vehicles: Array.isArray(body.vehicles) ? body.vehicles.map(String) : [],
    // The shim runs one pool at a time and refuses a second (D8). The screen
    // reads that here so it can grey a button rather than warn about it.
    running: Boolean(body.running),
    runningWorkers: Number(body.runningWorkers ?? 0),
    runningSince: String(body.runningSince ?? ''),
    runningNote: String(body.runningNote ?? ''),
  }
}

function poolUnreachable(reason) {
  return {
    kind: 'broken',
    error: 'Unreachable',
    message: `${COMMAND_API} did not answer (${reason}). Is \`cqrs serve\` running?`,
  }
}

async function askPool(path, method, body, { fetchImpl = globalThis.fetch, base = COMMAND_API } = {}) {
  const init = { method }
  if (body !== undefined) {
    init.headers = { 'Content-Type': 'application/json' }
    init.body = JSON.stringify(body)
  }
  try {
    const res = await fetchImpl(`${base}${path}`, init)
    let parsed = {}
    try {
      parsed = await res.json()
    } catch {
      // A body that is not JSON is still an answer; the status carries it.
    }
    return describePool({ status: res.status, body: parsed })
  } catch (err) {
    return poolUnreachable(err?.message ?? String(err))
  }
}

// fetchPool reads what the log holds. A GET: it changes nothing.
export function fetchPool(opts = {}) {
  return askPool('/pool', 'GET', undefined, opts)
}

// seedPool builds the log.
//
// POST, never GET, and the size is always stated. This writes ten thousand
// events, and a GET that did that would be fired by a reload, a link preview
// or a browser prefetch. The server checks the size against its own fixed
// list, so a screen offering a size the server has not got gets a 400 rather
// than a surprise.
export function seedPool(size, opts = {}) {
  return askPool('/pool/seed', 'POST', { size }, opts)
}

// dropPool deletes the log and its three buckets. POST, for the same reason.
export function dropPool(opts = {}) {
  return askPool('/pool/rm', 'POST', undefined, opts)
}

// describeRun turns the answer to POST /pool/run into what a row shows.
//
// Three outcomes, not two. `busy` is its own kind because the shim allows one
// run at a time and answers 409 (D8): that is an ANSWER — somebody else is
// measuring — and a screen that showed it as a fault would send the reader
// looking for a bug.
//
// The rate is computed here, once. Two tabs each dividing events by seconds is
// two places to divide by zero, and the answer would be `Infinity` on screen.
export function describeRun({ status, body = {} }) {
  if (status === 409) {
    return {
      kind: 'busy',
      error: body.error ?? 'PoolRunning',
      message: body.message ?? 'a pool is already running',
    }
  }
  if (status !== 200) {
    return {
      kind: 'broken',
      error: body.error ?? `HTTP ${status}`,
      message: body.message ?? 'the write side would not run the pool',
    }
  }
  const seconds = Number(body.elapsedMs ?? 0) / 1000
  const events = Number(body.events ?? 0)
  const share = body.share ?? {}
  return {
    kind: 'ok',
    workers: Number(body.workers ?? 0),
    maxPending: Number(body.maxPending ?? 0),
    ackWait: String(body.ackWait ?? ''),
    killAt: Number(body.killAt ?? 0),
    events,
    acked: Number(body.acked ?? 0),
    dropped: Number(body.dropped ?? 0),
    seconds,
    rate: seconds > 0 ? Math.round(events / seconds) : 0,
    share: {
      workers: Number(share.workers ?? 0),
      busy: Number(share.busy ?? 0),
      idle: Number(share.idle ?? 0),
      acked: Array.isArray(share.acked) ? share.acked.map(Number) : [],
    },
  }
}

// runPool starts one pool and waits for it to finish.
//
// It can take ninety seconds to answer, and that is on purpose (D3): the run
// is a plain POST that returns when the run ends, and the PROGRESS comes from
// the KV watch the page already has open. No second channel to keep in step.
export async function runPool(cfg, { fetchImpl = globalThis.fetch, base = COMMAND_API } = {}) {
  try {
    const res = await fetchImpl(`${base}/pool/run`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cfg),
    })
    let parsed = {}
    try {
      parsed = await res.json()
    } catch {
      // A body that is not JSON is still an answer; the status carries it.
    }
    return describeRun({ status: res.status, body: parsed })
  } catch (err) {
    return poolUnreachable(err?.message ?? String(err))
  }
}
