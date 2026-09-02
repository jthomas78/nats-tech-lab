import { fileURLToPath, URL } from 'node:url'

import { federation } from '@module-federation/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

/* The federation plugin gives the host a shared-module scope so a remote's Vue
   resolves to the shell's instance rather than a second copy (two Vues in one
   page means `inject` stops crossing the boundary and reactivity splits).

   It declares NO remotes: containers are registered at runtime from the
   curated registry, which is what keeps adding a plugin a configuration
   change rather than a shell rebuild (BR-AS03).

   Skipped under Vitest — the specs exercise the adapter with an injected
   runtime and must not need federation's build-time virtual modules. */
const federationPlugin = process.env.VITEST
  ? []
  : [
      federation({
        name: 'lab_shell',
        remotes: {},
        shared: {
          vue: { singleton: true, requiredVersion: '^3.5' },
          // PrimeVue remotes read the theme configured once by the host.
          '@primeuix/styled': { singleton: true, requiredVersion: '^0.7.4' },
        },
        /* No .d.ts generation or consumption: there is no TypeScript in this
           repo's frontends, and the dts worker shells out to tsc against a
           tsconfig that does not exist, which takes the dev server down on
           startup. */
        dts: false,
      }),
    ]

export default defineConfig({
  plugins: [vue(), ...federationPlugin],
  resolve: {
    /* One Vue, one PrimeVue, whatever a nested workspace resolves for itself.
       An example plugin under `plugins/` installs its own copies, so a spec
       that imports a plugin's source got a second Vue — and a second Vue means
       `renderSlot` reads a null rendering instance the moment a PrimeVue
       component from one copy renders inside a tree owned by the other. It is
       the same singleton the federation config declares for the browser, said
       again for the resolver. */
    dedupe: ['vue', 'primevue', '@primeuix/styled'],
    alias: {
      // Shared UniFi theme preset at the repo root (see CLAUDE.md).
      '@unifi-theme': fileURLToPath(new URL('../shared/unifi-theme', import.meta.url)),
      // Shared AppShell.vue + app-shell.css (see .claude/plans/AppShell-Extraction-Plan.md).
      '@ui-shell': fileURLToPath(new URL('../shared/ui-shell', import.meta.url)),
      '@nats-shared': fileURLToPath(new URL('../demos/01-dictionary/shared/nats', import.meta.url)),
    },
  },
  build: {
    // Federation requires a real ES module output; the default esbuild target
    // is fine but the container init runs as top-level await.
    target: 'esnext',
  },
  test: {
    environment: 'happy-dom',
  },
  server: {
    // 7110 — the same port the shell's own container publishes, so a
    // registry entry curated for the stack is not silently wrong when the
    // shell is run from source instead. 7100-7106 are held by docker-compose
    // whenever the stack is up and 7107/7108 are claimed in
    // .claude/launch.json; the example plugin's remote takes 7111.
    port: 7110,
    strictPort: true,
    proxy: {
      // Exact mint route: a prefix grant would expose operator credentials
      // to federated code in the shell's realm (BR-AS25/BR-AS27).
      '^/api/auth/shellConnectInfo(?:\\?.*)?$': {
        target: 'http://localhost:7202',
        changeOrigin: true,
      },
      '/nats': {
        target: 'ws://localhost:9222',
        ws: true,
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/nats/, ''),
      },
    },
    fs: {
      // Shared theme, frame and NATS URL resolver.
      allow: [fileURLToPath(new URL('..', import.meta.url))],
    },
  },
})
