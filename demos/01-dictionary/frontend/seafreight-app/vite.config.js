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
  test: {
    environment: 'happy-dom',
  },
  server: {
    port: 7101,
    fs: {
      // Allow importing the shared theme from outside the app root.
      allow: [fileURLToPath(new URL('../../../..', import.meta.url))],
    },
    proxy: {
      // More specific than '/api' below — must come first so Vite's
      // prefix match picks this one for auth-service (Phase 15c) instead
      // of falling through to shipping-service.
      '/api/auth': {
        target: 'http://localhost:7203',
        changeOrigin: true,
      },
      '/api': {
        target: 'http://localhost:7200',
        changeOrigin: true,
      },
    },
  },
})
