/* The shell's demo state (BR-AS79, task 16e).

   One reactive store, built in BOTH plugin sources. Nothing in it reads
   `pluginSource()`, and nothing may: R-1 requires the panel, its three states,
   its retry and its menu-card status to be identical under `build` and under
   `registry`, with no change to a demo's files between the two runs. The
   easiest way to keep that promise is to give the code no way to tell.

   Two refresh policies, deliberately separate, because they answer different
   questions:

   - The PRE-MOUNT check. One probe, now, for the demo a reader just opened.
     It is never served from the poll's memory: a reader who starts a demo and
     clicks in must not meet a stale "not ready" from thirty seconds ago.
   - The MENU-CARD poll. Every demo, on a slow timer, so the cards on the home
     page populate WITHOUT anyone opening a demo. It is allowed to be a little
     old — a card is a glance, not a decision.

   A demo the catalogue does not list has no state and never will. That is the
   normal case for a plugin with no lab demo behind it: it mounts, it reports
   nothing, and no fault is raised anywhere.
*/
import { reactive } from 'vue'

import { createDemoCatalogueClient } from './demoCatalogueClient.js'
import { DEMO_STATE, probeDemo } from './readinessProbe.js'

/** How often the menu cards re-check. Slow on purpose; a card is a glance. */
export const MENU_REFRESH_MS = 30000

export function createDemoStore({
  fetch = globalThis.fetch?.bind(globalThis),
  client = createDemoCatalogueClient({ fetch }),
  probe = probeDemo,
  setInterval: setTimer = setInterval,
  clearInterval: clearTimer = clearInterval,
} = {}) {
  /* `reactive`, not a bag of refs: a template reads `store.byDemo[id].state`
     through plain property access, which unwraps, and a ref nested in a plain
     object would not. */
  const state = reactive({
    loaded: false,
    revision: null,
    /* Keyed by pluginId, because a route being opened knows its plugin, not
       its directory. The entry keeps `demo` for the route it probes. */
    byPlugin: {},
    /* The last probe result per pluginId, or undefined for never checked. */
    results: {},
    /* pluginIds with a probe in flight, so a panel can say "checking" and a
       second click cannot start a second request. */
    checking: {},
  })

  let timer = null
  const inFlight = new Map()

  async function load() {
    const answer = await client.fetchDemoCatalogue()
    state.revision = answer.revision
    const byPlugin = {}
    for (const entry of answer.demos) byPlugin[entry.pluginId] = entry
    state.byPlugin = byPlugin
    state.loaded = true
    return answer
  }

  /** The catalogue entry for a plugin, or null when no demo is associated. */
  function entryFor(pluginId) {
    return state.byPlugin[pluginId] ?? null
  }

  /**
   * Probe one demo now.
   *
   * Shared in flight: two callers asking at once get one request and the same
   * answer. That is not an optimisation — it is what stops the pre-mount check
   * and the poll from racing to write two different results.
   */
  function refresh(pluginId) {
    const entry = entryFor(pluginId)
    if (entry === null) return Promise.resolve(null)
    const running = inFlight.get(pluginId)
    if (running) return running

    state.checking[pluginId] = true
    const request = probe({ readiness: entry.readiness, fetch })
      .then((result) => {
        state.results[pluginId] = result
        return result
      })
      .finally(() => {
        inFlight.delete(pluginId)
        delete state.checking[pluginId]
      })
    inFlight.set(pluginId, request)
    return request
  }

  /** The last answer for a plugin, or null when nobody has asked yet. */
  function resultFor(pluginId) {
    return state.results[pluginId] ?? null
  }

  /**
   * The menu-card refresh policy. Loads the catalogue once, probes everything,
   * then repeats slowly — so a card shows a status without anyone opening the
   * demo. Idempotent: calling it twice does not start two timers.
   */
  async function startMenuRefresh() {
    if (!state.loaded) await load()
    const sweep = () => Promise.all(Object.keys(state.byPlugin).map(refresh))
    await sweep()
    if (timer === null) timer = setTimer(sweep, MENU_REFRESH_MS)
  }

  function stop() {
    if (timer !== null) clearTimer(timer)
    timer = null
  }

  /**
   * The pre-mount gate's question: may this plugin mount?
   *
   * Always probes, never reuses the poll's answer, for the reason above.
   * A plugin with no associated demo returns `available` with no entry — it is
   * not a demo, so there is nothing to be unready about.
   */
  async function checkBeforeMount(pluginId) {
    if (!state.loaded) await load()
    const entry = entryFor(pluginId)
    if (entry === null) return { state: DEMO_STATE.AVAILABLE, cause: null, failing: [], checkedAt: null }
    return refresh(pluginId)
  }

  return { state, load, entryFor, refresh, resultFor, startMenuRefresh, stop, checkBeforeMount }
}
