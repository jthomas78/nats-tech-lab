// The rail is a lesson index; commands belong to the view that prints them.
import { POOL_KV, READ_KV, STREAM, WRITE_KV } from '../config.js'

export const SHOWCASE_COMMANDS = Object.freeze({
  write: `nats kv ls ${WRITE_KV}`,
  read: `nats kv ls ${READ_KV}`,
  stream: `nats stream view ${STREAM}`,
  info: `nats stream info ${STREAM}`,
})

// The two runs that happen ONCE (04.9.8). They are declared here, above the
// tab list, because the tab header and the Run button must print the same
// line — a header that disagrees with the button under it is worse than no
// header at all.
//
// Live does NOT drain (D6). It runs open-ended until Stop, which is what
// makes it the only stoppable tab.
export const LIVE_PLAN = Object.freeze({
  workers: 4, maxPending: 1000, ackWait: '30s', drain: false,
})

// Redelivery injects a real fault: the worker that fetches this sequence
// stops fetching and never acks and never naks. The run then has to wait out
// the whole ack-wait before the server hands the message to somebody else,
// which is the thing the tab is about.
export const REDELIVERY_PLAN = Object.freeze({
  workers: 4, ackWait: '30s', killAt: 94, drain: true,
})

// The ack-wait dominates: the run cannot finish before the abandoned message
// is redelivered. 30s of waiting plus the drain either side of it.
export const REDELIVERY_SECONDS = 45
export const LIVE_SECONDS = 0

export const LESSONS = Object.freeze([
  Object.freeze({
    key: 'lesson-01',
    label: 'Stream + CQRS',
    // title is the breadcrumb's last step; heading is the <h1>. They differ on
    // purpose: the trail already names the lesson one step to the left, so a
    // breadcrumb ending "Lesson 01 - ..." would say it twice in one line. The
    // heading has no such neighbour — it carries the lesson number itself,
    // because the eyebrow line that used to do that is gone.
    title: 'Odometer',
    heading: 'Lesson 01 - One event source + CQRS',
    tabs: Object.freeze([
      { key: 'overview', label: 'Overview' },
      { key: 'showcase', label: 'Showcase' },
      { key: 'performance', label: 'Performance', cmd: 'cqrs rehydrate -vehicle V1 -snapshot=false' },
    ]),
  }),
  Object.freeze({
    key: 'lesson-02',
    label: 'Scaling a consumer',
    title: 'Worker pool',
    heading: 'Lesson 02 - One consumer, many workers',
    // Tabs, not rail rows: the pool is ONE subject under four conditions, not
    // four subjects. D10a — odometer-pool-workers gets no tab, because the
    // Live tab already draws its contents as worker cards.
    tabs: Object.freeze([
      // Overview first, the same slot it holds on lesson 01 (04.12.3). The
      // reader meets the explanation before the Run buttons. It carries no
      // `cmd`: it runs nothing, and a command printed under it would be a
      // command for some other tab.
      { key: 'overview', label: 'Overview' },
      { key: 'live', label: 'Live', cmd: poolRunCmd(LIVE_PLAN) },
      { key: 'starvation', label: 'Starvation', cmd: 'cqrs pool -workers 8 -max-pending 3' },
      { key: 'redelivery', label: 'Redelivery', cmd: poolRunCmd(REDELIVERY_PLAN) },
      // Renamed 04.9.7. "1 vs 4" was a lie about the tab's own contents: it
      // has held four worker counts since it was written. The header prints
      // ONE command; the tab prints the four it actually runs.
      { key: 'scaling', label: 'Performance', cmd: 'cqrs pool -workers N -drain' },
      // The read-only view, like the bucket lists on lesson 01 Showcase. Without this tab the pool's damage is only ever a single total,
      // and you cannot see WHICH vehicle lost the kilometres.
      { key: 'pool', label: POOL_KV, cmd: `nats kv ls ${POOL_KV}` },
    ]),
  }),
])

// No argument, and no badge. A lesson has no count to show, and a rail that
// counted something would be a rail that grew when that something did.
export function railSections() {
  return [
    { eyebrow: 'JetStream', items: LESSONS.map((l) => ({ key: l.key, label: l.label })) },
  ]
}

export function lessonFor(view) {
  return LESSONS.find((l) => l.key === view) ?? LESSONS[0]
}

// The command that gives an idle pool something to fold.
//
// It lives here because this file already owns "which command produces what
// you are looking at". A caught-up pool is not a broken screen, and the panel
// that says so has to be able to print the way out of it.
export const SEED_CMD = 'cqrs seed -vehicle truck-7 -n 2000'

// The same thing for lesson 02, and it is a DIFFERENT command because it is a
// different log (04.8.9).
//
// `cqrs seed` appends to ODOMETER. Offering it to a caught-up pool would have
// done the reader two disservices at once: the pool would still have had
// nothing to fold, and lesson 01's log would have grown by 2 000 events the
// reader never asked for.
//
// The size matches DefaultPoolSize in cqrs/pool_seed.go. A re-seed replaces
// what is there rather than adding to it, so pressing this twice does not
// slowly turn a 10 000-event demo into a 40 000-event one.
//
// The number is exported on its own as well: the Run buttons re-seed to the
// same size between runs (D7), and a second copy of "10000" written out
// beside the command is a copy that can go stale.
export const POOL_SEED_EVENTS = 10_000
export const POOL_SEED_CMD = `cqrs pool -seed ${POOL_SEED_EVENTS}`

// The other half of the seed group (04.9.3). A reader who seeded a million
// events and wants the disk back should not have to guess the flag, and the
// button that does it prints this.
export const POOL_RM_CMD = 'cqrs pool -rm'

// How often the seed group re-reads GET /pool (04.9.8).
//
// The shim is the only thing that knows whether a run is in flight, and the
// answer changes without the page doing anything: a run ends by itself, or
// somebody starts one in a terminal. Two seconds is slow enough to be free
// and quick enough that a Run button does not stay grey after a run ends.
export const POOL_POLL_MS = 2000

// What one seeded event costs on ODOMETER_POOL, measured against the live
// server 2026-09-16: 810 120 bytes for 10 000 events. Used to price a seed
// BEFORE it happens, on an empty log where the real divisor is not available
// yet. Once the log holds anything, its own bytes-per-event wins.
export const POOL_BYTES_PER_EVENT = 81

// The command that builds the rehydrate fixture, for one size.
//
// It lives here for the same reason SEED_CMD does: this file owns "which
// command produces what you are looking at". The Rehydrate panel prints this
// under its seed button, so a reader who would rather watch a million appends
// go past in a terminal has the exact line — and view/commands.spec.js holds
// it to the flags cqrs/main.go actually defines.
export function benchCmd(size) {
  return `cqrs bench -size ${size}`
}

// What the panel offers. It is a fallback: the real list comes from the server
// (GET /bench), so the screen can never offer a size the server would refuse.
// This is what it draws before the first answer arrives.
export const BENCH_SIZES = Object.freeze([10000, 100000, 1000000])

// The vehicle one fixture size lives on.
//
// This MIRRORS benchVehicle() in cqrs/bench.go, and it has to. The panel now
// draws a row per size whether or not that size is seeded, and an unseeded
// size has no server answer to read the name out of. lessons.spec.js holds
// the two spellings together.
export function benchVehicle(size) {
  const n = Number(size)
  if (!Number.isFinite(n)) return 'bench-0'
  if (n % 1_000_000 === 0) return `bench-${n / 1_000_000}m`
  if (n % 1_000 === 0) return `bench-${n / 1_000}k`
  return `bench-${n}`
}

// Roughly what one fixture event costs on disk, used ONLY to price a seed
// before it happens. Measured: 1 000 000 events came to 83 MB. Once the
// stream exists the panel divides its real bytes by its real count instead,
// because a measured number always beats a remembered one.
export const BENCH_BYTES_PER_EVENT = 83

export function tabsFor(view) {
  return LESSONS.find((l) => l.key === view)?.tabs ?? []
}

// D11 — the breadcrumb carries the lesson, so a reader who lands deep still
// knows which half of the demo they are in.
export function crumbFor(view) {
  const lesson = lessonFor(view)
  return { lesson: lesson.label, title: lesson.title, heading: lesson.heading }
}

// The command one press of a Run button is equivalent to (04.9.5, D2).
//
// Built, not written out, because the same four numbers drive the run itself.
// A hand-written string beside a POST is two claims about one run, and only
// one of them is checked.
//
// `-drain` is what makes it a run: the bare `cqrs pool` reports and changes
// nothing (section 11, D12). The order of the flags is the order they are
// listed here, so two presses of the same tab print the same line.
//
// A field left out is left off the line. Handing the binary `-kill-at 0` is
// not the same request as not mentioning it: 0 is the value that means "kill
// nothing", and printing it invites a reader to think a fault was injected.
// `drain` defaults to true because four of the five runs drain. The Live tab
// is the exception (D6): it runs open-ended until Stop, so printing `-drain`
// under it would describe a different run from the one the button makes.
export function poolRunCmd({ workers, maxPending, ackWait, killAt, drain = true } = {}) {
  const words = drain ? ['cqrs', 'pool', '-drain'] : ['cqrs', 'pool']
  if (workers != null) words.push('-workers', String(workers))
  if (maxPending != null) words.push('-max-pending', String(maxPending))
  if (ackWait != null) words.push('-ack-wait', String(ackWait))
  if (killAt != null) words.push('-kill-at', String(killAt))
  return words.join(' ')
}

// Lesson 02 · Starvation — the four caps one press runs (04.9.6, D12).
//
// Not shortened, and the order is the order they run in. A cap of 1 is the
// clearest possible starvation setup: eight workers and ONE message in flight
// for the whole consumer. A cap of 64 is already past the point where the cap
// binds with eight workers, so it is the honest control row. Two points are
// not a curve, which is why the middle two stay.
export const STARVATION_CAPS = Object.freeze([1, 3, 8, 64])

// Eight, on every cap. The tab varies one knob; Performance varies the other.
export const STARVATION_WORKERS = 8

// What the press COSTS, not what it measures.
//
// This is an estimate, and it is allowed to be one: D4 asks the screen to
// price a press before the reader commits ninety seconds to it. Every number
// the tab REPORTS comes from the run. Re-measure it if the caps or the
// baseline event count change.
//
// Timed against the live server 2026-09-17 at the 10 000-event baseline: the
// four runs took 7.6 / 3.6 / 2.8 / 2.6 s and the three re-seeds 5 s each, so
// the whole set was 32 s. Rounded up, because an estimate that runs under is
// the one that makes a reader think the screen has hung.
export const STARVATION_SECONDS = 40

// Lesson 02 · Performance — the four worker counts one press runs (04.9.7, D9).
//
// Doubling, not a spread: 1 is the control (one worker cannot race itself, so
// it folds the whole log), and each step after it doubles. A reader can then
// read "did doubling the workers halve the time" straight off the column
// instead of doing arithmetic.
export const PERFORMANCE_WORKERS = Object.freeze([1, 2, 4, 8])

// The cap stays PUT across all four runs, and that is the whole design of the
// tab. Starvation varies the cap at eight workers; this varies the workers at
// one cap. A tab that moved both would measure neither.
export const PERFORMANCE_MAX_PENDING = 1000

// What the press COSTS, same as STARVATION_SECONDS and on the same terms.
//
// Timed live 2026-09-17 at the 10 000-event baseline: 1 worker is the slow
// one and the rest are quick, so the runs dominate less than the three
// re-seeds do. Rounded up.
export const PERFORMANCE_SECONDS = 50
