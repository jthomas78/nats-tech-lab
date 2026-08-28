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
            <th style="width: 14%">Action</th>
            <th style="width: 20%">Entry</th>
            <th>Detail</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(r, i) in rows" :key="i" data-testid="audit-row">
            <td class="mono" data-testid="audit-revision">
              <template v-if="r.revision !== null && r.revision !== undefined">{{ r.revision }}</template>
              <span v-else class="lab-muted" title="a refused write consumes no revision">—</span>
            </td>
            <td class="lab-muted">{{ when(r.at) }}</td>
            <td data-testid="audit-actor">{{ r.actor }}</td>
            <td>
              <span class="pill" :class="r.outcome === 'accepted' ? 'ok' : 'bad'">{{ r.outcome }}</span>
              <span class="lab-muted op">{{ r.op }}</span>
            </td>
            <td class="mono" data-testid="audit-entry">{{ r.entryId }}</td>
            <td class="lab-muted">{{ r.detail }}</td>
          </tr>
        </tbody>
      </table>
      <p v-if="!loading && !rows.length" class="lab-muted">No writes recorded yet.</p>
    </div>

    <p class="lab-muted footnote" data-testid="audit-actor-note">
      Every write is made by the same shared <code>admin</code> credential the rest of this
      console uses — the actor column records that identity and claims nothing stronger.
      Refusals are kept alongside accepted writes: “what was curated, and when” is a question
      asked after an incident, and it has no answer if only successes are kept.
    </p>
  </div>
</template>

<style scoped>
.audit {
  display: flex;
  flex-direction: column;
  gap: 0.875rem;
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
}
.tbl td {
  padding: 0.4rem 0.5rem;
  border-top: 1px solid var(--p-content-border-color);
}
.pill {
  font-size: 11px;
  padding: 0.1rem 0.45rem;
  border-radius: 999px;
  border: 1px solid var(--p-content-border-color);
}
.op {
  margin-left: 0.4rem;
  font-size: 11px;
}
.footnote {
  margin: 0;
  max-width: 82ch;
}
</style>
