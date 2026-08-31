/*
  Decision 54's gate: the shell paints its native frame before it mints a
  credential or dials NATS. `nextTick` alone only waits for the DOM patch, so
  the real signal is a browser paint — two animation frames.

  With a timeout beside it, because a frame is not guaranteed to arrive. A
  background tab does not composite, so `requestAnimationFrame` never fires
  there — and a shell opened from a deep link in a new background tab would sit
  with no connection, no registry read and no remote plugins until the user
  first looked at it. The timeout keeps the gate honest in the case it exists
  for (a visible tab paints in one or two frames, far inside it) while making
  "never painted" a delay rather than a dead shell.
*/

export const PAINT_TIMEOUT_MS = 1000

/**
 * @param {object} [deps] injected in specs; defaults read the live browser
 * @param {(cb: FrameRequestCallback) => number} [deps.requestAnimationFrame]
 * @param {typeof setTimeout} [deps.setTimeout]
 * @param {typeof clearTimeout} [deps.clearTimeout]
 * @param {number} [deps.timeoutMs]
 * @returns {() => Promise<void>} resolves once, on whichever arrives first
 */
export function createAfterPaint({
  requestAnimationFrame: raf = (cb) => window.requestAnimationFrame(cb),
  setTimeout: schedule = setTimeout,
  clearTimeout: cancel = clearTimeout,
  timeoutMs = PAINT_TIMEOUT_MS,
} = {}) {
  return () =>
    new Promise((resolve) => {
      let settled = false
      // Guarded, not because a double resolve would throw, but because the
      // frames can still land after the timeout fired and the timer can still
      // fire after the frames: whichever is second must be a no-op.
      const settle = () => {
        if (settled) return
        settled = true
        cancel(timer)
        resolve()
      }
      const timer = schedule(settle, timeoutMs)
      raf(() => raf(settle))
    })
}
