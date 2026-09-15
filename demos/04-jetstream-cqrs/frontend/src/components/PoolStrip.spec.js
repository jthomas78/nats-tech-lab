// The strip is the one drawing that shows an event being thrown away, so these
// its guard the thing that is easy to lose in a refactor: the doomed chip, and
// the sentence under it that says nothing will report the loss.

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import PoolStrip from './PoolStrip.vue'

const worker = (n, holding) => ({ worker: n, holding, status: 'working' })
const strip = (props) => mount(PoolStrip, { props })

describe('PoolStrip', () => {
  it('draws one chip per sequence, ending at the head', () => {
    const w = strip({ head: 10, foldSeq: 7 })
    expect(w.find('[data-testid="chip-10"]').exists()).toBe(true)
    expect(w.find('[data-testid="chip-11"]').exists()).toBe(false)
  })

  it('names the watermark on the chip that is it', () => {
    const w = strip({ head: 10, foldSeq: 7 })
    const mark = w.find('[data-testid="chip-7"]')
    expect(mark.classes()).toContain('mark')
    expect(mark.text()).toContain('lastSeq')
  })

  it('says which worker is holding an event in flight', () => {
    const w = strip({ head: 10, foldSeq: 7, rows: [worker(1, 8)] })
    const chip = w.find('[data-testid="chip-8"]')
    expect(chip.classes()).toContain('flight')
    expect(chip.text()).toContain('worker 1')
  })

  it('colours an event behind the watermark as doomed, and says so in words', () => {
    const w = strip({ head: 10, foldSeq: 7, rows: [worker(3, 6)] })
    expect(w.find('[data-testid="chip-6"]').classes()).toContain('doomed')
    expect(w.find('[data-testid="strip-drop"]').text()).toContain('no error, no retry')
  })

  it('prints no warning line when nothing is being dropped', () => {
    const w = strip({ head: 10, foldSeq: 7, rows: [worker(1, 9)] })
    expect(w.find('[data-testid="strip-drop"]').exists()).toBe(false)
  })

  it('draws the consumer as ONE bar — the position is not a worker’s', () => {
    const w = strip({ head: 10, foldSeq: 7 })
    const bar = w.find('[data-testid="strip-consumer"]')
    expect(bar.text()).toContain('odometer-pool')
    expect(bar.text()).toContain('one durable consumer')
  })

  // The heartbeat bucket does not carry the consumer's limits, so the panel
  // does not know them. A blank is honest; a plausible 1000 is not.
  it('leaves the limits out when nobody told it what they are', () => {
    const w = strip({ head: 10, foldSeq: 7 })
    expect(w.text()).not.toContain('MaxAckPending')
  })

  it('prints the limits it is given', () => {
    const w = strip({ head: 10, foldSeq: 7, maxAckPending: '1000', ackWait: '30s' })
    expect(w.text()).toContain('MaxAckPending 1000')
    expect(w.text()).toContain('AckWait 30s')
  })

  it('reads the same thing aloud that the colours say', () => {
    const w = strip({ head: 10, foldSeq: 7, rows: [worker(3, 6)] })
    const label = w.find('[data-testid="pool-strip"]').attributes('aria-label')
    expect(label).toContain('#6 in flight and about to be dropped')
    expect(label).toContain('#7 the watermark')
  })
})

// A window that follows the watermark does not always reach the head. The
// strip has to say so; a log that appears to stop at the last chip is a lie.
describe('PoolStrip — the head', () => {
  it('says "head" when the chips reach it', () => {
    const w = strip({ head: 10, foldSeq: 7 })
    expect(w.find('[data-testid="strip-head"]').text()).toBe('head')
  })

  it('counts the events it could not draw when the pool is behind', () => {
    const w = strip({ head: 26204, foldSeq: 12000 })
    const text = w.find('[data-testid="strip-head"]').text()
    expect(text).toContain('more')
    expect(text).toContain('#26,204')
  })
})
