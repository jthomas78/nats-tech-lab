---
adr: 55
title: Split the Lab Compose File into Cell Tiers, and Make Every Host Port a Variable
status: Accepted
date: 2026-09-08
scope: lab
context: platform
decision: The one flat docker-compose.yml becomes three tier files under deploy/cell/ (infra, runtime, dedicated) plus an include-only compose.yaml, and one file under deploy/global/ (control). Every published host port is a ${VAR:-default} fed by one env file per cell, and container_name is removed everywhere.
why: A second local region could not start. Every host port and every container name was a literal, so two cells collided on both. Files split by when a thing ships; projects split by where it runs.
related: [52, 54]
applies: [54]
---

# ADR-055: Split the Lab Compose File into Cell Tiers, and Make Every Host Port a Variable

**Status:** **Accepted 2026-09-08** — both bands are implemented. `deploy/cell/` holds infra, runtime and dedicated; `deploy/global/` holds the control plane. The hub NATS `gateway {}` config is still not written.
**Date:** 2026-09-08
**Deciders:** Jeremy (repo owner)
**Governed by:** [ADR-054](../v3/ADR-054-v3-platform-portability-rules-multi-region.md) — the Proposed Linebooker V3 portability rules. This ADR is rule 1 (one image, config from environment) and the multi-region shape applied to the lab's local stack.
**Related:** `demos/01-dictionary/diagrams/multi-cluster-and-region/multi-region-control-plane-topology-3.html` (figure 4 maps each file to its Helm/Argo counterpart); `demos/01-dictionary/diagrams/multi-cluster-and-region/nats-network-topology-2.html` (figure 2 maps each file to the NATS parts it creates); [ADR-052](ADR-052-lab-data-one-postgres-instance-database-per-service.md); `CLAUDE.md` § "Docker Host Port Allocation"; `.claude/memory/compose_split_aws_deployment_decision.md`

## Context

`demos/01-dictionary/docker-compose.yml` was one 992-line file with 26
services, `name: poc`, a `container_name:` on every container, and 25
published host ports written as literals (`4222:4222`, `7100:80`, and so on).

That shape works for exactly one running copy. The target topology
(revision 3) is a small global band plus **N regional cells**, and the agreed
sequence is to prove two cells on a laptop before any AWS work. Two cells on
one Docker engine cannot both bind `4222`, and cannot both create a container
called `lb-nats`.

So the blocker was not conceptual. It was two literals.

A second, smaller force: the file mixed things with very different release
cadences. NATS and Postgres own data and are dangerous to replace. The Go
services own none and are replaced constantly. The five micro-frontend plugin
fixtures are test scaffolding and are not part of a cell at all. One file gave
no way to say any of that.

## Decision

**Two axes, kept separate.**

- **Files = when you ship it.** Four tier files, in **two bands** under
  `demos/01-dictionary/deploy/`:

  | File | Band | Holds | Owns data |
  |---|---|---|---|
  | `cell/compose.infra.yaml` | cell | `nats`, `postgres`, `temporal-postgres`, `temporal`, `temporal-ui` | yes |
  | `cell/compose.runtime.yaml` | cell | the six business services, the five real frontends, `registry-publisher-seed`, `jaeger`, `otlp-bridge` | no |
  | `cell/compose.dedicated.yaml` | cell | `mfe-plugin-host` and the six plugin fixtures | no |
  | `global/compose.control.yaml` | global | `accounts-service` — the provisioner | no |

  `cell/compose.yaml` is `include:`-only and lists the first two.
  `compose.dedicated.yaml` and the global control file are both deliberately
  left out of it.

- **Projects = where it runs.** `-p lb-za-1` and `-p lb-au-1`. Both run the
  **same files**; only `--env-file` differs.

**One hub per trust domain. The mesh has one. A sovereign cell is its own.**

That is why `accounts-service` is in the global band and not in a cell. A cell
is a region, and a region does not mint its own accounts: the trust tree is
minted once, offline, and held by Linebooker ([ADR-054](../v3/ADR-054-v3-platform-portability-rules-multi-region.md)
rule 5), and every regional server resolves against it. So the thing that mints
accounts is a property of the **trust domain**, not of the region. Two cells
running two provisioners against one resolver directory is the same writer
twice, not high availability.

The two drawings do not disagree about this, which was the earlier reading.
`nats-network-topology-2.html` draws the **mesh** — one hub.
`c4-linebooker-v3-deployment-production.html` draws the shape a **sovereign**
cell needs — a local control-plane slice. Botswana is the sovereign case:
`L2-020` gives it no gateway path for regulated tenant accounts, so per
`nats-network-topology-2.html` it is "a second trust domain, not a third
cluster in this mesh". It therefore gets its own hub — by running **the same
file** in its own project:

```
the mesh      docker compose -p lb-global \
                --env-file ../environments/local-za-1.env \
                -f compose.control.yaml up -d

a sovereign   docker compose -p lb-bw-1 \
cell            --env-file ../environments/local-bw-1.env \
                -f compose.yaml -f ../global/compose.control.yaml up -d
```

No second copy and no fork. The env file is the only difference — the same
trick the cell band already proves.

**Every published host port is `${VAR:-default}`.** The default is the value
the flat file used, so `deploy/environments/local-za-1.env` reproduces the old
stack port for port. `local-au-1.env` offsets by **+50** inside the two fixed
bands `CLAUDE.md` pins (`7100-7199` frontends, `7200-7299` backends), so za-1
takes the low half of each band and au-1 the high half and neither leaves its
band, and by **+100** for the conventional ports (NATS, Postgres, Temporal UI,
Jaeger) which sit in no band.

The same variables substitute into the origin strings that carry a port —
`REGISTRY_ALLOWED_ORIGINS`, `REGISTRY_FETCH_ORIGINS`, `ASSET_ALLOWED_ORIGIN`,
`PLUGIN_PUBLIC_ORIGIN`. A port variable that reached `ports:` but not those
would produce a cell whose plugins load and whose registry refuses them.

**`container_name:` is removed from all 25 containers**, and `name: poc` is
removed from the project. Compose then names every container, network and
volume after the project, so `lb-za-1` and `lb-au-1` are fully disjoint.

## Options Considered

**A — Split by tier, parameterise ports (chosen).** Four files, one env file
per cell.

**B — Keep one file, use Compose profiles per cell.** Profiles select
services; they do not remap ports or rename containers, so the actual blocker
survives untouched.

**C — Go straight to Kind/k3d locally, skip the Compose split.** Correct
long-term shape and rejected on 2026-09-08 as Option A of the Compose/Helm
decision: it means fighting NATS storage, secrets and Helm at once with no
proof the tier split is right.

## Trade-off Analysis

**What the split costs.** Five files instead of one. A service now has a
correct home and a wrong home, and putting it in the wrong one is a new class
of mistake that a single file could not make.

**What holds it down.** Three constraints forced themselves during the work
and are now load-bearing, not preferences:

1. **The plugin fixtures cannot be separated.** `example-plugin-frontend`
   defines the YAML anchor `&plugin_dependencies` that three others use, and a
   YAML anchor does not cross a file boundary. `mfe-plugin-host` is in the same
   file for the same class of reason: the plugins name it as a build context
   with `service:mfe-plugin-host`.
2. **Relative paths are now relative to `deploy/cell/`.** Build contexts became
   `../../../..` for the repo root and bind mounts became `../../nats/...`.
   Compose resolves an included file's paths against that file's own directory,
   and all four tier files sit beside `compose.yaml`, so this is unambiguous.
3. **`nats-logs` is shared across two tier files.** `nats` writes it,
   `observability-service` reads it. It is declared in both files identically;
   Compose merges the top-level `volumes:` section after the include.
4. **`deploy/global/` sits at the same depth as `deploy/cell/`.** Every
   relative path in the control file — `context: ../../../..`, the three
   `../../nats/...` bind mounts — is therefore unchanged by the move. That
   depth equality is the reason the move cost nothing.
5. **A host port can hide in a data file, not only in compose.** `registry.json` is mounted
   read-only and is the same bytes in every cell, so its literal `7112` survived the split and broke
   au-1. The preload file is *configuration*, and it now carries `${VAR:-default}` like everything
   else — see **BR-AS74** in `BUSINESS_RULES-APP-SHELL.md`. Check every mounted data file for a port,
   not only the compose files.
6. **A `depends_on` cannot name a service in another project.** Compose fails
   the whole project with `invalid compose project`, and `required: false`
   does **not** relax it (tested, Compose v5.4.0 — it only relaxes a service
   that exists but has not started). So the control file names no cell
   service, and the five cell services that named `accounts-service` no longer
   do. `restart: on-failure` replaces the ordering in both directions, which
   is the idiom three of those five frontends already used for the same nginx
   DNS race. `registry-publisher-seed` moved from `restart: "no"` to
   `restart: on-failure` for the same reason; the seed is idempotent.

**Proof the split changed nothing.** `docker compose config` was resolved for
the old flat file and for `compose.yaml` + `compose.dedicated.yaml` under
`local-za-1.env`, and the two are **byte-identical** once `name:` and
`container_name:` are removed. The two cells' resolved configs publish 25 host
ports each with **zero** overlap.

## Consequences

- Two cells can run side by side on one laptop, which is what the AWS work
  needs proved first.
- The old flat `demos/01-dictionary/docker-compose.yml` is now a second
  definition of the same stack. It must be deleted, or reduced to a one-line
  pointer, before anyone edits either copy.
- `CLAUDE.md`'s port table becomes a **band** rule rather than a fixed list —
  a port belongs to a cell, not to the repo.
- The global band exists but is only half of it. `deploy/global/` still needs
  a hub NATS config with a `gateway {}` block; `nats/nats.conf` has no
  `cluster {}`, `gateway {}` or `leafnodes {}` block at all today, so every
  hub line will be additive to that file.
- **A cell alone no longer boots a whole stack.** Bring the global band up
  too, or the five services that talk to `accounts-service` retry until you
  do. That is the honest cost of the band split, and it is the same cost the
  AWS shape has.

**Two anomalies found by running both cells (2026-09-08), diagnosed 2026-09-08.**
An earlier revision of this ADR blamed both on the shared NATS trust
directories. **That was wrong, and the correction matters more than the
anomalies.** Four checks disproved it: both cells' `/data/jwt` hold four
**byte-identical** JWTs (4526, 7785, 1560, 4502 bytes) — which is what a
correctly shared trust tree looks like, not drift; there are no cross-cell
connections (`172.19.x` against `172.21.x`); both `PUBSUB` streams have the
same 8-subject shape; and each account JWT's own import list holds two tenant
imports and no self-import. One operator and one set of account JWTs across
many regional servers **is** how NATS multi-region works, and it is exactly
ADR-054 rule 5. The sharing is the design.

The two real causes:

1. **`PUBSUB` 2272 against 1136 is a container start-order race.**
   `RegisterRefdataNotify` (`backend/shipping-service/dictionary/internal/eventhandler/platform_notify.go`)
   bridges `evt.*.refdata.>` to `notify._platform.refdata.>` with
   `DeliverPolicy: jetstream.DeliverNewPolicy`. In za-1 the refdata seed ran
   **before** the bridge attached, so nothing was republished — envelopes
   carry `evt.` only. In au-1 the bridge attached first, so every seeded
   change was republished — `evt.` **and** `notify.`. Pre-existing, unrelated
   to the compose split, and it makes an observation count depend on start
   order.
2. **au-1's empty `KV_mfe-registry` is a hardcoded port in a data file.**
   `demos/01-dictionary/registry.json` names
   `http://localhost:7112/remoteEntry.js` literally. The matching allowlist
   *is* parameterised (`REGISTRY_ALLOWED_ORIGINS`), so au-1 allows 7161–7165,
   the preload origin is not on that list, and the entry is withheld — the
   service logs it. This one **is** a gap in the split: a port can hide in a
   data file, not only in compose.

**That hazard, now fixed (BR-AC46, 2026-09-08).** `nats/creds` used to be
mounted **read-write** into `accounts-service` — one writable host directory,
and two writers as soon as a sovereign cell ran its own control plane. It was
never observed to bite, and the fix was the one named here: a read-only mount
plus a separate writable path, not a per-cell trust tree.

The read side became a **PATH-style list**, `/etc/nats/creds:/var/lib/nats/creds`,
read left to right, first directory wins. That choice is why it cost almost
nothing: `Discover(credsDir string)` keeps its signature, so all four scanning
services (shipping, refdata, pricing, organizations) inherited the list without
a single call site changing. The write side is its own variable,
`NATS_CREDS_WRITE_DIR`, naming exactly one directory that is never the seed —
and the handler field was renamed `CredsDir` → `CredsWriteDir`, because a field
called `CredsDir` that must not be the seed directory is a trap.

The writable half is a **named volume, per Compose project**, so `lb-za-1` and
`lb-au-1` do not share minted tenants, the same way they do not share a
Postgres. Only the seeded accounts are common.

**A seventh load-bearing detail came out of this one: a correct compose file
can still fail at runtime, on ownership.** A named volume mounted at a path the
image does not contain is created `root:root 0755`, and `accounts-service` runs
as `app` (uid 1000) — the resolved config was right, every mount flag was
right, and the first mint failed with `permission denied`. Docker seeds a
volume from the image's own directory *including its ownership*, so the fix is
`mkdir` + `chown app:app` **before** the `USER app` line in the Dockerfile;
Compose has no way to set a volume's owner. This was found by minting a tenant,
not by reading `docker compose config` — which is the general lesson. Proven
live afterwards: `credstest.creds` written `0600 app:app` into the volume,
visible to `shipping-service` through its read-only mount of the same volume,
`git status` on `nats/creds/` clean, and the file removed again on suspend.

## Action Items

- [x] Create `deploy/cell/` tier files and `deploy/environments/local-{za-1,au-1}.env`.
- [x] Parameterise all 25 host ports and the four port-carrying origin settings.
- [x] Remove `container_name:` and `name: poc`.
- [x] Prove the resolved config is unchanged, and that the two cells do not clash.
- [x] Bring both cells up together: 17 containers each, 25 published ports each with zero overlap, two distinct NATS `server_id`s, 28 JetStream streams per cell with independent message counts, and a marker row written into za-1's Postgres is absent from au-1's.
- [x] Write `deploy/global/compose.control.yaml`, drop the control band from `cell/compose.yaml`'s `include:`, and prove all four shapes resolve (cell alone under both env files, global alone, and cell + global merged). The merged config adds the 44-line `accounts-service` block and removes nothing.
- [ ] ~~Give each cell its own trust tree~~ — **withdrawn.** The shared trust tree is correct; see the corrected diagnosis above.
- [x] Parameterise `demos/01-dictionary/registry.json`'s hardcoded `http://localhost:7112/remoteEntry.js`. Done as **BR-AS74**: `mfe-registry-service` expands `${VAR}`/`${VAR:-default}` in the mounted preload file before parsing it (`registry/internal/preload/expand.go`, 9 specs in `expand_test.go`), the file's origin became `http://localhost:${PLUGIN_CATALOG_PORT:-7112}/remoteEntry.js`, and `compose.runtime.yaml` passes `PLUGIN_CATALOG_PORT` through. Proven live: au-1's registry now logs `seeded=1 withheld=0` where it withheld the only preloaded plugin before.
- [x] Mount `nats/creds` read-only and give `accounts-service` a per-domain writable path. Done as **BR-AC46**: read-only seed bind everywhere (the writer included), a per-project `nats-creds-minted` volume read-write in `accounts-service` and read-only in all four scanners, `NATS_CREDS_DIR` as a PATH-style list, `NATS_CREDS_WRITE_DIR` for the single write target, and an `app`-owned `/var/lib/nats/creds` baked into the image. 10 specs in `shared/natstenants/discover_pathlist_test.go`; all four compose shapes still resolve; proven live in both cells.
- [x] Make `RegisterRefdataNotify` start-order independent, so an observation count does not depend on which container won. Done as **BR-063**: the bridge's ordered consumer moved from `DeliverNewPolicy` to `DeliverLastPerSubjectPolicy`, so a change published before the consumer exists is caught up rather than lost forever, and the catch-up is bounded by the corpus (contexts x typeKeys) instead of the stream's whole history — `All` would also have been deterministic but repeats one invalidation N times and grows without bound. The bridge now logs its own catch-up size. Proven live: both cells log `republished=8`, identical, and identical again after a restart — the same measurement that read au-1 2272 / za-1 1136 before. 3 specs in `platform_notify_test.go`; the package's suite dropped 11.7s -> 1.4s because the helper that existed to retry past the race no longer has to.
- [ ] Retire `demos/01-dictionary/docker-compose.yml`.
- [ ] Write the hub NATS `gateway {}` config (additive to `nats/nats.conf`, which has no `cluster {}`/`gateway {}`/`leafnodes {}` today).
- [ ] Move migrations out of service start into a one-shot job (they race at three tasks).
