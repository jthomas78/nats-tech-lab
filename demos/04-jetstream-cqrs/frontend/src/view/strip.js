// The log, drawn as chips, with the pool's watermark on it.
//
// The lane (view/lane.js) answers "how far behind is each worker". It cannot
// answer "why is that event about to vanish", because a flat axis has no place
// to put one event. This does: one chip per sequence near the head, coloured by
// what the pool is about to do with it.
//
// Every state below is read off something the page already watches — the stream
// head, KV odometer-pool's lastSeq, and each worker's own heartbeat. Nothing
// here is guessed, and an unknown is drawn as `pending`, never as a fact.
//
//   folded    seq < lastSeq, nobody holding it. Already in the fold.
//   mark      seq === lastSeq. THE watermark. This is the position that
//             decides every drop, and it belongs to the consumer.
//   flight    a worker is holding it and it is ahead of the watermark, so
//             its fold will count.
//   doomed    a worker is holding it and it is AT OR BEHIND the watermark.
//             It will be acked and thrown away — no error, no retry.
//   pending   stored, not handed out yet.
//
// `doomed` is the whole lesson. It is a normal blue chip one moment and a dead
// one the next, and nothing in a log file ever says so.

// How many chips fit before they stop being readable. Eight, as the mockup
// drew it.
export const STRIP_SPAN = 8

// WHERE the window sits is the whole design of this drawing, and the obvious
// answer is wrong. A window pinned to the head shows eight `pending` chips the
// moment a seed runs ahead of the pool, and the watermark — the only chip that
// explains anything — sits thousands of sequences off-screen.
//
// So the window is anchored on the ACTION: the oldest event a worker is still
// holding, or the watermark when nobody is holding anything. One chip of
// already-folded past is kept on the left for context, and the rest of the
// window runs forward towards the head.
export function stripWindow({ head = 0, foldSeq = 0, holds = [], span = STRIP_SPAN } = {}) {
  const h = Number(head) || 0
  if (h <= 0) return null

  const live = holds.map((n) => Number(n) || 0).filter((n) => n > 0)
  const anchor = live.length ? Math.min(...live) : Number(foldSeq) || h
  const from = Math.max(1, Math.min(anchor - 1, h - span + 1))
  const to = Math.min(h, from + span - 1)
  return { from, to, head: h }
}

export function stripChips({ head = 0, foldSeq = 0, workers = [], span = STRIP_SPAN } = {}) {
  const mark = Number(foldSeq) || 0
  // One worker per sequence it holds. A worker holding nothing holds 0, which
  // is not a sequence, so it never matches.
  const held = new Map()
  for (const w of workers) {
    const seq = Number(w?.holding) || 0
    if (seq > 0) held.set(seq, w)
  }

  const win = stripWindow({ head, foldSeq: mark, holds: [...held.keys()], span })
  if (!win) return []

  const chips = []
  for (let seq = win.from; seq <= win.to; seq += 1) {
    const worker = held.get(seq)
    chips.push({
      seq,
      worker: worker ? worker.worker : null,
      state: chipState(seq, mark, Boolean(worker)),
    })
  }
  return chips
}

// How far the head is beyond the right-hand chip. Zero means the strip reaches
// the head and the reader is looking at the end of the log; anything else is a
// backlog, and the panel says so rather than letting the strip imply the log
// stops where the chips do.
export function headGap(chips = [], head = 0) {
  const h = Number(head) || 0
  const last = chips.length ? chips[chips.length - 1].seq : 0
  return Math.max(0, h - last)
}

function chipState(seq, mark, holding) {
  if (holding) return seq <= mark && mark > 0 ? 'doomed' : 'flight'
  if (mark > 0 && seq === mark) return 'mark'
  if (mark > 0 && seq < mark) return 'folded'
  return 'pending'
}

// The sentence under the strip, built from the chips themselves so it cannot
// disagree with them. Null when nothing is about to be dropped — an empty
// warning line is worse than no warning line.
export function dropNote(chips = [], foldSeq = 0) {
  const doomed = chips.filter((c) => c.state === 'doomed')
  if (doomed.length === 0) return null
  const list = doomed.map((c) => `#${c.seq}`).join(', ')
  return `${list} landed after #${foldSeq} · at or behind lastSeq ${foldSeq} · dropped, no error, no retry`
}
