import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

// Demo 04's UI. Ports follow 20<demo number><increment>:
//
//   20401  this dev server
//   20402  the command API (`cqrs serve`)
//   20403  the NATS WebSocket the browser watches
//
// The two shared aliases are the only thing this demo reaches for outside
// its own folder, alongside the go.work entry. That is deliberate: the repo
// has one visual identity and one page shell, and a demo does not get to
// invent a second (root CLAUDE.md, "Frontend Design System").
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@unifi-theme': fileURLToPath(new URL('../../../shared/unifi-theme', import.meta.url)),
      '@ui-shell': fileURLToPath(new URL('../../../shared/ui-shell', import.meta.url)),
    },
  },
  test: {
    environment: 'happy-dom',
  },
  server: {
    port: 20401,
    strictPort: true,
    fs: {
      // The theme and the shell live outside this app root.
      allow: [fileURLToPath(new URL('../../..', import.meta.url))],
    },
  },
})
