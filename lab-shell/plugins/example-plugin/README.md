# Example Plugin (`lab-shell` micro-frontend)

The plugin BR-AS15 asks for: a real remote, built by its own toolchain, served
from its own port, loaded by the shell at runtime from the curated registry —
and never compiled by the shell.

That last part is the whole point. A plugin the host builds would demonstrate
nothing about independent deployment, so this package deliberately has its own
`package.json`, its own Vite config and its own `node_modules`. The shell has
never heard of it; it learns the URL from the registry document at boot.

## Run it

The plugin ships as its own image and comes up with the stack:

```bash
docker compose up -d example-plugin-frontend
```

from `demos/01-dictionary/`. Port **7111**, and the shell is on **7110** — both
are containers now, and the dev servers below publish the same two ports so a
curated remote URL is correct either way.

To iterate on the source instead, stop the container and run the dev server:

```bash
npm install && npm run dev
```

Also 7111 (`strictPort`) — the shell reaches it by URL, so a plugin that
quietly moved would present as an unreachable remote rather than a moved one.

Compose mounts `demos/01-dictionary/registry.json` into the registry service;
startup seeds only unseen ids and never reverts curation. This is also the single
seed file for local development. To update already-curated rows after changing
origins or manifests, run from `demos/01-dictionary/backend/mfe-registry-service/`:

```bash
go run ./cmd/seed-registry -file ../../registry.json
```

The file is `demos/01-dictionary/registry.json` relative to the repository root.
The service must be running and its configured origin allowlist must cover each
remote. Existing rows require this explicit operator update; restart does not
replace them.

Phase 4 uses the operator's NATS curation subjects, not REST writes. The CLI
mints an Admin credential from `http://localhost:7202` (`-url` / `ACCOUNTS_URL`)
and connects to `nats://localhost:4222` (`-nats-url` / `NATS_URL`). It preserves
revision checks and auditing, including the first write at revision zero.

Curation is operator state, not source: a development remote on `localhost` has
no business compiled into the platform's own service, and a registry that can
be changed without rebuilding either side is what BR-AS03 actually claims.

## Its container

`Dockerfile` here, built from this directory's own `package.json` and lockfile
— the shell's image copies none of it. The built `dist/` is served by the
shared Go host in `shared/mfe-plugin-host`, which every plugin now uses; the
per-plugin `nginx.conf` was deleted in Phase 15c, once the last plugin that
still ran nginx (`demo-catalog`) moved onto the host too. The host is a static
file server with **no `proxy_pass`-shaped route of any kind**: a plugin origin
with a route to accounts-service or the bus would be a second, unaudited door
beside the narrow one the shell keeps. Its route set is exactly `/healthz` plus
the asset root. Two behaviours carried over from the nginx it replaced are
load-bearing rather than boilerplate:

- A missing file is a **404, with no SPA fallback** — the
  `example-plugin-unreachable` entry demonstrates a fetch that 404s, and an
  `/index.html` fallback would answer 200 with HTML and fail later, in the
  wrong state.
- `Access-Control-Allow-Origin: http://localhost:7110` — federation loads
  `remoteEntry.js` and its chunks as ES modules, fetched in CORS mode, which
  Vite's `cors: true` supplies in development and the host has to state
  explicitly. Named origin rather than `*`: the allowlist on the registry side
  is only half a statement if this side hands its code to anyone. An unset
  allowed origin is a startup error, not a permissive default.

## What it contributes

One of every contribution kind, which is what makes it a proof rather than a
demo:

| Kind | Contribution | Where it lands |
| --- | --- | --- |
| `route` | `overview` | `/example` |
| `navigation` | `overview-nav` | the shell's Features group |
| `extension` | `home-panel` | `shell/home-main/v1` — a **shell-owned** point |
| `extension` | `demo-sidebar` | `demo-catalog/details-sidebar/v1` — a **plugin-owned** point (the cross-owner case) |
| `shell-control` | `stream-toggle` | the topbar, on `/example` only |
| `shell-footer` | `version` | the footer bar |

## Independent fixture services

`demos/01-dictionary/registry.json` curates the plugin fixtures. Four of them are broken on purpose —
one per state the shell has to render — and each is broken in a different
place, because "the plugin is unavailable" is not one state:

| Entry | State | Broken where |
| --- | --- | --- |
| `example-plugin-slow` | `loading`, then `active` | a six-second delay in the remote's module scope |
| `example-plugin-unreachable` | `failed` | a curated URL with no chunk behind it |
| `example-plugin-activate-throws` | `failed`, cause `activate-threw` | the chunk arrives; `activate()` throws |
| `example-plugin-incompatible` | `incompatible` | `shellApiVersion: 2` — rejected on metadata, its remote never fetched |

The healthy plugin stays on 7111; slow, activate-throws and incompatible have
independent services on 7113, 7114 and 7115. Each exposes only `plugin`, and
each serves its own `/manifest.json`. Unreachable has no service of its own.

A fifth failure — a contribution throwing while it *renders* — is inside the
healthy plugin: `render-throws` sits next to `home-panel` on the home screen,
so the isolation claim is visible as a broken card beside a working one rather
than argued in prose.

**These are curated registry entries, not switches in the URL.** A remote
nominated by a query parameter, a `postMessage` or a message payload is refused
and never fetched (BR-AS01), so a failure mode the browser could select would
contradict the rule it is meant to demonstrate. Choosing which of the five the
shell sees is an operator edit to the registry file.

## The no-host-rebuild proof

```bash
cd lab-shell && node tools/hostBundleFingerprint.mjs --record
# edit something visible here, then:
npm run build
cd .. && node tools/hostBundleFingerprint.mjs --verify
```

The change is on screen after a reload; the host bundle's digest is unchanged,
and the check also asserts the shell's own output contains no plugin name,
container name or remote URL anywhere.
