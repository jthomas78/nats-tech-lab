// The geometry of the lag lane — the one drawing this screen exists for.
//
// Pure maths, so it is specced without a browser. The lane maps a stream
// sequence onto an x position on a fixed axis: sequence 1 at the left end, the
// stream head at the right end. Each side's marker sits where that side has
// folded up to, and the gap between the marker and the right end IS the lag.

import { READ_KV, STREAM, WRITE_KV } from '../config.js'
import { lag } from '../nats/model.js'

export const AXIS = Object.freeze({ x0: 40, x1: 960 })

// A head of 0 (an empty stream) puts every marker at the left end rather than
// dividing by zero. A sequence beyond the head is clamped to the head: it means
// this browser's stream tail is behind a projector, not that a projector ran
// ahead of the log, and drawing past the end would say the second thing.
export function positionOf(seq, head, axis = AXIS) {
  const h = Number(head) || 0
  const s = Number(seq) || 0
  if (h <= 0) return axis.x0
  const fraction = Math.min(1, Math.max(0, s / h))
  return axis.x0 + (axis.x1 - axis.x0) * fraction
}

// A marker's label normally sits to the LEFT of its dot, so it reads as the
// end of the segment. Near sequence 1 there is no room, and an end-anchored
// label runs off the left edge of the drawing — which is exactly where a fresh
// stream puts both markers. So the label flips to the right of the dot instead.
//
// The width is an estimate: this is a monospace label at font-size 10 in
// viewBox units, so characters are near enough 6 units wide. It only decides
// which side to draw on, so being a few units out costs nothing.
export const LABEL_WIDTH = 190
export const LABEL_GAP = 8

export function labelPlacement(x, axis = AXIS, width = LABEL_WIDTH) {
  if (x - LABEL_GAP - width < 0) return { anchor: 'start', x: x + LABEL_GAP }
  return { anchor: 'end', x: x - LABEL_GAP }
}

// One row per thing that folds. Phase 04.7 made this N rows instead of two
// markers: a worker pool has one row per worker, and the panel does not know
// how many workers there are until it reads the heartbeat bucket.
//
// A row is `{ id, text, seq }` plus three optional fields: `kind: 'log'` draws
// the log itself (always full width — it IS the head, so it cannot be behind
// it), `tone` is passed through untouched for the drawing to colour, and
// `say` overrides `text` in the alt text when the spoken wording differs.
//
// `row.label` on the way OUT is the label PLACEMENT, not the words — the words
// are `row.text`. The two had the same name once and it cost an hour.
//
// The geometry lives here, not in the component, because it is arithmetic and
// arithmetic is specced. Rows are 26 units apart and the axis sits 18 below
// the last row, which is exactly where the old fixed write/log/read drawing
// already had them — see the spec that proves the three-row case is unchanged.
export const ROW = Object.freeze({ top: 18, gap: 26, lift: 8, axisGap: 18, foot: 16 })

export function laneRows({ head = 0, rows = [] } = {}, axis = AXIS) {
  const h = Number(head) || 0
  const placed = rows.map((row, i) => {
    const isLog = row.kind === 'log'
    const seq = isLog ? h : Number(row.seq) || 0
    const x = isLog ? axis.x1 : positionOf(seq, h, axis)
    const y = ROW.top + i * ROW.gap
    return {
      ...row,
      kind: row.kind || 'fold',
      seq,
      lag: isLog ? 0 : Math.max(0, h - seq),
      x,
      y,
      labelY: y - ROW.lift,
      label: labelPlacement(x, axis),
    }
  })
  const axisY = ROW.top + Math.max(0, placed.length - 1) * ROW.gap + ROW.axisGap
  return { head: h, axis, rows: placed, axisY, height: axisY + ROW.foot }
}

// The two-marker lane is three rows: write, the log, read. It keeps its own
// `write` and `read` fields because the existing drawing and its specs name
// them, and because those two are not interchangeable the way workers are.
export function lanePoints(input = {}, axis = AXIS) {
  const l = lag(input)
  const logText = input.logLabel || `${STREAM} · ${l.head} events, the only source of truth`
  const laid = laneRows(
    {
      head: l.head,
      rows: [
        { id: 'write', kind: 'fold', tone: 'write', text: `${WRITE_KV} · snapshot at ${l.writeSeq}`, seq: l.writeSeq, say: 'The write-side snapshot' },
        { id: 'log', kind: 'log', tone: 'log', text: logText },
        { id: 'read', kind: 'fold', tone: 'read', text: `${READ_KV} · projected to ${l.readSeq}`, seq: l.readSeq, say: 'The read model' },
      ],
    },
    axis,
  )
  const [write, , read] = laid.rows
  return { ...l, ...laid, write, read }
}

// The alt text for the SVG. Screen readers get the same three positions the
// picture shows, in words.
export function laneDescription(points, scope = 'all vehicles') {
  if (points.head <= 0) return `Nothing on the stream yet for ${scope}.`
  const folds = (points.rows || []).filter((row) => row.kind !== 'log')
  const lines = folds.map(
    (row) => `${row.say || row.text} sits at sequence ${row.seq}, ${row.lag} behind the head. `,
  )
  return `Sequence axis from 1 to ${points.head} for ${scope}. ` + lines.join('')
}
