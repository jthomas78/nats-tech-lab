// The read path. One WebSocket, three subscriptions, no HTTP.
//
// Commands do NOT come through here — they go to the Go shim on 20402 over
// HTTP (plan section 9.2, D3). Keeping the two apart is not tidiness: it is
// what makes the CQRS split visible in the browser's own network tab.
//
// Three things are watched, and they are three because they move separately:
//
//   KV odometer-write   the write-side snapshot. Always trails the head.
//   KV odometer-read    the read model. Trails the head by a different amount.
//   stream ODOMETER     the head itself, and the log the table draws.
//
// A single "current state" subscription would hide exactly the thing this
// screen is for.

import { DeliverPolicy, jetstream, jetstreamManager } from '@nats-io/jetstream'
import { Kvm } from '@nats-io/kv'
import { nanos, wsconnect } from '@nats-io/nats-core'
import { computed, reactive, ref, shallowRef } from 'vue'

import { NATS_WS, READ_KV, STREAM, WRITE_KV } from '../config.js'
import { lag, logEvent, maxSeq, readModel, writeSnapshot } from './model.js'

// How many stream rows the table keeps. The seed command writes 10 000 trips
// to make the replay worth timing, and a browser that tried to hold all of
// them would be measuring the browser, not the demo.
export const LOG_LIMIT = 200

// One ordered consumer per viewer, and the name has to say which viewer.
// An ordered consumer is named `<name_prefix>_1`, so two screens sharing a
// prefix ask the server for the SAME consumer, and the second one takes the
// first one's messages: the log then sits empty with no error anywhere. Two
// tabs on this page is a normal thing to do during a demo.
//
// The name is minted per CALL, not per module. A module-level constant is
// shared by every caller in the page, and Vite's hot reload makes exactly that
// happen in development: the replaced component connects again while the old
// consumer is still being torn down, both under one name, and the new screen
// silently stops receiving. Once per call, the two never collide.
function newViewerId() {
  return `demo04-ui-${Math.random().toString(36).slice(2, 8)}`
}

// How long the server keeps this viewer's consumer after the tab goes away.
// A unique name per viewer means a closed tab leaves one behind, and a demo
// that is opened and closed twenty times should not leave twenty consumers in
// `nats consumer ls`.
const VIEWER_TTL_MS = 30_000

export function useOdometer() {
  const viewer = newViewerId()
  const status = ref('idle')
  const error = ref('')
  const head = ref(0)
  const messages = ref(0)
  const writes = reactive(new Map())
  const reads = reactive(new Map())
  const log = ref([])

  const connection = shallowRef(null)
  let stopping = false
  let connecting = null
  const closers = []

  const connected = computed(() => status.value === 'connected')

  // vehicles is the union of both buckets. A vehicle can appear in one and
  // not the other for a moment, and that moment is the demo.
  const vehicles = computed(() => {
    const ids = new Set([...writes.keys(), ...reads.keys()])
    return [...ids].sort()
  })

  const lags = computed(() =>
    lag({
      head: head.value,
      writeSeq: maxSeq(writes.values()),
      readSeq: maxSeq(reads.values()),
    }),
  )

  // The guard is set before the first await, not after. onMounted and a hot
  // reload can both call this, and two connections would create two ordered
  // consumers — see newViewerId for why that used to cost the screen its log.
  function connect() {
    if (connecting) return connecting
    connecting = start().finally(() => {
      connecting = null
    })
    return connecting
  }

  async function start() {
    stopping = false
    status.value = 'connecting'
    error.value = ''
    try {
      const nc = await wsconnect({ servers: NATS_WS, name: 'demo04-odometer-ui' })
      connection.value = nc
      status.value = 'connected'

      const js = jetstream(nc)
      const jsm = await jetstreamManager(nc)

      await readStreamInfo(jsm)
      await Promise.all([
        watchBucket(js, WRITE_KV, writes, writeSnapshot),
        watchBucket(js, READ_KV, reads, readModel),
      ])
      await tailStream(js)
      trackStatus(nc)
    } catch (err) {
      status.value = 'error'
      error.value = describe(err)
      await disconnect()
    }
  }

  async function disconnect() {
    stopping = true
    await connecting
    for (const close of closers.splice(0)) {
      try {
        close()
      } catch {
        // A watcher that is already finished is not a failure.
      }
    }
    const nc = connection.value
    connection.value = null
    if (nc) {
      try {
        await nc.close()
      } catch {
        // Same.
      }
    }
    if (status.value !== 'error') status.value = 'closed'
  }

  // readStreamInfo gets the head sequence once, up front. Without it the log
  // table would have to replay from 1 to learn where the head is.
  async function readStreamInfo(jsm) {
    const info = await jsm.streams.info(STREAM)
    head.value = Number(info.state?.last_seq ?? 0)
    messages.value = Number(info.state?.messages ?? 0)
  }

  // watchBucket drains a KV watcher forever. watch() replays every current
  // key first and then stays open, so the screen is correct the moment it
  // loads and stays correct after.
  async function watchBucket(js, bucket, into, shape) {
    const kv = await new Kvm(js).open(bucket)
    const watcher = await kv.watch()
    closers.push(() => watcher.stop())
    drain(watcher, (entry) => applyEntry(into, entry, shape))
  }

  // tailStream follows the log. It starts near the head rather than at
  // sequence 1 — see LOG_LIMIT. An ordered consumer is used because nothing
  // here acks: this is a viewer, and dropping it must cost the server nothing.
  async function tailStream(js) {
    const start = Math.max(1, head.value - LOG_LIMIT + 1)
    const consumer = await js.consumers.get(STREAM, {
      name_prefix: viewer,
      opt_start_seq: start,
      deliver_policy: DeliverPolicy.StartSequence,
      inactive_threshold: nanos(VIEWER_TTL_MS),
    })
    const msgs = await consumer.consume()
    closers.push(() => msgs.stop())
    drain(msgs, (msg) => {
      if (msg.seq > head.value) head.value = msg.seq
      const row = logEvent({
        subject: msg.subject,
        seq: msg.seq,
        time: msg.time,
        body: parse(msg),
      })
      if (row === null) return
      log.value = [row, ...log.value].slice(0, LOG_LIMIT)
      messages.value = Math.max(messages.value, 1)
    })
  }

  // trackStatus turns the connection's own events into the topbar tag. A
  // reconnect is normal — nats-core retries on its own — so it is reported,
  // not treated as a failure.
  function trackStatus(nc) {
    drain(nc.status(), (s) => {
      if (s.type === 'disconnect') status.value = 'reconnecting'
      if (s.type === 'reconnect') status.value = 'connected'
    })
  }

  // drain runs an async iterator in the background and stops quietly when the
  // page disconnects. An error after disconnect() is the iterator being torn
  // down, not a problem to show.
  function drain(iterator, onItem) {
    ;(async () => {
      try {
        for await (const item of iterator) onItem(item)
      } catch (err) {
        if (stopping) return
        status.value = 'error'
        error.value = describe(err)
      }
    })()
  }

  return {
    status,
    error,
    connected,
    head,
    messages,
    writes,
    reads,
    log,
    vehicles,
    lags,
    connect,
    disconnect,
  }
}

// applyEntry folds one KV change into a Map. A delete removes the row rather
// than leaving a stale one: the bucket no longer holds it, so neither does
// the screen.
export function applyEntry(into, entry, shape) {
  const row = shape(entry.key, entry.operation === 'PUT' ? safeJson(entry) : {})
  if (row === null) return null
  if (entry.operation === 'PUT') {
    into.set(row.id, { ...row, revision: entry.revision })
  } else {
    into.delete(row.id)
  }
  return row.id
}

function safeJson(entry) {
  try {
    return entry.json()
  } catch {
    return {}
  }
}

function parse(msg) {
  try {
    return msg.json()
  } catch {
    return {}
  }
}

function describe(err) {
  return err?.message ? String(err.message) : String(err)
}
