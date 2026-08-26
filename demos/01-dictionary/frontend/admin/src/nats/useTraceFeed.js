// Shared feed over the trace-request-reply KV bucket (Phase 44/45 originally
// had Pulse/Traces/Messages each hand-roll this same bootstrap+subscribe
// pair — an architecture review found ~150 duplicated lines across three
// near-identical copies and this composable is the seam that replaces them).
// One KV entry is one whole trace's spans array, keyed by traceId — this
// owns fetching and keeping that Map current; it does no filtering or
// aggregation of its own, since Pulse/Traces/Messages each need a different
// shape (unfiltered histogram buckets, toolbar-filtered waterfall rows,
// flattened per-span rows) — see each consumer's own computed()s for that.
//
// onUpsert is an escape hatch for a consumer that needs to react to each
// individual trace update as it lands, not just read the accumulated Map —
// RpcPanel's Messages tab uses it to flatten each trace's spans into its own
// insertion-ordered, MAX_ROWS-capped per-span list, which a plain watch()
// on the whole Map can't reconstruct (Map iteration order doesn't preserve
// per-span arrival order across traces).
import { onMounted, onUnmounted, ref, watch } from 'vue'

import { getKvBucketEntries } from '../api'
import { parseKvNotifySubject } from './kvNotifySubject.js'
import { usePlatformConnection } from './usePlatformConnection.js'

// normalizeSpans unwraps the KV record's stored spans into the flat span
// objects every consumer here already expects, carrying the one thing the
// wire span does not have onto each: `attributedTenant`, the account the span
// arrived from.
//
// The wrapper exists on the server precisely so that a server-derived token
// and a publisher-authored document stay distinguishable (BR-051), so
// flattening it here needs a name that keeps saying so — `attributedTenant`
// is not a field on natstrace's traceSpan and must never be looked for on
// one. It is a per-SPAN value: an ordinary cross-account trace holds spans
// from a tenant account and from PLATFORM, so there is no such thing as
// "the trace's tenant" except by convention (the root span's).
//
// A record written before Phase 48c has bare spans and no attribution to
// recover; those normalize with `attributedTenant: ''` and render as
// unattributed rather than being guessed at. The window is one BucketMaxAge.
function normalizeSpans(spans) {
  return spans.map((entry) =>
    entry && typeof entry === 'object' && entry.span
      ? { ...entry.span, attributedTenant: entry.tenant ?? '' }
      : { ...entry, attributedTenant: '' },
  )
}

export function useTraceFeed({ onUpsert } = {}) {
  const traces = ref(new Map()) // traceId -> span objects, each with attributedTenant (see normalizeSpans)
  // NOT sticky: it reflects the MOST RECENT snapshot read, and a successful
  // resync clears it. A snapshot that failed once but succeeded on reconnect
  // has left no gap behind, so there is nothing left to warn about.
  const bootstrapFailed = ref(false)

  function upsertTrace(traceId, spans) {
    const next = new Map(traces.value)
    next.set(traceId, spans)
    traces.value = next
    onUpsert?.(traceId, spans)
  }

  // bootstrap reads the whole KV bucket and merges it in. It runs on mount AND
  // on every reconnect (see the watch below), which is what makes a dropped
  // socket a non-event for completeness rather than a permanent hole.
  //
  // The span-count guard is what makes it safe to re-run against a live feed.
  // A trace record is append-only server-side (tracestore.appendSpan appends
  // and de-duplicates by spanId), so span count is a monotonic version number
  // for that key. Without the guard, a snapshot read that was issued before a
  // live notify but resolved after it would overwrite the newer spans array
  // with an older one — turning the resync that closes gaps into a source of
  // them.
  async function readSnapshot() {
    let entries
    try {
      entries = await getKvBucketEntries('platform', 'trace-request-reply')
    } catch {
      bootstrapFailed.value = true
      return false // best-effort — the live subscribe still works even if this fails
    }
    bootstrapFailed.value = false
    for (const entry of entries ?? []) {
      const record = entry?.value
      if (entry?.op !== 'PUT' || !record?.traceId || !Array.isArray(record.spans)) continue
      const existing = traces.value.get(record.traceId)
      if (existing && existing.length >= record.spans.length) continue
      upsertTrace(record.traceId, normalizeSpans(record.spans))
    }
    return true
  }

  // A failed snapshot read retries on its own, with backoff, until a newer
  // resync supersedes it or the composable is torn down. The read goes over a
  // connection that has only just been re-established, to a NATS that may still
  // be coming up (the projector reconnecting, the bucket not yet served), so a
  // single failure there is a race, not a verdict — waiting for the *next*
  // reconnect to retry would leave the panel warning about a gap that no longer
  // exists.
  let snapshotGen = 0
  let stopped = false

  async function bootstrap() {
    const gen = ++snapshotGen
    let delay = 500
    while (gen === snapshotGen && !stopped) {
      if (await readSnapshot()) return
      await new Promise((resolve) => setTimeout(resolve, delay))
      delay = Math.min(delay * 2, 10000)
    }
  }

  const { connected, epoch, subscribe: subscribePlatform } = usePlatformConnection()
  let unsubscribe = null

  function connectLive() {
    if (!connected.value) return
    unsubscribe = subscribePlatform('notify._platform.kv.trace-request-reply.>', (payload, subject) => {
      const parsed = parseKvNotifySubject(subject)
      if (!parsed || !payload?.traceId || !Array.isArray(payload.spans)) return
      // internal/kvstore's internal key is {kvContext}.{key} — here
      // "_platform.trace.{traceId}" — so the notify's key segment (everything
      // after the bucket token) already carries the "trace." prefix baked in.
      const traceId = parsed.key.startsWith('trace.') ? parsed.key.slice('trace.'.length) : payload.traceId
      upsertTrace(traceId, normalizeSpans(payload.spans))
    })
  }
  function disconnectLive() {
    unsubscribe?.()
    unsubscribe = null
  }

  // Subscribe BEFORE reading the snapshot, on mount and on reconnect alike.
  // The two are not interchangeable: snapshot-then-subscribe leaves the gap
  // between the read and the subscription uncovered, while subscribe-then-
  // snapshot covers it twice (the guard in bootstrap absorbs the overlap).
  // connectLive is a no-op until connected, so on a cold start the watch
  // below does the real work.
  function resync() {
    disconnectLive()
    connectLive()
    bootstrap()
  }

  onMounted(resync)
  onUnmounted(() => {
    stopped = true
    disconnectLive()
  })
  // Watch `epoch`, NOT `connected`. Both matter but only epoch covers both
  // reconnect kinds — nats-core absorbs the common ones internally without
  // ever flipping `connected` (see connectionFactory.js's epoch comment), so a
  // watch on `connected` resyncs for the rare outer reconnect and sleeps
  // through the frequent inner one.
  //
  // What makes the resync sufficient: notify.* is a core-NATS, fire-and-forget
  // publish with no replay, so nothing sent while the socket was down is
  // redelivered — but the KV bucket it notifies about is a durable JetStream
  // projection, so every message missed on the wire is still readable. That is
  // exactly the contract connectionFactory.subscribe documents, and which this
  // composable used to state and then not honour.
  watch(epoch, resync)

  return { traces, connected, bootstrapFailed }
}
