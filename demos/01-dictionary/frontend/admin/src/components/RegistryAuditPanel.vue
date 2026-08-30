<script setup>
import Message from 'primevue/message'
import { onMounted, ref } from 'vue'

import { getRegistryAudit } from '../api'

// Every write the registry accepted — and every write it refused — in the
// order the server recorded them (BR-AS23). A refusal consumes no revision,
// so its revision cell is deliberately empty: the number counts documents
// that have existed, and a refused write produced none.

const rows = ref([])
const loading = ref(true)
const loadError = ref('')

// How a row reports itself. The op is the platform's own vocabulary
// (`upsert`, `set-enabled`) and is shown as written rather than prettified —
// but the three outcomes are different in kind, so they are coloured apart:
// a refusal is a failure, an upsert introduced or changed an entry, and a
// set-enabled only moved a switch on one that was already curated.
function action(row) {
  if (row.outcome !== 'accepted') return { tone: 'bad', label: 'refused' }
  if (row.op === 'upsert') return { tone: 'busy', label: 'written' }
  return { tone: 'ok', label: 'curated' }
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    rows.value = await getRegistryAudit(200)
  } catch (e) {
    loadError.value = e.message
  } finally {
    loading.value = false
  }
}

function when(at) {
  if (!at) return ''
  const d = new Date(at)
  return Number.isNaN(d.getTime()) ? at : d.toLocaleString()
}

onMounted(load)
</script>

<template>
  <div class="audit">
    <Message v-if="loadError" severity="error" :closable="false">{{ loadError }}</Message>

    <div class="lab-panel">
      <table v-if="!loading" class="tbl">
        <thead>
          <tr>
            <th style="width: 6%">Rev</th>
            <th style="width: 18%">When</th>
            <th style="width: 12%">Actor</th>
            <th style="width: 16%">Action</th>
            <th style="width: 20%">Entry</th>
            <th>Detail</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(r, i) in rows" :key="i" data-testid="audit-row">
            <td class="mono" data-testid="audit-revision">
              <template v-if="r.revision !== null && r.revision !== undefined">{{ r.revision }}</template>
              <span v-else class="lab-dim" title="a refused write consumes no revision">—</span>
            </td>
            <td class="lab-muted">{{ when(r.at) }}</td>
            <td data-testid="audit-actor">{{ r.actor }}</td>
            <td>
              <span class="pill" :class="action(r).tone"><span class="pip"></span>{{ action(r).label }}</span>
              <span class="id mono">{{ r.op }}</span>
            </td>
            <td class="mono" data-testid="audit-entry">{{ r.entryId }}</td>
            <td :class="r.outcome === 'accepted' ? 'lab-muted' : 'bad'">
              {{ r.detail }}
              <span v-if="r.outcome !== 'accepted'" class="lab-muted">— no revision assigned</span>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-if="!loading && !rows.length" class="lab-muted">No writes recorded yet.</p>
    </div>

    <div class="grid-2">
      <div class="lab-panel" data-testid="audit-actor-note">
        <h3>Refusals are recorded, not just rejected</h3>
        <p class="lab-muted note">
          A refused write consumes no revision — the number would then lie about how many
          documents have existed — but it is still written here, with its cause. Every write is
          made by the same shared <span class="mono">admin</span> credential the rest of this
          console uses: the actor column records that identity and claims nothing stronger.
          “What was curated, and when” is a question asked after an incident, and it has no
          answer if only successes are kept.
        </p>
      </div>
      <div class="lab-panel" data-testid="audit-sourcing-note">
        <h3>The log is the event-sourced part</h3>
        <p class="lab-muted note">
          The plugin list itself is plain CRUD — only its current state is ever read, so
          nothing replays it. The write history is the piece something genuinely replays,
          which is the distinction this repo draws everywhere else between an aggregate and a
          lookup table.
        </p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.audit {
  display: flex;
  flex-direction: column;
  gap: 0.875rem;
}
.grid-2 {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.875rem;
  align-items: start;
}
.tbl {
  width: 100%;
  border-collapse: collapse;
}
.tbl th {
  text-align: left;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  padding: 0 0.5rem 0.4rem;
  color: var(--p-text-muted-color);
}
.tbl td {
  padding: 0.4rem 0.5rem;
  border-top: 1px solid var(--p-content-border-color);
  vertical-align: top;
}
/* The op under its outcome: same second-line treatment the registry table
   gives an id under a name. */
.id {
  display: block;
  font-size: 11px;
  color: var(--p-text-disabled-color);
}
.bad {
  color: var(--err);
}
.note {
  margin: 0;
  font-size: 12px;
}
</style>
