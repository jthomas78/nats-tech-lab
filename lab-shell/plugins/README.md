# Micro-frontend plugins

Two folders, split by **role**, never by catalogue source:

| Folder | Holds | Lifecycle |
| --- | --- | --- |
| `shell/` | `demo-catalog` — the one plugin the shell owns | curated/preloaded; must not receive a publisher credential or announcer lifecycle |
| `fixtures/` | the five `example-plugin*` directories, plus anything `new-plugin.sh` creates | announced from the cell's dedicated compose band |

`plugin-source` (`build` / `registry`) is a setting of the running shell, not a
property of a plugin, so it is never encoded in a path (decision 11 of the
app-shell plan). Domain and demo plugins live with their owning demo under
`demos/<demo>/frontend/`, not here. The generated `demos/README.md` lists them.

`fixtures/example-plugin` has two jobs: it is the healthy fixture, and it is
the template `new-plugin.sh` copies. Change it with both in mind.

Create a served, announced fixture plugin with:

```bash
./scripts/new-plugin.sh acme-widget 7116
```

The command copies the migrated `example-plugin` shape, keeps the new plugin's
own package files and build, adds its single Compose service, adds it to the
publisher bootstrap list, updates the registry origin/health mappings, reserves
its release volume, and adds the README port row. Run it from the repository
root, then review the copied proof-plugin contributions and replace them with
the new plugin's real surface.

After scaffolding, regenerate the operator fixtures before starting the stack:

```bash
cd demos/01-dictionary/deploy/cell
docker compose -p poc --env-file ../environments/local-za-1.env -f compose.yaml -f compose.dedicated.yaml -f ../global/compose.control.yaml down -v
../../nats/bootstrap-operator.sh --force
docker compose -p poc --env-file ../environments/local-za-1.env -f compose.yaml -f compose.dedicated.yaml -f ../global/compose.control.yaml up -d --build
```

The signing seed remains a runtime read-only mount. It and the NATS credential
must never be copied into the plugin image.

## Previewing a plugin's contributions

Each plugin's dev server serves `/__preview` — every contribution it declares,
rendered on its own port with no shell running:

```bash
cd lab-shell/plugins/fixtures/example-plugin
npm run dev            # http://localhost:7111/__preview
```

The page, the stub shell API and the sample props all live in
`shared/mfe-preview/`; a plugin adopts it with the single `previewHarness()`
line already present in every `vite.config.js` here. It is `apply: 'serve'`, so
it changes nothing about the federated build. See
`shared/mfe-preview/README.md`.
