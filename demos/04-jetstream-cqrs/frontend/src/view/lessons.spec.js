import { describe, expect, it } from 'vitest'

import { GUIDE, LESSONS, crumbFor, lessonFor, railSections, tabsFor } from './lessons.js'

describe('the rail is a lesson index', () => {
  it('holds three rows and nothing else', () => {
    const items = railSections().flatMap((s) => s.items)
    expect(items).toHaveLength(3)
    expect(items.map((i) => i.label)).toEqual([
      'How it works',
      '01 · Stream + CQRS',
      '02 · Scaling a consumer',
    ])
  })

  it('never grows — not for a vehicle, not for a bucket, not for a stream', () => {
    const before = JSON.stringify(railSections())
    // The rail takes no arguments on purpose. There is nothing to pass it that
    // could add a row, which is the whole of decision D9.
    expect(railSections).toHaveLength(0)
    expect(JSON.stringify(railSections())).toBe(before)
  })

  it('bands the rows under exactly two eyebrows', () => {
    expect(railSections().map((s) => s.eyebrow)).toEqual(['Guide', 'Lessons'])
  })

  it('carries no badge, because a lesson has no count', () => {
    const items = railSections().flatMap((s) => s.items)
    expect(items.every((i) => i.badge === undefined)).toBe(true)
  })
})

describe('the tabs each lesson carries', () => {
  it('gives lesson 01 the overview and one tab per storage object', () => {
    expect(tabsFor('lesson-01').map((t) => t.label)).toEqual([
      'Overview',
      'ODOMETER',
      'odometer-write',
      'odometer-read',
    ])
  })

  it('gives lesson 02 the four pool conditions', () => {
    expect(tabsFor('lesson-02').map((t) => t.label)).toEqual([
      'Live',
      'Starvation',
      'Redelivery',
      '1 vs 4',
    ])
  })

  it('gives odometer-pool-workers no tab of its own — D10a', () => {
    const every = LESSONS.flatMap((l) => l.tabs.map((t) => t.label))
    expect(every).not.toContain('odometer-pool-workers')
  })

  it('prints a command for every tab, so nothing on screen is unreproducible', () => {
    const every = LESSONS.flatMap((l) => l.tabs)
    expect(every.every((t) => typeof t.cmd === 'string' && t.cmd.length > 0)).toBe(true)
  })

  it('has no tabs for the guide', () => {
    expect(tabsFor(GUIDE.key)).toEqual([])
  })
})

describe('the breadcrumb carries the lesson — D11', () => {
  it('names the lesson between the demo and the page', () => {
    expect(crumbFor('lesson-02', null)).toEqual({
      lesson: '02 · Scaling a consumer',
      title: 'Worker pool',
    })
  })

  it('puts the chosen vehicle at the end of lesson 01', () => {
    expect(crumbFor('lesson-01', 'truck-7')).toEqual({
      lesson: '01 · Stream + CQRS',
      title: 'truck-7',
    })
  })

  it('says all vehicles when no vehicle is chosen', () => {
    expect(crumbFor('lesson-01', null).title).toBe('All vehicles')
  })

  it('bands the guide under Guide, not under a lesson', () => {
    expect(crumbFor('about', null)).toEqual({ lesson: 'Guide', title: 'How it works' })
  })

  it('falls back to lesson 01 for a view it does not know', () => {
    expect(lessonFor('nonsense').key).toBe('lesson-01')
  })
})
