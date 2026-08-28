# Example Plugin (`lab-shell` micro-frontend)

The plugin BR-AS15 asks for: a real remote, built by its own toolchain, served
from its own port, loaded by the shell at runtime from the curated registry —
and never compiled by the shell.

That last part is the whole point. A plugin the host builds would demonstrate
nothing about independent deployment, so this package deliberately has its own
`package.json`, its own Vite config and its own `node_modules`. The shell has
never heard of it; it learns the URL from the registry document at boot.

## Run it

```bash
npm install && npm run dev
```

Port **7110** (`strictPort`) — the shell reaches it by URL, so a plugin that
quietly moved would present as an unreachable remote rather than a moved one.
The shell itself is on 7109.

To point the shell at it, seed the curated document in this directory into a
running `accounts-service` (Phase 2a — curation is rows now, not a mounted
file):

```bash
go run ./cmd/seed-registry -file ../../../../lab-shell/plugins/example-plugin/registry.dev.json
```

from `demos/01-dictionary/backend/accounts-service/`. The service must already
be running, and `REGISTRY_ALLOWED_ORIGINS` must include `http://localhost:7110`
or every entry here is refused (BR-AS20) — compose sets it.

Curation is operator state, not source: a development remote on `localhost` has
no business compiled into the platform's own service, and a registry that can
be changed without rebuilding either side is what BR-AS03 actually claims.

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

## The four failure states, on demand

`registry.dev.json` curates five entries. Four of them are broken on purpose —
one per state the shell has to render — and each is broken in a different
place, because "the plugin is unavailable" is not one state:

| Entry | State | Broken where |
| --- | --- | --- |
| `example-plugin-slow` | `loading` | a six-second delay in the remote's module scope |
| `example-plugin-unreachable` | `failed` | a curated URL with no chunk behind it |
| `example-plugin-activate-throws` | `failed`, cause `activate-threw` | the chunk arrives; `activate()` throws |
| `example-plugin-incompatible` | `incompatible` | `shellApiVersion: 2` — rejected on metadata, its remote never fetched |

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
