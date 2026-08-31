<script setup>
import Button from 'primevue/button'
import Message from 'primevue/message'
import { computed, onMounted, ref, watch } from 'vue'

import { getRegistryEntries, setRegistryEntryEnabled, upsertRegistryEntry } from '../api'
import { usePlatformConnection } from '../nats/usePlatformConnection.js'

// The curated frontend plugin registry (Phase 2, accounts-service `registry`
// module). Curation is service state, not a file: everything here is read from
// and written back to Postgres, keyed on a server-assigned monotonic revision.
//
// Two shapes of refusal are first-class UI, not error toasts:
//   · stale revision (BR-AS18) — the registry moved on; nothing is merged, and
//     the offer is a reload, not a force.
//   · origin not allowlisted (BR-AS20) — stated by stage and cause only, never
//     echoing the URL or the configured origins (BR-AS04).
// There is no removal: `active` has no exit transition, so disabling is the
// whole lifecycle (BR-AS24).

// The shell API this platform serves. An entry built against any other version
// is refused on metadata alone, before its remote is fetched (BR-AS13) — worth
// colouring in the table, because it is the one number that fails a plugin
// without anything having gone wrong at runtime.
const SHELL_API_VERSION = 1

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

// Contribution kinds, counted rather than listed: the useful thing at a glance
// is the *shape* of what an entry adds to the shell, not its ids.
const KIND_LABELS = {
  route: 'route',
  navigation: 'nav',
  extension: 'extension',
  'shell-control': 'control',
  'shell-footer': 'footer',
}

function contributionSummary(entry) {
  const counts = new Map()
  for (const c of entry.contributions || []) {
    const label = KIND_LABELS[c.kind] || c.kind
    counts.set(label, (counts.get(label) || 0) + 1)
  }
  return [...counts].map(([label, n]) => (n === 1 ? label : `${n} ${label}s`)).join(', ')
}

// One place decides how a row reports itself, because the four states are not
// the same kind of fact: `revoked` is a security event, `withheld` is a
// judgement the server made about the entry, `disabled` is a curation
// decision, `enabled` is neither.
function state(entry) {
  /* First, and under its own word. The panel already spends "withheld" on a
     non-conforming origin, and an operator reading one word for two unrelated
     causes would have no way to tell a narrowed allowlist from a revoked
     publisher key. Re-enabling is the only way back, one entry at a time
     (BR-AS38). */
  if (entry.withheld) {
    return { tone: 'bad', label: 'revoked', note: 'publisher key revoked; enable to restore' }
  }
  if (!entry.conforming) {
    return { tone: 'bad', label: 'withheld', note: 'stored, not served to any shell' }
  }
  if (entry.enabled) return { tone: 'ok', label: 'enabled', note: '' }
  return { tone: 'off', label: 'disabled', note: 'still running in shells until they reload' }
}

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
    if (e.conflict || e.status === 409) {
      stale.value = { yours: e.body?.yourRevision ?? revision.value, current: e.body?.currentRevision }
    } else if (e.code === 'origin-not-allowed' || e.status === 422) {
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
// A direct navigation can mount before the app's PLATFORM mint completes.
// Retry that read on establishment, and recover after later reconnects.
watch(usePlatformConnection().epoch, load)
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
      <div class="stat">
        <div class="k">Revision</div>
        <div class="v mono" data-testid="registry-revision">{{ revision }}</div>
        <div class="n">server-assigned, monotonic</div>
      </div>
      <div class="stat">
        <div class="k">Curated</div>
        <div class="v">{{ entries.length }} entries</div>
        <div class="n">
          <span class="ok">{{ enabledCount }} enabled</span> · {{ servedCount }} served to shells
        </div>
      </div>
      <div class="stat">
        <div class="k">Store</div>
        <div class="v ok">Postgres → KV</div>
        <div class="n">KV is written through on every accepted write</div>
      </div>
      <div class="stat">
        <div class="k">Origins</div>
        <div class="v">{{ origins.length }} allowlisted</div>
        <div class="n">service configuration · a write outside them is refused</div>
      </div>
    </div>

    <div class="lab-panel">
      <table v-if="!loading" class="tbl">
        <thead>
          <tr>
            <th style="width: 24%">Plugin</th>
            <th style="width: 8%">Version</th>
            <th style="width: 8%">Shell API</th>
            <th style="width: 14%">Route prefix</th>
            <th style="width: 18%">Contributions</th>
            <th style="width: 9%">Source</th>
            <th style="width: 15%">State</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="e in entries" :key="e.id" data-testid="entry-row">
            <td>
              <b>{{ e.name }}</b>
              <span class="id mono" data-testid="entry-id">{{ e.id }}</span>
            </td>
            <td class="mono">{{ e.version }}</td>
            <td class="mono" :class="{ warn: e.shellApiVersion !== SHELL_API_VERSION }">
              {{ e.shellApiVersion }}
            </td>
            <td class="mono">{{ e.routePrefix }}</td>
            <td :class="contributionSummary(e) ? 'lab-muted' : 'lab-dim'">
              {{ contributionSummary(e) || '— none —' }}
            </td>
            <td>
              <!-- Deliberately not a pill. The State column next to it is a
                   judgement that can change; this one is a fact about how the
                   row got here and never changes, and decision 80 asks for the
                   two to be told apart at a glance rather than read. -->
              <span class="tier mono" data-testid="entry-source">{{ e.source || 'unknown' }}</span>
            </td>
            <td>
              <span class="pill" :class="state(e).tone"><span class="pip"></span>{{ state(e).label }}</span>
              <span v-if="state(e).note" class="id">{{ state(e).note }}</span>
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

    <div class="grid-2">
      <div class="lab-panel" data-testid="origins-panel">
        <h3>Remote origins — service configuration, not registry data</h3>
        <table class="tbl">
          <tbody>
            <tr v-for="o in origins" :key="o">
              <td class="mono">{{ o }}</td>
            </tr>
            <tr v-if="!origins.length">
              <td class="lab-dim">— none configured —</td>
            </tr>
          </tbody>
        </table>
        <p class="lab-muted note">
          An entry marked <em class="bad">withheld</em> is stored but not served: its remote
          origin is not one this platform is configured to load code from. An entry pointing
          anywhere else is refused as it is written, so a compromised write cannot aim a shell
          at an arbitrary host. Widening this list is a deployment change with its own review —
          it cannot be done from this screen, by anyone.
        </p>
      </div>

      <div class="lab-panel" data-testid="write-effects-panel">
        <h3>What a write does</h3>
        <table class="tbl">
          <tbody>
            <tr>
              <td class="lab-muted" style="width: 46%">Entry added</td>
              <td>indexed live — metadata only, no remote fetched</td>
            </tr>
            <tr>
              <td class="lab-muted">Entry withdrawn, or its URL changed</td>
              <td class="warn">reload offered, never applied under the user</td>
            </tr>
            <tr>
              <td class="lab-muted">Plugin already running</td>
              <td>keeps rendering until that shell reloads</td>
            </tr>
            <tr>
              <td class="lab-muted">Store unavailable</td>
              <td class="ok">built-ins still served; the shell renders</td>
            </tr>
          </tbody>
        </table>
        <p class="lab-muted note">
          Every accepted write raises the revision and publishes on
          <span class="mono">notify._platform.registry.frontend-plugins.changed</span>.
        </p>
      </div>
    </div>

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
/* Four stats on one grid, matching the Phase 2 mockup's rhythm: a small
   uppercase key, the value, and a note that says what the value means. */
.stat-row {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 1.5rem;
}
.stat .k {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--p-text-disabled-color);
}
.stat .v {
  font-size: 15px;
  line-height: 22px;
  font-weight: 600;
  margin-top: 2px;
}
.stat .n {
  font-size: 11px;
  color: var(--p-text-muted-color);
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
/* A second line under a cell's own value — the id under a name, the caveat
   under a state. Dimmer than muted on purpose: it is context, not content. */
.tier {
  display: inline-block;
  font-size: 10px;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  padding: 1px 6px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 3px;
  color: var(--p-text-muted-color);
}
.id {
  display: block;
  font-size: 11px;
  color: var(--p-text-disabled-color);
}
.ok {
  color: var(--ok);
}
.warn {
  color: var(--warn);
}
.bad {
  color: var(--err);
  font-style: normal;
}
.note {
  margin: 0.625rem 0 0;
  font-size: 12px;
}
.row-actions {
  text-align: right;
  white-space: nowrap;
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
  border-left: 3px solid var(--err);
}
</style>
