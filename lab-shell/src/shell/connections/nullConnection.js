import { reactive } from 'vue'

/*
  Phase 16, decision 3 — the null connection has the REAL surface.

  `build` mode has no broker, so it has no connection. It still has a session:
  `createRegistrySession` owns lifecycle — start after paint, establish on
  every epoch, take it down again — and none of that is about NATS. Branching
  inside that file on whether a connection exists would put catalogue-source
  knowledge into the one module whose entire job is lifecycle, so the source
  knowledge stays in the host and the session runs UNMODIFIED in both modes.

  The surface is therefore exactly what the real connection offers a consumer:
  `state.epoch`, `subscribe`, `request`, `start`, `flush`, `close`.

  Two absences are deliberate, not omissions:

  - `state.connected` is NOT here. The real connection reports it because
    there is a socket to be up or down; here there is nothing to be either,
    and a hard-coded `true` would be a claim nobody measured while a `false`
    would make `ShellFooter` say "connection offline · registry may be out of
    date" about a catalogue that cannot be out of date. Leaving it undefined
    is the honest answer, and the footer stays quiet by its existing check.
  - `request` REJECTS with the same `connection-unavailable` the real
    connection throws while its socket is down. It is not a stub that answers:
    nothing may believe it asked a service and got a reply.
*/
export function createNullConnection() {
  /* Reactive because `createRegistrySession` watches `state.epoch` to know a
     link was established. One epoch is emitted on start, which is the same
     signal the real connection emits on its first successful dial — so the
     boot read happens for the same reason, through the same watcher. */
  const state = reactive({ epoch: 0, error: null })
  let stopped = false

  const api = {
    state,

    async start() {
      if (stopped || state.epoch > 0) return api
      state.epoch++
      return api
    },

    async close() {
      stopped = true
    },

    /* No subject is ever requested in `build` mode. Rejecting rather than
       resolving keeps a caller that forgot which mode it is in on the
       failure path it already handles. */
    async request() {
      throw new Error('connection-unavailable')
    },

    /* A real handle, so an `unsubscribe()` in a caller's teardown is not a
       crash. Nothing will ever call the handler: there is no publisher. */
    subscribe() {
      return { unsubscribe() {} }
    },

    async flush() {},
  }

  return api
}
