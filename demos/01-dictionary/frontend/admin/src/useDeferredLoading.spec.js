import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, ref } from 'vue'
import { mount } from '@vue/test-utils'

import { useDeferredLoading } from './useDeferredLoading'

// The threshold exists because PrimeVue's DataTable mask has a 300ms leave
// animation (`--p-mask-transition-duration`), so an overlay raised for a 40ms
// fetch is on screen for ~340ms. These specs pin the two halves of the trade:
// a fast load must show nothing at all, and a slow one must still report.

// Mounted rather than called bare, because the composable registers
// onUnmounted — outside a component instance that warns and leaks its timer.
function harness(source) {
  const seen = []
  const w = mount(
    defineComponent({
      setup() {
        const visible = useDeferredLoading(source)
        return () => {
          seen.push(visible.value)
          return h('div', String(visible.value))
        }
      },
    }),
  )
  return { w, seen, visible: () => w.text() === 'true' }
}

describe('useDeferredLoading', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('stays down for a load that finishes inside the threshold', async () => {
    const src = ref(true)
    const { w, seen } = harness(src)
    src.value = false
    await vi.advanceTimersByTimeAsync(1000)
    // Never true at any point — a single true frame is a mask, and a mask is
    // 300ms of fade-out regardless of how briefly it was asked for.
    expect(seen).not.toContain(true)
    expect(w.text()).toBe('false')
  })

  it('comes up for a load that outruns the threshold', async () => {
    const src = ref(true)
    const { w } = harness(src)
    await vi.advanceTimersByTimeAsync(179)
    expect(w.text()).toBe('false')
    await vi.advanceTimersByTimeAsync(2)
    expect(w.text()).toBe('true')
  })

  it('drops immediately when the load ends, without serving out the threshold', async () => {
    const src = ref(true)
    const { w } = harness(src)
    await vi.advanceTimersByTimeAsync(200)
    expect(w.text()).toBe('true')
    src.value = false
    await vi.advanceTimersByTimeAsync(0)
    expect(w.text()).toBe('false')
  })

  it('re-arms per load rather than latching after the first one', async () => {
    const src = ref(false)
    const { w } = harness(src)
    src.value = true
    await vi.advanceTimersByTimeAsync(200)
    expect(w.text()).toBe('true')
    src.value = false
    await vi.advanceTimersByTimeAsync(0)
    // A second slow load must raise it again — a latched flag would leave the
    // interval refreshes permanently masked or permanently bare.
    src.value = true
    await vi.advanceTimersByTimeAsync(200)
    expect(w.text()).toBe('true')
  })

  it('cancels a pending timer on unmount', async () => {
    const src = ref(true)
    const { w } = harness(src)
    // Armed by the immediate watch during setup.
    expect(vi.getTimerCount()).toBe(1)
    w.unmount()
    // Counted BEFORE advancing: advancing first fires the pending timer and
    // leaves a count of 0 either way, which is a spec that cannot fail.
    expect(vi.getTimerCount()).toBe(0)
  })
})
