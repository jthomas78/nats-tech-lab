import { existsSync, readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

// 04.12.1 — the guard that keeps one lesson per file.
//
// `README.md` is the lab shell's intro text, and AboutPanel.vue rendered the
// whole of it as lesson 01's "What it does". Lines 356 to 620 were lesson 02,
// so a reader who opened lesson 01's Overview was handed the pool lesson as
// well, measurements and all.
//
// The split moved those sections to docs/LESSON-02.md. This spec is a live
// guard, like recorded.spec.js: it reads the two files off disk and fails
// both ways round — if a lesson 02 heading comes back into the intro, and if
// the lesson 02 file stops holding them. A split that silently reverts is a
// split nobody notices.

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

const ROOT = findDemoRoot()
const README = join(ROOT, 'README.md')
const LESSON2 = join(ROOT, 'docs/LESSON-02.md')
const GUIDE = join(ROOT, 'CLAUDE.md')

// Named one by one, for the reason recorded.spec.js gives: a wildcard goes
// quiet on the day somebody adds a section the list does not know about.
const LESSON_02_HEADINGS = [
  '## Lesson 02 — scaling a consumer',
  '### Lesson 02 has its own log',
  '### What the damage looks like',
  '### The four runs',
  '### What the redelivery actually costs',
  '### Do the two projections agree — yes, and a rebuild is exact',
  '### Does `MaxAckPending` starve workers — no',
  '### Does it actually go faster — 1 vs 4',
]

const read = (p) => {
  expect(existsSync(p), p + ' does not exist').toBe(true)
  return readFileSync(p, 'utf8')
}

describe('the two lessons have two source files', () => {
  it('has a lesson 02 document', () => {
    expect(existsSync(LESSON2), 'docs/LESSON-02.md is the lesson 02 source').toBe(true)
  })

  describe('the lab shell intro is lesson 01 only', () => {
    LESSON_02_HEADINGS.forEach((heading) => {
      it('does not carry ' + heading, () => {
        expect(read(README)).not.toContain(heading)
      })
    })
  })

  describe('the lesson 02 document is lesson 02', () => {
    LESSON_02_HEADINGS.forEach((heading) => {
      it('carries ' + heading, () => {
        expect(read(LESSON2)).toContain(heading)
      })
    })
  })

  // The measurements moved WITH their sections (D23 — nothing is retyped).
  // A file with the headings and none of the numbers would pass the checks
  // above and teach nothing.
  it('keeps the measured numbers with the sections they belong to', () => {
    const text = read(LESSON2)
    expect(text).toContain('1 146')
    expect(text).toContain('ODOMETER_POOL')
  })

  it('points each file at the other, so neither is a dead end', () => {
    expect(read(README)).toContain('docs/LESSON-02.md')
    expect(read(LESSON2)).toContain('README.md')
  })

  // 04.12.4 — the demo's own guide has a table of where everything lives. A
  // new source file that the table does not know about is a file the next
  // reader will not find.
  it('is listed in the demo guide', () => {
    expect(read(GUIDE)).toContain('docs/LESSON-02.md')
  })

  // The intro still has to introduce the demo. D22 chose A because the lab
  // shell keeps the headline finding; a split that took that out would be the
  // wrong split, quietly.
  it('leaves the lab shell its headline finding', () => {
    expect(read(README)).toContain('## The finding')
  })
})
