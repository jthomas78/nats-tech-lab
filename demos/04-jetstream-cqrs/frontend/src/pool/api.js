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
