import { describe, expect, it } from 'vitest'

import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'

import { BENCH_SIZES, LESSONS, POOL_SEED_CMD, SEED_CMD, benchVehicle, crumbFor, lessonFor, railSections, tabsFor } from './lessons.js'

describe('the rail is a lesson index', () => {
  it('holds two rows and nothing else', () => {
    const items = railSections().flatMap((s) => s.items)
    expect(items).toHaveLength(2)
    expect(items.map((i) => i.label)).toEqual([
      'Stream + CQRS',
      'Scaling a consumer',
    ])
  })

  it('never grows — not for a vehicle, not for a bucket, not for a stream', () => {
    const before = JSON.stringify(railSections())
    // The rail takes no arguments on purpose. There is nothing to pass it that
    // could add a row, which is the whole of decision D9.
    expect(railSections).toHaveLength(0)
    expect(JSON.stringify(railSections())).toBe(before)
  })

  it('bands the rows under the Lessons eyebrow', () => {
    expect(railSections().map((s) => s.eyebrow)).toEqual(['JetStream'])
  })

  it('carries no badge, because a lesson has no count', () => {
    const items = railSections().flatMap((s) => s.items)
    expect(items.every((i) => i.badge === undefined)).toBe(true)
  })
})

describe('the tabs each lesson carries', () => {
  it('gives lesson 01 Overview, Showcase and Performance', () => {
    expect(tabsFor('lesson-01').map((t) => t.label)).toEqual([
      'Overview',
      'Showcase',
      'Performance',
    ])
  })

  // 04.12.3 — Overview goes FIRST, the same slot it has on lesson 01, so the
  // reader meets the explanation before the buttons.
  it('gives lesson 02 an Overview, the four pool conditions, then the bucket it folds into', () => {
    expect(tabsFor('lesson-02').map((t) => t.label)).toEqual([
      'Overview',
      'Live',
      'Starvation',
      'Redelivery',
      'Performance',
      'odometer-pool',
    ])
  })

  it('gives odometer-pool-workers no tab of its own — D10a', () => {
    const every = LESSONS.flatMap((l) => l.tabs.map((t) => t.label))
    expect(every).not.toContain('odometer-pool-workers')
  })

  // Overview is the exception, on both lessons: it runs nothing, so a command
  // printed under it would be a command for some other tab.
  it('keeps commands on each single-purpose lesson 02 tab', () => {
    const every = tabsFor('lesson-02').filter((t) => t.key !== 'overview')
    expect(every.every((t) => typeof t.cmd === 'string' && t.cmd.length > 0)).toBe(true)
  })

  it('gives neither lesson a command on its Overview', () => {
    const overviews = LESSONS.map((l) => l.tabs.find((t) => t.key === 'overview'))
    expect(overviews.every((t) => t && t.cmd === undefined)).toBe(true)
  })

  it('has no tabs for the guide', () => {
    expect(tabsFor('about')).toEqual([])
  })
})

describe('the breadcrumb carries the lesson — D11', () => {
  // The heading is the page's own name and says which lesson it is, because
  // the eyebrow line that used to say so is gone. The breadcrumb keeps the
  // short title: the trail already names the lesson one step to its left, and
  // saying it twice in one line tells the reader nothing new.
  it('names the lesson between the demo and the page', () => {
    expect(crumbFor('lesson-02')).toEqual({
      lesson: 'Scaling a consumer',
      title: 'Worker pool',
      heading: 'Lesson 02 - One consumer, many workers',
    })
  })

  it('names lesson 01 without a vehicle', () => {
    expect(crumbFor('lesson-01')).toEqual({
      lesson: 'Stream + CQRS',
      title: 'Odometer',
      heading: 'Lesson 01 - One event source + CQRS',
    })
  })

  it('falls back to lesson 01 for a view it does not know', () => {
    expect(lessonFor('nonsense').key).toBe('lesson-01')
  })
})

// benchVehicle() is a COPY of the Go function of the same name. The panel draws
// a row for a size nobody has seeded yet, and an unseeded size has no server
// answer to read a name out of — so the name has to be spelled here too.
//
// A copy that drifts is worse than no copy: the picker would offer a vehicle
// the write side has never heard of, and the measurement would come back
// empty with no error. This reads the Go file and holds the two together.
describe('the fixture vehicle names match the Go that writes them', () => {
  function findBenchGo(from = process.cwd()) {
    let dir = from
    for (let i = 0; i < 8; i += 1) {
      const hit = resolve(dir, 'cqrs/bench.go')
      if (existsSync(hit)) return hit
      const up = dirname(dir)
      if (up === dir) break
      dir = up
    }
    throw new Error('cqrs/bench.go not found above ' + from)
  }

  it('names the three sizes the way bench.go does', () => {
    expect(BENCH_SIZES.map(benchVehicle)).toEqual(['bench-10k', 'bench-100k', 'bench-1m'])
  })

  it('found the Go source, so a silent pass is not possible', () => {
    const go = readFileSync(findBenchGo(), 'utf8')
    expect(go).toContain('func benchVehicle(')
    expect(go).toContain('bench-%dm')
    expect(go).toContain('bench-%dk')
  })
})

// 04.8.9 — the two lessons have two different seeds, because they have two
// different logs. `cqrs seed` writes to ODOMETER. A caught-up POOL needs
// events in ODOMETER_POOL, and pressing lesson 01's seed would have given it
// none while filling the log lesson 01 is drawn from.
describe('each lesson seeds its own log', () => {
  it('gives lesson 02 a seed command of its own', () => {
    expect(POOL_SEED_CMD).toMatch(/^cqrs pool -seed \d+$/)
    expect(POOL_SEED_CMD).not.toBe(SEED_CMD)
  })

  it('keeps lesson 01 seeding lesson 01', () => {
    expect(SEED_CMD.startsWith('cqrs seed ')).toBe(true)
    expect(POOL_SEED_CMD.startsWith('cqrs pool ')).toBe(true)
  })
})
