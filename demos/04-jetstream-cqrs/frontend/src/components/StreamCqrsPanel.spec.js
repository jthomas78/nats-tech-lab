import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import { READ_KV, WRITE_KV } from '../config.js'
import StreamCqrsPanel from './StreamCqrsPanel.vue'

// D10 — the acceptance test for task 04.7.9a.
//
// CLAUDE.md: "Two buckets, not one. The split is the demo." Moving the storage
// objects out of the rail and into a tab strip is only safe while the Overview
// tab still shows BOTH buckets at once. A tab per bucket is for browsing keys;
// it cannot make the argument, because the argument IS the difference between
// the two, and you cannot see a difference one tab at a time.
//
// PrimeVue renders every TabPanel, so "the element is in the DOM" alone would
// not prove Overview is what opens. The first test pins the opening tab too.

const mountPanel = (props) =>
  mount(StreamCqrsPanel, {
    props,
    global: { plugins: [PrimeVue] },
  })

const VEHICLE = {
  vehicle: 'truck-7',
  writeDoc: { plate: 'AB 12 CD', status: 'registered', totalKm: 120, lastSeq: 8 },
  readDoc: { plate: 'AB 12 CD', status: 'registered', totalKm: 118, lastSeq: 7 },
  positions: { head: 9, writeSeq: 8, readSeq: 7 },
  head: 9,
  scope: 'truck-7',
}

const ALL = {
  vehicle: null,
  writeRows: [{ vehicle: 'truck-7', plate: 'AB 12 CD', status: 'registered', totalKm: 120, lastSeq: 8 }],
  readRows: [{ vehicle: 'truck-7', plate: 'AB 12 CD', status: 'registered', totalKm: 118, lastSeq: 7 }],
  positions: { head: 9, writeSeq: 8, readSeq: 7 },
  head: 9,
  scope: 'all vehicles',
}

describe('lesson 01 opens on Overview', () => {
  it('opens on the tab that makes the argument, not on a bucket', () => {
    const w = mountPanel(ALL)
    expect(w.vm.tab).toBe('overview')
  })

  it('prints the command that produced the open tab', () => {
    const w = mountPanel(ALL)
    expect(w.get('.cmd').text()).toContain('nats kv ls')
  })
})

describe('D10 — both buckets stay side by side on Overview', () => {
  it('draws both bucket panels when one vehicle is picked', () => {
    const w = mountPanel(VEHICLE)
    const cols = w.get('[data-testid="overview-buckets"]')
    expect(cols.find('[data-testid="bucket-write"]').exists()).toBe(true)
    expect(cols.find('[data-testid="bucket-read"]').exists()).toBe(true)
  })

  it('draws both key lists when no vehicle is picked', () => {
    const w = mountPanel(ALL)
    const cols = w.get('[data-testid="overview-buckets"]')
    expect(cols.find('[data-testid="bucket-keys-write"]').exists()).toBe(true)
    expect(cols.find('[data-testid="bucket-keys-read"]').exists()).toBe(true)
  })

  it('names both buckets, so the split is readable without the rail', () => {
    const text = mountPanel(ALL).get('[data-testid="overview-buckets"]').text()
    expect(text).toContain(WRITE_KV)
    expect(text).toContain(READ_KV)
  })
})

describe('the storage objects that left the rail', () => {
  it('gives each one a tab of its own', () => {
    const w = mountPanel(ALL)
    for (const key of ['overview', 'stream', 'write', 'read']) {
      expect(w.find(`[data-testid="lesson-01-tab-${key}"]`).exists()).toBe(true)
    }
  })

  it('keeps the lag lane on the page, not behind a tab of its own', () => {
    expect(mountPanel(ALL).find('svg').exists()).toBe(true)
  })
})
