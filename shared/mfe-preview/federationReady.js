/*
  Wait for Module Federation's shared scope before touching a Vue API.

  A plugin's dev server rewrites every bare `vue` import — the harness's
  included — to federation's shared-module loader, and in the browser that
  loader fills its exports ASYNCHRONOUSLY: `import { h } from 'vue'` is
  `undefined` for the first few ticks and only then becomes the real function
  through a live binding.

  Plugin code never notices, because a component only touches Vue inside
  `setup()`/render, long after boot. The harness does notice, because it calls
  `createApp` itself. So it waits: the pending loads the runtime publishes if
  they are there, and otherwise the binding itself, which is what actually has
  to be true.
*/

const CACHE_KEY = '__mf_module_cache__'

export async function whenSharedScopeReady(vue, { timeoutMs = 5000 } = {}) {
  const pending = globalThis[CACHE_KEY]?.pendingShareLoads
  if (Array.isArray(pending)) await Promise.allSettled(pending)
  if (typeof vue.createApp === 'function') return

  /* No federation in this dev server, or a slower init than the cache
     advertised. Poll the binding rather than a private field: it is the only
     signal that means what the caller needs. */
  const deadline = Date.now() + timeoutMs
  while (typeof vue.createApp !== 'function') {
    if (Date.now() > deadline) throw new Error('Vue never became available — is the plugin’s federation runtime loading?')
    await new Promise((resolve) => setTimeout(resolve, 10))
  }
}
