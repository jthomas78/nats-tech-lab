/*
  Failure switch (a) of task 1b-4: a remote that is slow rather than broken, so
  the pending-region animation (BR-AS08, BR-AS14) can be seen for as long as it
  takes to look at it.

  The delay is at module scope, so it is paid while the chunk is being
  evaluated — exactly where a real slow remote costs time, and before the
  loader's activate() step.
*/

export const LOAD_DELAY_MS = 6000

await new Promise((resolve) => {
  setTimeout(resolve, LOAD_DELAY_MS)
})

export { components, activate } from './plugin.js'
