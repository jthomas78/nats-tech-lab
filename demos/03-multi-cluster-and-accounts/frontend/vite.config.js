import { fileURLToPath, URL } from 'node:url'

import { federation } from '@module-federation/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

// Demo 03's T4 playground page. Ports follow demo 04's 20<demo><n>:
//
//   20301  this dev server
//   20302  the control service (`playground serve`)
//
// The page talks to the control service only. It never talks to NATS, and
// it never runs a command of its own: every button is one POST to 20302.
//
// ONE build, TWO entries, as demo 04:
//
//   index.html      the standalone app — `main.js` → `App.vue`, own AppShell
//   remoteEntry.js  the federated remote `lab-shell` loads — `src/plugin.js`
export default defineConfig({
  // Every asset is served under the shell's public path layout,
  // `/plugins/<id>/…`, so the proxied and the direct URL are the same path.
  base: '/plugins/demo-03/',
  plugins: [
    vue(),
    federation({
      // Must match `remote.name` in public/manifest.json. snake_case, because
      // a container name becomes a global identifier.
      name: 'demo_03',
      filename: 'remoteEntry.js',
      bundleAllCSS: true,
      exposes: { './plugin': './src/plugin.js' },
      shared: {
        // One Vue and one PrimeVue theme engine per page. The shell owns both;
        // see demo 04's vite.config.js for what broke when a remote had its own.
        vue: { singleton: true, requiredVersion: '^3.5' },
        '@primeuix/styled': { singleton: true, requiredVersion: '^0.7.4' },
      },
      dts: false,
    }),
  ],
  resolve: {
    alias: {
      '@unifi-theme': fileURLToPath(new URL('../../../shared/unifi-theme', import.meta.url)),
      '@ui-shell': fileURLToPath(new URL('../../../shared/ui-shell', import.meta.url)),
    },
  },
  test: {
    environment: 'happy-dom',
  },
  build: {
    target: 'esnext',
    minify: false,
  },
  server: {
    host: '127.0.0.1',
    port: 20301,
    strictPort: true,
    fs: {
      allow: [fileURLToPath(new URL('../../..', import.meta.url))],
    },
  },
})
