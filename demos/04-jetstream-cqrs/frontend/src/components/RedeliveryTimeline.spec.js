import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import { redeliveryRows } from '../view/redelivery.js'
import RedeliveryTimeline from './RedeliveryTimeline.vue'

const run = redeliveryRows()[0]
const mountIt = (r = run) =>
  mount(RedeliveryTimeline, { props: { run: r }, global: { plugins: [PrimeVue] } })

describe('RedeliveryTimeline', () => {
  it('starts the clock at the silence, not at the first delivery', () => {
    // The pool clock starts when the worker is killed. Nothing recorded when
    // the event was FIRST handed out, so the drawing must not imply it did.
    const text = mountIt().text()
    expect(text).toContain('t = 0s')
    expect(text).toContain(`worker ${run.killedWorker} goes silent`)
    expect(text).not.toContain('delivery 1')
  })

  it('marks the redelivery at the measured wait, not at the AckWait', () => {
    expect(mountIt().text()).toContain(`t = ${run.waitedSeconds}s`)
  })

  it('names both workers, because they are not the same worker', () => {
    const text = mountIt().text()
    expect(text).toContain(`worker ${run.toWorker}`)
    expect(text).toContain(`delivery ${run.delivery}`)
  })

  it('ends on the drop, which is the point of the drawing', () => {
    const text = mountIt().text()
    expect(text).toContain(`lastSeq ${run.foldAt}`)
    expect(text).toContain('dropped')
  })

  it('says the same thing aloud that the drawing shows', () => {
    const label = mountIt().find('svg').attributes('aria-label')
    expect(label).toContain(String(run.killSeq))
    expect(label).toContain(String(run.waitedSeconds))
    expect(label).toContain('dropped')
  })

  it('redraws for a different run rather than hard-coding the first', () => {
    const second = redeliveryRows()[1]
    const text = mountIt(second).text()
    expect(text).toContain(`t = ${second.waitedSeconds}s`)
    expect(text).toContain(`worker ${second.killedWorker} goes silent`)
  })
})
