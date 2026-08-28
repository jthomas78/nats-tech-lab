<script setup>
/*
  The plugin inventory — one row per plugin with its status and the reason for
  it, plus the contribution-level refusals a plugin-level status cannot express
  (a plugin can be `available` and still have lost one footer item to a full
  region).

  This screen is what makes the whole design inspectable during the BR-AS15
  review: every failure mode the phase demonstrates shows up here as a status
  and a cause code, without opening a console.
*/
import { computed, inject } from 'vue'

import { SHELL } from '../shell/shellKey.js'

const shell = inject(SHELL)
const rows = computed(() => shell.inventory)

const TONE = {
  active: 'ok',
  available: 'ok',
  loading: 'busy',
  discovered: 'busy',
  disabled: 'muted',
  incompatible: 'warn',
  failed: 'err',
}
</script>

<template>
  <section class="plugins">
    <header>
      <h1>Plugins</h1>
      <p
        v-if="shell.registryError"
        class="degraded"
      >
        The plugin registry could not be read ({{ shell.registryError.code }}).
        Built-in plugins are listed; remote plugins are not.
      </p>
      <p v-else>
        Everything the shell discovered this session, and what became of it.
      </p>
    </header>

    <table>
      <thead>
        <tr><th>Plugin</th><th>Status</th><th>Cause</th><th>Refused contributions</th></tr>
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
          <td>
            <span
              class="pill"
              :class="TONE[row.status]"
            >{{ row.status }}</span>
          </td>
          <td>
            <span
              v-if="row.reasonCode"
              class="code"
            >{{ row.reasonCode }}</span>
            <span
              v-else
              class="none"
            >—</span>
          </td>
          <td>
            <span
              v-if="row.refusals.length === 0"
              class="none"
            >—</span>
            <ul v-else>
              <li
                v-for="refusal in row.refusals"
                :key="refusal.qualifiedId"
              >
                <span class="code">{{ refusal.code }}</span> {{ refusal.qualifiedId }}
              </li>
            </ul>
          </td>
        </tr>
      </tbody>
    </table>
  </section>
</template>

<style scoped>
.plugins { display: flex; flex-direction: column; gap: 18px; }
h1 { font-size: 20px; margin: 0 0 6px; }
header p { margin: 0; color: var(--p-text-muted-color); }
.degraded { color: var(--warn); }
table { width: 100%; border-collapse: collapse; }
th {
  text-align: left; padding: 6px 10px;
  font-size: 10px; font-weight: 700; letter-spacing: 0.08em; text-transform: uppercase;
  color: var(--p-text-disabled-color);
  border-bottom: 1px solid var(--lab-panel-border);
}
td { padding: 10px; border-bottom: 1px solid var(--lab-panel-border); vertical-align: top; }
.id { display: block; font-size: 11px; color: var(--p-text-disabled-color); }
.pill {
  display: inline-flex; padding: 1px 9px; border-radius: 100px;
  border: 1px solid var(--lab-panel-border); font-size: 11px; font-weight: 600;
}
.pill.ok { color: var(--ok); }
.pill.busy { color: var(--lab-accent); }
.pill.warn { color: var(--warn); }
.pill.err { color: var(--err); }
.pill.muted { color: var(--p-text-disabled-color); }
.code { font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace; font-size: 11px; }
.none { color: var(--p-text-disabled-color); }
ul { margin: 0; padding-left: 16px; }
li { color: var(--p-text-muted-color); }
</style>
