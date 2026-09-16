import { computed, ref } from 'vue'

import { useRunProgress } from './useRunProgress.js'

// A set of runs, one after another (plan 04.9.6 and 04.9.7, decisions D5, D7,
// D12).
//
// Starvation varies the cap — 1 / 3 / 8 / 64 — and Performance varies the
// worker count — 1 / 2 / 4 / 8. The numbers differ; the SEQUENCE is identical:
// run, re-seed, run again, and fill a row as each run ends. So it is written
// once here. Two copies would drift, and the one that drifted would be the tab
// nobody re-read.
//
// D7 — the re-seed is not optional and is not left to the reader. Every run is
// a `-drain`, which folds the log from sequence 1 and stops at zero pending.
// The second run of a set would otherwise fold an empty stream and report a
// beautiful, meaningless zero.
//
// D5 — a row starts empty and only ever fills from a result. There is no
// recorded fallback. A fallback is how a screen shows numbers after a run that
// never happened.
//
// This owns the sequence and nothing else: the HTTP is handed in as `seed` and
// `run`, so a spec drives the whole thing without a server.
export function useRunSet({ label, events, ackWaitMs, plans, seed, run, session }) {
  const progress = useRunProgress(session ? { session } : {})

  const blank = () => plans.map((plan) => ({ plan, result: null }))

  const rows = ref(blank())
  const busy = ref(false)
  const broken = ref(null)

  const running = computed(() => busy.value)

  const press = async () => {
    // One press, one set. The shim would refuse a second run anyway, but a
    // screen that offered it and then reported the refusal is a screen that
    // set the reader up.
    if (busy.value) return

    busy.value = true
    broken.value = null
    rows.value = blank()
    progress.start({ label, runs: plans.length, events, ackWaitMs })

    try {
      for (let i = 0; i < plans.length; i += 1) {
        // Between runs, never before the first: the fixture above the tabs
        // already seeded the log, and re-seeding it would charge the reader
        // for the same ten thousand events twice.
        if (i > 0) {
          progress.reseeding()
          const re = await seed(events)
          if (re.kind !== 'ok') {
            broken.value = re
            return
          }
          progress.nextRun()
        }

        const out = await run(plans[i])
        if (out.kind !== 'ok') {
          broken.value = out
          return
        }
        rows.value = rows.value.map((r, j) => (j === i ? { ...r, result: out } : r))
      }
    } finally {
      progress.finish()
      busy.value = false
    }
  }

  return {
    rows,
    running,
    broken,
    progress,
    press,
    // The workers watch calls this. See useRunProgress: it is the only input
    // the bar has, and it is a bucket the page was already reading (D3).
    observe: progress.observe,
  }
}
