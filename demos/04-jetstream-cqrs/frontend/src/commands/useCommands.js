// The command side's little bit of state: what is in flight, and what came
// back. That is all it is allowed to hold.
//
// It deliberately keeps NO copy of a vehicle's state. The answer to "did that
// work" is not stored here — it arrives on the read path, as a new row in the
// log and a moving marker on the lane. A command bar that updated the screen
// itself would be the browser pretending to be the projector.

import { ref } from 'vue'

import { send } from './api.js'

// How many answers the panel keeps. Enough to show a refusal next to the
// accepted command that caused it, not enough to become a second log.
export const OUTCOME_LIMIT = 6

export function useCommands(options = {}) {
  const pending = ref('') // the command name in flight, or ''
  const outcomes = ref([])

  async function run(name, form) {
    if (pending.value) return null
    pending.value = name
    try {
      const outcome = await send(name, form, options)
      outcomes.value = [{ ...outcome, at: new Date() }, ...outcomes.value].slice(0, OUTCOME_LIMIT)
      return outcome
    } finally {
      pending.value = ''
    }
  }

  function clear() {
    outcomes.value = []
  }

  return { pending, outcomes, run, clear }
}
