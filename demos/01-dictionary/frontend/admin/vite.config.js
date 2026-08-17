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
    port: 7100,
    fs: {
      // Allow importing the shared theme from outside the app root.
      allow: [fileURLToPath(new URL('../../../..', import.meta.url))],
    },
    proxy: {
      // Phase 15c/19 — accounts-service's own auth routes (connectInfo,
      // adminConnectInfo, tenants). More specific than '/api' below, so
      // Vite's prefix match picks this one first — mirrors
      // seafreight-app/vite.config.js and nginx.conf's production rule.
      // Without this, /api/auth/* silently falls through to the general
      // '/api' rule below (shipping-service, port 7200) and 404s, since
      // these routes only exist on accounts-service.
      '/api/auth': {
        target: 'http://localhost:7202',
        changeOrigin: true,
      },
      // Phase 14c — accounts-service, ahead of the general '/api' rule so
      // Vite's longest-prefix match picks it first. rewrite drops only the
      // '/api/platform' segment, mirroring nginx.conf's production rule.
      '/api/platform/accounts': {
        target: 'http://localhost:7202',
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api\/platform/, '/api'),
        headers: { Authorization: 'Basic YWRtaW46YWNjb3VudHMtc3Bpa2UtcGFzcw==' },
      },
      // Phase 26 — trading-partner-service. Context-scoped, so (like
      // nginx.conf's production rule) no prefix rewrite is needed, just a
      // passthrough with the shared BasicAuth secret injected.
      '/api/trading-partners': {
        target: 'http://localhost:7204',
        changeOrigin: true,
        headers: { Authorization: 'Basic YWRtaW46dHJhZGluZy1wYXJ0bmVyLXNwaWtlLXBhc3M=' },
      },
      // Phase 30h — the cross-account NATS/JetStream diagnostic endpoints
      // (Connections, Services, Account Activity, Log, KV, Streams/Replay)
      // moved to observability-service. More specific than the general
      // '/api' rule below, so Vite's prefix match picks these first —
      // mirrors nginx.conf's production rule.
      '/api/nats': {
        target: 'http://localhost:7205',
        changeOrigin: true,
      },
      '/api/kv': {
        target: 'http://localhost:7205',
        changeOrigin: true,
      },
      '/api/jetstream': {
        target: 'http://localhost:7205',
        changeOrigin: true,
      },
      '/api': {
        target: 'http://localhost:7200',
        changeOrigin: true,
      },
    },
  },
})
