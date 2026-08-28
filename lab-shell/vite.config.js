import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      // Shared UniFi theme preset at the repo root (see CLAUDE.md).
      '@unifi-theme': fileURLToPath(new URL('../shared/unifi-theme', import.meta.url)),
      // Shared AppShell.vue + app-shell.css (see .claude/plans/AppShell-Extraction-Plan.md).
      '@ui-shell': fileURLToPath(new URL('../shared/ui-shell', import.meta.url)),
    },
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
      // BR-AS01 — the curated plugin registry on accounts-service. Mirrors
      // admin/vite.config.js's rule: drop the '/platform' segment and inject
      // the Basic credentials the browser never holds itself.
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
