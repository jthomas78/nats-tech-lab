// What the rail says, and nothing else.
//
// The rail used to mix four kinds of thing as peers: a guide, one row per
// vehicle, one row per storage object. Adding the worker pool would have made
// it eleven rows and counting, and a reader could not tell which rows were a
// lesson and which were data.
//
// So the rail is a lesson index (plan section 10.6.1, option B, D9). Three
// rows. It takes no arguments, which is the point: there is nothing you could
// pass it that would add a row. What left the rail became a control INSIDE the
// panel — the vehicles are a picker in the pagehead, the storage objects are a
// tab strip.
//
// Every tab names the command that produces it. A number on a screen that
// cannot be reproduced in a terminal is a claim, not a demonstration.

import { POOL_KV, READ_KV, STREAM, WRITE_KV } from '../config.js'

export const GUIDE = Object.freeze({ key: 'about', label: 'How it works' })

export const LESSONS = Object.freeze([
  Object.freeze({
    key: 'lesson-01',
    label: '01 · Stream + CQRS',
    title: 'Odometer',
    tabs: Object.freeze([
      // D10 — Overview shows BOTH buckets side by side. This is not a layout
      // preference. CLAUDE.md: "Two buckets, not one. The split is the demo."
      // A change that leaves one bucket visible here has broken the demo.
      { key: 'overview', label: 'Overview', cmd: `nats kv ls` },
      { key: 'stream', label: STREAM, cmd: `nats stream view ${STREAM}` },
      { key: 'write', label: WRITE_KV, cmd: `nats kv ls ${WRITE_KV}` },
      { key: 'read', label: READ_KV, cmd: `nats kv ls ${READ_KV}` },
      // 04.7.14 — the demo's headline question, and the only tab that asks
      // the write side to DO something rather than showing what it already
      // did. It gets a tab and not a corner of Overview because "how much
      // does a snapshot buy you" is the question this whole demo exists for.
      // readOnly is read by StreamCqrsPanel: a read-only tab is shown WITHOUT
      // the write door. Rehydrate rebuilds an aggregate and measures it, so a
      // Register / Record trip / Retire row above it would be an invitation to
      // change the thing being measured while it is being measured (04.7.15).
      {
        key: 'rehydrate',
        label: 'Rehydrate',
        cmd: 'cqrs rehydrate -vehicle V1 -snapshot=false',
        readOnly: true,
      },
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
      // The read-only view. Lesson 01 gives every bucket it folds into a tab
      // that is just `nats kv ls` on the screen, and lesson 02 folds into one
      // too. Without this tab the pool's damage is only ever a single total,
      // and you cannot see WHICH vehicle lost the kilometres.
      { key: 'pool', label: POOL_KV, cmd: `nats kv ls ${POOL_KV}` },
    ]),
  }),
])

// No argument, and no badge. A lesson has no count to show, and a rail that
// counted something would be a rail that grew when that something did.
export function railSections() {
  return [
    { eyebrow: 'Guide', items: [{ key: GUIDE.key, label: GUIDE.label }] },
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

export function tabsFor(view) {
  if (view === GUIDE.key) return []
  return LESSONS.find((l) => l.key === view)?.tabs ?? []
}

// D11 — the breadcrumb carries the lesson, so a reader who lands deep still
// knows which half of the demo they are in.
export function crumbFor(view, vehicle = null) {
  if (view === GUIDE.key) return { lesson: 'Guide', title: GUIDE.label }
  const lesson = lessonFor(view)
  const title = lesson.key === 'lesson-01' ? (vehicle ?? 'All vehicles') : lesson.title
  return { lesson: lesson.label, title }
}
