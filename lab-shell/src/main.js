import '@unifi-theme/unifi.css'
import 'primeicons/primeicons.css'

import { definePreset } from '@primevue/themes'
import Aura from '@primevue/themes/aura'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'

import { createUnifiPreset, enableDarkMode, themeOptions } from '@unifi-theme/preset.js'

import App from './App.vue'
import { demoCatalogManifest, DEMO_CATALOG_MODULE } from './plugins/demo-catalog/manifest.js'
import { createPermissionEvaluator } from './shell/auth/permissions.js'
import { bootShell, withRuntime } from './shell/bootShell.js'
import { createFederatedAdapter } from './shell/loader/federatedAdapter.js'
import { createBuiltinAdapter, createPluginLoader } from './shell/loader/pluginLoader.js'
import { createRegistryClient } from './shell/registry/registryClient.js'
import { createShellRoutes } from './shell/routing/shellRoutes.js'
import { SHELL } from './shell/shellKey.js'
import HomeView from './views/HomeView.vue'
import NotFoundView from './views/NotFoundView.vue'
import PluginErrorView from './views/PluginErrorView.vue'
import PluginsView from './views/PluginsView.vue'

enableDarkMode()

/* Phase 1a has no sign-in yet, so every contribution's permission is granted.
   The evaluator is here rather than skipped so the *shape* is already the real
   one: when Phase 2 supplies claims from accounts-service, only this line
   changes, not any call site. */
const permissions = createPermissionEvaluator({ permissions: ['*'] })

/* Wrapped rather than top-level `await`: TLA constrains the build target, and
   the shell has to boot the same way in every browser the demos are shown in. */
async function bootstrap() {
  const shell = await bootShell({
    registryClient: createRegistryClient({ fetch: globalThis.fetch.bind(globalThis) }),
    builtins: [demoCatalogManifest],
    permissions,
  })

  const plugins = new Map(shell.plugins.map((plugin) => [plugin.id, plugin]))

  const loader = createPluginLoader({
    allowlist: shell.allowlist,
    statuses: shell.statuses,
    adapters: {
      /* Phase 1a ships one adapter. Phase 1b adds `federated` beside it without
         touching anything above — that is what the adapter seam is for. */
      builtin: createBuiltinAdapter({
        [DEMO_CATALOG_MODULE]: () => import('./plugins/demo-catalog/index.js'),
      }),
      /* Phase 1b. No remote is named here: the adapter registers containers at
         runtime from the curated registry, so adding a plugin never touches
         this file (BR-AS03, proven by tools/hostBundleFingerprint.mjs). */
      federated: createFederatedAdapter(),
    },
  })

  const router = createRouter({
    history: createWebHistory(),
    routes: [
      /* The shell's own two screens. Neither carries feature content: Home
         hosts `shell/home-main/v1` and Plugins is the inventory — the frame
         and its regions, which is exactly what BR-AS09 says the shell owns. */
      { path: '/', name: 'shell/home', component: HomeView, meta: { title: 'Home' } },
      { path: '/plugins', name: 'shell/plugins', component: PluginsView, meta: { title: 'Plugins' } },
      ...createShellRoutes({
        contributions: shell.contributions,
        loader,
        plugins,
        errorComponent: PluginErrorView,
      }),
      { path: '/:pathMatch(.*)*', name: 'not-found', component: NotFoundView },
    ],
  })

  const app = createApp(App)
  app.provide(SHELL, withRuntime(shell, { loader, plugins, router }))
  app.use(createPinia())
  app.use(router)
  app.use(PrimeVue, {
    theme: {
      preset: createUnifiPreset(definePreset, Aura),
      options: themeOptions,
    },
  })
  app.mount('#app')
}

bootstrap()
