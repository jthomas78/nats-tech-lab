import { existsSync, readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

// 04.11 — the page that teaches the mechanism, guarded.
//
// Lesson 02's four run tabs each answer one question with one number. None of
// them shows WHY the number moves. This page draws the mechanism: one stream
// and many workers, where the order is lost, redelivery, and what
// `MaxAckPending` actually caps.
//
// It is read as a FILE, not imported as a module, for the same reason
// recorded.spec.js is: what is being guarded is the content of a document, and
// the two decisions it must obey are decisions about content.
//
//   D18 — no measured number on this page. 04.9.9 deleted every recorded
//         constant from lesson 02 and recorded.spec.js keeps them out. A
//         drawing that prints "1 146 dropped" puts one straight back.
//   D19 — the drawings name NOTHING. No vehicle count, no worker count, no
//         cap value. A drawing that says "8 workers" while the reader has 4
//         selected is worse than one that names neither.

function findDemoRoot(from = process.cwd()) {
  let dir = from
  for (let i = 0; i < 8; i += 1) {
    if (existsSync(resolve(dir, 'BUSINESS_RULES-ODOMETER.md'))) return dir
    const up = dirname(dir)
    if (up === dir) break
    dir = up
  }
  throw new Error('BUSINESS_RULES-ODOMETER.md not found above ' + from)
}

const PAGE = join(findDemoRoot(), 'diagrams/lesson-02-how-it-works.html')

const html = () => {
  expect(existsSync(PAGE), PAGE + ' does not exist').toBe(true)
  return readFileSync(PAGE, 'utf8')
}

// The prose, without the palette. <style> is full of hex colours and pixel
// sizes, and none of it is something a reader is told.
const prose = () => {
  const text = html().replace(/<style[\s\S]*?<\/style>/g, '')
  return text.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ')
}

const FIGURES = [
  'One stream, many workers',
  'Where the order is lost',
  'Redelivery',
  'What MaxAckPending caps',
]

describe('lesson-02-how-it-works.html', () => {
  it('exists', () => {
    expect(existsSync(PAGE)).toBe(true)
  })

  FIGURES.forEach((heading) => {
    it('draws "' + heading + '"', () => {
      expect(prose()).toContain(heading)
    })
  })

  it('draws four figures, one per question', () => {
    expect(html().match(/<figure/g) ?? []).toHaveLength(FIGURES.length)
  })

  // Every drawing says aloud what it shows. RedeliveryTimeline.vue set the
  // precedent and its spec checks the same thing.
  it('gives every drawing a label a screen reader can read', () => {
    const svgs = html().match(/<svg[^>]*>/g) ?? []
    expect(svgs.length).toBeGreaterThanOrEqual(FIGURES.length)
    svgs.forEach((tag) => {
      expect(tag, tag).toContain('aria-label=')
      expect(tag, tag).toContain('role="img"')
    })
  })

  it('carries the one sentence the whole page illustrates', () => {
    const text = prose()
    expect(text).toContain('two events of the SAME vehicle')
    expect(text).toContain('the later one finishes first')
  })

  it('names the two dials, and which way each one moves the risk', () => {
    const text = prose()
    expect(text).toContain('key gap')
    expect(text).toContain('MaxAckPending')
  })

  it('cites the rules instead of restating them', () => {
    const text = prose()
    expect(text).toContain('BR-OD07')
    expect(text).toContain('BR-OD08')
  })

  // The misreading this page exists to kill. "Starvation" sounds like some
  // workers never get an event; the measurement says every worker acked.
  it('says starvation does not mean a worker goes unfed', () => {
    const text = prose().toLowerCase()
    expect(text).toContain('every worker')
    expect(text).toMatch(/throughput/)
  })

  // D18 and D19, checked on the prose. BR-OD07/BR-OD08 are rule NAMES, and a
  // sequence number in a drawing of the mechanism is the mechanism, so the
  // check is for quantities the reader's own controls set.
  it('names no worker count, vehicle count or cap value — D19', () => {
    const text = prose().replace(/BR-OD\d+/g, '')
    expect(text).not.toMatch(/\d+\s*(workers|vehicles|consumers)\b/i)
    expect(text).not.toMatch(/(max-pending|maxackpending|cap(ped)? (of|at))\s*\d/i)
  })

  it('prints no measured number — D18', () => {
    const text = prose().replace(/BR-OD\d+/g, '')
    expect(text).not.toMatch(/\d[\d\s,]{3,}/)
    expect(text).not.toMatch(/\d+(\.\d+)?\s*(s|ms|seconds|events\/s)\b/)
  })
})
