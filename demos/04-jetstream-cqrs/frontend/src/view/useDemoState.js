/* The demo's whole live state, in one place, so two entries can share it.

   Demo 04 is rendered two ways from ONE build (task 16d of the app shell
   plan): standalone on port 20401, where `App.vue` supplies its own
   `@ui-shell/AppShell`, and embedded in `lab-shell`, where the shell supplies
   the outer chrome and a plugin entry must NOT render `AppShell` itself
   (BR-AS09). The two differ only in where the rail, the breadcrumb and the
   connection tag are placed. Everything else — the NATS watch, the command
   runner, the lesson selection, the panels — is this function, called once by
   whichever entry is rendering.

   It is a composable, not a store: one call, one NATS connection. Calling it
   twice in one page would open a second watch, which is why neither panel
   calls it.

   It returns a `reactive` object rather than a bag of refs, because the whole
   bag is handed to `LessonPanels.vue` as one prop. A ref reached through a
   plain object is NOT unwrapped in a template — `state.crumb.heading` would
   read a property off the ref itself and render nothing — and `reactive`
   unwraps on access, so both entries and the shared body read the same way. */
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'

import {
  COMMAND_API,
  NATS_WS,
  POOL_KV,
  POOL_TRUTH_KV,
  POOL_WORKERS_KV,
  READ_KV,
  STREAM,
  WRITE_KV,
} from '../config.js'
import { useCommands } from '../commands/useCommands.js'
import { useOdometer } from '../nats/useOdometer.js'
import { crumbFor, railSections } from './lessons.js'

/* One label and one colour per connection state. A reconnect is reported, not
   hidden: nats-core retries on its own and the screen should say so. */
const STATES = {
  idle: { label: 'Not connected', severity: 'danger' },
  connecting: { label: 'Connecting', severity: 'warn' },
  connected: { label: 'Watching', severity: 'success' },
  reconnecting: { label: 'Reconnecting', severity: 'warn' },
  closed: { label: 'Disconnected', severity: 'secondary' },
  error: { label: 'Not connected', severity: 'danger' },
}

export function useDemoState() {
  const odometer = useOdometer()
  const { status, error, pool, poolWorkers, poolTruth, connect, disconnect } = odometer

  onMounted(connect)
  onBeforeUnmount(disconnect)

  const { pending, outcomes, run } = useCommands()

  const connection = computed(() => STATES[status.value] ?? STATES.idle)

  const view = ref('lesson-01')
  const isLesson01 = computed(() => view.value === 'lesson-01')
  const isLesson02 = computed(() => view.value === 'lesson-02')

  const sections = railSections()
  const crumb = computed(() => crumbFor(view.value))

  /* What the page is actually watching, not what it hoped to. The three pool
     buckets only exist once somebody has run `cqrs pool`, so naming them
     unconditionally would claim a subscription the page has not got. */
  const watching = computed(() => {
    const names = [WRITE_KV, READ_KV, STREAM]
    if (pool.size) names.push(POOL_KV)
    if (poolWorkers.size) names.push(POOL_WORKERS_KV)
    if (poolTruth.size) names.push(POOL_TRUTH_KV)
    return names.join(', ')
  })

  return reactive({
    ...odometer,
    error,
    pending,
    outcomes,
    run,
    connection,
    view,
    isLesson01,
    isLesson02,
    sections,
    crumb,
    watching,
    wiring: { NATS_WS, COMMAND_API },
  })
}
