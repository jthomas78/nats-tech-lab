# Module Federation: from the browser to this lab shell

This tutorial starts with the general mechanism, then follows `demo-catalog`
through this repository. After working through it, you should be able to explain
what a remote exposes, trace when its JavaScript loads, distinguish discovery
from loading, and find the right layer when a plugin fails.

The mental model is **a shell that learns where a separately built feature lives,
loads its public JavaScript module when needed, and renders it inside the shell's
existing application**. The feature has its own build; it does not get a separate
browser or an iframe. Its code runs in the same page as the shell.

The examples below distinguish general federation concepts from this project's
plugin contract. Source links describe the implementation inspected on
2026-09-25; dependencies and behavior can change after that date.

## 1. Start with what federation adds

An ordinary `import './Feature.vue'` makes a feature part of the application's
build graph. Lazy `import('./Feature.vue')` can put it in a separate chunk, but
the application build still knows and produces that chunk.

Module Federation lets another build own the feature. That build publishes
selected modules; the consuming application loads them at runtime. Shared
dependency coordination lets the two builds cooperate inside one running page.
See the [Module Federation concepts](https://webpack.js.org/concepts/module-federation/)
for the underlying model; the Webpack configuration on that page is not this
repository's Vite configuration.

| Term | Meaning | Example here |
|---|---|---|
| Host | The application consuming a remote module | `lab_shell` |
| Remote | A separately built provider of exposed modules | `demo_catalog` |
| Container | The runtime interface through which a remote supplies modules | The interface exported by its entry |
| Expose | A public module key, mapped to a source file by the remote build | `./plugin` → `./src/plugin.js` |
| Remote entry | JavaScript that provides access to the container | `http://localhost:7112/remoteEntry.js` |
| Federation runtime | Registers remote locations and resolves exposed modules | `@module-federation/runtime` |
| Share scope | Runtime bookkeeping for shared dependency providers | The shared Vue provider |

Host and remote are roles, not necessarily different frameworks or permanent
application categories. A general federated application can consume and expose
modules. In this lab, the shell consumes and the plugins expose.

An exposed module is an ordinary JavaScript module with exports. Federation does
not require it to be a Vue component. This project exposes an object containing
components and an optional activation function. The shell decides how to use
those exports; federation supplies them.

## 2. What the browser actually does

Loading an exposed module is asynchronous. The runtime locates the remote entry,
loads and initializes the container, coordinates shared dependencies, and asks
for the exposed module. Entry, implementation chunks, shared dependencies and
styles can require separate requests. The remote entry is not necessarily the
whole feature bundle.

Here, remote entries are ES modules. The adapter explicitly registers
`type: 'module'`; it does not ask the browser to treat ESM syntax as a classic
script. Browser module loading and evaluation are asynchronous, and a module's
top-level code can execute before the shell receives its exports. The exact
request order and chunk names are build/runtime implementation details: use the
Network panel to observe them rather than assuming one fixed waterfall.
[MDN's `import()` guide](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Operators/import)
explains the browser mechanism; the
[runtime API](https://module-federation.io/guide/runtime/runtime-api) adds remote
and shared-module coordination around it.

Federation does not supply this project's discovery service, menu policy,
permissions, activation lifecycle or error cards. It also does not sandbox
remote code. Application-level failure handling can contain rejected promises
and rendering errors; it cannot turn same-page JavaScript into an isolated
process or prevent an infinite synchronous loop from blocking the page.

## 3. The stack and the dependency-sharing contract

The [host package](package.json) declares these dependencies. The installed
versions below were observed when this tutorial was written, not promised by
the version ranges forever.

| Package | Declared range | Observed installed version |
|---|---|---|
| `vue` | `^3.5` | `3.5.39` |
| `vite` | `^7` | `7.3.6` |
| `@module-federation/vite` | `^1.21.0` | `1.21.0` |
| `@module-federation/runtime` | `^2.9.0` | `2.9.0` |

The Vite plugin transforms the builds and creates their federation integration.
The runtime performs dynamic registration and loading in the browser. Vue renders
the resulting components. Vue Router, Pinia and PrimeVue are supporting shell
dependencies; their presence does not mean every one is shared through federation.

Both the [host config](vite.config.js) and
[catalog config](plugins/shell/demo-catalog/vite.config.js) share `vue` and
`@primeuix/styled` as singletons. `requiredVersion` describes the acceptable
version range; `singleton` requests one shared instance. The runtime's selection
and incompatibility behavior depends on its configuration and version. Do not
read this as an unconditional promise that the host always wins, or that every
version mismatch is rejected. See the
[shared configuration reference](https://module-federation.io/configure/shared).

One Vue instance matters because components cooperate through rendering state,
reactivity and dependency injection. Separate Vue runtimes can break that
cooperation. The `@primeuix/styled` singleton lets remote PrimeVue components see
the theme state configured by the shell. Sharing Vue alone would not share that
styling store. The host also uses `resolve.dedupe` for local module resolution;
that is a separate build/test concern from federation's browser share scope.

There are two compatibility questions here: federation coordinates library
versions such as Vue, while the shell validates `shellApiVersion` in plugin
metadata. Passing one does not prove the other.

## 4. A minimal Vue/Vite example

These snippets illustrate the direct-entry pattern used locally. They are not a
replacement for the repository's manifests, validation or lifecycle loader.
They assume projects with Vue, Vite, `@vitejs/plugin-vue` and the federation
packages installed at compatible versions.

The remote publishes a public module:

```js
// Remote vite.config.js
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { federation } from '@module-federation/vite'

export default defineConfig({
  plugins: [vue(), federation({
    name: 'lesson_remote',
    filename: 'remoteEntry.js',
    exposes: { './plugin': './src/plugin.js' },
    shared: { vue: { singleton: true, requiredVersion: '^3.5' } },
    bundleAllCSS: true,
    dts: false,
  })],
  build: { target: 'esnext' },
  server: { port: 7112, strictPort: true, cors: true },
})
```

```js
// Remote src/plugin.js
import { defineComponent, h } from 'vue'

export const components = {
  greeting: defineComponent({
    setup: () => () => h('p', 'Hello from another build'),
  }),
}
```

The host config establishes sharing but names no build-time remotes:

```js
// Host vite.config.js
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { federation } from '@module-federation/vite'

export default defineConfig({
  plugins: [vue(), federation({
    name: 'lesson_host',
    remotes: {},
    shared: { vue: { singleton: true, requiredVersion: '^3.5' } },
    dts: false,
  })],
  build: { target: 'esnext' },
  server: { port: 7110, strictPort: true },
})
```

```js
// Host: illustrative lazy loader called when the feature is needed
export async function loadGreeting() {
  const runtime = await import('@module-federation/runtime')
  runtime.init({ name: 'lesson_host', remotes: [], shared: {} })
  runtime.registerRemotes([{
    name: 'lesson_remote',
    entry: 'http://localhost:7112/remoteEntry.js',
    type: 'module',
  }])
  const plugin = await runtime.loadRemote('lesson_remote/plugin')
  if (!plugin?.components?.greeting) throw new Error('Missing greeting component')
  return plugin.components.greeting
}
```

The host could use `defineAsyncComponent(loadGreeting)` in a Vue template. Its
federation build config supplies the sharing setup; the empty `shared` in this
runtime call does not mean the application has no shared dependencies. The real
adapter also caches runtime initialization and remote registration, while the
plugin loader coordinates repeated and concurrent requests.

Notice the three related strings: container `lesson_remote`, exposure
`./plugin`, and runtime request `lesson_remote/plugin`. A filename such as
`src/plugin.js` is the remote's internal implementation path, not the host's
public import address. The [exposes reference](https://module-federation.io/configure/exposes)
and [Vite integration](https://module-federation.io/integrations/build-tool/vite)
cover this configuration.

## 5. Three files that must not be confused

| File | Owner and purpose | How it relates to this shell |
|---|---|---|
| `remoteEntry.js` | Federation-generated JavaScript container entry | The direct entry loaded by this adapter |
| `mf-manifest.json` | Optional federation-generated metadata describing the remote, assets and sharing | Supported by federation tooling, but not the application's plugin contract or the direct entry used in this walkthrough |
| Plugin `public/manifest.json` | This application's plugin metadata: identity, API version, remote coordinates, routes, navigation and extension points | Describes what the shell may admit and where its contributions belong |

The [catalog's manifest](plugins/shell/demo-catalog/public/manifest.json) is
application metadata pointing to `remoteEntry.js`. It is not a renamed
`mf-manifest.json`. The curated registry carries application manifest data;
reading that data does not mean the browser has loaded the remote JavaScript.
The [federation manifest fields](https://module-federation.io/guide/advanced/manifest-fields.html)
and [manifest/snapshot guide](https://module-federation.io/guide/basic/manifest-snapshot)
describe the separate federation format.

Likewise, **preloading registry metadata is not preloading plugin code**. The
server can populate its registry at startup while the browser still waits until
a route or extension slot needs a component before downloading that plugin.

## 6. Follow `demo-catalog` through the project

First check the selected discovery mode. [pluginSource.js](src/shell/pluginSource.js)
reads `VITE_PLUGIN_SOURCE` once: unset or empty selects `registry`; `build`
selects a generated catalogue served from the shell's origin. An unknown value
fails boot. In build mode the discovery branch does not mint registry credentials
or connect to NATS. Both paths feed the same admission and loading machinery.
Build catalogue and same-origin plugin asset support live under
[tools/buildCatalogue](tools/buildCatalogue); a path-only remote URL is resolved
against the shell document by the adapter.

The sequence below describes **registry mode**, the default:

```mermaid
sequenceDiagram
    participant Seed as Registry preload
    participant Registry as Registry service
    participant Shell as Shell discovery and index
    participant Router as Route / slot
    participant Loader as Plugin loader
    participant MF as Federation runtime
    participant Remote as demo_catalog
    Seed->>Registry: Startup application manifests (unseen IDs)
    Note over Shell: Native frame paints before discovery
    Shell->>Registry: NATS frontend-plugins.read.v1
    Registry-->>Shell: Curated manifest document
    Shell->>Shell: Validate, allowlist, index contributions
    Shell->>Registry: Subscribe to changed hints; flush and catch up
    opt Later registry change
        Registry-->>Shell: notify._platform.mfe-registry.frontend-plugins.changed
        Shell->>Registry: Re-read authoritative document
        Registry-->>Shell: Current document
        Shell->>Shell: Reconcile admitted metadata
    end
    Router->>Loader: First visit to /demos
    Loader->>Loader: Check policy; share in-flight load
    Loader->>MF: registerRemotes(name, entry, type=module)
    Loader->>MF: loadRemote("demo_catalog/plugin")
    MF->>Remote: Load entry / required modules and negotiate sharing
    Remote-->>Loader: JavaScript module exports
    Loader->>Remote: activate(shellApi), if exported
    Remote-->>Loader: Activation completes
    Loader-->>Router: module.components.catalog
    Router->>Router: Demo readiness gate, then mount
```

1. **The registry learns the metadata.**
   [registry.json](../demos/01-dictionary/registry.json) contains the catalog entry
   and other entries. Server preload inserts previously unseen IDs; existing
   curated entries are preserved. The catalog is a separate remote on port
   `7112`, not source bundled into the host. Consult the actual registry file
   rather than assuming this is its only preloaded plugin.

2. **The shell discovers before it downloads plugin code.**
   [main.js](src/main.js) wires the native shell, loader and discovery session.
   [registryTransport.js](src/shell/registry/registryTransport.js) reads
   `api._platform.mfe-registry.frontend-plugins.read.v1`.
   [registrySession.js](src/shell/registry/registrySession.js) handles the initial
   read, subscription/catch-up gap and reconnect reads.
   [changeSubscription.js](src/shell/registry/changeSubscription.js) treats
   `notify._platform.mfe-registry.frontend-plugins.changed` as a hint to reread,
   not as the full authoritative registry. The registry's KV name is
   `frontend-plugins`; the browser's contract here is the service's NATS API.

3. **Metadata is admitted and placed.**
   [bootShell.js](src/shell/bootShell.js) validates each manifest, declares its
   extension points, updates the remote allowlist, and supplies admitted metadata
   to the [contribution registry](src/shell/contributions/contributionRegistry.js).
   The catalog declares `/demos`, `/demos/:id`, a navigation contribution and
   `demo-catalog/details-sidebar/v1`. Its route component names are `catalog`
   and `intro`. Metadata can produce a menu item before either component loads.

4. **The first use crosses into federation.**
   [shellRoutes.js](src/shell/routing/shellRoutes.js) creates lazy Vue Router
   records. Visiting `/demos` asks the
   [plugin loader](src/shell/loader/pluginLoader.js) for `demo-catalog`.
   It checks the allowlist and status, then delegates to the
   [federated adapter](src/shell/loader/federatedAdapter.js).
   The adapter initializes identity `lab_shell`, registers `demo_catalog` at
   `http://localhost:7112/remoteEntry.js` with `type: 'module'`, and requests
   `demo_catalog/plugin`. It accepts either `plugin` or `./plugin` in the
   application manifest by removing the leading `./`.

5. **Activation connects the plugin to the shell's narrow API.**
   [plugin.js](plugins/shell/demo-catalog/src/plugin.js) exports
   `components = { catalog: MenuView, intro: DemoIntroView }` and
   `activate(shellApi)`. The loader passes a shared API with `version: 1` and
   `ui.ExtensionRegion`; both API containers are frozen. The Vue component
   definition itself is not frozen. The catalog stores the API through its
   [shellApi.js](plugins/shell/demo-catalog/src/shellApi.js), allowing its
   extension-region wrapper to use the host implementation. This API does not
   expose the shell's NATS connection.

6. **Vue mounts the named component.**
   The route selects `module.components.catalog` or `module.components.intro`.
   The [demo gate](src/shell/demos/demoGate.js) runs after module loading and
   before component mount. An associated demo must be ready; a plugin with no
   associated demo passes through. Readiness is a separate question from whether
   federation successfully loaded JavaScript. The gate does not continuously
   recheck an already running component.

The contribution registry answers **what is allowed to appear, and where**.
The plugin loader answers **when code is loaded and activated, and what status a
failure produces**. The federation adapter answers **how to obtain that module**.
Keeping those jobs separate lets the shell index features without importing
them, and lets loader policy be tested without a real remote server.

## 7. Failure handling and deployment details

| Symptom or condition | Relevant layer and behavior |
|---|---|
| Invalid manifest or unsupported shell API | Validation marks the plugin incompatible before fetching its code; `unsupported-shell-api-version` is the API-version reason code |
| URL not admitted | The loader's allowlist check prevents the adapter load |
| Missing entry/chunk or failed remote load | Plugin-local failure, commonly `chunk-load-failed`; route/slot UI can show an error |
| Load never settles | Shell load deadline is 20 seconds: `load-timeout` |
| Activation throws or never settles | `activate-threw`, or `activate-timeout` after 10 seconds |
| Styles missing but components render | Inspect remote CSS loading and the shared theme store |
| Demo backend unavailable after code loads | Inspect the readiness gate or feature's own backend behavior, not just federation |

The loader shares an in-flight request so concurrent consumers do not activate
the plugin independently. Successful modules are reused. Its retry handling
distinguishes a rejected activation from one that timed out and may still be
running; avoid reducing this to “activate can only ever be called once.” A
timeout stops the shell waiting, not the underlying JavaScript. It does not
cancel a browser module import or undo remote side effects.

[PluginSlot.vue](src/shell/ui/PluginSlot.vue) contains slot loading and rendering
failures; lazy route resolution supplies an error component and retry wrapper
when loading fails. Native shell screens remain usable when discovery fails.
These are application resilience features, not federation-provided isolation.

Cross-origin ESM needs suitable CORS responses on the entry and subsequent
module assets. A successful request to a remote's HTML homepage proves little:
the shell loads its JavaScript directly. Development `cors: true` in the catalog
config enables those requests locally; deployed asset serving must also supply
the necessary headers. See [MDN's module script documentation](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/script).

The catalog uses `bundleAllCSS: true` because its `index.html` is never opened
when consumed as a remote. `cssCodeSplit: false` alone does not ensure its styles
are attached by this federation path. Missing CSS and a separate
`@primeuix/styled` theme store are two different causes of an unstyled remote.

## 8. Guided exercises

### A. Trace the names without running infrastructure

Open the host and catalog Vite configs, catalog manifest and federated adapter
linked above. Locate `lab_shell`, `demo_catalog`, `./plugin`, `remoteEntry.js`
and the two route component names. Explain why changing the remote's source
filename need not change the manifest if its public expose remains `./plugin`.

Then read `pluginSource.js` and the source branch in `main.js`. Identify which
parts of the walkthrough remain the same in build mode. The answer should
include validation, contribution placement, the loader and route mounting.

### B. Observe a running stack

With your existing shell and catalog services running, open the shell on port
`7110`, then browser developer tools. Use a fresh page and inspect Network plus
the Plugins screen before and after visiting `/demos`. Filter for
`remoteEntry`, `7112`, JavaScript and CSS. Cache state and already-used extension
slots can change which requests you see; a plugin can be loaded by a slot before
you visit its route.

Set breakpoints in `federatedAdapter.load`, `pluginLoader`'s activation step,
and the catalog's `activate`. Follow the returned exports to the route component.
Visit `/demos/:id` using an actual catalog item and check that the already loaded
plugin is reused. NATS registry traffic travels over WebSocket, so it will not
appear as an HTTP GET of each plugin's `public/manifest.json` in this discovery
path.

This exercise assumes an already provisioned environment. For running demo 01's
services, follow [its own guidance](../demos/01-dictionary/CLAUDE.md); federation
alone does not provision its credentials or backend services.

### C. Run focused checks and builds

From the repository root, with dependencies already installed:

```bash
npm --prefix lab-shell test -- src/shell/loader/federatedAdapter.spec.js src/shell/loader/pluginLoader.spec.js src/shell/loader/pluginLoader.timeout.spec.js
npm --prefix lab-shell test -- src/shell/registry/manifestSchema.spec.js src/shell/registry/registrySession.spec.js src/shell/registry/changeSubscription.spec.js
npm --prefix lab-shell test -- src/shell/contributions/contributionRegistry.spec.js src/shell/routing/shellRoutes.spec.js src/shell/ui/PluginSlot.spec.js
npm --prefix lab-shell run build
npm --prefix lab-shell/plugins/shell/demo-catalog run build
```

The loader tests inject a runtime/adapter: they check policy and API calls,
not a real browser's federation handshake. Builds check bundling and produce
`dist` output; they do not prove deployed CORS or live NATS discovery. The browser
exercise supplies that different kind of evidence.

## 9. Debug in the order the feature becomes available

1. Check discovery mode and whether the plugin appears in the Plugins inventory.
2. Inspect its validation status and reason code before looking for a network bug.
3. Check admitted route/component names and permissions if navigation is missing.
4. Check that container name, remote URL and expose agree with the remote build.
5. Inspect entry and chunk requests for HTTP errors, CORS, stale assets or ESM
   being handled as a classic script.
6. If code loads, distinguish activation failure from a missing component export.
7. If mounting is held back, inspect demo readiness. If rendering looks wrong,
   check shared Vue, loaded CSS and the shared styling engine.
8. Use the shell's explicit retry/reload behavior. A changed remote URL is not a
   promise that already evaluated JavaScript will be unloaded and replaced.

## 10. Glossary and reading path

| Term | Keep this distinction in mind |
|---|---|
| Discovery | Obtaining application metadata; no remote module need be loaded yet |
| Admission | Shell validation/policy deciding which metadata may participate |
| Contribution | A declared route, navigation item or other supported placement |
| Extension point | A named place where accepted contributions can render |
| Activation | Optional application callback after loading, before use |
| Mount | Vue creates/renders a component instance; distinct from module evaluation |
| Preload | In this walkthrough, server-side registry seeding, not eager browser code loading |
| Singleton | A request to use one shared dependency instance in the share scope |

For a second pass, read the sources in this order:

1. [Module Federation concepts](https://webpack.js.org/concepts/module-federation/)
   — independent builds, containers and asynchronous loading; translate its
   Webpack-specific examples rather than copying them into Vite.
2. [Official Vite integration](https://module-federation.io/integrations/build-tool/vite)
   — the closest online starting point to this project's build stack.
3. [Exposes](https://module-federation.io/configure/exposes) and
   [shared dependencies](https://module-federation.io/configure/shared)
   — the public module boundary and dependency contract.
4. [Runtime API](https://module-federation.io/guide/runtime/runtime-api)
   — registration and loading; compare its calls with the local adapter.
5. [MDN dynamic import](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Operators/import)
   and [Vite production builds](https://vite.dev/guide/build)
   — browser evaluation versus generated assets.
6. [Federation runtime troubleshooting](https://module-federation.io/guide/troubleshooting/runtime)
   and [Vite troubleshooting](https://vite.dev/guide/troubleshooting)
   — consult after locating the failing layer.

Current upstream examples sometimes import `@module-federation/enhanced/runtime`
and use newer initialization APIs. This repository directly imports
`@module-federation/runtime` at observed version `2.9.0` and uses
`init`, `registerRemotes`, and `loadRemote`. Use upstream documentation to learn
the concepts, but check the installed package and local adapter before copying
an API or changing package names.
