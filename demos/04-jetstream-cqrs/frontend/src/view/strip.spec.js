// These its are the strip's rules. The drawing is only allowed to say what the
// page has actually watched, so each state here is tied to a fact: the head,
// the pool's lastSeq, or a worker's own heartbeat.

import { describe, expect, it } from 'vitest'

import { STRIP_SPAN, dropNote, headGap, stripChips } from './strip.js'

const worker = (n, holding) => ({ worker: n, holding })

describe('stripChips', () => {
  it('draws nothing when the log is empty — an empty strip is not a log', () => {
    expect(stripChips({ head: 0 })).toEqual([])
  })

  it('ends at the head when the pool is anywhere near it', () => {
    const chips = stripChips({ head: 100, foldSeq: 96 })
    expect(chips).toHaveLength(STRIP_SPAN)
    expect(chips[chips.length - 1].seq).toBe(100)
    expect(chips[0].seq).toBe(93)
  })

  // The bug this rule exists for: a seed puts the head 14 000 events ahead, a
  // head-pinned window draws eight `pending` chips, and the watermark — the
  // only chip that explains anything — is off-screen.
  it('follows the watermark into a backlog instead of staying at the head', () => {
    const chips = stripChips({ head: 26204, foldSeq: 12000 })
    expect(chips[0].seq).toBe(11999)
    expect(chips.some((c) => c.state === 'mark')).toBe(true)
  })

  it('anchors on the OLDEST event still held, because that is the one at risk', () => {
    const chips = stripChips({
      head: 26204,
      foldSeq: 12000,
      workers: [worker(1, 12050), worker(2, 11998)],
    })
    expect(chips[0].seq).toBe(11997)
    expect(chips.find((c) => c.seq === 11998).state).toBe('doomed')
  })

  it('never draws a sequence below 1 on a short log', () => {
    const chips = stripChips({ head: 3, foldSeq: 2 })
    expect(chips.map((c) => c.seq)).toEqual([1, 2, 3])
  })

  it('marks the watermark itself, and only it', () => {
    const chips = stripChips({ head: 10, foldSeq: 7 })
    expect(chips.filter((c) => c.state === 'mark').map((c) => c.seq)).toEqual([7])
  })

  it('calls everything behind the watermark folded and everything ahead pending', () => {
    const chips = stripChips({ head: 10, foldSeq: 7 })
    const by = (s) => chips.filter((c) => c.state === s).map((c) => c.seq)
    expect(by('folded')).toEqual([3, 4, 5, 6])
    expect(by('pending')).toEqual([8, 9, 10])
  })

  it('shows a held event ahead of the watermark as in flight, and says whose', () => {
    const chips = stripChips({ head: 10, foldSeq: 7, workers: [worker(1, 8)] })
    const eight = chips.find((c) => c.seq === 8)
    expect(eight.state).toBe('flight')
    expect(eight.worker).toBe(1)
  })

  it('shows a held event AT OR BEHIND the watermark as doomed — the lesson', () => {
    const chips = stripChips({ head: 10, foldSeq: 7, workers: [worker(3, 6)] })
    expect(chips.find((c) => c.seq === 6).state).toBe('doomed')
  })

  it('treats a worker holding nothing as holding nothing', () => {
    const chips = stripChips({ head: 10, foldSeq: 7, workers: [worker(2, 0)] })
    expect(chips.every((c) => c.worker === null)).toBe(true)
  })

  it('draws no watermark at all before the pool has folded anything', () => {
    const chips = stripChips({ head: 4, foldSeq: 0 })
    expect(chips.every((c) => c.state === 'pending')).toBe(true)
  })
})

describe('dropNote', () => {
  it('stays silent when nothing is about to be dropped', () => {
    expect(dropNote(stripChips({ head: 10, foldSeq: 7 }), 7)).toBeNull()
  })

  it('names the sequences that are going, and says nothing will report it', () => {
    const chips = stripChips({ head: 10, foldSeq: 7, workers: [worker(3, 6)] })
    const note = dropNote(chips, 7)
    expect(note).toContain('#6')
    expect(note).toContain('lastSeq 7')
    expect(note).toContain('no error, no retry')
  })

  it('lists every doomed event, not just the first', () => {
    const chips = stripChips({ head: 10, foldSeq: 7, workers: [worker(3, 5), worker(4, 6)] })
    expect(dropNote(chips, 7)).toContain('#5, #6')
  })
})

describe('headGap', () => {
  it('is zero when the chips reach the head', () => {
    expect(headGap(stripChips({ head: 10, foldSeq: 7 }), 10)).toBe(0)
  })

  it('counts the events past the right-hand chip, so a backlog is not hidden', () => {
    const chips = stripChips({ head: 26204, foldSeq: 12000 })
    expect(headGap(chips, 26204)).toBe(26204 - chips[chips.length - 1].seq)
  })
})
