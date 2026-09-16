import { describe, expect, it } from 'vitest'

import { LESSONS, crumbFor, lessonFor, railSections, tabsFor } from './lessons.js'

describe('the rail is a lesson index', () => {
  it('holds two rows and nothing else', () => {
    const items = railSections().flatMap((s) => s.items)
    expect(items).toHaveLength(2)
    expect(items.map((i) => i.label)).toEqual([
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

  it('bands the rows under the Lessons eyebrow', () => {
    expect(railSections().map((s) => s.eyebrow)).toEqual(['Lessons'])
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

  it('gives lesson 02 the four pool conditions, then the bucket it folds into', () => {
    expect(tabsFor('lesson-02').map((t) => t.label)).toEqual([
      'Live',
      'Starvation',
      'Redelivery',
      '1 vs 4',
      'odometer-pool',
    ])
  })

  it('gives odometer-pool-workers no tab of its own — D10a', () => {
    const every = LESSONS.flatMap((l) => l.tabs.map((t) => t.label))
    expect(every).not.toContain('odometer-pool-workers')
  })

  it('keeps commands on each single-purpose lesson 02 tab', () => {
    const every = tabsFor('lesson-02')
    expect(every.every((t) => typeof t.cmd === 'string' && t.cmd.length > 0)).toBe(true)
  })

  it('has no tabs for the guide', () => {
    expect(tabsFor('about')).toEqual([])
  })
})

describe('the breadcrumb carries the lesson — D11', () => {
  it('names the lesson between the demo and the page', () => {
    expect(crumbFor('lesson-02')).toEqual({
      lesson: '02 · Scaling a consumer',
      title: 'Worker pool',
    })
  })

  it('names lesson 01 without a vehicle', () => {
    expect(crumbFor('lesson-01')).toEqual({
      lesson: '01 · Stream + CQRS',
      title: 'Odometer',
    })
  })

  it('falls back to lesson 01 for a view it does not know', () => {
    expect(lessonFor('nonsense').key).toBe('lesson-01')
  })
})
