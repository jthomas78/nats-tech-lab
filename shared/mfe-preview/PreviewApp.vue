<script setup>
/*
  The preview page itself: load the plugin the way the shell would, then list
  and render everything it contributes.

  The sequence deliberately mirrors `lab-shell/src/shell/loader/pluginLoader.js`
  — fetch the manifest, import the entry module, call `activate()` at most once
  with a shell API, and only then render — so a plugin that misbehaves in the
  shell misbehaves here, at the same step, with the same failure visible.
*/
import { computed, ref, shallowRef } from 'vue'

import { buildItems } from './contributions.js'
import PreviewStage from './PreviewStage.vue'
import { createPreviewShellApi } from './shellApiStub.js'

const props = defineProps({
  options: { type: Object, required: true },
})

const manifest = shallowRef(null)
const manifestError = ref(null)
const activationError = ref(null)
const loadError = ref(null)
const items = shallowRef([])
const selectedKey = ref(null)
const ready = ref(false)

async function boot() {
  let module
  try {
    /* @vite-ignore — the entry path is configuration, not a literal Vite can
       analyse. The dev server resolves and transforms it exactly as it would
       for the federated build. */
    module = await import(/* @vite-ignore */ props.options.entry)
  } catch (error) {
    loadError.value = String(error?.message ?? error)
    ready.value = true
    return
  }

  try {
    const response = await fetch(props.options.manifest)
    if (!response.ok) throw new Error(`${response.status} ${response.statusText}`)
    manifest.value = await response.json()
  } catch (error) {
    manifestError.value = String(error?.message ?? error)
  }

  let fixtures = {}
  if (props.options.fixtures) {
    try {
      const loaded = await import(/* @vite-ignore */ props.options.fixtures)
      fixtures = loaded.default ?? loaded.fixtures ?? {}
    } catch (error) {
      console.warn('[mfe-preview] fixtures failed to load', error)
    }
  }

  /* Once, before anything renders — the loader's contract. A plugin that
     throws here (example-plugin-activate-throws does, by design) still gets
     its components listed: the harness reports the fault instead of refusing
     to draw, because seeing what a half-activated plugin looks like is the
     reason to open this page. */
  try {
    await module.activate?.(createPreviewShellApi())
  } catch (error) {
    activationError.value = String(error?.message ?? error)
  }

  items.value = buildItems({ manifest: manifest.value, components: module.components ?? {}, fixtures })
  selectedKey.value = items.value.find((item) => item.component)?.key ?? items.value[0]?.key ?? null
  ready.value = true
}

void boot()

const groups = computed(() => {
  const byKind = new Map()
  for (const item of items.value) {
    if (!byKind.has(item.kindLabel)) byKind.set(item.kindLabel, [])
    byKind.get(item.kindLabel).push(item)
  }
  return [...byKind].map(([label, entries]) => ({ label, entries }))
})

const selected = computed(() => items.value.find((item) => item.key === selectedKey.value) ?? null)
const title = computed(() => manifest.value?.name ?? props.options.title)
const pretty = (value) => JSON.stringify(value, null, 2)
</script>

<template>
  <div class="mfe-preview">
    <header class="mfe-preview-top">
      <div class="mfe-preview-brand">
        <span class="mfe-preview-dot" />
        <strong>{{ title }}</strong>
        <span
          v-if="manifest?.version"
          class="mfe-preview-ver"
        >v{{ manifest.version }}</span>
      </div>
      <div class="mfe-preview-top-right">
        contributions, rendered without the shell
      </div>
    </header>

    <div
      v-if="loadError"
      class="mfe-preview-banner is-bad"
    >
      <strong>The plugin entry did not load.</strong>
      <code>{{ options.entry }}</code> — {{ loadError }}
    </div>
    <div
      v-if="manifestError"
      class="mfe-preview-banner is-warn"
    >
      <strong>No manifest.</strong>
      {{ options.manifest }} — {{ manifestError }}. Only exported components are listed, without their placement.
    </div>
    <div
      v-if="activationError"
      class="mfe-preview-banner is-bad"
    >
      <strong>activate() threw.</strong>
      {{ activationError }} — in the shell this marks the whole plugin failed.
    </div>

    <div class="mfe-preview-body">
      <nav class="mfe-preview-rail">
        <div
          v-for="group in groups"
          :key="group.label"
          class="mfe-preview-group"
        >
          <span class="mfe-preview-group-label">{{ group.label }}</span>
          <button
            v-for="item in group.entries"
            :key="item.key"
            type="button"
            class="mfe-preview-item"
            :class="{ 'is-active': item.key === selectedKey, 'is-inert': !item.component }"
            @click="selectedKey = item.key"
          >
            <span class="mfe-preview-item-id">{{ item.id }}</span>
            <span class="mfe-preview-item-sub">{{ item.componentKey ?? 'metadata only' }}</span>
          </button>
        </div>
        <p
          v-if="ready && items.length === 0"
          class="mfe-preview-empty"
        >
          Nothing to preview — this plugin exports no components and declares no contributions.
        </p>
      </nav>

      <main class="mfe-preview-main">
        <template v-if="selected">
          <div class="mfe-preview-head">
            <span class="mfe-preview-badge">{{ selected.kindLabel }}</span>
            <h1>{{ selected.id }}</h1>
            <p>{{ selected.placement }}</p>
          </div>

          <div
            v-if="selected.missing"
            class="mfe-preview-banner is-bad"
          >
            <strong>Component not exported.</strong>
            The manifest names <code>{{ selected.componentKey }}</code>, the bundle does not export it.
          </div>

          <PreviewStage
            v-else-if="selected.component"
            :key="selected.key"
            :component="selected.component"
            :component-props="selected.props"
            :stage="selected.stage"
          />

          <div
            v-else
            class="mfe-preview-note"
          >
            The shell renders this from the manifest alone — there is no component to draw.
          </div>

          <details class="mfe-preview-details">
            <summary>Props passed</summary>
            <pre>{{ pretty(selected.props) }}</pre>
          </details>
          <details
            v-if="selected.contribution"
            class="mfe-preview-details"
          >
            <summary>Manifest entry</summary>
            <pre>{{ pretty(selected.contribution) }}</pre>
          </details>
        </template>

        <p
          v-else-if="ready"
          class="mfe-preview-empty"
        >
          Select a contribution.
        </p>
        <p
          v-else
          class="mfe-preview-empty"
        >
          Loading the plugin…
        </p>
      </main>
    </div>
  </div>
</template>
