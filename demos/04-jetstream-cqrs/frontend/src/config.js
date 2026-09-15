// Where the two back ends are.
//
// They are two on purpose. Commands go to the Go shim over HTTP; reads come
// from NATS over a WebSocket. That is the CQRS split, visible in the browser's
// own network tab.
//
// Ports follow 20<demo number><increment> — see demos/04-jetstream-cqrs/CLAUDE.md.
export const COMMAND_API = import.meta.env.VITE_COMMAND_API ?? 'http://127.0.0.1:20402'
export const NATS_WS = import.meta.env.VITE_NATS_WS ?? 'ws://127.0.0.1:20403'

export const STREAM = 'ODOMETER'
export const WRITE_KV = 'odometer-write'
export const READ_KV = 'odometer-read'

// Lesson 02. POOL_KV is a THIRD projection of the same log, kept apart from
// READ_KV on purpose: the pool is deliberately wrong, and a demo that damaged
// the read model to show that would have nothing correct left to compare
// against. POOL_WORKERS_KV is one key per worker — heartbeat and counters.
export const POOL_KV = 'odometer-pool'
export const POOL_WORKERS_KV = 'odometer-pool-workers'

// Must match names.go. The first token is the fixed literal `evt`, never a
// wildcard — an open first token overlaps $SYS.> and JetStream refuses it.
export const SUBJECT_PREFIX = 'evt.odometer.vehicle'
