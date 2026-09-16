// The read path over HTTP. The one exception to "reads come from NATS".
//
// Every other read on this screen arrives over the WebSocket, because every
// other read is a PROJECTION that something already folded. A rehydration is
// not a projection: it is the write side rebuilding an aggregate from the log
// right now, and it is the demo's headline question. There is nothing in KV to
// watch for it, so the browser asks the write side to do it and report.
//
// It is a GET. It appends nothing, and pressing the button a hundred times
// leaves the log exactly as it was — which is why the panel can offer it as a
// button at all.
//
// Like commands/api.js, this file checks no business rule and decides nothing.
// It sends, and it hands back an outcome for every path including failure.

import { COMMAND_API } from '../config.js'

// describeResult turns one HTTP answer into one half of the comparison.
//
// The kinds mirror commands/api.js on purpose: `ok` or `broken`, and never an
// exception. There is no `refused` here, because a rehydration refuses
// nothing — no command is being judged, so no rule can say no.
export function describeResult({ snapshot, id, status, body = {} }) {
  if (status === 200) {
    return {
      kind: 'ok',
      id: body.id ?? id,
      usedSnapshot: Boolean(body.usedSnapshot),
      fromSeq: Number(body.fromSeq ?? 0),
      eventsRead: Number(body.eventsRead ?? 0),
      lastSeq: Number(body.lastSeq ?? 0),
      elapsedMs: Number(body.elapsedMs ?? 0),
      status: String(body.status ?? ''),
      plate: String(body.plate ?? ''),
    }
  }
  return {
    kind: 'broken',
    id,
    usedSnapshot: snapshot,
    error: body.error ?? `HTTP ${status}`,
    message: body.message ?? 'the write side did not answer with a rehydration',
  }
}

export function unreachable({ snapshot, id, reason }) {
  return {
    kind: 'broken',
    id,
    usedSnapshot: snapshot,
    error: 'Unreachable',
    message: `${COMMAND_API} did not answer (${reason}). Is \`cqrs serve\` running?`,
  }
}

// fetchRehydration asks for ONE side of the comparison.
//
// `snapshot` is always spelled out in the query string, never left to the
// server's default. The default is the cheap mode by design, so a caller that
// forgot the flag would silently measure the wrong thing and the two cards
// would show the same number.
// `source` says WHICH log to rebuild from: 'live' is the demo's own ODOMETER,
// 'bench' is the disposable fixture the panel can seed (04.7.16). It is always
// spelled out for the same reason `snapshot` is — a default that silently
// pointed the headline measurement at the wrong log would look like a result.
export async function fetchRehydration(
  id,
  snapshot,
  { fetchImpl = globalThis.fetch, base = COMMAND_API, source = 'live' } = {},
) {
  const query =
    `id=${encodeURIComponent(id)}` +
    `&snapshot=${snapshot ? 'true' : 'false'}` +
    `&source=${encodeURIComponent(source)}`
  try {
    const res = await fetchImpl(`${base}/rehydrate?${query}`, { method: 'GET' })
    let parsed = {}
    try {
      parsed = await res.json()
    } catch {
      // A body that is not JSON is still an answer; the status carries it.
    }
    return describeResult({ snapshot, id, status: res.status, body: parsed })
  } catch (err) {
    return unreachable({ snapshot, id, reason: err?.message ?? String(err) })
  }
}

// fetchBoth runs the comparison.
//
// SEQUENTIALLY, and that is not an oversight. Two rehydrations in flight at
// once share a connection and a CPU, so each would measure the other as well
// as itself -- and this demo has already shipped one headline number that was
// mostly measurement overhead (04.7.12). The cold side runs first so the
// snapshot side cannot be flattered by a warm cache it did not earn.
export async function fetchBoth(id, opts = {}) {
  const cold = await fetchRehydration(id, false, opts)
  const warm = await fetchRehydration(id, true, opts)
  return { cold, warm }
}

// describeBench turns the fixture endpoint's answer into what the panel draws.
//
// `bytes` is carried whether or not anything is seeded. Plan 04.7.16 makes it
// a standing rule that a length never travels without it, and a shape that
// dropped the field when the count was zero would make that rule optional.
export function describeBench({ status, body = {} }) {
  if (status !== 200) {
    return {
      kind: 'broken',
      error: body.error ?? `HTTP ${status}`,
      message: body.message ?? 'the write side did not answer about the fixture',
    }
  }
  return {
    kind: 'ok',
    stream: String(body.stream ?? ''),
    subject: String(body.subject ?? ''),
    writeKv: String(body.writeKv ?? ''),
    exists: Boolean(body.exists),
    events: Number(body.events ?? 0),
    bytes: Number(body.bytes ?? 0),
    sizes: Array.isArray(body.sizes) ? body.sizes.map(Number) : [],
    fixtures: Array.isArray(body.fixtures)
      ? body.fixtures.map((f) => ({
          size: Number(f.size ?? 0),
          vehicle: String(f.vehicle ?? ''),
          events: Number(f.events ?? 0),
          snapSeq: Number(f.snapSeq ?? 0),
          tailLeft: Number(f.tailLeft ?? 0),
        }))
      : [],
    elapsedMs: Number(body.elapsedMs ?? 0),
  }
}

function benchUnreachable(reason) {
  return {
    kind: 'broken',
    error: 'Unreachable',
    message: `${COMMAND_API} did not answer (${reason}). Is \`cqrs serve\` running?`,
  }
}

async function askBench(path, method, { fetchImpl = globalThis.fetch, base = COMMAND_API } = {}) {
  try {
    const res = await fetchImpl(`${base}${path}`, { method })
    let parsed = {}
    try {
      parsed = await res.json()
    } catch {
      // A body that is not JSON is still an answer; the status carries it.
    }
    return describeBench({ status: res.status, body: parsed })
  } catch (err) {
    return benchUnreachable(err?.message ?? String(err))
  }
}

// fetchBench reads what the fixture holds. A GET: it changes nothing.
export function fetchBench(opts = {}) {
  return askBench('/bench', 'GET', opts)
}

// seedFixture builds one fixture.
//
// POST, never GET, and the reason is not style. This writes up to a million
// events, and a GET that did that would be fired by a reload, a link preview
// or a browser prefetch. The size is checked again on the server against its
// own fixed list, so a screen that offered a size the server does not have
// gets a 400 rather than a surprise.
export function seedFixture(size, opts = {}) {
  return askBench(`/bench/seed?size=${encodeURIComponent(size)}`, 'POST', opts)
}
