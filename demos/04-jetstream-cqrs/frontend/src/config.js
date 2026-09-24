// Where the two back ends are.
//
// They are two on purpose. Commands go to the Go shim over HTTP; reads come
// from NATS over a WebSocket. That is the CQRS split, visible in the browser's
// own network tab.
//
// Ports follow 20<demo number><increment> — see demos/04-jetstream-cqrs/CLAUDE.md.
/* Standalone, the page is served on 20401 and calls 20402 directly. Embedded
   in `lab-shell`, that same absolute URL is cross-origin, the browser throws
   the answer away and every button reads `Failed to fetch`. So the embedded
   entry moves this base onto the shell's own origin — see `setCommandApi`.
   `let`, not `const`, because an ES module export is a live binding: the call
   sites below all read it as a default argument, which is evaluated per call,
   so they see the move without any of them being touched. */
export let COMMAND_API = import.meta.env.VITE_COMMAND_API ?? 'http://127.0.0.1:20402'

/* This demo's directory name. It is a fact about this demo, not about the
   shell, and it is what the shell's route is keyed on — the same stable
   identifier the readiness route uses, so the route does not change with the
   plugin source (app-shell BR-AS78). */
export const DEMO_NAME = '04-jetstream-cqrs'

/* The one string here that belongs to the shell: its public path layout for a
   demo's declared API routes (app-shell BR-AS82). This file already restates
   the layout once — `vite.config.js` sets `base: '/plugins/demo-04/'` for the
   same reason — and the shell holds the authoritative copy in
   `lab-shell/src/shell/demos/demoCatalogueLocation.js`. Known limit, recorded
   rather than claimed: nothing here imports that constant, because this demo
   is a sealed unit and must not reach into the shell. `plugin.spec.js` asserts
   the value; if the shell ever moves the prefix, that spec is where it shows. */
export const EMBEDDED_COMMAND_API = `/demo-api/${DEMO_NAME}`

/**
 * Point the command API at a different base. Called once by the embedded
 * entry, before anything renders.
 *
 * @param {string} base An origin or an absolute path, with no trailing slash.
 */
export function setCommandApi(base) {
  COMMAND_API = base
}
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

// Lesson 02's OWN log, and its own correct fold (plan 04.8, D8). Must match
// names.go.
//
// Until 04.8 the pool folded ODOMETER -- the log every other screen here is
// drawn from. It published nothing there, so it looked harmless, but its
// consumer starves and redelivers on purpose, and under -kill-at it abandons
// messages unacked. One press on lesson 02 left that on the demo's own log.
//
// `evt.odometer-pool.>` does not overlap `evt.odometer.>` -- the SECOND token
// differs. A dot instead of the hyphen would put the pool back inside the
// demo's own filter, which is the whole thing this split exists to avoid.
//
// Careful with the stream name: `ODOMETER` is a PREFIX of `ODOMETER_POOL`, so
// a half-copied name still reads as plausible on screen.
//
// POOL_TRUTH_KV is the SAME log folded correctly, one message at a time. It is
// what the damage is measured against (D3). It is a third bucket rather than a
// reuse of READ_KV because READ_KV folds a different log, and comparing the
// pool with it would be comparing two different questions.
export const POOL_STREAM = 'ODOMETER_POOL'
export const POOL_TRUTH_KV = 'odometer-pool-truth'
// The pool's own subject prefix, to go with its own stream. Hyphen in the
// second token, for the same reason the stream is separate at all.
export const POOL_SUBJECT_PREFIX = 'evt.odometer-pool.vehicle'

// Must match names.go. The first token is the fixed literal `evt`, never a
// wildcard — an open first token overlaps $SYS.> and JetStream refuses it.
export const SUBJECT_PREFIX = 'evt.odometer.vehicle'

// The rehydrate benchmark's own log (plan 04.7.16). Must match names.go.
//
// It is SEPARATE from ODOMETER on purpose. Every other tab in this app is
// drawn from ODOMETER, and a million-event fixture dropped into it would bury
// all of them. This one is disposable: `cqrs bench -rm` and it is gone.
//
// `evt.odometer-bench.>` does not overlap `evt.odometer.>` — the second token
// differs. A DOT instead of the hyphen would put the fixture inside the demo's
// own filter, which is the whole thing this split exists to avoid.
export const BENCH_STREAM = 'ODOMETER_BENCH'
export const BENCH_WRITE_KV = 'odometer-bench-write'
