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
    title: 'Odometer',
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

export function tabsFor(view) {
  return LESSONS.find((l) => l.key === view)?.tabs ?? []
}

// D11 — the breadcrumb carries the lesson, so a reader who lands deep still
// knows which half of the demo they are in.
export function crumbFor(view) {
  const lesson = lessonFor(view)
  return { lesson: lesson.label, title: lesson.title }
}
