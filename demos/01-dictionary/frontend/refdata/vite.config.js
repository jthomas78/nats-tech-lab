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
      // Shared refdata helpers — BR-D32's locale ordering/labelling lives here
      // so this app and the two shipping apps present locales identically.
      '@refdata': fileURLToPath(new URL('../../shared/refdata', import.meta.url)),
      // Demo-01 shared NATS WebSocket URL resolver (Phase 45).
      '@nats-shared': fileURLToPath(new URL('../../shared/nats', import.meta.url)),
    },
  },
  server: {
    port: 7102,
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
      // Phase 32 — accounts-service's auth routes (refdataAdminConnectInfo
      // mints this app's PLATFORM-account NATS credential). More specific
      // than '/api' below, so Vite's prefix match picks this one first —
      // mirrors frontend/admin's vite.config.js and nginx.conf's production
      // rule. Without this, /api/auth/* would silently fall through to the
      // general '/api' rule (refdata-service) and 404, since these routes
      // only exist on accounts-service.
      '/api/auth': {
        target: 'http://localhost:7202',
        changeOrigin: true,
      },
      // refdata-service (not the shipping backend). Its code defaults to
      // :8080 too, which collides if the shipping backend is also running
      // locally — run refdata-service with HTTP_ADDR=:8081 (see README.md's
      // dev-mode instructions) while iterating on this frontend. Business
      // reads and corpus/item administration now go over NATS (api.js) —
      // this proxy remains only for whatever REST this app hasn't yet been
      // weaned off (Phase 33 retires refdata-service's REST surface
      // entirely).
      '/api': {
        target: 'http://localhost:7201',
        changeOrigin: true,
      },
      // Phase 38c-ii — organizations-service's compliance document byte
      // ingress (BR-TP40), on its own prefix rather than under /api because
      // /api means refdata-service throughout this app. Mirrors nginx.conf's
      // production rule; see there for why the body-size limit matters in
      // production and not here (Vite's proxy imposes none).
      '/files': {
        target: 'http://localhost:7204',
        changeOrigin: true,
      },
    },
  },
})
