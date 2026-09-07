# `shared/mfe-preview` — contribution preview harness

Look at a plugin's contributions **without running the app shell**.

Every micro-frontend plugin under `lab-shell/plugins/` is its own app on its own
port, but its screens only ever appeared inside the shell: to see a topbar
control you booted the shell, waited for the registry, and navigated to the
route that control is scoped to. This harness adds one route — `/__preview` — to
a plugin's own dev server that mounts every contribution the plugin declares.

```bash
cd lab-shell/plugins/example-plugin
npm run dev            # then open http://localhost:7111/__preview
```

With the Compose stack up, those ports are taken by the plugins' own containers.
Use the `*-preview-715x` entries in `.claude/launch.json` instead.

## Adopting it

One line in the plugin's `vite.config.js`. Nothing else, and nothing copied:

```js
import { previewHarness } from '../../../shared/mfe-preview/vitePreview.js'

export default defineConfig({
  plugins: [vue(), federation({ /* … */ }), previewHarness()],
})
```

`scripts/new-plugin.sh` copies `example-plugin`, so a scaffolded plugin has it
already.

## What it does

- Reads the plugin's own `public/manifest.json` and lists every contribution,
  grouped by kind. A contribution missing from the manifest is missing here too
   — the preview cannot flatter a plugin the shell would not place.
- Imports the plugin entry and calls `activate()` once, with a **stub shell
  API** (`{ version: 1, ui: { ExtensionRegion } }`). `demo-catalog` refuses to
  activate without one. A plugin that throws in `activate()` still gets its
  components listed, under a banner saying what failed.
- Frames each contribution by where the shell would put it: a page, a column of
  extensions, a 44px topbar strip, a footer strip.
- Contains a render error in the contribution that threw, the way the shell's
  `PluginSlot` does, and shows the props it was given.
- Applies `shared/unifi-theme/unifi.css`, plus PrimeVue and the lab preset for
  plugins that declare them — so a contribution looks here as it looks there.

## Sample props

Placeholders are invented per kind: route params become `sample-<name>`, an
extension gets its target as `context.point`. When that is not good enough — a
view that looks a record up by id — add an optional `preview.fixtures.js` at the
plugin root, keyed by contribution id:

```js
export default { intro: { id: '01-dictionary' } }
```

Data only, no logic. `demo-catalog` has the only one.

## Boundaries worth keeping

- **Dev only.** The Vite plugin is `apply: 'serve'`, so it contributes nothing
  to `vite build` and `remoteEntry.js` is unchanged. The preview cannot become
  part of the thing it previews, and BR-AS03/BR-AS15 stay provable.
- **No routing, no shell chrome, no sibling plugins.** `<router-link>` renders
  inert, and an extension point drawn through the stub API is an empty labelled
  box. What the shell owns, the preview does not fake.
- **Vue is loaded asynchronously here.** Federation rewrites every bare `vue`
  import to its shared-scope loader, whose exports arrive a few ticks later, so
  no module in this directory may call a Vue API at module scope — see
  `federationReady.js` and the note at the top of `shellApiStub.js`.
