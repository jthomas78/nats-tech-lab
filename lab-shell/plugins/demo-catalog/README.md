# Demo Catalog

An independent federated plugin on **7112**. It owns `/demos`, `/demos/:id`
and `demo-catalog/details-sidebar/v1`. The demo README is compiled here through
`?raw`; changing it requires only this plugin to be rebuilt.

From this directory: `npm ci && npm run build`. From `demos/01-dictionary/`:
`docker compose up --build -d demo-catalog-frontend`.

`activate(shellApi)` stores the frozen v1 API in plugin scope. Descendant views
use the local `ExtensionRegion` wrapper; neither views nor entry import host
runtime modules. Vue and the PrimeVue theme engine are shared as singletons through Module
Federation; the host owns theme configuration. The expose loader includes the
catalog stylesheet without requiring its index page to be opened.

`public/manifest.json` is this package's identity. Operator curation lives in
`demos/01-dictionary/registry.json`, the single seed file. From the registry
service directory, explicitly seed with
`go run ./cmd/seed-registry -file ../../registry.json`.
