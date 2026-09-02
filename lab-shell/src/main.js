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
import { createConnectionRegistry, SHELL_PLATFORM } from './shell/connections/connectionRegistry.js'
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
export async function bootstrap() {
  const shell = await bootShell({
    permissions,
  })

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
        manifestFor: shell.manifestFor,
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
  /* One profile, because one credential can be minted today. A migrating app
     adds its own entry here when its credential arrives; the registry rejects
     a profile nobody declared, so there is no second guard to keep in step. */
  const connections = createConnectionRegistry({
    profiles: { [SHELL_PLATFORM]: {} },
    connect: async () => createShellConnection({ connect: dial }),
  })
  const connection = await connections.acquire(SHELL_PLATFORM)
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
      /* `shell.manifestFor` rather than a index the host rebuilds: the shell
         indexes the VALIDATED manifest as it admits it, so a manifest that
         failed validation cannot reach the loader wearing the raw shape it
         was refused in, and there is no second copy for a later read to
         leave stale. */
      void installShellRoutes({
        router,
        contributions: shell.contributions,
        loader,
        manifestFor: shell.manifestFor,
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
  /* Copy on every read the plane installed. This is what carries a hint to
     the screen: the plane reads on start, on a hint and after a reconnect,
     and before this seam existed none of those repainted anything — the
     ageing interval below was doing it, so a hint could sit up to a whole
     interval behind the truth. */
  const publishHealth = () => { health.signals = healthPlane.snapshot() }
  const healthPlane = createHealthPlane({
    transport: createHealthTransport({ request: connection.request }),
    subscribe: (subject, handler) => connection.subscribe(subject, handler),
    onChange: publishHealth,
  })

  const app = createApp(App)
  /* Deliberately narrow. The router, the connection registry and the health
     plane itself were provided here and injected by nothing — a member no
     screen reads is a member every screen has to be checked against. What is
     left is what App.vue and the views actually resolve. */
  app.provide(SHELL, withRuntime(shell, { loader, connection: connection.state, health }))
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
  const healthTimer = setInterval(() => {
    /* Read, then republish regardless of the read's outcome. The read is a
       floor cadence: a first read that lost the race with the connection
       coming up would otherwise leave every signal `unknown` with nothing to
       wake it. The unconditional republish is what lets a KEPT reading age
       into `stale` on schedule, which no read reports because nothing
       changed. */
    void healthPlane.refresh().finally(publishHealth)
  }, 5_000)
  if (import.meta.hot) import.meta.hot.dispose(() => {
    clearInterval(healthTimer)
    healthPlane.stop()
    void session.stop()
  })
}

/* Not called on import. The host is the one module a spec could never reach,
   because reaching it mounted an app and dialled a broker; exporting the
   function and gating the call is what makes composition assertable at all
   (finding 10). */
if (import.meta.env?.MODE !== 'test') bootstrap()
