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
  const bootstrapFailed = ref(false) // sticky — the initial KV read never succeeded
  const everDisconnected = ref(false) // sticky — the live feed has dropped at least once since mount

  function upsertMessage(spanId, record) {
    const next = new Map(messages.value)
    next.set(spanId, record)
    messages.value = next
    onUpsert?.(spanId, record)
  }

  // Ingestion is best-effort end to end (BR-047, ADR-047 A7): the emit is a
  // fire-and-forget core-NATS publish. Nothing here can recover an envelope
  // that never reached the stream, which is why the panel says so rather
  // than implying a completeness it cannot deliver.
  async function bootstrap() {
    let entries
    try {
      entries = await getKvBucketEntries('platform', BUCKET)
    } catch {
      bootstrapFailed.value = true
      return // best-effort — the live subscribe below still works even if this fails
    }
    for (const entry of entries ?? []) {
      const record = entry?.value
      if (entry?.op !== 'PUT' || !record?.span?.spanId) continue
      upsertMessage(record.span.spanId, record)
    }
  }

  const { connected, subscribe: subscribePlatform } = usePlatformConnection()
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

  onMounted(() => {
    bootstrap()
    connectLive()
  })
  onUnmounted(disconnectLive)
  watch(connected, (isConnected) => {
    if (isConnected) {
      disconnectLive()
      connectLive()
    } else {
      everDisconnected.value = true
    }
  })

  return { messages, upsertMessage, connected, bootstrapFailed, everDisconnected }
}
