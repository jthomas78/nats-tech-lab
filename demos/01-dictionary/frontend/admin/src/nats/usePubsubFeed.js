// Shared feed over the pubsub-messages KV bucket (Phase 43c, BR-047/BR-048) —
// the publish-side sibling of useTraceFeed.js, deliberately the same
// bootstrap-fetch-plus-live-subscribe shape so the Messages panel reuses a
// proven adapter rather than hand-rolling a third copy of it (the mistake an
// architecture review already caught once across Pulse/Traces/Messages).
//
// Two differences from useTraceFeed, both following from what a record IS:
//
//   - One KV entry is ONE envelope keyed by spanId, not a whole trace's
//     spans array keyed by traceId. There is nothing to merge — an
//     obs.pubsub.* observation is standalone — so this stores the record
//     as-is instead of accumulating an array.
//   - Each record carries a `tenant` alongside the envelope. It is NOT in
//     the envelope itself: pubsubstore derives it from the
//     monitor.{tenant}.pubsub.> subject the message arrived on, a token the
//     NATS server inserts (BR-AC34's import remap), so it cannot be spoofed
//     by a tenant writing a field into its own payload. This is the whole
//     reason the Messages panel can name the publisher where the Traces
//     panel can only show a coarse PLATFORM/TENANT split.
//
// Like useTraceFeed it does no filtering of its own — the panel's family
// filter, row cap and pause are its own concern.
import { onMounted, onUnmounted, ref, watch } from 'vue'

import { getKvBucketEntries } from '../api'
import { parseKvNotifySubject } from './kvNotifySubject.js'
import { usePlatformConnection } from './usePlatformConnection.js'

const BUCKET = 'pubsub-messages'
const KEY_PREFIX = 'msg.'

export function usePubsubFeed({ onUpsert } = {}) {
  const messages = ref(new Map()) // spanId -> { tenant, span }
  // NOT sticky — same rule as useTraceFeed: it reflects the MOST RECENT
  // snapshot read, and a successful resync clears it.
  const bootstrapFailed = ref(false)

  function upsertMessage(spanId, record) {
    const next = new Map(messages.value)
    next.set(spanId, record)
    messages.value = next
    onUpsert?.(spanId, record)
  }

  // Two different "best-effort"s meet here, and only one of them is
  // recoverable — worth keeping straight, because the panel's warning text
  // used to conflate them:
  //
  //   - INGESTION is best-effort end to end (BR-047, ADR-047 A7): the emit is
  //     a fire-and-forget core-NATS publish, so an envelope that never reached
  //     the stream is gone and nothing here can recover it. Unrecoverable.
  //   - DELIVERY to this browser is also best-effort (notify.* is core NATS,
  //     no replay), but whatever it missed is still in the KV bucket, which is
  //     a durable projection. Recoverable — by re-running this on reconnect,
  //     which is what the watch below now does.
  //
  // Unlike useTraceFeed's records, one entry here is ONE standalone envelope
  // keyed by spanId, written once and never appended to, so a re-read needs no
  // monotonic guard: re-setting a key stores the identical value.
  async function readSnapshot() {
    let entries
    try {
      entries = await getKvBucketEntries('platform', BUCKET)
    } catch {
      bootstrapFailed.value = true
      return false // best-effort — the live subscribe below still works even if this fails
    }
    bootstrapFailed.value = false
    for (const entry of entries ?? []) {
      const record = entry?.value
      if (entry?.op !== 'PUT' || !record?.span?.spanId) continue
      upsertMessage(record.span.spanId, record)
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
    unsubscribe = subscribePlatform(`notify._platform.kv.${BUCKET}.>`, (payload, subject) => {
      const parsed = parseKvNotifySubject(subject)
      if (!parsed || !payload?.span) return
      // pubsubstore's internal key is {kvContext}.{key} — "_platform.msg.
      // {spanId}" — so the notify's key segment already carries the "msg."
      // prefix baked in, same shape as trace-request-reply's "trace.".
      const spanId = parsed.key.startsWith(KEY_PREFIX) ? parsed.key.slice(KEY_PREFIX.length) : payload.span.spanId
      if (!spanId) return
      upsertMessage(spanId, payload)
    })
  }
  function disconnectLive() {
    unsubscribe?.()
    unsubscribe = null
  }

  // Subscribe before reading the snapshot so the window between the two is
  // covered by the live feed rather than lost — see useTraceFeed.resync for
  // the full reasoning; this is deliberately the same shape.
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

  return { messages, upsertMessage, connected, bootstrapFailed }
}
