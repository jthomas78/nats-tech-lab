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
      // Demo-01 shared NATS WebSocket URL resolver (Phase 45).
      '@nats-shared': fileURLToPath(new URL('../../shared/nats', import.meta.url)),
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
      // Phase 45 — the browser's NATS WebSocket connection, same-origin so
      // it works identically on localhost and behind a remote TLS proxy
      // (see shared/nats/resolveWsUrl.js). Mirrors this app's nginx.conf
      // `location /nats` rule. `ws: true` is what makes Vite forward the
      // Upgrade handshake rather than answering it as a plain HTTP request.
      '/nats': {
        target: 'ws://localhost:9222',
        ws: true,
        rewriteWsOrigin: true,
        rewrite: (path) => path.replace(/^\/nats/, ''),
      },
      // More specific than '/api' below — must come first so Vite's
      // prefix match picks this one for the auth routes (Phase 15c, folded
      // into accounts-service in Phase 19) instead of falling through to
      // shipping-service.
      '/api/auth': {
        target: 'http://localhost:7202',
        changeOrigin: true,
      },
      '/api': {
        target: 'http://localhost:7200',
        changeOrigin: true,
      },
    },
  },
})
