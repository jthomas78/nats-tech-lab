import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import RunControl from './RunControl.vue'
import { poolRunCmd } from '../view/lessons.js'

// The one Run control, shared by all four of lesson 02's tabs (plan 04.9.5,
// decisions D2 and D4).
//
// One component, not four. Four buttons written separately drift: one gets a
// price and the others do not, one prints its command and the others forget.
//
// D4: a run is priced BEFORE it happens. A screen that goes quiet for ninety
// seconds looks broken, and a reader who was never told the cost is spending
// time they did not agree to.
//
// D2: the commands are a visible block under the button, not a tooltip. A
// number this demo shows must be reproducible in a terminal.

const plan = (over = {}) => ({
  runs: 4,
  workers: 8,
  seconds: 90,
  commands: [poolRunCmd({ workers: 8, maxPending: 1 })],
  ...over,
})

const at = (w, id) => w.find(`[data-testid="${id}"]`)

describe('the price of a press', () => {
  it('states the runs, the workers and the time', () => {
    const w = mount(RunControl, { props: plan() })
    const cost = at(w, 'run-cost').text()
    expect(cost).toContain('8 workers')
    expect(cost).toContain('4 runs')
    expect(cost).toContain('90 seconds')
  })

  // Two inputs, because a price that is really a fixed string would pass the
  // spec above on its own.
  it('prices a single run as one run, not four', () => {
    const w = mount(RunControl, { props: plan({ runs: 1, workers: 1, seconds: 20 }) })
    const cost = at(w, 'run-cost').text()
    expect(cost).toContain('1 run')
    expect(cost).not.toContain('1 runs')
    expect(cost).toContain('1 worker')
    expect(cost).not.toContain('1 workers')
  })
})

describe('what the press will actually type', () => {
  it('prints every command, one per line, in the order it runs them', () => {
    const cmds = [
      poolRunCmd({ workers: 8, maxPending: 1 }),
      poolRunCmd({ workers: 8, maxPending: 3 }),
    ]
    const w = mount(RunControl, { props: plan({ commands: cmds }) })
    const lines = at(w, 'run-cmd').findAll('code').map((c) => c.text())
    expect(lines).toEqual(cmds)
  })

  // The guard commands.spec.js applies to every other printed command applies
  // here too: a flag the binary does not define fails in front of the reader.
  it('builds a command the binary would accept', () => {
    expect(poolRunCmd({ workers: 8, maxPending: 1, ackWait: '30s' }))
      .toBe('cqrs pool -drain -workers 8 -max-pending 1 -ack-wait 30s')
  })

  it('leaves out what was not asked for', () => {
    expect(poolRunCmd({ workers: 4 })).toBe('cqrs pool -drain -workers 4')
  })
})

describe('pressing it', () => {
  it('asks its owner to run, once', async () => {
    const w = mount(RunControl, { props: plan() })
    await at(w, 'run-go').trigger('click')
    expect(w.emitted('run')).toHaveLength(1)
  })

  // D8: the screen refuses a second pool rather than warning about one. The
  // shim allows one run at a time, so a second press cannot be offered.
  it('cannot be pressed while something else holds the shim', async () => {
    const w = mount(RunControl, { props: plan({ locked: true }) })
    expect(at(w, 'run-go').attributes('disabled')).toBeDefined()
  })

  // D6: only Live can be stopped mid-run. Every other tab runs to completion.
  it('offers no Stop unless it was given one', () => {
    expect(at(mount(RunControl, { props: plan() }), 'run-stop').exists()).toBe(false)
  })

  it('offers Stop when the tab is stoppable, and only while it runs', async () => {
    const w = mount(RunControl, { props: plan({ stoppable: true, running: true }) })
    await at(w, 'run-stop').trigger('click')
    expect(w.emitted('stop')).toHaveLength(1)
  })
})

describe('the bar, while the set runs', () => {
  // D12, the condition the phase was approved on: one bar, visible for the
  // whole set.
  it('is absent before the first press', () => {
    expect(at(mount(RunControl, { props: plan() }), 'run-bar').exists()).toBe(false)
  })

  it('shows the percentage and the note it was handed', () => {
    const w = mount(RunControl, {
      props: plan({ running: true, percent: 38, note: 'Starvation · run 2 of 4' }),
    })
    expect(at(w, 'run-bar').text()).toContain('38')
    expect(at(w, 'run-bar').text()).toContain('run 2 of 4')
  })

  // A set that has stopped moving says so on the bar. Silence with a bar at
  // 40% is the same picture as a fast run, and the reader cannot tell them
  // apart without being told.
  it('says so when the run has stalled', () => {
    const w = mount(RunControl, { props: plan({ running: true, percent: 40, stalled: true }) })
    expect(at(w, 'run-stalled').exists()).toBe(true)
  })

  it('is quiet when nothing has stalled', () => {
    const w = mount(RunControl, { props: plan({ running: true, percent: 40 }) })
    expect(at(w, 'run-stalled').exists()).toBe(false)
  })
})
