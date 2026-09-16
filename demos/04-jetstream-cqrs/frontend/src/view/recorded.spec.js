import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

// 04.9.9 — the guard that keeps lesson 02 honest.
//
// The lesson used to show numbers that were measured once, written into a
// file, and then shown for ever. That was defensible while nothing on the
// screen could run a pool. Every tab runs one now, so a recorded number is
// simply an old number in a demo that can produce a current one.
//
// This spec is a live guard, like commands.spec.js. It reads the source tree
// rather than a module, because the thing being guarded is the ABSENCE of a
// module, and a spec that imports what must not exist cannot run at all.

// Walk up to the app's src/ rather than hard-coding a depth, for the reason
// commands.spec.js gives: vitest's root moves with the directory the runner
// was started from, and a wrong path here would throw, not pass quietly.
function findSrc(from = process.cwd()) {
  let dir = from
  for (let i = 0; i < 8; i += 1) {
    const hit = resolve(dir, 'src/view/lessons.js')
    if (existsSync(hit)) return resolve(dir, 'src')
    const up = dirname(dir)
    if (up === dir) break
    dir = up
  }
  throw new Error('src/view/lessons.js not found above ' + from)
}

const SRC = findSrc()

// The three files that held the recorded runs, and the two specs that read
// them. Named one by one on purpose: a wildcard would go quiet on the day
// somebody adds a fourth.
const GONE = [
  'view/drain.js',
  'view/drain.spec.js',
  'view/starvation.js',
  'view/starvation.spec.js',
  'view/redelivery.js',
  'view/redelivery.spec.js',
]

// The names those files exported. A deleted file whose constants were pasted
// into a component is the same demo with a worse hiding place.
const NAMES = [
  'DRAIN_RUNS',
  'DRAIN_SOURCE',
  'drainRows',
  'STARVATION_RUNS',
  'STARVATION_SOURCE',
  'starvationRows',
  'REDELIVERY_RUNS',
  'REDELIVERY_SOURCE',
  'REDELIVERY_MEASURED_AT',
  'redeliveryRows',
]

const walk = (dir) =>
  readdirSync(dir, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? walk(join(dir, e.name)) : [join(dir, e.name)],
  )

const sources = () =>
  walk(SRC).filter((f) => f.endsWith('.js') || f.endsWith('.vue'))

describe('lesson 02 keeps no recorded measurements', () => {
  it.each(GONE)('has deleted %s', (path) => {
    expect(existsSync(join(SRC, path))).toBe(false)
  })

  it.each(NAMES)('names %s nowhere in the app', (name) => {
    const guilty = sources()
      .filter((f) => !f.endsWith('recorded.spec.js'))
      .filter((f) => new RegExp(`\\b${name}\\b`).test(readFileSync(f, 'utf8')))
    expect(guilty).toEqual([])
  })

  // The count that outlived the runs it came from. It was printed as prose
  // in one place and contradicted a recorded 10 029 in another.
  it('no longer prints the stale event count as prose', () => {
    const guilty = sources()
      .filter((f) => !f.endsWith('recorded.spec.js'))
      .filter((f) => /74[\s ]?109|74[\s ]?040|74[\s ]?079/.test(readFileSync(f, 'utf8')))
    expect(guilty).toEqual([])
  })
})
