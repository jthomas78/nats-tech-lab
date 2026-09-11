# Micro-frontend plugins

The five `example-plugin*` directories are announced fixtures. `demo-catalog`
is different: it is the one curated/preloaded plugin and must not receive a
publisher credential or announcer lifecycle.

Create a served, announced plugin with:

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
docker compose -p poc --env-file ../environments/local-za-1.env -f compose.yaml -f ../global/compose.control.yaml down -v
../../nats/bootstrap-operator.sh --force
docker compose -p poc --env-file ../environments/local-za-1.env -f compose.yaml -f ../global/compose.control.yaml up -d --build
```

The signing seed remains a runtime read-only mount. It and the NATS credential
must never be copied into the plugin image.

## Previewing a plugin's contributions

Each plugin's dev server serves `/__preview` — every contribution it declares,
rendered on its own port with no shell running:

```bash
cd lab-shell/plugins/example-plugin
npm run dev            # http://localhost:7111/__preview
```

The page, the stub shell API and the sample props all live in
`shared/mfe-preview/`; a plugin adopts it with the single `previewHarness()`
line already present in every `vite.config.js` here. It is `apply: 'serve'`, so
it changes nothing about the federated build. See
`shared/mfe-preview/README.md`.
