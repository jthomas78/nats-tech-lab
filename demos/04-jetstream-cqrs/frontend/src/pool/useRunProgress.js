import { computed, ref } from 'vue'

// One progress bar for a whole set of runs (plan 04.9.4, decisions D3, D11, D12).
//
// D3: there is NO new transport here. The pool already writes one key per
// worker into KV odometer-pool-workers and the browser already watches that
// bucket. This turns those rows into a percentage. A run that reported its own
// progress down a second channel would only agree with the workers bucket for
// as long as nothing went wrong -- which is the half of the lesson that matters
// least.
//
// D12 is the condition 04.9 was approved on: the bar is visible from the first
// press to the last run of a set, INCLUDING the re-seed between runs. Four bars
// that each vanish at the end would read as four finished sets.
//
// D11: the run lives in the shim, not in the tab. A reader who reloads the page
// mid-set must find the set still going, so the plan is parked in sessionStorage
// and read back when the composable is built.

// Where a set in flight is parked so a reload can find it.
const PLAN_KEY = 'lesson02.runset'

// A worker row carries `acked` and nothing else that counts progress. See the
// header of view/pool.js: a heartbeat holds worker, status, holding, acked,
// dropped and behind, and this must not invent a field.
const ackedIn = (rows) =>
  (rows ?? []).reduce((total, row) => total + (Number(row?.acked) || 0), 0)

export function useRunProgress({ session = globalThis.sessionStorage } = {}) {
  // The plan: what the whole set is, decided once when Run is pressed.
  const plan = ref(null)
  // Which run of the set is in flight, 1-based.
  const index = ref(1)
  // The fraction of the CURRENT run that is acked, 0 to 1.
  const fraction = ref(0)
  const reseed = ref(false)
  const done = ref(false)

  // Stall watching. `mark` is the acked total the last time anything moved,
  // `movedAt` is when that was.
  const mark = ref(0)
  const movedAt = ref(0)
  const quietFor = ref(0)

  const park = () => {
    if (!plan.value) {
      session?.removeItem?.(PLAN_KEY)
      return
    }
    session?.setItem?.(PLAN_KEY, JSON.stringify({ ...plan.value, index: index.value }))
  }

  // A reload lands here. Anything unreadable is treated as no set at all --
  // a broken parked plan must not stop the screen from drawing.
  try {
    const parked = JSON.parse(session?.getItem?.(PLAN_KEY) ?? 'null')
    if (parked && Number(parked.runs) > 0) {
      plan.value = parked
      index.value = Number(parked.index) || 1
    }
  } catch {
    session?.removeItem?.(PLAN_KEY)
  }

  const running = computed(() => Boolean(plan.value) && !done.value)
  const runsTotal = computed(() => plan.value?.runs ?? 0)
  const runIndex = computed(() => (plan.value ? index.value : 0))

  // The percentage is of the SET, not of the run in flight. Run 2 of 4 half
  // done is 37.5%, and a bar that said 50% there would go backwards when run 3
  // started.
  const percent = computed(() => {
    if (done.value) return 100
    if (!plan.value) return 0
    const whole = (index.value - 1 + fraction.value) / plan.value.runs
    return Math.round(Math.min(1, Math.max(0, whole)) * 100)
  })

  // The threshold is the pool's OWN -ack-wait, handed in with the plan. Below
  // it a quiet worker is a worker waiting for a redelivery, which is the
  // lesson rather than a fault.
  const stalled = computed(
    () => running.value && !reseed.value && quietFor.value > (plan.value?.ackWaitMs ?? 0),
  )

  const note = computed(() => {
    if (!plan.value) return ''
    const parts = [plan.value.label, `run ${index.value} of ${plan.value.runs}`]
    if (reseed.value) parts.push('re-seeding the log')
    if (stalled.value) parts.push('nothing has moved for longer than the ack-wait')
    return parts.join(' · ')
  })

  const start = (next) => {
    plan.value = { ...next }
    index.value = 1
    fraction.value = 0
    reseed.value = false
    done.value = false
    mark.value = 0
    movedAt.value = 0
    quietFor.value = 0
    park()
  }

  // Between runs. The bar stays up (D12) and the stall clock restarts, because
  // a log being rebuilt is not a pool that has stopped.
  const reseeding = () => {
    reseed.value = true
    fraction.value = 0
    mark.value = 0
    movedAt.value = 0
    quietFor.value = 0
  }

  const nextRun = () => {
    if (!plan.value) return
    index.value = Math.min(plan.value.runs, index.value + 1)
    reseed.value = false
    fraction.value = 0
    mark.value = 0
    movedAt.value = 0
    quietFor.value = 0
    park()
  }

  // Called on every change of the workers bucket. `now` is passed in rather
  // than read from the clock so a spec can state the time it means.
  const observe = (rows, now = Date.now()) => {
    if (!plan.value) return
    reseed.value = false
    const acked = ackedIn(rows)
    fraction.value = plan.value.events > 0 ? acked / plan.value.events : 0
    if (movedAt.value === 0 || acked > mark.value) {
      mark.value = acked
      movedAt.value = now
      quietFor.value = 0
      return
    }
    quietFor.value = now - movedAt.value
  }

  const finish = () => {
    done.value = true
    plan.value = null
    session?.removeItem?.(PLAN_KEY)
  }

  return {
    running, percent, runIndex, runsTotal, stalled, note,
    start, nextRun, reseeding, observe, finish,
  }
}
