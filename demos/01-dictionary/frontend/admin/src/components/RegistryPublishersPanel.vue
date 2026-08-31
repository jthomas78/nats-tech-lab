<script setup>
import Button from 'primevue/button'
import Message from 'primevue/message'
import { computed, onMounted, ref, watch } from 'vue'

import {
  addPublisherKey,
  getRegistryPublishers,
  setPublisherKeyState,
  transferPlugin,
  upsertPublisher,
} from '../api'
import { usePlatformConnection } from '../nats/usePlatformConnection.js'

// Phase 7b — the publisher trust table. Two separations this panel exists to
// keep visible, because collapsing either is the easy mistake:
//
//   · A publisher is not its key (decision 103). One stable id holds many
//     keys, so the table is publishers with their keys nested, never a flat
//     key list. Rotation adds a successor and retires the old key.
//   · Retired is not revoked (BR-AS38). Retired signs nothing new and
//     everything it already signed stays admitted; revoked withdraws trust and
//     the entries it signed are withheld. They are rendered apart — different
//     colour, different note — because an operator acting on the wrong one
//     either breaks working plugins or leaves a leaked key trusted.
//
// Ownership is its own column and its own op (BR-AS46): a plugin id is
// transferred on purpose, and nothing else moves with it. There is no delete
// anywhere here — a trust anchor that can be silently emptied is not one.

const revision = ref(0)
const publishers = ref([])
const loading = ref(true)
const loadError = ref('')
const busyKey = ref('')

// A stale refusal parks here until it is acknowledged by reloading.
const stale = ref(null)
// A refusal that belongs to the open form, not to the page.
const formRefusal = ref('')

// Which form is open, if any: 'publisher' | 'key' | 'transfer'.
const form = ref(null)
const draft = ref({ id: '', name: '', publisherId: '', publicKey: '', pluginId: '' })

const keyCount = computed(() => publishers.value.reduce((n, p) => n + (p.keys || []).length, 0))
const revokedCount = computed(
  () => publishers.value.reduce((n, p) => n + (p.keys || []).filter((k) => k.state === 'revoked').length, 0),
)
const ownedCount = computed(() => publishers.value.reduce((n, p) => n + (p.plugins || []).length, 0))

// One place decides how a key reports itself, because the three states differ
// in kind, not in degree.
const KEY_STATES = {
  enabled: { tone: 'ok', label: 'enabled', note: 'may sign new announcements' },
  retired: { tone: 'off', label: 'retired', note: 'signs nothing new · what it signed stays valid' },
  revoked: { tone: 'bad', label: 'revoked', note: 'trust withdrawn · its entries are withheld' },
}

function keyState(key) {
  return KEY_STATES[key.state] || { tone: 'off', label: key.state, note: '' }
}

// A key is long and its middle carries no meaning to a reader; the head and
// tail are what an operator compares against what they were sent.
function shortKey(publicKey) {
  return publicKey.length > 20 ? `${publicKey.slice(0, 10)}…${publicKey.slice(-6)}` : publicKey
}

function when(at) {
  if (!at) return ''
  const d = new Date(at)
  return Number.isNaN(d.getTime()) ? at : d.toLocaleString()
}

function apply(doc) {
  revision.value = doc.revision
  publishers.value = doc.publishers || []
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    apply(await getRegistryPublishers())
  } catch (e) {
    loadError.value = e.message
  } finally {
    loading.value = false
  }
}

// Every write goes through here so a stale refusal is handled in exactly one
// place, whatever triggered the write.
async function write(fn) {
  try {
    apply(await fn())
    return true
  } catch (e) {
    if (e.conflict || e.status === 409) {
      stale.value = { yours: e.body?.yourRevision ?? revision.value, current: e.body?.currentRevision }
    } else if (form.value) {
      formRefusal.value = e.message
    } else {
      loadError.value = e.message
    }
    return false
  }
}

async function moveKey(publisher, key, state) {
  busyKey.value = key.publicKey
  await write(() => setPublisherKeyState(publisher.id, key.publicKey, state, revision.value))
  busyKey.value = ''
}

async function reloadFromStale() {
  stale.value = null
  await load()
}

function openForm(which, publisherId = '') {
  formRefusal.value = ''
  draft.value = { id: '', name: '', publisherId, publicKey: '', pluginId: '' }
  form.value = which
}

function closeForm() {
  form.value = null
  formRefusal.value = ''
}

async function submitForm() {
  formRefusal.value = ''
  const d = draft.value
  let ok = false
  if (form.value === 'publisher') {
    ok = await write(() => upsertPublisher({ id: d.id.trim(), name: d.name.trim() }, revision.value))
  } else if (form.value === 'key') {
    ok = await write(() => addPublisherKey(d.publisherId, d.publicKey.trim(), revision.value))
  } else if (form.value === 'transfer') {
    ok = await write(() => transferPlugin(d.publisherId, d.pluginId.trim(), revision.value))
  }
  if (ok) closeForm()
}

onMounted(load)
// A direct navigation can mount before the app's PLATFORM mint completes.
// Retry that read on establishment, and recover after later reconnects.
watch(usePlatformConnection().epoch, load)
</script>

<template>
  <div class="publishers">
    <Message
      v-if="loadError"
      severity="error"
      :closable="false"
    >
      {{ loadError }}
    </Message>

    <div
      v-if="stale"
      class="lab-panel stale"
      data-testid="publisher-stale"
    >
      <h3>The trust table moved on while you were editing</h3>
      <p class="lab-muted">
        You were writing against revision <strong>{{ stale.yours }}</strong>. The table is on
        <strong>{{ stale.current }}</strong>. Nothing was written, and the two sets of changes
        are not merged for you — who is trusted to sign is not something a server should guess
        at.
      </p>
      <Button
        label="Reload on the current revision"
        size="small"
        data-testid="publisher-stale-reload"
        @click="reloadFromStale"
      />
    </div>

    <div class="lab-panel stat-row">
      <div class="stat">
        <div class="k">
          Revision
        </div>
        <div
          class="v mono"
          data-testid="publisher-revision"
        >
          {{ revision }}
        </div>
        <div class="n">
          the table's own counter, not the plugin document's
        </div>
      </div>
      <div class="stat">
        <div class="k">
          Publishers
        </div>
        <div class="v">
          {{ publishers.length }}
        </div>
        <div class="n">
          {{ keyCount }} keys held between them
        </div>
      </div>
      <div class="stat">
        <div class="k">
          Revoked
        </div>
        <div
          class="v"
          :class="revokedCount ? 'bad' : ''"
          data-testid="publisher-revoked-count"
        >
          {{ revokedCount }} keys
        </div>
        <div class="n">
          trust withdrawn · counted apart from retired
        </div>
      </div>
      <div class="stat">
        <div class="k">
          Ownership
        </div>
        <div class="v">
          {{ ownedCount }} plugin ids
        </div>
        <div class="n">
          stated here, never derived from an origin
        </div>
      </div>
    </div>

    <div class="lab-panel">
      <div class="head">
        <h3>Trusted publishers</h3>
        <Button
          label="Add publisher"
          size="small"
          data-testid="add-publisher"
          @click="openForm('publisher')"
        />
      </div>
      <table
        v-if="!loading"
        class="tbl"
      >
        <thead>
          <tr>
            <th style="width: 22%">
              Publisher
            </th>
            <th style="width: 30%">
              Signing key
            </th>
            <th style="width: 18%">
              State
            </th>
            <th style="width: 14%">
              Added
            </th>
            <th />
          </tr>
        </thead>
        <tbody>
          <template
            v-for="p in publishers"
            :key="p.id"
          >
            <tr data-testid="publisher-row">
              <td
                colspan="5"
                class="owner"
              >
                <span class="name">{{ p.name || p.id }}</span>
                <span
                  class="id mono"
                  data-testid="publisher-id"
                >{{ p.id }}</span>
                <span class="owns">
                  <template v-if="p.plugins && p.plugins.length">
                    owns
                    <span
                      v-for="id in p.plugins"
                      :key="id"
                      class="mono chip"
                      data-testid="publisher-plugin"
                    >{{ id }}</span>
                  </template>
                  <span
                    v-else
                    class="lab-dim"
                  >owns no plugin ids yet</span>
                </span>
                <span class="owner-actions">
                  <Button
                    label="Add key"
                    size="small"
                    text
                    @click="openForm('key', p.id)"
                  />
                  <Button
                    label="Transfer plugin"
                    size="small"
                    text
                    @click="openForm('transfer', p.id)"
                  />
                </span>
              </td>
            </tr>
            <tr
              v-for="k in p.keys || []"
              :key="k.publicKey"
              data-testid="publisher-key-row"
            >
              <td />
              <td
                class="mono"
                :title="k.publicKey"
                data-testid="publisher-key"
              >
                {{ shortKey(k.publicKey) }}
              </td>
              <td>
                <span
                  class="pill"
                  :class="keyState(k).tone"
                  :data-testid="`key-state-${k.state}`"
                >
                  <span class="pip" />{{ keyState(k).label }}
                </span>
                <span class="id">{{ keyState(k).note }}</span>
              </td>
              <td class="lab-muted">
                {{ when(k.addedAt) }}
              </td>
              <td class="row-actions">
                <Button
                  v-if="k.state !== 'retired' && k.state !== 'revoked'"
                  label="Retire"
                  size="small"
                  text
                  :disabled="busyKey === k.publicKey"
                  :data-testid="`retire-${k.publicKey}`"
                  @click="moveKey(p, k, 'retired')"
                />
                <Button
                  v-if="k.state !== 'revoked'"
                  label="Revoke"
                  size="small"
                  text
                  severity="danger"
                  :disabled="busyKey === k.publicKey"
                  :data-testid="`revoke-${k.publicKey}`"
                  @click="moveKey(p, k, 'revoked')"
                />
                <Button
                  v-else
                  label="Restore to enabled"
                  size="small"
                  text
                  :disabled="busyKey === k.publicKey"
                  :data-testid="`restore-${k.publicKey}`"
                  @click="moveKey(p, k, 'enabled')"
                />
              </td>
            </tr>
            <tr v-if="!(p.keys || []).length">
              <td />
              <td
                colspan="4"
                class="lab-dim"
              >
                holds no keys · it can own plugin ids but sign nothing
              </td>
            </tr>
          </template>
        </tbody>
      </table>
      <p
        v-if="!loading && !publishers.length"
        class="lab-muted"
      >
        No publishers are trusted yet. Until one is, nothing signed is admitted.
      </p>
    </div>

    <div class="grid-2">
      <div
        class="lab-panel"
        data-testid="publisher-rotation-note"
      >
        <h3>Retiring is not revoking</h3>
        <p class="lab-muted note">
          Rotation adds the successor key and retires the old one: the old key signs nothing
          new, and every entry it already signed stays admitted. Revocation is the other
          answer — trust is withdrawn, and the entries that key signed are re-evaluated and
          withheld. Restoring a revoked key back to enabled lets it sign again; it does not
          bring back what was withheld while it was revoked.
        </p>
      </div>
      <div
        class="lab-panel"
        data-testid="publisher-ownership-note"
      >
        <h3>A key proves who signed, not what they may speak for</h3>
        <p class="lab-muted note">
          Ownership is this table's own column, not something inferred from where an artifact
          was served. Two teams deploying behind one host is the normal case, and
          origin-derived ownership would let either sign for the other. So a plugin id moves
          by its own transfer, and nothing else moves with it.
        </p>
      </div>
    </div>

    <div
      v-if="form"
      class="scrim"
      @click.self="closeForm"
    >
      <div
        class="lab-panel drawer"
        data-testid="publisher-form"
      >
        <h3 v-if="form === 'publisher'">
          Add a publisher
        </h3>
        <h3 v-else-if="form === 'key'">
          Add a signing key
        </h3>
        <h3 v-else>
          Transfer a plugin id
        </h3>

        <Message
          v-if="formRefusal"
          severity="error"
          :closable="false"
          data-testid="publisher-form-refused"
        >
          {{ formRefusal }}
        </Message>

        <template v-if="form === 'publisher'">
          <label class="field">
            <span class="lbl">Publisher id</span>
            <input
              v-model="draft.id"
              class="inp mono"
              data-testid="field-publisher-id"
            >
          </label>
          <label class="field">
            <span class="lbl">Name</span>
            <input
              v-model="draft.name"
              class="inp"
              data-testid="field-publisher-name"
            >
          </label>
        </template>

        <template v-else-if="form === 'key'">
          <label class="field">
            <span class="lbl">Public key</span>
            <input
              v-model="draft.publicKey"
              class="inp mono"
              data-testid="field-public-key"
            >
            <span class="id">
              An NKey public key. Minted outside the NATS trust chain, so a leaked signing key
              cannot connect as anything.
            </span>
          </label>
        </template>

        <template v-else>
          <label class="field">
            <span class="lbl">Plugin id</span>
            <input
              v-model="draft.pluginId"
              class="inp mono"
              data-testid="field-plugin-id"
            >
            <span class="id">Moves ownership to this publisher. Keys stay with whoever holds them.</span>
          </label>
        </template>

        <div class="drawer-foot">
          <span class="lab-muted">Writing against revision {{ revision }}</span>
          <span>
            <Button
              label="Cancel"
              size="small"
              text
              @click="closeForm"
            />
            <Button
              label="Save"
              size="small"
              data-testid="publisher-form-save"
              @click="submitForm"
            />
          </span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.publishers {
  display: flex;
  flex-direction: column;
  gap: 0.875rem;
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 0.5rem;
}
.head h3 {
  margin: 0;
}
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
/* The publisher is a band across the table and its keys are the rows beneath
   it: the nesting is the point — a flat key list would read as if the key
   were the identity. */
.owner {
  display: flex;
  align-items: baseline;
  gap: 0.5rem;
  flex-wrap: wrap;
  background: var(--p-content-hover-background, transparent);
}
.owner .name {
  font-weight: 600;
}
.owner-actions {
  margin-left: auto;
}
.owns {
  font-size: 11px;
  color: var(--p-text-muted-color);
}
.chip {
  margin-left: 0.3rem;
  padding: 1px 5px;
  border-radius: 3px;
  border: 1px solid var(--p-content-border-color);
  font-size: 11px;
}
.id {
  display: block;
  font-size: 11px;
  color: var(--p-text-disabled-color);
}
.bad {
  color: var(--err);
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
