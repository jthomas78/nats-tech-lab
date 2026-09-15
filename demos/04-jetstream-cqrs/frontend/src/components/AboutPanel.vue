<script setup>
// The first rail entry: what this demo is, and the drawings of it.
//
// Both halves are files that already exist in the repo, shown as they are:
//
//   Notes     ../../../README.md                      — the lab shell's intro
//   Diagrams  ../../../diagrams/demo04-jetstream-cqrs.html — the solution page
//
// Nothing is retyped and nothing is redrawn. If a rule changes, the README and
// the diagram page change, and this screen changes with them. A second copy of
// the explanation here would be a second thing to keep true.
//
// The diagram page is a whole HTML document, so it goes in a frame rather than
// being pasted into this one — its own <style> block would otherwise leak into
// the app. `srcdoc` means the file is compiled in, not fetched. The frame is
// given no scripts; it is only allowed to be same-origin so this component can
// read its height and grow to fit, which avoids a scrollbar inside a scrollbar.
//
// The two views are a real PrimeVue `Tabs` carrying `class="panel-tabs"`, which
// is the repo's one style for a top tab strip (shared/unifi-theme/LAYOUT.md,
// "Panel top tabs"). A custom chip/pill toggle is explicitly not allowed for
// this role — chips are for filters. The strip therefore sits FLUSH on the
// page and the card treatment lives on each tab's content, not around the
// tabs: wrapping the strip in a card puts the tablist on a background it does
// not expect and costs a pile of compensating overrides.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'

import readme from '../../../README.md?raw'
import diagramPage from '../../../diagrams/demo04-jetstream-cqrs.html?raw'
import blocksPng from '../../../diagrams/cqrs-blocks.png'
import { renderNotes } from '../about/notes.js'

const TABS = [
  { key: 'notes', label: 'What it does', file: 'README.md' },
  { key: 'diagrams', label: 'Classes and sequences', file: 'diagrams/demo04-jetstream-cqrs.html' },
]

const tab = ref('notes')
const current = computed(() => TABS.find((t) => t.key === tab.value))

const notes = computed(() => renderNotes(readme, { 'diagrams/cqrs-blocks.png': blocksPng }))

const frame = ref(null)
const frameHeight = ref(800)

// The frame is sized to its content, so the page has one scrollbar instead of
// two. A guess would either crop the last diagram or leave a hole under it.
function measure() {
  const doc = frame.value?.contentDocument
  if (!doc) return
  frameHeight.value = Math.max(
    doc.documentElement?.scrollHeight ?? 0,
    doc.body?.scrollHeight ?? 0,
    400,
  )
}

// The drawings are SVGs at 100% width, so they reflow when the window changes
// and the height they need changes with them.
onMounted(() => window.addEventListener('resize', measure))
onBeforeUnmount(() => window.removeEventListener('resize', measure))
</script>

<template>
  <section
    class="about"
    data-testid="about-panel"
  >
    <header>
      <p class="eyebrow">
        Demo 04 · JetStream as an event source, with CQRS
      </p>
      <code class="src">{{ current.file }}</code>
    </header>

    <p class="lead">
      One log is the only source of truth. The write side reads it to check a
      rule. The read side folds it into an answer. Both are below — first in
      words, then drawn.
    </p>

    <Tabs
      v-model:value="tab"
      class="panel-tabs"
    >
      <TabList>
        <Tab
          v-for="t in TABS"
          :key="t.key"
          :value="t.key"
          :data-testid="`about-tab-${t.key}`"
        >
          {{ t.label }}
        </Tab>
      </TabList>
      <TabPanels>
        <TabPanel value="notes">
          <!-- eslint-disable vue/no-v-html -- our own README, compiled in at build -->
          <article
            class="card notes"
            data-testid="about-notes"
            v-html="notes"
          />
          <!-- eslint-enable vue/no-v-html -->
        </TabPanel>
        <TabPanel value="diagrams">
          <!-- v-if, not a hidden panel. PrimeVue renders every TabPanel, and a
               hidden iframe has no layout to measure — the frame would size
               itself to nothing and the drawings would be cropped. Mounting it
               only while its tab is open makes @load fire with real geometry.
               AccountsView.vue uses the same v-if for the same reason. -->
          <div
            v-if="tab === 'diagrams'"
            class="card frame"
            data-testid="about-diagrams"
          >
            <iframe
              ref="frame"
              title="Class and sequence diagrams for demo 04"
              sandbox="allow-same-origin"
              :srcdoc="diagramPage"
              :style="{ height: `${frameHeight}px` }"
              @load="measure"
            />
          </div>
        </TabPanel>
      </TabPanels>
    </Tabs>
  </section>
</template>

<style scoped>
/* No card here. The tab strip sits flush on the page and each tab's content
   carries the card instead (LAYOUT.md, "Placement"). */
.about {
  margin-top: 20px;
}

/* The same card every other panel on this screen draws — BucketPanel, LagLane,
   EventLog. It sits on the tab CONTENT so the two views look like the panels
   they sit beside, and so the tablist's hairline stays on the page. */
.card {
  padding: 14px 16px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
  background: var(--lab-panel-bg);
  border-top: 2px solid var(--p-primary-color);
}

header {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.eyebrow {
  margin: 0;
}

.src {
  margin-left: auto;
  color: var(--p-text-disabled-color);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
}

.lead {
  max-width: 84ch;
  margin: 12px 0 0;
  color: var(--p-text-muted-color);
}

/* Everything about the strip itself — the hairline, the active underline, the
   type — comes from `.panel-tabs` in shared/unifi-theme/unifi.css. Nothing is
   restyled here; that is the point of there being one tab style in the repo.
   Only the gap above it is this component's business. */
.panel-tabs {
  margin-top: 14px;
}

/* The README is prose, so it gets a reading measure rather than the full
   panel width. The diagrams are the opposite — they want everything. */
.notes {
  max-width: 84ch;
}

.notes :deep(> :first-child) {
  margin-top: 0;
}

.notes :deep(h1) {
  margin: 0 0 6px;
  font-size: 20px;
}

.notes :deep(h2) {
  margin: 28px 0 8px;
  padding-top: 14px;
  border-top: 1px solid var(--lab-panel-border);
  font-size: 15px;
}

.notes :deep(h3) {
  margin: 18px 0 6px;
  font-size: 13px;
}

.notes :deep(p),
.notes :deep(li) {
  color: var(--p-text-muted-color);
}

.notes :deep(blockquote) {
  margin: 12px 0;
  padding: 2px 0 2px 14px;
  border-left: 2px solid var(--p-primary-color);
  color: var(--p-text-color);
}

.notes :deep(code) {
  padding: 1px 5px;
  border-radius: 3px;
  background: color-mix(in srgb, var(--lab-panel-border) 55%, transparent);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 12px;
}

.notes :deep(pre) {
  overflow-x: auto;
  margin: 10px 0;
  padding: 10px 12px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
  background: var(--p-content-background);
}

.notes :deep(pre code) {
  padding: 0;
  background: none;
  font-size: 11px;
  line-height: 17px;
}

.notes :deep(table) {
  width: 100%;
  margin: 12px 0;
  border-collapse: collapse;
  font-size: 12px;
}

.notes :deep(th),
.notes :deep(td) {
  padding: 4px 14px 4px 0;
  border-bottom: 1px solid color-mix(in srgb, var(--lab-panel-border) 50%, transparent);
  text-align: left;
}

.notes :deep(th) {
  color: var(--p-text-disabled-color);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.notes :deep(img) {
  display: block;
  width: 100%;
  height: auto;
  margin: 12px 0;
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
}

.notes :deep(a) {
  color: var(--p-primary-color);
}

.frame iframe {
  display: block;
  width: 100%;
  border: 0;
}
</style>
