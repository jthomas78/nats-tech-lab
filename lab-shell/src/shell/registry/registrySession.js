import { watch } from 'vue'
import { createChangeSubscription } from './changeSubscription.js'

/*
  Lifecycle only (BR-AS30). This starts a connection after paint, establishes
  the session on every epoch, and takes it all down again — it does not decide
  when to read, order reads against each other, or hold anything back for
  later. All of that is the subscription's, in one machine (see
  changeSubscription.js); this file used to keep a second copy of half of it.

  What stays here is the one thing the subscription cannot know: what a read
  IS — which conditional token to send, and where the result goes.
*/
export function createRegistrySession({ connection, client, shell, onResult, afterPaint }) {
  let disposed = false
  let booted = false
  let unwatch = null

  const read = async ({ unconditional = false, reason }) => {
    if (disposed) return null
    const result = await client.fetchRegistry({ heldRevision: unconditional ? null : shell.registry.heldRevision })
    if (!disposed) onResult({ ...result, reason })
    return result
  }

  const subscription = createChangeSubscription({
    subscribe: connection.subscribe,
    read,
    currentRevision: () => shell.registry.heldRevision !== null ? shell.registry.revision : null,
  })

  const establish = async () => {
    /* Every epoch after the first is a re-established link, and the
       subscription holds it whether or not the first one has finished. */
    if (booted) {
      void subscription.onReconnect()
      return
    }
    booted = true
    await subscription.refresh({ unconditional: true, reason: 'boot' })
    if (disposed) return
    subscription.start()
    await connection.flush()
    // Close the read→subscribe gap. Core NATS cannot replay a write made
    // between the boot snapshot and subscription registration.
    await subscription.refresh({ reason: 'subscribed' })
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
