import '@unifi-theme/unifi.css'
import 'primeicons/primeicons.css'

import { definePreset } from '@primevue/themes'
import { jwtAuthenticator, wsconnect } from '@nats-io/nats-core'
import { resolveWsUrl } from '@nats-shared/resolveWsUrl.js'
import Aura from '@primevue/themes/aura'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import { createApp, nextTick, reactive } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'

import { createUnifiPreset, enableDarkMode, themeOptions } from '@unifi-theme/preset.js'

import App from './App.vue'
import { createPermissionEvaluator } from './shell/auth/permissions.js'
import { createAfterPaint } from './shell/afterPaint.js'
import { bootShell, withRuntime } from './shell/bootShell.js'
import { createConnectionRegistry, CREDENTIAL_PROFILE } from './shell/connections/connectionRegistry.js'
import { createShellConnection } from './shell/connections/shellConnection.js'
import { createShellDialer } from './shell/connections/shellDialer.js'
import { createFederatedAdapter } from './shell/loader/federatedAdapter.js'
import { createPluginLoader } from './shell/loader/pluginLoader.js'
import { createHealthPlane, createHealthTransport } from './shell/registry/healthPlane.js'
import { createRegistryTransport } from './shell/registry/registryTransport.js'
import { createRegistrySession } from './shell/registry/registrySession.js'
import { createShellRoutes, installShellRoutes } from './shell/routing/shellRoutes.js'
import { installWithdrawalGuard } from './shell/routing/withdrawnRoutes.js'
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

const waitForPaint = createAfterPaint()

/* Wrapped rather than top-level `await`: TLA constrains the build target, and
   the shell has to boot the same way in every browser the demos are shown in. */
async function bootstrap() {
  const shell = await bootShell({
    permissions,
  })

  const plugins = new Map(shell.plugins.map((plugin) => [plugin.id, plugin]))

  const loader = createPluginLoader({
    allowlist: shell.allowlist,
    statuses: shell.statuses,
    adapters: {
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

  /* Nobody new enters a withdrawn plugin's route (BR-AS57). The route record
     stays registered — the occupant keeps their URL, and an unchanged return
     is then a change to a set rather than to the route table. */
  installWithdrawalGuard({ router, contributions: shell.contributions })

  const dial = createShellDialer({ fetch: window.fetch.bind(window), location: window.location, dial: wsconnect, authenticate: jwtAuthenticator, resolveWsUrl })
  const connections = createConnectionRegistry({ connect: async ({ profile }) => {
    if (profile !== CREDENTIAL_PROFILE.SHELL_PLATFORM) throw new Error('profile-unavailable')
    return createShellConnection({ connect: dial })
  } })
  const connection = await connections.acquire(CREDENTIAL_PROFILE.SHELL_PLATFORM)
  const session = createRegistrySession({
    connection,
    client: createRegistryTransport({ request: connection.request }),
    shell,
    // A double animation frame yields a browser paint before credential
    // minting starts; nextTick alone only waits for the DOM patch. The
    // timeout inside createAfterPaint covers the tab that never paints.
    afterPaint: async () => {
      await nextTick()
      await waitForPaint()
    },
    onResult: (discovery) => {
      const { added, addedRoutes } = shell.applyRegistry(discovery)
      if (added.length === 0) return
      /* Re-synced rather than merged from `added`: the registry holds the
         VALIDATED manifests, and a manifest that failed validation must not
         reach the loader wearing the raw shape it was refused in. */
      for (const plugin of shell.plugins) plugins.set(plugin.id, plugin)
      void installShellRoutes({
        router,
        contributions: shell.contributions,
        loader,
        plugins,
        errorComponent: PluginErrorView,
        routes: addedRoutes,
      })
    },
  })
  /* The health plane is started alongside the session and never awaited by
     it (BR-AS65). A health read that hangs must not delay a single plugin:
     the catalogue is what the boot depends on, and health is decoration on
     top of whatever the boot produced. */
  const health = reactive({ signals: {} })
  const healthPlane = createHealthPlane({
    transport: createHealthTransport({ request: connection.request }),
    subscribe: (subject, handler) => connection.subscribe(subject, handler),
  })
  const refreshHealth = async () => {
    /* Read, then copy. The read is what makes this a floor cadence and not
       just a repaint: the plane otherwise reads on start, on a hint and after
       a reconnect, so a first read that lost the race with the connection
       coming up would leave every signal `unknown` forever with nothing to
       wake it. Copying afterwards regardless of the read's outcome is what
       lets a kept reading age into `stale` on schedule. */
    await healthPlane.refresh()
    health.signals = healthPlane.snapshot()
  }

  const app = createApp(App)
  app.provide(SHELL, withRuntime(shell, { loader, plugins, router, connections, connection: connection.state, health, healthPlane, refreshHealth }))
  app.use(createPinia())
  app.use(router)
  app.use(PrimeVue, {
    theme: {
      preset: createUnifiPreset(definePreset, Aura),
      options: themeOptions,
    },
  })
  app.mount('#app')
  void session.start()
  healthPlane.start()
  /* Re-read the plane on the same cadence the registry probes on, so a
     reading that has aged into `stale` is replaced rather than merely
     labelled. The interval only copies memory into a reactive object; the
     network read is the plane's own business. */
  const healthTimer = setInterval(() => { void refreshHealth() }, 5_000)
  if (import.meta.hot) import.meta.hot.dispose(() => {
    clearInterval(healthTimer)
    healthPlane.stop()
    void session.stop()
  })
}

bootstrap()
