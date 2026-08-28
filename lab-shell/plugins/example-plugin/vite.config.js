import { fileURLToPath, URL } from 'node:url'

import { federation } from '@module-federation/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

/*
  The BR-AS15 proof plugin — built and served entirely on its own, by its own
  toolchain, with no import of the shell and no entry in the shell's build.
  That independence is the point of the package: BR-AS03 is not provable from
  a plugin the host compiles.

  `name` here is the Module Federation container name and must match the
  `remote.name` in the plugin's registry entry (see registry.dev.json). It is
  snake_case rather than kebab because a container name becomes a global
  identifier in some federation output formats; the plugin *id* stays
  kebab-case, and the manifest carries the mapping.
*/
export default defineConfig({
  plugins: [
    vue(),
    federation({
      name: 'example_plugin',
      filename: 'remoteEntry.js',
      exposes: {
        // The one entry point. Everything the shell renders comes from the
        // `components` record this module exports (the same shape the
        // built-in demo catalog exports).
        './plugin': './src/plugin.js',
        // The activate()-throws variant (1b-4) — a second exposed module so
        // the failure is a *curated registry entry*, not a query parameter
        // the browser can forge (BR-AS01).
        './plugin-activate-throws': './src/plugin-activate-throws.js',
        // The slow variant (1b-4): the same components behind a module-scope
        // delay, so the pending-region animation can be watched for as long as
        // it takes to look at it.
        './plugin-slow': './src/plugin-slow.js',
      },
      shared: {
        // Singletons: two Vue instances in one page would give the plugin its
        // own reactivity system, and `inject` across the boundary would stop
        // working. The shell owns the version (BR-AS09).
        vue: { singleton: true, requiredVersion: '^3.5' },
      },
      /* No .d.ts generation or consumption. There is no TypeScript here, and
         the dts worker shells out to tsc against a tsconfig that does not
         exist — it takes the dev server down on startup. Typing the boundary
         is the plugin API's job (Phase 2), not federation's. */
      dts: false,
    }),
  ],
  resolve: {
    alias: {
      '@unifi-theme': fileURLToPath(new URL('../../../shared/unifi-theme', import.meta.url)),
    },
  },
  build: {
    // Federation needs a real ES module output and no eager inlining.
    target: 'esnext',
    minify: false,
    cssCodeSplit: false,
  },
  server: {
    // 7110 — the next free frontend port after the shell's 7109 (CLAUDE.md's
    // 7100-7199 range). strictPort so a silently-moved port cannot make the
    // shell's curated remote URL wrong in a way that looks like a plugin bug.
    port: 7110,
    strictPort: true,
    cors: true,
  },
  preview: { port: 7110, strictPort: true, cors: true },
})
