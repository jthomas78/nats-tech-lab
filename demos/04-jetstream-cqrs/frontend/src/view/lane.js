// The geometry of the lag lane — the one drawing this screen exists for.
//
// Pure maths, so it is specced without a browser. The lane maps a stream
// sequence onto an x position on a fixed axis: sequence 1 at the left end, the
// stream head at the right end. Each side's marker sits where that side has
// folded up to, and the gap between the marker and the right end IS the lag.

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

export function lanePoints(input = {}, axis = AXIS) {
  const l = lag(input)
  const writeX = positionOf(l.writeSeq, l.head, axis)
  const readX = positionOf(l.readSeq, l.head, axis)
  return {
    ...l,
    axis,
    write: { seq: l.writeSeq, lag: l.writeLag, x: writeX, label: labelPlacement(writeX, axis) },
    read: { seq: l.readSeq, lag: l.readLag, x: readX, label: labelPlacement(readX, axis) },
  }
}

// The alt text for the SVG. Screen readers get the same three positions the
// picture shows, in words.
export function laneDescription(points, scope = 'all vehicles') {
  if (points.head <= 0) return `Nothing on the stream yet for ${scope}.`
  return (
    `Sequence axis from 1 to ${points.head} for ${scope}. ` +
    `The write-side snapshot sits at sequence ${points.write.seq}, ${points.write.lag} behind the head. ` +
    `The read model sits at sequence ${points.read.seq}, ${points.read.lag} behind the head.`
  )
}
