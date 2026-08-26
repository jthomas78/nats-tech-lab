// Shared feed over the trace-request-reply KV bucket (Phase 44/45 originally
// had Pulse/Traces/Messages each hand-roll this same bootstrap+subscribe
// pair — an architecture review found ~150 duplicated lines across three
// near-identical copies and this composable is the seam that replaces them).
// One KV entry is ONE SPAN, keyed trace.{traceId}.{spanId} (BR-053, Phase
// 48g) — assembling the trace is this composable's job, and the reason the
// projector can get away with a plain idempotent Put per span instead of the
// read-modify-write that used to lose spans under concurrent writes. This
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

// normalizeSpan flattens one stored {tenant, span} record into the flat span
// object every consumer here already expects, carrying the one thing the wire
// span does not have onto it: `attributedTenant`, the account the span
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
function normalizeSpan(entry) {
  return entry && typeof entry === 'object' && entry.span
    ? { ...entry.span, attributedTenant: entry.tenant ?? '' }
    : { ...entry, attributedTenant: '' }
}

// spansFromRecord reads a KV value — or the identical notify payload — into
// [traceId, spans]. Two shapes are accepted, and the second one has a
// deadline:
//
//   - {tenant, span} — one span, its traceId inside the span. What the
//     projector writes since 48g.
//   - {traceId, spans[]} — a whole trace merged into one entry. What it wrote
//     BEFORE 48g, and what is still sitting in the bucket for at most one
//     BucketMaxAge (15 min) after the projector is deployed. Reading it here
//     is what makes that deploy invisible rather than a blank panel; it can
//     be deleted once no such record can exist, and nothing else depends on
//     it.
//
// Anything else returns null and is dropped, which is also how a malformed
// live notify is refused.
function spansFromRecord(record) {
  if (!record || typeof record !== 'object') return null
  if (record.span?.traceId && record.span?.spanId) {
    return [record.span.traceId, [normalizeSpan(record)]]
  }
  if (record.traceId && Array.isArray(record.spans)) {
    return [record.traceId, record.spans.map(normalizeSpan)]
  }
  return null
}

export function useTraceFeed({ onUpsert } = {}) {
  const traces = ref(new Map()) // traceId -> span objects, each with attributedTenant (see normalizeSpan)
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

  // mergeSpans folds spans into whatever this trace already holds, replacing
  // by spanId rather than appending — the join that the projector no longer
  // does (BR-053). Idempotent for the same reason its per-span Put is: a span
  // seen twice overwrites itself.
  //
  // A span with no spanId is dropped. It cannot be de-duplicated, so keeping
  // it would grow the trace by one on every re-read — and every consumer here
  // keys on spanId anyway.
  //
  // onUpsert still fires once per TRACE with its whole merged span list, not
  // once per span, which is what lets RpcPanel's flat per-span list stay
  // insertion-ordered across a bootstrap that reads the spans of one trace
  // out of several separate KV entries.
  function mergeSpans(traceId, spans) {
    const merged = [...(traces.value.get(traceId) ?? [])]
    const indexBySpanId = new Map(merged.map((span, i) => [span.spanId, i]))
    for (const span of spans) {
      if (!span?.spanId) continue
      const at = indexBySpanId.get(span.spanId)
      if (at === undefined) {
        indexBySpanId.set(span.spanId, merged.length)
        merged.push(span)
      } else {
        merged[at] = span
      }
    }
    upsertTrace(traceId, merged)
  }

  // bootstrap reads the whole KV bucket and merges it in. It runs on mount AND
  // on every reconnect (see the watch below), which is what makes a dropped
  // socket a non-event for completeness rather than a permanent hole.
  //
  // What made it safe to re-run against a live feed used to be a span-count
  // guard: the whole trace arrived in one entry, so a snapshot issued before a
  // live notify but resolved after it would overwrite the newer spans array
  // with an older one, and comparing lengths was the only defence.
  //
  // Since 48g there is nothing to overwrite. Each span arrives on its own and
  // is merged by spanId, so a late-resolving snapshot can only re-state spans
  // already held — a stale read is a no-op instead of a rollback, and the
  // guard is gone rather than merely satisfied. The spec that pinned the old
  // behaviour still passes, which is the point.
  //
  // The one thing the grouping below buys: the spans of one trace now arrive
  // as several separate KV entries, so they are collected first and merged
  // once per trace, rather than firing onUpsert once per span.
  async function readSnapshot() {
    let entries
    try {
      entries = await getKvBucketEntries('platform', 'trace-request-reply')
    } catch {
      bootstrapFailed.value = true
      return false // best-effort — the live subscribe still works even if this fails
    }
    bootstrapFailed.value = false
    const grouped = new Map()
    for (const entry of entries ?? []) {
      if (entry?.op !== 'PUT') continue
      const parsed = spansFromRecord(entry.value)
      if (!parsed) continue
      const [traceId, spans] = parsed
      const acc = grouped.get(traceId)
      if (acc) acc.push(...spans)
      else grouped.set(traceId, [...spans])
    }
    for (const [traceId, spans] of grouped) mergeSpans(traceId, spans)
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
    // One subject per SPAN since 48g — "...trace.{traceId}.{spanId}.changed" —
    // which this wildcard subscription already covered unchanged, since it was
    // always watching the whole bucket rather than a per-trace subject.
    unsubscribe = subscribePlatform('notify._platform.kv.trace-request-reply.>', (payload, subject) => {
      if (!parseKvNotifySubject(subject)) return
      // The ids come from the payload, not the key. They are the same values —
      // the projector derives the key FROM the span — and reading the payload
      // is what keeps the pre-48g merged shape decodable through one code
      // path. The same choice usePubsubFeed.js already makes.
      const parsed = spansFromRecord(payload)
      if (!parsed) return
      mergeSpans(parsed[0], parsed[1])
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
