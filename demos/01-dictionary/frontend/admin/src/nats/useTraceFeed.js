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

export function useTraceFeed({ onUpsert } = {}) {
  const traces = ref(new Map()) // traceId -> raw span objects (from the KV record's `spans` array)
  const bootstrapFailed = ref(false) // sticky — the initial KV read never succeeded
  const everDisconnected = ref(false) // sticky — the live feed has dropped at least once since mount

  function upsertTrace(traceId, spans) {
    const next = new Map(traces.value)
    next.set(traceId, spans)
    traces.value = next
    onUpsert?.(traceId, spans)
  }

  async function bootstrap() {
    let entries
    try {
      entries = await getKvBucketEntries('platform', 'trace-request-reply')
    } catch {
      bootstrapFailed.value = true
      return // best-effort bootstrap — live subscribe below still works even if this fails
    }
    for (const entry of entries ?? []) {
      const record = entry?.value
      if (entry?.op !== 'PUT' || !record?.traceId || !Array.isArray(record.spans)) continue
      upsertTrace(record.traceId, record.spans)
    }
  }

  const { connected, subscribe: subscribePlatform } = usePlatformConnection()
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
      upsertTrace(traceId, payload.spans)
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

  return { traces, connected, bootstrapFailed, everDisconnected }
}
