import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      // Shared UniFi theme preset at the repo root (see CLAUDE.md).
      '@unifi-theme': fileURLToPath(new URL('../../../../shared/unifi-theme', import.meta.url)),
      // Shared AppShell.vue + app-shell.css (see .claude/plans/AppShell-Extraction-Plan.md).
      '@ui-shell': fileURLToPath(new URL('../../../../shared/ui-shell', import.meta.url)),
      // Demo-01 shared refdata-label composable (Phase 11.6).
      '@refdata': fileURLToPath(new URL('../../shared/refdata', import.meta.url)),
    },
  },
  server: {
    port: 7100,
    fs: {
      // Allow importing the shared theme from outside the app root.
      allow: [fileURLToPath(new URL('../../../..', import.meta.url))],
    },
    proxy: {
      // Phase 14c — accounts-service, ahead of the general '/api' rule so
      // Vite's longest-prefix match picks it first. rewrite drops only the
      // '/api/platform' segment, mirroring nginx.conf's production rule.
      '/api/platform/accounts': {
        target: 'http://localhost:7202',
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api\/platform/, '/api'),
        headers: { Authorization: 'Basic YWRtaW46YWNjb3VudHMtc3Bpa2UtcGFzcw==' },
      },
      '/api': {
        target: 'http://localhost:7200',
        changeOrigin: true,
      },
    },
  },
})
