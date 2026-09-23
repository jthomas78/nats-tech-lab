<script setup>
/*
  The plugin inventory — one row per plugin with its version, status and the
  reason for it, plus the contribution-level refusals a plugin-level status
  cannot express (a plugin can be `available` and still have lost one footer
  item to a full region).

  This screen is what makes the whole design inspectable during the BR-AS15
  review: every failure mode the phase demonstrates shows up here as a status
  and a cause code, without opening a console.
*/
import { computed, inject } from 'vue'

import { HEALTH_STATE } from '../shell/registry/healthPlane.js'
import { healthCheckedAt, healthLabel, healthTone } from '../shell/registry/healthText.js'
import FirstBootNote from '../shell/ui/FirstBootNote.vue'
import { describeContributions, statusDetail } from '../shell/registry/inventoryText.js'
import { SHELL } from '../shell/shellKey.js'
import { SHELL_API_VERSION } from '../shell/versions.js'

const shell = inject(SHELL)
const rows = computed(() => shell.inventory)
const rejected = computed(
  () => rows.value.filter((r) => r.status === 'incompatible' || r.status === 'disabled').length,
)

/* A page reload applies pending removals and changed entries. Live discovery
   can add plugins, but cannot tear down an already-activated plugin. */
const reload = () => window.location.reload()

/* Two cells, never one verdict (BR-AS60). A plugin whose UI is served fine
   while its API is down is the case an operator most needs to see, and a
   merged column would hide it. Health decorates the row; it never changes the
   status beside it, because a plugin loaded correctly whether or not its
   backend is answering right now. */
const signalsFor = (id) => shell.health?.signals?.[id] ?? {}

/* BR-AS80 / F-2 — the whole `build`-mode health mapping, and it lives here
   because it is a presentation answer, not a measurement.

   `monitored: false` says there is nothing set up to watch. It is a fact
   about the deployment, so it is answered once for the page rather than
   per plugin, and no `HealthSignal` is invented to carry it. `not configured`
   and `unknown` stay distinct: `unknown` means monitoring exists and has no
   current reading, which is a different thing an operator acts on differently.

   Nothing is said about WHEN, because nothing was checked — so `not
   configured` cannot age, and the title says so rather than showing a time.
   The constant and the tone are the shared ones, so the wording cannot drift
   from the registry-mode cell beside it. */
const monitored = computed(() => shell.health?.monitored !== false)
const signalState = (id, side) => (
  monitored.value ? healthLabel(signalsFor(id)[side]) : HEALTH_STATE.NOT_CONFIGURED
)
const signalToneFor = (id, side) => (
  monitored.value
    ? healthTone(signalsFor(id)[side]?.state)
    : healthTone(HEALTH_STATE.NOT_CONFIGURED)
)
const signalTitle = (id, side) => {
  if (!monitored.value) return 'no monitoring is configured for this deployment'
  const at = healthCheckedAt(signalsFor(id)[side])
  return at ? `last checked ${at}` : 'never checked'
}

const TONE = {
  active: 'ok',
  available: 'off',
  loading: 'busy',
  discovered: 'busy',
  disabled: 'off',
  incompatible: 'warn',
  failed: 'bad',
}

const LIFECYCLE = ['discovered', 'validated', 'indexed', 'first use', 'loaded', 'activated once', 'rendered']

/* BR-AS13's tolerance table, stated where an operator can check it against the
   rows above. */
const TOLERANCE = [
  ['Unknown optional field', 'tolerated'],
  ['Invalid required field', 'that plugin only'],
  ['Unsupported shell API', 'that plugin only, pre-execution'],
  ['Duplicate or reserved id', 'that contribution only'],
  ['Unparsable registry', 'native frame remains available'],
]
</script>

<template>
  <section class="plugins">
    <header class="page-head">
      <div>
        <h1>Plugins</h1>
        <p
          v-if="shell.registryError"
          class="degraded"
        >
          The plugin registry could not be read ({{ shell.registryError.code }}).
          The native frame remains available; previously discovered plugins stay listed.
        </p>
        <p
          v-else-if="shell.registry?.degraded"
          class="degraded"
        >
          The plugin registry is degraded: its database and cache are unavailable.
          The native frame remains available; previously discovered plugins stay listed.
        </p>
        <p v-else>
          Registry rev {{ shell.registry?.revision ?? 'n/a' }}
          <template v-if="shell.registry?.fetchedAt">
            · fetched {{ new Date(shell.registry.fetchedAt).toLocaleTimeString() }}
          </template>
          · shell API {{ SHELL_API_VERSION }} · {{ rows.length }} entries,
          {{ rejected }} rejected before any remote code ran.
        </p>
      </div>
      <button
        class="btn ghost"
        type="button"
        @click="reload"
      >
        Reload registry
      </button>
    </header>

    <!-- The five disabled rows below are the announced tier waiting for an
         operator, not five failures (BR-AS66). -->
    <FirstBootNote />

    <div class="lab-panel">
      <table class="tbl">
        <thead>
          <tr>
            <th style="width: 18%">
              Plugin
            </th>
            <th style="width: 8%">
              Version
            </th>
            <th style="width: 8%">
              Shell API
            </th>
            <th style="width: 11%">
              Status
            </th>
            <th style="width: 10%">
              Frontend
            </th>
            <th style="width: 10%">
              Backend
            </th>
            <th style="width: 16%">
              Contributions
            </th>
            <th>Detail</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="row in rows"
            :key="row.id"
          >
            <td>
              <b>{{ row.name }}</b>
              <span class="id">{{ row.id }}</span>
            </td>
            <td class="mono">
              {{ row.version ?? '—' }}
            </td>
            <td class="mono">
              {{ row.shellApiVersion ?? '—' }}
            </td>
            <td>
              <span
                class="pill"
                :class="TONE[row.status]"
              ><span class="pip" />{{ row.status }}</span>
            </td>
            <td
              v-for="side in ['frontend', 'backend']"
              :key="side"
            >
              <span
                class="pill"
                :class="signalToneFor(row.id, side)"
                :title="signalTitle(row.id, side)"
              ><span class="pip" />{{ signalState(row.id, side) }}</span>
            </td>
            <td :class="describeContributions(row.contributionKinds) ? 'lab-muted' : 'lab-dim'">
              {{ describeContributions(row.contributionKinds) || '— not indexed —' }}
            </td>
            <td :class="row.status === 'failed' ? 'bad' : row.status === 'incompatible' ? 'warn' : 'lab-muted'">
              {{ statusDetail(row) }}
              <ul v-if="row.refusals.length">
                <li
                  v-for="refusal in row.refusals"
                  :key="refusal.qualifiedId"
                >
                  <span class="mono">{{ refusal.code }}</span> {{ refusal.qualifiedId }}
                </li>
              </ul>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="grid-2">
      <div class="lab-panel">
        <h3>Lifecycle</h3>
        <div class="lifecycle">
          <template
            v-for="(step, i) in LIFECYCLE"
            :key="step"
          >
            <span class="lab-dim sep">{{ i === 0 ? '' : '›' }}</span>
            <span
              class="tag"
              :class="{ gate: step === 'first use' }"
            >{{ step }}</span>
          </template>
        </div>
        <p class="lifecycle-note">
          Everything left of <b>first use</b> is metadata only — no remote code has run,
          so permissions and routes are already answerable.
        </p>
      </div>
      <div class="lab-panel">
        <h3>Rejection is per entry, never per registry</h3>
        <table class="tbl">
          <tbody>
            <tr
              v-for="[cause, effect] in TOLERANCE"
              :key="cause"
            >
              <td class="lab-muted">
                {{ cause }}
              </td>
              <td>{{ effect }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<style scoped>
.plugins { display: flex; flex-direction: column; gap: 18px; }
.page-head { display: flex; align-items: flex-end; justify-content: space-between; gap: 24px; }
h1 { margin: 0; font-size: 20px; line-height: 26px; font-weight: 600; }
.page-head p { margin: 2px 0 0; font-size: 12px; color: var(--p-text-muted-color); }
.degraded { color: var(--warn); }
.btn {
  display: inline-flex; align-items: center; height: 28px; padding: 0 12px;
  border-radius: 5px; font-size: 12px; font-weight: 600; cursor: pointer;
  background: var(--lab-accent); color: var(--lab-accent-ink); border: 1px solid transparent;
}
.btn.ghost { background: none; border-color: var(--lab-panel-border); color: var(--p-text-color); }
table.tbl { width: 100%; border-collapse: collapse; font-size: 13px; }
table.tbl th {
  text-align: left; padding: 4px 8px;
  font-size: 11px; font-weight: 700; letter-spacing: 0.04em; text-transform: uppercase;
  color: var(--p-text-muted-color);
  border-bottom: 1px solid var(--lab-panel-border);
}
table.tbl td {
  padding: 4px 8px; vertical-align: top;
  border-bottom: 1px solid color-mix(in srgb, var(--lab-panel-border) 55%, transparent);
}
.mono { font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace; font-size: 11px; }
.id { display: block; font-size: 11px; color: var(--p-text-disabled-color); }
.bad { color: var(--err); }
.warn { color: var(--warn); }
/* .pill and its five tones are the shared theme's (unifi.css) — one chip,
   one definition, so this view and the Admin UI's registry panels can never
   drift apart on what `enabled` looks like. */
.grid-2 { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 18px; }
.lifecycle { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; font-size: 12px; }
.tag {
  display: inline-flex; align-items: center; padding: 1px 8px; border-radius: 100px;
  border: 1px solid var(--lab-panel-border); font-size: 11px; color: var(--p-text-muted-color);
}
.tag.gate { color: var(--p-text-color); border-color: var(--lab-accent); }
.sep:empty { display: none; }
.lifecycle-note { margin: 12px 0 0; font-size: 12px; color: var(--p-text-muted-color); }
.lifecycle-note b { color: var(--p-text-color); font-weight: 600; }
ul { margin: 4px 0 0; padding-left: 16px; color: var(--p-text-muted-color); }
</style>
