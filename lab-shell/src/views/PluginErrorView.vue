<script setup>
/* Rendered in place of a route whose plugin would not load (BR-AS04). The
   frame, the nav and every other plugin are untouched — only this panel
   reports the failure.

   Stage and cause code only. The underlying error text is not shown: a
   federation failure quotes the remote's URL back at you, and BR-AS04 puts
   registry URLs, credentials and tokens on a denylist for anything the user
   can see. The detail goes to the console, where a developer looks for it. */
import { computed, inject, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { failureStage } from '../shell/loader/failureStage.js'
import { SHELL } from '../shell/shellKey.js'

const shell = inject(SHELL, null)
const route = useRoute()
const router = useRouter()

const retrying = ref(false)

const pluginId = computed(() => route.meta?.pluginId ?? '')
const record = computed(() => shell?.statuses?.get(pluginId.value) ?? null)
const manifest = computed(() =>
  shell?.plugins instanceof Map ? (shell.plugins.get(pluginId.value) ?? null) : null,
)
const stage = computed(() => failureStage(record.value?.reasonCode ?? null))

/* Retry is a real second attempt, not a reload: `failed -> loading` is a legal
   transition and the loader has already dropped its cached in-flight promise,
   so re-entering the route runs the whole load again. */
async function retry() {
  if (retrying.value) return
  retrying.value = true
  try {
    const plugin = manifest.value
    if (plugin && shell?.loader) await shell.loader.load(plugin)
  } catch {
    /* The status record is the report; a rejection here just means the retry
       failed too, and the panel is already showing why. */
  } finally {
    retrying.value = false
    /* Re-resolve the route either way: on success the real component renders,
       on failure this panel re-renders with the fresh cause. */
    await router.replace({ path: route.path, query: { ...route.query, r: Date.now() } })
  }
}
</script>

<template>
  <div class="failure-wrap">
    <div class="lab-panel failure">
      <h2>{{ route.meta.title }} could not be loaded</h2>
      <p class="lab-muted lead">
        The shell reached this route, but the plugin did not load.
        Nothing else on the page is affected.
      </p>

      <div class="detail">
        <div>
          <span class="lab-dim">plugin</span> {{ pluginId }}
          <template v-if="manifest?.version">
            {{ manifest.version }}
          </template>
          <template v-if="manifest?.shellApiVersion">
            · shell api {{ manifest.shellApiVersion }}
          </template>
        </div>
        <div>
          <span class="lab-dim">route</span>
          {{ route.meta.contributionId ?? route.name }} &rarr; {{ route.path }}
        </div>
        <div><span class="lab-dim">stage</span> {{ stage }}</div>
        <div><span class="lab-dim">cause</span> {{ record?.reasonCode ?? 'unknown' }}</div>
      </div>

      <div class="actions">
        <button
          class="btn"
          type="button"
          :disabled="retrying"
          @click="retry"
        >
          {{ retrying ? 'Retrying…' : 'Retry' }}
        </button>
        <router-link
          class="btn ghost"
          to="/plugins"
        >
          Plugin status
        </router-link>
        <router-link
          class="btn ghost"
          to="/"
        >
          Back to Home
        </router-link>
      </div>

      <p class="lab-dim note">
        Error summaries never include credentials, tokens or registry URLs.
        Details are in the browser console.
      </p>
    </div>
  </div>
</template>

<style scoped>
.failure-wrap { display: flex; justify-content: center; }
.failure {
  max-width: 640px;
  padding: 22px 24px;
  border-color: color-mix(in srgb, var(--err) 45%, var(--lab-panel-border));
}
h2 { margin: 0; font-size: 16px; line-height: 22px; font-weight: 600; }
.lead { margin: 6px 0 0; font-size: 13px; line-height: 20px; }
.detail {
  margin-top: 16px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
  background: var(--lab-nested-bg);
  padding: 10px 12px;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  line-height: 18px;
  color: var(--p-text-muted-color);
}
.actions { display: flex; gap: 10px; margin-top: 16px; }
.btn {
  display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 12px;
  border: 1px solid transparent; border-radius: 5px; font-size: 12px; font-weight: 600;
  background: var(--lab-accent); color: var(--lab-accent-ink);
  text-decoration: none; cursor: pointer;
}
.btn[disabled] { opacity: 0.6; cursor: default; }
.btn.ghost {
  background: none; border-color: var(--lab-panel-border); color: var(--p-text-color);
}
.note { margin: 12px 0 0; font-size: 11px; }
</style>
