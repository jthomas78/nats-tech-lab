import { fileURLToPath, URL } from 'node:url'

import { federation } from '@module-federation/vite'
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
//
// ONE build, TWO entries (task 16d of the app shell plan):
//
//   index.html    the standalone app — `main.js` → `App.vue`, own AppShell
//   remoteEntry.js  the federated remote `lab-shell` loads — `src/plugin.js`
//
// Neither is built separately and neither is edited to switch between them.
export default defineConfig({
  // Every asset this app emits is served under the shell's one public path
  // layout, `/plugins/<id>/…` (BR-AS77) — the entry, the lazy chunks, the CSS,
  // the fonts and the images, not the entry alone. The dev server therefore
  // serves the standalone app at http://localhost:20401/plugins/demo-04/ as
  // well, so the proxied and the direct URL are the same path and a relative
  // asset URL is correct in both.
  base: '/plugins/demo-04/',
  plugins: [
    vue(),
    federation({
      // The Module Federation container name. It must match `remote.name` in
      // public/manifest.json, and it is snake_case because a container name
      // becomes a global identifier in some federation output formats. The
      // plugin *id* stays kebab-case; the manifest carries the mapping.
      name: 'demo_04',
      filename: 'remoteEntry.js',
      // Remote entries must load their CSS even when their index.html is never opened.
      bundleAllCSS: true,
      exposes: { './plugin': './src/plugin.js' },
      shared: {
        // Singletons: two Vue instances in one page would give the plugin its
        // own reactivity system, and `inject` across the boundary would stop
        // working. The shell owns the version (BR-AS09).
        vue: { singleton: true, requiredVersion: '^3.5' },
        // The PrimeVue theme engine, for the same reason and with the same
        // words as `lab-shell/vite.config.js`. The host calls
        // `app.use(PrimeVue, { theme })` exactly once; a remote that bundles
        // its own copy of this package gets a second, unconfigured theme
        // service, and its components then render their `*-style` block
        // without the `*-variables` block that carries the preset's tokens.
        // Embedded tabs lost their padding, underline and active colour that
        // way until 2026-09-24. Both folders resolve 0.7.4.
        '@primeuix/styled': { singleton: true, requiredVersion: '^0.7.4' },
      },
      // No .d.ts generation: there is no TypeScript here, and the dts worker
      // shells out to tsc against a tsconfig that does not exist.
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
    // Federation needs a real ES module output and no eager inlining.
    target: 'esnext',
    minify: false,
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
