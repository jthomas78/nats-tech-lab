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
import { bootShell } from './shell/bootShell.js'
import { createBuiltinAdapter, createPluginLoader } from './shell/loader/pluginLoader.js'
import { createRegistryClient } from './shell/registry/registryClient.js'
import { createShellRoutes } from './shell/routing/shellRoutes.js'
import { SHELL } from './shell/shellKey.js'
import NotFoundView from './views/NotFoundView.vue'
import PluginErrorView from './views/PluginErrorView.vue'

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
    },
  })

  const router = createRouter({
    history: createWebHistory(),
    routes: [
      /* The shell owns no content route of its own — `/` is a doorway into a
         plugin's namespace, not a page (BR-AS09). */
      { path: '/', redirect: '/demos' },
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
  app.provide(SHELL, { ...shell, loader, plugins, router })
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
