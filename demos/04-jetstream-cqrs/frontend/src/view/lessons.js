// The rail is a lesson index; commands belong to the view that prints them.
import { POOL_KV, READ_KV, STREAM, WRITE_KV } from '../config.js'

export const SHOWCASE_COMMANDS = Object.freeze({
  write: `nats kv ls ${WRITE_KV}`,
  read: `nats kv ls ${READ_KV}`,
  stream: `nats stream view ${STREAM}`,
  info: `nats stream info ${STREAM}`,
})

export const LESSONS = Object.freeze([
  Object.freeze({
    key: 'lesson-01',
    label: '01 · Stream + CQRS',
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
    label: '02 · Scaling a consumer',
    title: 'Worker pool',
    heading: 'Lesson 02 - One consumer, many workers',
    // Tabs, not rail rows: the pool is ONE subject under four conditions, not
    // four subjects. D10a — odometer-pool-workers gets no tab, because the
    // Live tab already draws its contents as worker cards.
    tabs: Object.freeze([
      { key: 'live', label: 'Live', cmd: 'cqrs pool -workers 4 -max-pending 1000 -ack-wait 30s' },
      { key: 'starvation', label: 'Starvation', cmd: 'cqrs pool -workers 8 -max-pending 3' },
      { key: 'redelivery', label: 'Redelivery', cmd: 'cqrs pool -workers 4 -ack-wait 30s -kill-at 94' },
      // The header prints ONE command; the tab itself prints the whole run as
      // a terminal (view/drain.js). Keep the two spelled the same way — a
      // header that disagrees with the block underneath it is worse than no
      // header at all.
      { key: 'scaling', label: '1 vs 4', cmd: 'cqrs pool -workers N -drain' },
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
    { eyebrow: 'Lessons', items: LESSONS.map((l) => ({ key: l.key, label: l.label })) },
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
export const POOL_SEED_CMD = 'cqrs pool -seed 10000'

// The other half of the seed group (04.9.3). A reader who seeded a million
// events and wants the disk back should not have to guess the flag, and the
// button that does it prints this.
export const POOL_RM_CMD = 'cqrs pool -rm'

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
export function poolRunCmd({ workers, maxPending, ackWait, killAt } = {}) {
  const words = ['cqrs', 'pool', '-drain']
  if (workers != null) words.push('-workers', String(workers))
  if (maxPending != null) words.push('-max-pending', String(maxPending))
  if (ackWait != null) words.push('-ack-wait', String(ackWait))
  if (killAt != null) words.push('-kill-at', String(killAt))
  return words.join(' ')
}
