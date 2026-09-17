<script setup>
// A lesson's Overview: what the lesson is, in words, and the drawings of it.
//
// Both halves are files that already exist in the repo, shown as they are, and
// the component is handed them (04.12.2, D21). It used to import lesson 01's
// two filenames itself, which made it lesson 01's panel and nothing else. Two
// lessons need the same panel, and a copy would be a second copy of the 250
// lines of CSS below.
//
// Nothing is retyped and nothing is redrawn. If a rule changes, the source
// file changes, and this screen changes with it. A second copy of the
// explanation here would be a second thing to keep true.
//
// The `page` half is a whole HTML document, so it goes in a frame rather than
// being pasted into this one — its own <style> block would otherwise leak into
// the app. `srcdoc` means the file is compiled in, not fetched. The frame is
// given no scripts; it is only allowed to be same-origin so this component can
// read its height and grow to fit, which avoids a scrollbar inside a scrollbar.
//
// `page` is optional. A lesson with no drawings yet shows one tab, not two: a
// tab that opens on nothing is a promise the screen does not keep.
//
// The two views are a real PrimeVue `Tabs` carrying `class="panel-tabs"`, which
// is the repo's one style for a top tab strip (shared/unifi-theme/LAYOUT.md,
// "Panel top tabs"). A custom chip/pill toggle is explicitly not allowed for
// this role — chips are for filters. The strip therefore sits FLUSH on the
// page and the card treatment lives on each tab's content, not around the
// tabs: wrapping the strip in a card puts the tablist on a background it does
// not expect and costs a pile of compensating overrides.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'

import { renderNotes } from '../about/notes.js'

const props = defineProps({
  // The line above the title, naming the demo and the lesson.
  eyebrow: { type: String, required: true },
  // One paragraph saying what the reader is about to read.
  lead: { type: String, required: true },
  // The markdown itself, compiled in by the caller with `?raw`.
  notes: { type: String, required: true },
  // The path of that file, printed so the reader can go and read it.
  notesFile: { type: String, required: true },
  notesLabel: { type: String, default: 'What it does' },
  // Images the markdown refers to, keyed by the path it writes.
  images: { type: Object, default: () => ({}) },
  // The drawings page, a whole HTML document. Optional — see above.
  page: { type: String, default: '' },
  pageFile: { type: String, default: '' },
  pageLabel: { type: String, default: 'Classes and sequences' },
  frameTitle: { type: String, default: 'Diagrams' },
})

const TABS = computed(() => {
  const tabs = [{ key: 'notes', label: props.notesLabel, file: props.notesFile }]
  if (props.page) tabs.push({ key: 'diagrams', label: props.pageLabel, file: props.pageFile })
  return tabs
})

const tab = ref('notes')
const current = computed(() => TABS.value.find((t) => t.key === tab.value) ?? TABS.value[0])

// A lesson that loses its drawings would otherwise leave the strip on a tab
// that no longer exists, and the header would print an empty filename.
watch(TABS, (tabs) => {
  if (!tabs.some((t) => t.key === tab.value)) tab.value = 'notes'
})

const notes = computed(() => renderNotes(props.notes, props.images))

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
        {{ props.eyebrow }}
      </p>
      <code class="src">{{ current.file }}</code>
    </header>

    <p class="lead">
      {{ props.lead }}
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
          <!-- eslint-disable vue/no-v-html -- our own markdown, compiled in at build -->
          <article
            class="card notes"
            data-testid="about-notes"
            v-html="notes"
          />
          <!-- eslint-enable vue/no-v-html -->
        </TabPanel>
        <TabPanel
          v-if="props.page"
          value="diagrams"
        >
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
              :title="props.frameTitle"
              sandbox="allow-same-origin"
              :srcdoc="props.page"
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

/* The card fills the panel, like every other panel on this screen and like the
   diagrams tab beside it. The README is not only prose — it carries code blocks,
   ASCII drawings and tables that a reading measure would wrap or scroll — so the
   measure is put on the running text alone, below. */
.notes {
  max-width: none;
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

/* Long lines of prose are hard to read, so paragraphs and list items keep a
   reading measure even though their card does not. Headings, rules, code and
   tables are free to use the whole width. */
.notes :deep(p),
.notes :deep(li) {
  max-width: 84ch;
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
