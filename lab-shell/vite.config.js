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
  server: {
    port: 5170,
    fs: {
      // Shared theme + demo READMEs (imported ?raw for intro pages).
      allow: [fileURLToPath(new URL('..', import.meta.url))],
    },
  },
})
