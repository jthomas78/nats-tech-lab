<script setup>
import Button from 'primevue/button'
import Message from 'primevue/message'
import { computed, onMounted, ref } from 'vue'

import { getRegistryEntries, setRegistryEntryEnabled, upsertRegistryEntry } from '../api'

// The curated frontend plugin registry (Phase 2, accounts-service `registry`
// module). Curation is service state, not a file: everything here is read from
// and written back to Postgres, keyed on a server-assigned monotonic revision.
//
// Two shapes of refusal are first-class UI, not error toasts:
//   · stale revision (BR-AS18) — the registry moved on; nothing is merged, and
//     the offer is a reload, not a force.
//   · origin not allowlisted (BR-AS20) — stated by stage and cause only, never
//     echoing the URL or the configured origins (BR-AS04).
// There is no delete: `active` has no exit transition, so disabling is the
// whole lifecycle (BR-AS24).

const revision = ref(0)
const origins = ref([])
const entries = ref([])
const loading = ref(true)
const loadError = ref('')
const busyId = ref('')

// A stale refusal parks here until it is acknowledged by reloading.
const stale = ref(null)
// An origin refusal belongs to the open drawer.
const originRefusal = ref('')

const draft = ref(null)

const enabledCount = computed(() => entries.value.filter((e) => e.enabled).length)
const servedCount = computed(() => entries.value.filter((e) => e.enabled && e.conforming).length)

function apply(doc) {
  revision.value = doc.revision
  origins.value = doc.allowedOrigins || []
  entries.value = doc.plugins || []
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    apply(await getRegistryEntries())
  } catch (e) {
    loadError.value = e.message
  } finally {
    loading.value = false
  }
}

// Every write goes through here so the two refusals are handled in exactly
// one place, whatever triggered the write.
async function write(fn) {
  try {
    apply(await fn())
    return true
  } catch (e) {
    if (e.status === 409) {
      stale.value = { yours: e.body?.yourRevision ?? revision.value, current: e.body?.currentRevision }
    } else if (e.status === 422) {
      originRefusal.value = e.message
    } else {
      loadError.value = e.message
    }
    return false
  }
}

async function toggle(entry) {
  busyId.value = entry.id
  await write(() => setRegistryEntryEnabled(entry.id, !entry.enabled, revision.value))
  busyId.value = ''
}

async function reloadFromStale() {
  stale.value = null
  await load()
}

function edit(entry) {
  originRefusal.value = ''
  draft.value = JSON.parse(JSON.stringify(entry))
}

function closeDrawer() {
  draft.value = null
  originRefusal.value = ''
}

async function saveDraft() {
  originRefusal.value = ''
  // `conforming` is a read-side judgement the server computes; it is not part
  // of the entry and is never written back.
  const { conforming, ...entry } = draft.value
  if (await write(() => upsertRegistryEntry(entry, revision.value))) closeDrawer()
}

onMounted(load)
</script>

<template>
  <div class="registry">
    <Message v-if="loadError" severity="error" :closable="false">{{ loadError }}</Message>

    <div v-if="stale" class="lab-panel stale" data-testid="stale-revision">
      <h3>The registry moved on while you were editing</h3>
      <p class="lab-muted">
        You were writing against revision <strong>{{ stale.yours }}</strong>. The registry
        is on <strong>{{ stale.current }}</strong>. Nothing was written, and the two sets of
        changes are not merged for you — two curation decisions are not something a server
        should guess at.
      </p>
      <Button
        label="Reload on the current revision"
        size="small"
        data-testid="stale-reload"
        @click="reloadFromStale"
      />
    </div>

    <div class="lab-panel stat-row">
      <div>
        <span class="lab-muted">Revision</span>
        <strong data-testid="registry-revision">{{ revision }}</strong>
        <span class="lab-muted">server-assigned, monotonic</span>
      </div>
      <div>
        <span class="lab-muted">Curated</span>
        <strong>{{ entries.length }}</strong>
        <span class="lab-muted">{{ enabledCount }} enabled · {{ servedCount }} served to shells</span>
      </div>
      <div>
        <span class="lab-muted">Origins</span>
        <strong>{{ origins.length }}</strong>
        <span class="lab-muted">service configuration, not registry data</span>
      </div>
    </div>

    <div class="lab-panel">
      <table v-if="!loading" class="tbl">
        <thead>
          <tr>
            <th>Plugin</th>
            <th>Version</th>
            <th>Shell API</th>
            <th>Route prefix</th>
            <th>Contributions</th>
            <th>State</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="e in entries" :key="e.id" data-testid="entry-row">
            <td>
              {{ e.name }}
              <div class="mono lab-muted" data-testid="entry-id">{{ e.id }}</div>
            </td>
            <td class="mono">{{ e.version }}</td>
            <td class="mono">{{ e.shellApiVersion }}</td>
            <td class="mono">{{ e.routePrefix }}</td>
            <td class="lab-muted">{{ (e.contributions || []).length }}</td>
            <td>
              <span v-if="!e.conforming" class="pill bad">withheld</span>
              <span v-else-if="e.enabled" class="pill ok">enabled</span>
              <span v-else class="pill off">disabled</span>
            </td>
            <td class="row-actions">
              <Button
                :label="e.enabled ? 'Disable' : 'Enable'"
                size="small"
                severity="secondary"
                text
                :loading="busyId === e.id"
                data-testid="toggle-enabled"
                @click="toggle(e)"
              />
              <Button
                label="Edit"
                size="small"
                severity="secondary"
                text
                data-testid="edit-entry"
                @click="edit(e)"
              />
            </td>
          </tr>
        </tbody>
      </table>
      <p v-if="!loading && !entries.length" class="lab-muted">Nothing is curated yet.</p>
    </div>

    <p class="lab-muted footnote">
      An entry marked <em>withheld</em> is stored but not served: its remote origin is not
      one this platform is configured to load code from. Widening that list is a deployment
      change with its own review — it cannot be done from this screen, by anyone.
    </p>

    <div v-if="draft" class="scrim" @click.self="closeDrawer">
      <aside class="drawer lab-panel">
        <h3>{{ draft.name }}</h3>

        <Message v-if="originRefusal" severity="error" :closable="false" data-testid="origin-refused">
          {{ originRefusal }} Nothing was stored, and no shell was ever offered this remote.
        </Message>

        <label class="field">
          <span class="lbl">Plugin id <span class="lab-muted">— immutable</span></span>
          <input class="inp mono" :value="draft.id" disabled />
        </label>
        <label class="field">
          <span class="lbl">Route prefix <span class="lab-muted">— immutable</span></span>
          <input class="inp mono" :value="draft.routePrefix" disabled />
        </label>
        <label class="field">
          <span class="lbl">Display name</span>
          <input v-model="draft.name" class="inp" data-testid="entry-name" />
        </label>
        <label class="field">
          <span class="lbl">Version</span>
          <input v-model="draft.version" class="inp mono" data-testid="entry-version" />
        </label>
        <label class="field">
          <span class="lbl">Exposed module</span>
          <input v-model="draft.remote.module" class="inp mono" data-testid="entry-module" />
        </label>
        <label class="field">
          <span class="lbl">Entry URL</span>
          <input v-model="draft.remote.url" class="inp mono" data-testid="entry-url" />
        </label>

        <p class="lab-muted">
          Contributions come from the plugin's own manifest and are never edited here —
          curation decides whether a plugin is served, not what it does.
        </p>

        <div class="drawer-foot">
          <span class="lab-muted">Writing against revision {{ revision }}</span>
          <div>
            <Button label="Cancel" size="small" severity="secondary" text @click="closeDrawer" />
            <Button label="Save entry" size="small" data-testid="entry-save" @click="saveDraft" />
          </div>
        </div>
      </aside>
    </div>
  </div>
</template>

<style scoped>
.registry {
  display: flex;
  flex-direction: column;
  gap: 0.875rem;
}
.stat-row {
  display: flex;
  gap: 2.5rem;
}
.stat-row > div {
  display: flex;
  flex-direction: column;
  gap: 0.15rem;
}
.stat-row strong {
  font-size: 20px;
  line-height: 26px;
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
  vertical-align: top;
}
.row-actions {
  text-align: right;
  white-space: nowrap;
}
.pill {
  font-size: 11px;
  padding: 0.1rem 0.45rem;
  border-radius: 999px;
  border: 1px solid var(--p-content-border-color);
}
.footnote {
  margin: 0;
  max-width: 78ch;
}
.scrim {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  justify-content: flex-end;
  z-index: 20;
}
.drawer {
  width: 460px;
  height: 100%;
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: 0.7rem;
  border-radius: 0;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}
.lbl {
  font-size: 12px;
  font-weight: 600;
}
.inp {
  padding: 0.35rem 0.5rem;
  border-radius: 4px;
  border: 1px solid var(--p-content-border-color);
  background: var(--p-content-background);
  color: inherit;
}
.drawer-foot {
  margin-top: auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}
.stale {
  border-left: 3px solid var(--p-red-500, #d9534f);
}
</style>
