import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import RedeliveryTimeline from './RedeliveryTimeline.vue'

// The drawing now takes the run the READER just made (04.9.9). Before that it
// took a row out of view/redelivery.js, so this spec imported the same file
// the component did and proved only that the two agreed.
//
// The runs below are written out here, in the spec, on purpose: a fixture a
// component also reads cannot fail when the component stops reading it.

const dropped = {
  killSeq: 490_094,
  killedWorker: 1,
  toWorker: 3,
  delivery: 2,
  waitedSeconds: 30.005,
  ackWait: '30s',
  foldAt: 499_994,
  ranOn: 9_900,
  recovered: false,
}

const recovered = {
  killSeq: 94,
  killedWorker: 2,
  toWorker: 1,
  delivery: 2,
  waitedSeconds: 5.001,
  ackWait: '5s',
  foldAt: 93,
  ranOn: 0,
  recovered: true,
}

const mountIt = (run = dropped) =>
  mount(RedeliveryTimeline, { props: { run }, global: { plugins: [PrimeVue] } })

describe('RedeliveryTimeline', () => {
  it('starts the clock at the silence, not at the first delivery', () => {
    // The pool clock starts when the worker is killed. Nothing recorded when
    // the event was FIRST handed out, so the drawing must not imply it did.
    const text = mountIt().text()
    expect(text).toContain('t = 0s')
    expect(text).toContain(`worker ${dropped.killedWorker} goes silent`)
    expect(text).not.toContain('delivery 1')
  })

  it('marks the redelivery at the measured wait, not at the AckWait', () => {
    expect(mountIt().text()).toContain(`t = ${dropped.waitedSeconds}s`)
  })

  it('names both workers, because they are not the same worker', () => {
    const text = mountIt().text()
    expect(text).toContain(`worker ${dropped.toWorker}`)
    expect(text).toContain(`delivery ${dropped.delivery}`)
  })

  it('ends on the drop, which is the point of the drawing', () => {
    const text = mountIt().text()
    expect(text).toContain(`lastSeq ${dropped.foldAt}`)
    expect(text).toContain('dropped')
  })

  it('says the same thing aloud that the drawing shows', () => {
    const label = mountIt().find('svg').attributes('aria-label')
    expect(label).toContain(String(dropped.killSeq))
    expect(label).toContain(String(dropped.waitedSeconds))
    expect(label).toContain('dropped')
  })

  it('redraws for a different run rather than hard-coding the first', () => {
    const text = mountIt(recovered).text()
    expect(text).toContain(`t = ${recovered.waitedSeconds}s`)
    expect(text).toContain(`worker ${recovered.killedWorker} goes silent`)
  })

  // A live run can end either way, and the recorded rows only ever ended one
  // way. A drawing that says "dropped" over an event the fold accepted is a
  // lie the reader has no way to catch.
  it('does not call it a drop when the fold accepted the event', () => {
    const w = mountIt(recovered)
    expect(w.text()).not.toContain('dropped')
    expect(w.text()).toContain('folded')
    expect(w.find('svg').attributes('aria-label')).toContain('folded')
  })
})
