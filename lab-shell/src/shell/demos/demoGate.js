/* The pre-mount gate (BR-AS79, task 16e).

   Before a plugin's route renders, the shell asks whether the demo behind it
   is ready. If it is not, the shell draws its OWN panel instead of mounting
   the plugin — because a plugin mounted against a demo that is not there
   reports the outage in its own words, in its own layout, one screen deeper
   than the reader needed to go.

   After a successful check the gate steps out of the way completely. Losing
   the connection five seconds later is the plugin's business, and BR-AS79
   splits it exactly there: the shell owns the moment BEFORE mount, the plugin
   owns every moment after. The gate never re-checks behind a running plugin.

   Why a wrapper component rather than a navigation guard: the gate must
   re-check every time the demo is OPENED, and vue-router resolves an async
   route component once and caches it. A component, by contrast, is mounted
   afresh on each navigation — so `onMounted` gives "check on open" for free,
   and the retry button is a second call to the same function.

   A plugin with no associated demo passes straight through. The store answers
   `available` for it, nothing is probed, and no fault is reported anywhere —
   which is the registry plugin with no lab demo behind it, working normally.
*/
import { defineComponent, h, onMounted, ref } from 'vue'

import DemoStatePanel from '@ui-shell/DemoStatePanel.vue'

import { showsRunCommand } from './audience.js'
import {
  demoCheckedAt,
  demoDetail,
  demoFailingNames,
  demoHeadline,
  demoRunInstruction,
  demoTone,
} from './demoReadinessText.js'
import { DEMO_STATE } from './readinessProbe.js'

/**
 * Wrap one resolved plugin component in its demo's readiness gate.
 *
 * @param {object} options
 * @param {any} options.component The plugin's own component.
 * @param {string} options.pluginId
 * @param {object} options.demoStore
 * @param {() => boolean} [options.operator] Injected for the specs.
 * @returns {any} The component to render for this route.
 */
export function withDemoGate({ component, pluginId, demoStore, operator = showsRunCommand }) {
  if (!demoStore || !component) return component

  return defineComponent({
    name: `DemoGate(${pluginId})`,
    inheritAttrs: false,
    setup(_props, { attrs }) {
      const result = ref(null)
      const busy = ref(true)

      async function check() {
        busy.value = true
        try {
          result.value = await demoStore.checkBeforeMount(pluginId)
        } finally {
          busy.value = false
        }
      }

      onMounted(check)

      return () => {
        /* The first check has not answered yet. Render nothing rather than a
           spinner: the check is one same-origin request against a local
           service, and a flash of "checking…" on every navigation is a worse
           lie about the shell's speed than a blank frame for 20ms. */
        if (result.value === null) return null

        if (result.value.state === DEMO_STATE.AVAILABLE) return h(component, attrs)

        const entry = demoStore.entryFor(pluginId)
        return h(DemoStatePanel, {
          tone: demoTone(result.value.state),
          headline: demoHeadline(result.value),
          detail: demoDetail(result.value),
          items: demoFailingNames(result.value),
          command: demoRunInstruction(result.value, {
            runCommand: entry?.runCommand ?? null,
            operator: operator(),
          }),
          checkedAt: demoCheckedAt(result.value),
          busy: busy.value,
          onRetry: check,
        })
      }
    },
  })
}
