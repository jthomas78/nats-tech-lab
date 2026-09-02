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
cd demos/01-dictionary
docker compose down -v
./nats/bootstrap-operator.sh --force
docker compose up --build
```

The signing seed remains a runtime read-only mount. It and the NATS credential
must never be copied into the plugin image.
