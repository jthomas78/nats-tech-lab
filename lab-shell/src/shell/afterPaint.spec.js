import { describe, expect, it, vi } from 'vitest'

import { createAfterPaint, PAINT_TIMEOUT_MS } from './afterPaint.js'

const settled = (promise) => {
  let done = false
  promise.then(() => { done = true })
  return () => done
}

describe('BR-AS30 — the boot gate waits for a paint, but not forever', () => {
  it('resolves after two animation frames and not after one', async () => {
    const frames = []
    const afterPaint = createAfterPaint({
      requestAnimationFrame: (cb) => frames.push(cb),
      setTimeout: vi.fn(() => 1),
      clearTimeout: vi.fn(),
    })
    const isDone = settled(afterPaint())
    frames.shift()()
    await Promise.resolve()
    expect(isDone()).toBe(false)
    frames.shift()()
    await Promise.resolve()
    expect(isDone()).toBe(true)
  })

  /* The case the timeout exists for: a shell deep-linked into a background
     tab. That tab does not composite, so no frame ever arrives — and without
     this the shell would never connect, never read the registry and never load
     a remote plugin until the user first looked at it. */
  it('resolves anyway when no frame ever arrives', async () => {
    let fire = null
    const afterPaint = createAfterPaint({
      requestAnimationFrame: () => {},
      setTimeout: (cb, ms) => { fire = () => cb(); return ms },
      clearTimeout: vi.fn(),
    })
    const isDone = settled(afterPaint())
    await Promise.resolve()
    expect(isDone()).toBe(false)
    fire()
    await Promise.resolve()
    expect(isDone()).toBe(true)
  })

  it('waits a second at most, so a visible tab is never held by the fallback', () => {
    const schedule = vi.fn(() => 1)
    createAfterPaint({ requestAnimationFrame: () => {}, setTimeout: schedule, clearTimeout: vi.fn() })()
    expect(schedule).toHaveBeenCalledWith(expect.any(Function), PAINT_TIMEOUT_MS)
    expect(PAINT_TIMEOUT_MS).toBe(1000)
  })

  it('cancels the timer once the frames land', async () => {
    const frames = []
    const cancel = vi.fn()
    const afterPaint = createAfterPaint({
      requestAnimationFrame: (cb) => frames.push(cb),
      setTimeout: () => 'timer-id',
      clearTimeout: cancel,
    })
    afterPaint()
    frames.shift()()
    frames.shift()()
    await Promise.resolve()
    expect(cancel).toHaveBeenCalledWith('timer-id')
  })

  /* Both paths can run: the timer fires in a tab that is then brought forward
     and paints. The second arrival must be inert. */
  it('settles once when a frame arrives after the timeout already fired', async () => {
    const frames = []
    let fire = null
    const afterPaint = createAfterPaint({
      requestAnimationFrame: (cb) => frames.push(cb),
      setTimeout: (cb) => { fire = cb; return 1 },
      clearTimeout: vi.fn(),
    })
    let resolutions = 0
    afterPaint().then(() => { resolutions++ })
    fire()
    frames.shift()()
    frames.shift()()
    await Promise.resolve()
    await Promise.resolve()
    expect(resolutions).toBe(1)
  })
})
