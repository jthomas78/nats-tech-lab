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
        shared: { vue: { singleton: true, requiredVersion: '^3.5' } },
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
    alias: {
      // Shared UniFi theme preset at the repo root (see CLAUDE.md).
      '@unifi-theme': fileURLToPath(new URL('../shared/unifi-theme', import.meta.url)),
      // Shared AppShell.vue + app-shell.css (see .claude/plans/AppShell-Extraction-Plan.md).
      '@ui-shell': fileURLToPath(new URL('../shared/ui-shell', import.meta.url)),
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
    // 7109, not 7100-7108: docker-compose publishes every port from 7100 to
    // 7106 and holds them whenever the stack is up, and 7107/7108 are already
    // claimed in .claude/launch.json. Phase 1b's example plugin takes 7110.
    port: 7109,
    proxy: {
      // BR-AS01 — the curated plugin registry, its own bounded context in
      // accounts-service since Phase 2a. Its own rule rather than an edit to
      // the accounts one: /api/platform/accounts is a shared prefix other
      // routes still need. Drops the '/platform' segment and injects the
      // Basic credentials the browser never holds itself.
      '/api/platform/registry': {
        target: 'http://localhost:7202',
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api\/platform/, '/api'),
        headers: { Authorization: 'Basic YWRtaW46YWNjb3VudHMtc3Bpa2UtcGFzcw==' },
      },
      '/api/platform/accounts': {
        target: 'http://localhost:7202',
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api\/platform/, '/api'),
        headers: { Authorization: 'Basic YWRtaW46YWNjb3VudHMtc3Bpa2UtcGFzcw==' },
      },
    },
    fs: {
      // Shared theme + demo READMEs (imported ?raw for intro pages).
      allow: [fileURLToPath(new URL('..', import.meta.url))],
    },
  },
})
