import { watch } from 'vue'
import { createChangeSubscription } from './changeSubscription.js'

// Lifecycle orchestration only. All installation still goes through the
// caller's applyRegistry path, and conditional state lives in shell.registry.
export function createRegistrySession({ connection, client, shell, onResult, afterPaint }) {
  let disposed = false
  let booted = false
  let listening = false
  let reconnectsDuringBoot = 0
  let unwatch = null
  let queue = Promise.resolve()

  const read = ({ unconditional = false, reason }) => {
    queue = queue.then(async () => {
      if (disposed) return null
      const result = await client.fetchRegistry({ heldRevision: unconditional ? null : shell.registry.heldRevision })
      if (!disposed) onResult({ ...result, reason })
      return result
    })
    return queue
  }
  const subscription = createChangeSubscription({
    subscribe: connection.subscribe,
    read,
    currentRevision: () => shell.registry.heldRevision !== null ? shell.registry.revision : null,
  })

  const establish = async () => {
    if (booted) {
      if (listening) void subscription.onReconnect()
      else reconnectsDuringBoot++
      return
    }
    booted = true
    await read({ unconditional: true, reason: 'boot' })
    if (disposed) return
    subscription.start()
    listening = true
    await connection.flush()
    // Close the read→subscribe gap. Core NATS cannot replay a write made
    // between the boot snapshot and subscription registration.
    await read({ reason: 'subscribed' })
    while (reconnectsDuringBoot-- > 0) void subscription.onReconnect()
    reconnectsDuringBoot = 0
  }

  return {
    async start() {
      await afterPaint()
      if (disposed) return
      unwatch = watch(() => connection.state.epoch, () => {
        void establish().catch(() => {
          if (!disposed) onResult({ ok: false, code: 'registry-unreachable' })
        })
      }, { flush: 'sync' })
      await connection.start()
    },
    async stop() {
      disposed = true
      unwatch?.()
      subscription.stop()
      await connection.close()
    },
  }
}
