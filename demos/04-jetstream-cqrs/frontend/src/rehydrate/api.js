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
export async function fetchRehydration(
  id,
  snapshot,
  { fetchImpl = globalThis.fetch, base = COMMAND_API } = {},
) {
  const query = `id=${encodeURIComponent(id)}&snapshot=${snapshot ? 'true' : 'false'}`
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
