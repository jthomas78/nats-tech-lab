# ADR-TBD: Where the plugin announcer runs

**Status:** Proposed
**Date:** 2026-09-02
**Deciders:** Jeremy (app-shell owner)

## Context

Phase 13e gave every announced MFE plugin its own announcer sidecar container.
Today the lab runs 5 plugin containers + 5 announcer containers. A plugin
developer therefore ships **two** runtime units and writes **two** Compose
stanzas (~45 lines for the announcer alone), plus a bootstrap loop entry.

What the sidecar actually owns (from `cmd/announce-plugin/main.go` and the
compose comments):

- one NATS connection on a per-publisher `<plugin>-announcer` credential
- one Ed25519 signing seed, mounted read-only, never in an image layer (decision 8)
- one build-owned manifest
- one persistent release counter (BR-AS67) in a named volume
- announce once at start; signed unregister on SIGTERM only (BR-AS54)
- `stop_grace_period: 30s` so a slow bus does not turn withdrawal into a kill

Constraints that must survive any change:

- **BR-AS54** — withdrawal only on an explicit authoritative action, never on
  crash or health failure.
- **BR-AS67** — the publisher owns a monotonic release counter; the registry
  never mints it.
- **Decision 8** — signing seed stays out of image layers and out of the NATS
  trust chain.
- **Credential naming rule** — one credential per holder, `nats.Name()` equals
  the credential name; the bootstrap comment explicitly rejects one shared
  announcer credential because it destroys attribution in the Connections panel.
- **13f acceptance** (`cmd/registry-acceptance`) drives `compose stop/start` of
  one plugin and asserts withdrawn → returned with release N / N+1 / N+2.
- Plugin remotes are on the `frontend` network only; announcers on `backend`.

Goal: fewer containers **and** fewer things a plugin developer must create.

## Options Considered

### Option A — The announcer moves into the plugin's own container (shared base image)

A shared Go image `mfe-plugin-host`: serves `dist/` (replicating
`nginx.conf`: `/healthz` no-store no-CORS, `try_files =404`, named CORS origin
with `Vary`), and runs the same announce/unregister client in-process from a
new package `shared/mferegistry/announcer`. A plugin with its own backend
imports the package directly instead of using the host image.

Plugin Dockerfile becomes:

```dockerfile
FROM node:24-alpine AS build
# ... npm ci && npm run build ...
FROM mfe-plugin-host:latest
COPY --from=build /repo/lab-shell/plugins/<id>/dist /srv
```

Compose has one stanza per plugin. Manifest is read from `/srv/manifest.json`
(no mount). Seed + creds stay as two read-only mounts. Release volume attaches
to the plugin container. SIGTERM to the plugin container = unregister.

| Dimension | Assessment |
|-----------|------------|
| Complexity | Med — one new Go image (~150 lines) + extract announcer package |
| Containers | 5 → 5 (announcers disappear) |
| Dev creates | 1 runtime unit, 1 Compose stanza, ~12 lines |
| Trust model | Unchanged per plugin: own key, own cred, own counter, own SIGTERM |
| BR impact | None. 13f acceptance passes as-is (stop/start the plugin container) |
| Team familiarity | High — same client code, same env vars |

**Pros:** Meets both goals. Per-plugin lifecycle preserved. This *is* the
sidecar pattern in Compose terms (Compose has no pod, so "same pod" = "same
container"). Same package serves backend-plugins later.

**Cons:** The plugin container must join the `backend` network to reach NATS,
so the public static origin and the NATS credential now share a network
namespace. Mitigation: the host serves only `/srv`, the seed/creds mount
outside it, no proxy routes exist. Acceptable for the lab; flag for a
production review. Also: nginx knowledge in `nginx.conf` is rewritten in Go
once (a variant keeps nginx behind a Go PID-1 supervisor, but two processes
in one container is what we are trying to avoid).

### Option B — One announce-manager container for all plugins

One `mfe-announcer` container reads a list of plugins (manifest, seed, creds
per row) and runs one worker per plugin: separate NATS connection, key, and
release file each.

| Dimension | Assessment |
|-----------|------------|
| Complexity | Med-High — worker supervision, per-worker retry, bounded concurrent shutdown, config reload |
| Containers | 10 → 6 |
| Dev creates | 1 runtime unit + 1 row in the manager config + 2 mounts |
| Trust model | Keys stay separate but **all live in one process** — larger blast radius |
| BR impact | **Breaks per-plugin withdrawal.** Stopping plugin A's container no longer unregisters A. Needs a new control path (desired-state config + reload, or a narrowly granted NATS command). Health may not be used (BR-AS54). 13f acceptance must be rewritten |
| Team familiarity | Med |

**Pros:** Fewest containers. Central place to see all publishers.

**Cons:** Loses the one property the sidecar exists for: a plugin's lifecycle
and its announcement are one unit. Needs new BR text for "how does one plugin
withdraw". A crash of the manager exits five publishers at once (no
unregister, correct per BR-AS54, but five entries go stale together).

### Option C — Keep the sidecars, template them (Compose `x-` anchor + scaffolder)

A YAML anchor `x-announcer` holds the 40 boilerplate lines. A plugin's
announcer stanza becomes ~8 lines (`<<: *announcer`, `PUBLISHER_ID`, three
mounts following a naming convention). A `make new-plugin id=...` script
scaffolds the plugin dir, both stanzas, the bootstrap loop entry, and the
health/allowed-origin mappings.

| Dimension | Assessment |
|-----------|------------|
| Complexity | Low — no Go changes |
| Containers | 10 → 10 |
| Dev creates | 2 runtime units, but ~20 lines total and one command |
| Trust model | Unchanged |
| BR impact | None |

**Pros:** Cheapest. Zero risk. The scaffolder is worth doing under **any**
option, because key minting, creds, bootstrap loop, `REGISTRY_HEALTH_ORIGINS`
and `REGISTRY_ALLOWED_ORIGINS` are per-plugin chores no topology removes.

**Cons:** Does not reduce container count. Only hides the second unit.

### Option D — No resident process: announce/unregister as one-shot jobs

`docker compose run announce <id>` at deploy; `... unregister <id>` to
withdraw. No sidecar. Release counter still in a volume.

| Dimension | Assessment |
|-----------|------------|
| Complexity | Low |
| Containers | 10 → 5 (+ short-lived jobs) |
| Dev creates | 1 runtime unit + 1 job stanza |
| Trust model | Unchanged per plugin |
| BR impact | BR-AS54 satisfied (a job is explicit). But withdrawal is no longer tied to the plugin stopping; 13f acceptance rewrites to drive jobs, not `compose stop`. `restart: unless-stopped` reconcile loop is lost — a wiped registry is not re-announced until someone re-runs the job |

**Pros:** Simplest runtime. Matches a CI/CD "deploy step" mental model.

**Cons:** Manual withdrawal; no self-heal after registry reset.

## Trade-off Analysis

The user's two goals pull apart only in Option B. A and D reduce containers
without touching the trust model; only A also keeps "plugin stops →
plugin withdraws", which is the behaviour Phase 5 and 13f were built to prove.
B trades that behaviour for four fewer containers and inherits a new design
problem (single-plugin withdrawal) that has no BR today.

The real DX cost is not the container. It is the ~45-line stanza plus the
bootstrap/keys/origins chores. Option C's scaffolder addresses those and is
orthogonal, so it should ship regardless.

## Decision (proposed)

**Option A, plus the scaffolder from Option C.**

1. Extract `cmd/announce-plugin` internals into `shared/mferegistry/announcer`
   (`Start(ctx, Config)`; owns connect, announce, counter, SIGTERM unregister).
2. Keep `cmd/announce-plugin` as a thin CLI over the package (unusual
   deployments, tests, `example-plugin-unreachable` which has no web server).
3. Add `shared/mfe-plugin-host` Go image: static server with the `nginx.conf`
   semantics + the announcer package.
4. Move the four served fixtures onto the host image; delete their announcer
   stanzas; add `backend` network to each. `example-plugin-unreachable` keeps
   the CLI form.
5. `scripts/new-plugin.sh` scaffolds dir, stanza, bootstrap loop entry,
   origins mappings, README port row.
6. Re-run `cmd/registry-acceptance` unchanged — it must pass as-is.

## Consequences

- **Easier:** one Dockerfile, one stanza, one `docker compose stop` per plugin.
  Backend-plugins later import the same package.
- **Harder:** the plugin origin is on the backend network. Document it in
  ARCHITECTURE-APP-SHELL.md as a lab trade-off and a production review item.
- **Revisit:** if a production shell runs on Kubernetes, the announcer becomes
  a real sidecar container in the same pod, and this package is its binary.
  The Go host then goes back to being optional.

## Action Items

1. [ ] Confirm business rules: no new BR is expected; BR-AS54/67 unchanged.
       Add a note under BR-AS67 that the counter file now lives in the plugin
       container's volume.
2. [ ] Add a phase entry (13h or 14) in
       `.claude/plans/Application-Shell-Microfrontend-Plan.md` with the design
       decisions above; stays PROPOSED until approved.
3. [ ] Extract package; keep `announce-plugin` `main_test.go` green.
4. [ ] Build `mfe-plugin-host`; port `nginx.conf` semantics with a test per
       location rule (`/healthz` no-store no-CORS; `=404`; named CORS + Vary).
5. [ ] Migrate four fixtures; delete four announcer stanzas; run 13f acceptance.
6. [ ] Scaffolder script + doc in `lab-shell/plugins/README`.
