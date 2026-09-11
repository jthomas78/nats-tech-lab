# NATS Multi-Region — Requirements & Plan

> **SUPERSEDED IN PLACE 2026-09-09.** Everything below was built inside
> `demos/01-dictionary/` and has been **removed from it**. Demo 01 is one region,
> one NATS server, and stays that way. Multi-region now lives in
> `demos/02-multi-region/` as two Compose projects (`lb-za-1`, `lb-au-1`) on three
> shared networks, with no hub cluster. Read this file as the record of how the
> mechanics were worked out, not as instructions for demo 01. See
> `.claude/memory/multi_region_lives_in_demo_02.md`.

**Status:** DRAFT — topology axis specified; **Phase 64 PROPOSED, awaiting approval** (2026-09-08). See § 0 (Phase 16a) for the carried-in decisions.
**Date:** 2026-07-23
**Depends on:** Demo 01 — Dictionary POC (Shapes A/B/C, refdata-service)

---

## 0. Decisions carried in from Phase 16a (2026-07-31)

The Phase 16 tenancy/subject-taxonomy formalization settled several of the open
questions in § 1 below. Authoritative source:
`obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-COMMUNICATIONS.md`
§ 2.3 and `.claude/plans/Main-POC-Plan.md` § Phase 16.

**Region is a deployment axis, not a data or subject axis.** The agreed model
is a strict three-level hierarchy:

```
region                        — separate stack deployment, its own NATS instance
  └── tenant                  — NATS account (the only tenancy boundary)
        └── company / group    — {context} subject token
              └── business unit — hyphenated into the same {context} token
```

Consequences for this plan:

1. **Region never appears in a subject token or a context value.** A regional
   deployment implies its own region, so there is nothing to encode. This
   *reverses* the assumption in § 1's open question 3 below, which treated
   `emea-acme` / `apac-orient` as "naturally regional" context values — those
   were pre-Phase-16 names and are being migrated (Phase 16d) to region-free
   company contexts (`acme`, `acme-northdiv`).
2. **Cross-region propagation is a JetStream concern, not a subject-naming
   one.** Because the same tenant is the *same account* in both regions and
   context values are region-free, a subject in region A is byte-identical to
   its counterpart in region B. Moving data between them is therefore stream
   replication, not translation.
3. **Both regional and global platform deployments are expected.**
   `refdata-service` in particular needs a platform-wide `_platform` corpus
   (standards-based reference data, shared templates) alongside per-company
   contexts, so "one deployment per region" is not universal — the split
   between region-local and globally-shared data is a first-class question for
   this plan, not an afterthought.

**Recommended mechanism (for evaluation, not yet decided):** JetStream
**Mirror** (or **Source**, where multiple origins must be merged or
transformed) rather than gateway-propagated core pub/sub. Core pub/sub across a
supercluster is best-effort — if the inter-region link is down when a message
is published, a subscriber on the far side never sees it and there is nothing
to replay. A mirror tracks its own sequence and resumes after a partition:

```
region A:  stream SHIPPING          (subjects evt.*.shipping.>)
region B:  stream SHIPPING_MIRROR   mirror: { name: "SHIPPING",
                                              filter_subject: "evt.acme-northdiv.shipping.>" }
```

Note what the filter is and isn't: `acme-northdiv` is a **`{context}` value —
a company/business unit**, not a tenant. The mirror is configured *inside* a
given tenant's account on both sides, so the tenant is already implied and
appears nowhere in the filter; a subject filter can only ever narrow within one
account. Filtering is therefore how you mirror *part of* a tenant's data (one
company or business unit) rather than the whole stream — it is not, and cannot
be, the mechanism that separates tenants.

Both clusters must share the same operator/account resolver so a tenant is the
*same* account on both sides. Gateways and mirrors do not merge accounts — the
account isolation guarantee holds across regions exactly as it does within one
cluster.

**Verifying propagation for one tenant/context** — four techniques, roughly
increasing in strength:

1. `nats stream info <mirror> --json` → `mirror.lag` / `mirror.active`. First
   alarm, but a whole-stream figure unless the mirror is subject-filtered.
2. Compare filtered message counts / last sequence between origin and mirror
   for the specific `evt.{context}.>` slice. Trivial when the mirror is
   filtered to exactly that slice.
3. `GetLastMsgBySubject` on a known subject on both sides; compare sequence and
   timestamp. Per-message proof.
4. **Canary/heartbeat** (recommended for an SLA rather than an ad-hoc check):
   publish a synthetic message on a dedicated heartbeat subject in region A,
   poll for it on region B's mirror with a timeout. Measures true end-to-end
   propagation latency for that specific slice, independent of other traffic.

**Not built:** nothing in this repo configures gateways, leaf nodes, or any
multi-cluster topology today — `nats/nats.conf` is a single server, single
region. Everything in this section is forward design.

Still open from § 1: the *why* (latency vs. residency vs. DR), which topology
to actually evaluate, new-demo vs. extend-01, per-data-class consistency
requirements, and which failure modes to demonstrate.

---

## 1. Problem Statement — answered 2026-09-08

**The driver is evaluation fidelity on one machine, not production latency or
residency.** Stated by the user: *"The main goal was to replicate the structure
locally without need to deploy it to the cloud."* Every question below is
answered against that.

This reverses a deferral written into the compose band headers. Both
`deploy/cell/compose.yaml` and `deploy/global/compose.control.yaml` argue that
Compose namespaces a network per project, so a separate `lb-global` project
cannot reach a cell's `nats`, and then wave the problem off — *"In AWS that
problem does not exist ... so the honest local shape today is the sovereign
one."* **That escape hatch is withdrawn.** If local replication is the goal, the
local topology has to actually work, so the network namespacing must be solved
rather than waited out. See `.claude/memory/local_mesh_replication_is_the_goal.md`.

Answers to § 1's numbered questions:

1. **Why multi-region?** All three motives (latency, residency, DR) apply to V3
   eventually, but none of them is what this phase buys. What it buys is a
   *runnable* topology: the mesh in the topology drawing, on one Docker engine,
   so cross-region patterns can be evaluated and demonstrated without cloud
   spend or a deploy pipeline. Cloud deployment is explicitly **not** the
   target of this plan.
2. **Topology in scope.** **Superclusters / gateways** — a full mesh of three
   independent clusters — plus **real clustering (RAFT) inside each cluster**
   so JetStream R3 is genuine rather than drawn. Deliberately *not* in scope:
   **leaf nodes** (a different mechanism, and the drawing does not use one) and
   **a single cluster stretched across regions** (named and rejected in § 1's
   own list, and rejected again here — R3 quorum across a 250-300 ms link
   makes every write pay the link). **Mirrors and sources** stay deferred: § 0
   already recommends them and they are the *next* phase's subject, once
   servers exist to mirror between.
3. **New demo vs. extend existing?** Extend `01-dictionary`. The cell band under
   `demos/01-dictionary/deploy/` already models a region per Compose project
   (ADR-055) and already parameterizes every host port per cell. A standalone
   `02-multi-region` would fork all of that.
4. **Which data crosses regions?** Still open, and deliberately untouched here.
   This phase changes no service and moves no data. See § 5.
5. **Consistency requirements per data class.** Still open. Same reason.
6. **Failure modes to demonstrate.** Partially in scope: killing one server in a
   cluster and watching R3 keep quorum is a verification task below. Region
   partition, failover/failback and split-brain are deferred — they need
   mirrors, and honest partition testing needs latency injection this phase
   does not do.

---

## 2. Requirements

Confirmed 2026-09-08 for the topology phase only. Data-flow requirements
(§ 1 questions 4-5) remain unwritten on purpose.

### 2.1 Functional Requirements

- **FR-1** Nine NATS servers run locally, in three clusters, exactly as the
  topology drawing shows: `hub` (`nats-hub-1..3`), `za` (`nats-za-1..3`),
  `au` (`nats-au-1..3`).
- **FR-2** Each cluster is a real RAFT cluster over routes, not three
  standalone servers.
- **FR-3** The three clusters form a **full gateway mesh** — hub-za, hub-au and
  za-au — so it is a mesh of clusters, not a hub-and-spoke of servers.
- **FR-4** JetStream streams, KV buckets and Object Stores in `za` and `au`
  are **R3 within their own cluster**, and no stream's replicas cross a
  gateway.
- **FR-4a** Each cluster's JetStream is its **own namespace**. `za` and `au`
  both create a stream named `SHIPPING`, and in one supercluster a stream name
  is unique per account across every gateway-connected cluster. The two
  regional copies must therefore be addressable and creatable independently —
  see D9.
- **FR-5** The `hub` cluster carries **policy and summaries only**, as drawn. No
  business stream, no business service connects to it.

  > **What `hub` means in this plan, and what it does not.** *Pinned
  > 2026-09-08 after the word cost two round trips in review.*
  >
  > `hub` is **one tile** in the topology drawing — the
  > `nats cluster · 3 nodes` tile, which
  > `../diagrams/multi-cluster-and-region/multi-region-control-plane-topology-3.html`
  > labels `3 nodes · the gateway hub`. It is **transport**. Nothing else.
  >
  > The drawing nests three levels and this plan only builds the innermost one:
  >
  > | Level | Name in the drawing | Contents | Phase 64 builds |
  > |---|---|---|---|
  > | 1 | `GLOBAL CONTROL PLANE` | the whole top band, 10 tiles in 3 groups | no |
  > | 2 | `MANAGEMENT BACKBONE` | 2 tiles: a NATS cluster and a Postgres | partly |
  > | 3 | `nats cluster · 3 nodes` | **this is `hub`** | **yes — only this** |
  >
  > So `hub` is **not** the global control plane, and `hub` is **not** the
  > management backbone either — the backbone's other tile is a Postgres
  > holding placement and entitlement rows, and Phase 64 does not build it.
  > The band's other two groups — the six `PLATFORM CONTROL SERVICES`, and the
  > `TRUST MATERIAL` (offline operator NKey, account JWT set) — are wholly out
  > of scope.
  >
  > **The repo is inconsistent about this word, deliberately flagged rather
  > than silently fixed.** `deploy/global/compose.control.yaml`'s header opens
  > "THE CONTROL PLANE. One hub per trust domain", where `hub` means the whole
  > band — level 1, not level 3. That file predates the drawing and its
  > sentence is still true of the *deployment unit* it describes. The two uses
  > are reconciled by the control-plane phase (§ 5 question 1), which is when
  > the band's services and this cluster finally sit together and one of the
  > two words has to give. Until then: in **this plan** and in **the drawing**,
  > `hub` is level 3.
  >
  > Practical consequence, so this is not merely vocabulary: after Phase 64
  > the `hub` cluster runs with **no control-plane service attached at all**.
  > That is a working topology and a non-working control plane, and it is the
  > same point § 2.3 makes about the ask this work started from.
- **FR-6** Both existing cells keep working unchanged: every service and
  frontend connects to its own regional cluster, and every host port a
  developer already knows stays where the cell env file puts it.
- **FR-7** Account isolation still holds across gateways — a tenant is the same
  account in every cluster, and no tenant sees another's subjects.

### 2.2 Non-Functional Requirements

- **NFR-1** Runs on **one Docker engine on one developer Mac**. No cloud, no
  second host, no Kubernetes.
- **NFR-2** One command brings the mesh up, in the spirit of the cell band's
  "one cell, one command". *Revised 2026-09-08:* one **script**, since the
  revised D1 keeps three Compose projects and needs `docker network create`
  first. The script is the one command; it is idempotent and re-runnable.
- **NFR-3** No new host port may land inside the reserved bands (7100-7199
  frontends, 7200-7299 backends) — which the drawing's gateway port `7222`
  otherwise would.
- **NFR-4** Latency between clusters is **not** simulated. The drawing's
  `250-300 ms` za-au round trip will read as sub-millisecond locally, and that
  is accepted and documented rather than faked.

### 2.3 Out of Scope

- Cross-region **mirrors and sources** (the next phase; § 0 has the design).
- **Leaf nodes.**
- **Latency injection** (`tc netem`); it needs `NET_ADMIN` on every NATS
  container and buys nothing until mirrors exist.
- **TLS** on routes or gateways. The lab has never used it and adding it here
  would hide topology failures behind certificate failures.
- **Resolving "one hub per trust domain."** Today each cell runs its own
  `accounts-service`, so the stack is two sovereign cells, not one mesh with
  one control plane. This phase does not fix that — see D5 and § 5.
  *Flagged in review 2026-09-08:* this is a real mismatch with the ask that
  started this work ("two regions and one global control plane"), and it is
  worth saying out loud rather than only in D5. Phase 64 builds the **network**
  the control plane will need; it does not build the control plane. The hub
  cluster stands up with no minter attached, which is a working topology and a
  non-working control plane. Also note `deploy/global/compose.control.yaml`
  cannot yet run standalone in its own `lb-global` project — it references
  `nats`, `postgres` and `refdata-service` and declares none of them, which its
  own header comment already admits. Both are the next phase.
- Real region failover, failback, split-brain demonstration.
- Any change to what data lives where.

---

## 3. Target Topology

Replaces the earlier placeholder sketch. This is the topology drawing's content
stated as facts, and it is what Phase 64 must produce.

```
                 CLUSTER hub  — global management backbone
                 policy + summaries only, no business streams
                 nats-hub-1 ── nats-hub-2 ── nats-hub-3   (routes, full mesh)
                                  │
                      gateway ────┼──── gateway
                                  │
   CLUSTER za — af-south-1                CLUSTER au — ap-southeast-2
   cell za-1                              cell au-1
   nats-za-1..3, R3 inside za  ←gateway→  nats-au-1..3, R3 inside au
```

Per server: client `4222`, route `6222`, gateway `7222` — **container** ports,
identical on all nine. Only the host mappings differ (D4).

---

## 4. Plan / Phases

- [x] Phase 0 — Confirm requirements (topology driver, scope, consistency
      needs) with user. **Done 2026-09-08** for the topology axis; the data-flow
      axis (§ 1 questions 4-5) is still unconfirmed and is not part of Phase 64.

### Phase 64 — APPROVED 2026-09-08 — Local NATS Supercluster: Three Clusters, Nine Servers, Gateway Mesh

> **Design gate.** PROPOSED per CLAUDE.md's AI Agent Workflow: no tasks, no
> specs and no code until the user approves the design decisions below.
> Scope confirmed with the user 2026-09-08: **NATS servers only** — the app
> services and frontends are left exactly as they are.
>
> **Revised 2026-09-08 after an adversarial design review, before any code.**
> The review found the first draft unbuildable in two places and thin in two
> more. Changes, all on paper:
>
> | # | Was | Now |
> |---|---|---|
> | **D1** | one Compose project `lb-mesh` | three projects + one external `lb-gateway` network — the cells define services under identical names and cannot merge |
> | **D9** | *absent* | a JetStream **domain** on each of the three clusters — `za`, `au` **and `hub`** — without it both cells' `SHIPPING` streams collide silently in the supercluster and FR-4 is unenforceable |
> | **D10** | *absent* | one data / logs / resolver volume **per server** — the single shared `nats-data` volume would be written by three servers |
> | **D6** | streams + KV | streams + KV + **Object Store** (`organizations-docs` sets no replicas at all) |
> | **D8** | withdraws ADR-055's reasoning | amends it — the revised D1 keeps the per-cell network boundary intact |
> | **D3, 64c** | gateways unmapped, implicitly | gateways ride `lb-gateway` with a stable `.gw` alias per server, sequenced **before** 64c |
> | **§ 5** | 4 questions | q2 closed; q5 (mesh-aware Admin monitoring) and q6 (do account JWTs really not cross a gateway?) added |
> | **FR-5** | one sentence | plus a pinned definition of the word **`hub`** — it is one tile (transport), not the control-plane band, and the repo uses the word both ways |
>
> D9 is the important one. It is not a robustness nicety — the first draft would
> have produced a topology that looks correct in `nats server report gateways`
> and quietly runs one global `SHIPPING` stream instead of two regional ones.

#### Goal

Turn the topology drawing into a running local stack. Today
`demos/01-dictionary/nats/nats.conf` is a **single standalone server**: no
`cluster {}` block, no `gateway {}` block, `server_name` hardcoded to
`"nats-dev-1"`, and Compose runs one `nats` per cell. So there are two servers
where the drawing wants nine, no gateway anywhere, and R3 is arithmetically
impossible. Nothing about the drawing needs the cloud — clustering and gateways
are configuration — so the gap is unwritten config, not a missing capability.

#### Design decisions

**D1 — Three Compose projects, joined by one external gateway network.**
`lb-za-1`, `lb-au-1` and `lb-hub` stay separate projects, exactly as they are
today. One long-lived Docker network, `lb-gateway`, is created once by hand and
declared `external: true` in all three; **only the nine NATS containers join
it**, nothing else. Routes stay on each project's own `backend` network; the
gateway block dials across `lb-gateway`.

*Revised 2026-09-08 after review. The original D1 — one project, `lb-mesh`,
holding the hub and both cells — cannot be built.* Both cells define services
under the same names (`nats`, `postgres`, `shipping-service`, `refdata-service`
and the rest), the same volume names and the same `nats://nats:4222`. One
Compose project cannot hold two services called `nats`, so folding both cells
into one project requires renaming or duplicating **every service in the cell
band**, which is far outside "NATS servers only". The original D1 also put both
regions' Postgres instances on one shared pair of networks, which is a
tenancy-adjacent regression nobody asked for.

What the revision costs, stated plainly:

- **A manual step before the first `up`.** `docker network create lb-gateway`,
  once per machine. NFR-2's "one command" now means one command *per project*
  plus a documented wrapper script; the network create is idempotent and lives
  in that script.
- Nothing else. Routes and gateways still need **no host port**, because
  `lb-gateway` is a Docker network and container-to-container traffic never
  touches the host — so NFR-3 still holds and the drawing's `7222` is still
  never published (D3).

What it buys back: the region-isolation proof **strengthens** rather than
weakens. Each region keeps its own network namespace, its own Postgres, its own
volumes; the only thing crossing a region boundary is a NATS gateway link,
which is exactly the AWS shape. D8's withdrawal of ADR-055's reasoning is
therefore no longer needed — see D8.

**D2 — One config file per cluster, `server_name` injected per container.**
Three files (`nats-hub.conf`, `nats-za.conf`, `nats-au.conf`) rather than nine,
because within a cluster only the server name differs — routes and the gateway
block are cluster-wide. `nats-server` interpolates environment variables in
configuration, so `server_name: $NATS_SERVER_NAME` with a per-container value
covers it. Nine hand-maintained files would drift; one file with everything
templated would be unreadable. Routes list all three of the cluster's own
service DNS names; a server ignores its own route entry, so the list is
identical on all three.

> **Refined during 64a, 2026-09-08 — two files, not three, and the service
> names are cell-independent.**
>
> **One regional conf, not two.** `demos/01-dictionary/nats/nats-cluster.conf`
> serves both regional clusters. `nats-server` expands `$VARS` in any value, so
> the cluster name and the JetStream domain are variables too
> (`NATS_CLUSTER_NAME` in `deploy/environments/local-<cell>.env` supplies both).
> `nats-za.conf` and `nats-au.conf` would have been byte-identical apart from
> two words, which is exactly the drift D2 set out to avoid. The hub still gets
> its own file in 64b, because it genuinely differs: no websocket listener.
>
> **The Compose service names cannot carry the region.** `nats-za-1` was the
> intended service name. It cannot be: `compose.runtime.yaml` is the *same file*
> for both cells, and a `depends_on` there cannot name a cell. So the services
> are `nats-1`, `nats-2`, `nats-3` in both projects, and the region appears in
> `server_name` (`nats-za-1`) instead — which is the name that shows in
> `nats server list`, `/varz` and the Admin UI, so nothing is lost. D3's
> `.gw` aliases in 64c stay as designed; they are network aliases, not service
> names, so they can carry the region.
>
> **`nats-1` also carries the network alias `nats`.** Roughly 23 existing
> references (`nats://nats:4222`, `http://nats:8222`, each frontend nginx's
> `proxy_pass http://nats:9222/`) keep working untouched. It is a *seed*
> address, not a single point of failure — a NATS client learns the other two
> servers from the server's INFO and fails over by itself, because
> `shared/natsconn` sets `MaxReconnects(-1)`.
>
> **`nats.conf` turned out to be half generated.** `bootstrap-operator.sh` used
> to splice the `system_account` + `resolver_preload` tail into `nats.conf` with
> awk. With more than one config file that meant more than one copy of the trust
> chain. Every config file now carries a plain
> `include "resolver-preload.generated.conf"`; the script writes only the
> generated file and *checks* that each config includes it. As a bonus, the old
> awk left a duplicate `# Generated` comment behind on every run — `nats.conf`
> had accumulated seven.

**D3 — Routes and gateways stay on Docker networks, unmapped.**
`6222` and `7222` are reachable container-to-container by service name and are
never published to the host. Routes ride each project's own `backend` network;
gateways ride the shared `lb-gateway` network (D1). Because both are Docker
networks, NFR-3 holds and the drawing's ports need no renumbering — the
reserved 7200-7299 band is a *host* port band and nothing here binds a host
port.

One consequence of the three-project shape: a gateway's `urls` must name a
container the other project can resolve. Compose namespaces DNS per project, so
a plain `nats-za-1` will not resolve from `lb-hub`. Each NATS container
therefore carries a `networks.lb-gateway.aliases` entry giving it a stable,
project-independent name (`nats-za-1.gw`, `nats-hub-2.gw`, and so on), and
every gateway `urls` list uses those aliases only.

> **Found during 64c, 2026-09-08 — the ambiguity runs the other way too, and
> it broke the routes.**
>
> D3 saw that a *gateway* `urls` list needs a project-independent name. It did
> not see that sharing a network makes the *route* URLs ambiguous in the
> opposite direction. Compose registers a service's own name as a DNS alias on
> **every** network the service joins, and Docker's embedded DNS answers a
> lookup from all of a container's networks. All nine servers are named
> `nats-1`/`nats-2`/`nats-3` (D2's refinement — the service name cannot carry
> the region), so the moment 64c put all nine on `lb-gateway`, a plain
> `nats-1` stopped meaning "my own cluster's first server". From
> `lb-za-1-nats-1-1` it resolved to `172.23.0.9` — the **hub**.
>
> The clusters still formed, which is what made this dangerous: NATS rejects a
> route whose cluster name does not match (`Rejecting connection, cluster name
> "hub" does not match "za"`), so `/routez` looked correct while every server
> was dialling the wrong cluster in a loop and filling its log with conflicts.
> A cluster-name check was the only thing standing between the design and
> three silently interleaved clusters.
>
> The fix keeps D3 intact rather than working around it. Every server gains a
> second alias on **its own `backend` network only** — `nats-za-1.rt`,
> `nats-au-2.rt`, `nats-hub-3.rt` — and both confs' `routes` name those
> instead of the service name. The names are globally unique, so they cannot be
> confused; they are absent from `lb-gateway`, so route traffic still never
> crosses it. Verified: `nats-hub-1.rt` and `nats-au-1.rt` do not resolve at
> all from inside `lb-za-1`.
>
> One NATS detail forced the shape. NATS expands `$VAR` as a whole config
> token but **not** inside a string, so `nats://nats-$NATS_CLUSTER_NAME-1.rt`
> cannot be built in the conf file. The full URL is handed in whole instead, as
> `NATS_ROUTE_1`/`_2`/`_3`, and `nats-cluster.conf` reads `routes = [
> $NATS_ROUTE_1 … ]`.

**D4 — Host port plan.** Monitoring is exposed for all nine servers, because
`/varz`, `/routez` and `/gatewayz` on each one is how the topology gets
verified. Client ports are exposed per cluster for `nats` CLI work. WebSocket
stays only where a browser connects.

| | monitor (host) | client (host) | websocket (host) |
|---|---|---|---|
| `za` 1/2/3 | 8222 / 8223 / 8224 | 4222 / 4223 / 4224 | 9222 on `nats-za-1` |
| `au` 1/2/3 | 8322 / 8323 / 8324 | 4322 / 4323 / 4324 | 9322 on `nats-au-1` |
| `hub` 1/2/3 | 8422 / 8423 / 8424 | 4422 / 4423 / 4424 | none (FR-5) |

`za`'s and `au`'s first server keep the exact numbers their cell env files
already publish (`NATS_CLIENT_PORT`, `NATS_MONITOR_PORT`, `NATS_WS_PORT`), so
FR-6 holds and nothing a developer has bookmarked moves. The `+1`/`+2` siblings
and the whole `hub` block are new variables in a new
`deploy/environments/local-mesh.env`.

**D5 — Operator mode: shared trust tree, per-cluster resolver. Known
contradiction, deliberately not resolved here.**
All nine servers mount the same `operator.jwt` and declare the same
`system_account`, so a tenant is one account everywhere (FR-7, and § 0's
requirement that both sides share the resolver). Within a cluster the `full`
resolver's `interval` gossip propagates account claims across all three
servers. **Account JWTs do not propagate across a gateway** — so each cluster
needs its own resolver seed, and a tenant minted against one cluster is unknown
to the others until something writes it there. Today each cell runs its own
`accounts-service`, which means the stack already has two independent minters
and the `hub` would have none. That directly contradicts
`compose.control.yaml`'s own "one hub per trust domain". **Phase 64 leaves it
alone**: services are out of scope, and fixing it properly is a control-plane
phase of its own. It is recorded here so it is not rediscovered as a surprise,
and it is the first thing § 5 asks.

**Read FR-5's pinned vocabulary before this decision.** The contradiction above
is only legible once `hub` is fixed to one meaning. `compose.control.yaml`'s
"one hub per trust domain" is a statement about the **whole control-plane
band**; this phase's `hub` is the **NATS cluster tile inside it**. Phase 64
builds a hub in the second sense while leaving the first sense unbuilt — which
is exactly why the sentence reads as a contradiction. It is a vocabulary
collision sitting on top of a real, separate gap: two minters exist, and the
one place a single global minter would live has none.

**D6 — R3 is not free, and it is the one place this phase touches Go.**
`Replicas` is set **nowhere** in the tree — `grep -rn "Replicas" --include="*.go"`
returns nothing — so every stream and KV bucket is R1 by default and would stay
R1 on a nine-server mesh, making FR-4 fail silently while the topology looks
right. Nor is there one seam to change: `shared/jstream` owns `evt.*` stream
creation, but `CreateOrUpdateKeyValue` / `CreateOrUpdateStream` appear at
roughly ten more sites across `shipping-service`, `refdata-service`,
`organizations-service`, `observability-service` and `mfe-registry-service`.

**Object Stores count too, and were missed in the first draft.**
`organizations-docs` is a JetStream stream behind an Object Store API, created
at `organizations-service/organizations/internal/objectstore/objectstore.go`
with no `Replicas` field at all. R3 must reach `ObjectStoreConfig.Replicas`
along with the rest, or compliance documents stay R1 while every stream around
them reports three — the worst kind of half-migration, because the topology
check passes. If R1 is wanted here on purpose, that is a decision to write down,
not a default to inherit.
Proposal: a single `NATS_STREAM_REPLICAS` environment knob, read once per
service and threaded to those call sites, **defaulting to 1** so a standalone or
single-server run is unaffected and only the mesh env file sets 3. This is the
sub-task most likely to grow, and it is why it is sequenced last.

**D7 — Business rules: none new; verification is live plus integration specs.**
This phase adds no domain rule, so CLAUDE.md quality rule 1 has nothing to
attach a domain spec to. Verification is therefore live topology checks (64e)
plus, where a spec can assert it cheaply, a Ginkgo integration check that a
stream reports three replicas. **To confirm with the user:** that no business
rule is expected here, and that no `BUSINESS_RULES-*.md` file changes.

**D8 — ADR-055 needs an amendment, not a withdrawal.** *Revised 2026-09-08.*
The original D8 said D1 withdrew ADR-055's "AWS solves the network problem"
reasoning, because one merged project abandoned the per-cell network boundary.
The revised D1 keeps that boundary, so nothing in ADR-055 is withdrawn. What is
still needed is an **amendment** recording two facts ADR-055 does not yet
carry: that a gateway link between two cell projects needs one external Docker
network, created out of band; and that the last unticked item on ADR-055's own
list (the hub `gateway {}` config) is what this phase writes. No new ADR. The
cell band is not replaced and gains no third run mode — a mesh is the same three
projects with a gateway network attached, which is why open question 2 below is
now closed.

**D9 — A JetStream domain per cluster. This is the blocker the first draft
missed.** *Added 2026-09-08 after review.*

> **WRONG — measured and reversed 2026-09-09.** D9 and D11 below say a
> per-cluster JetStream domain (`za`, `au`, `hub`) is mandatory. It is not
> legal. A domain names a JetStream *system*; every server in a cluster **and
> in a supercluster** must carry the **same** domain name, and the name may
> only change across a **leaf-node** link. A gateway is not a leaf link.
> Configured as written here, it silently broke JetStream: setting a domain
> suppresses JetStream traffic on the system account, so the clusters never
> learned each other's server names, `au` never elected a meta leader, and
> `stream add --cluster au` failed with `no suitable peers for placement
> (10005)`. `demos/02-multi-region/` now runs ONE domain, `lb`, on all six
> servers. Regions are separated by **accounts**, and a stream is pinned to a
> region by **placement** (`--cluster za|au`). The problem D9 names is real —
> a stream name IS unique per account across a supercluster — but the fix is
> an account per region, not a domain per cluster. See
> `demos/02-multi-region/nats/nats.conf` and
> `.claude/memory/demo02_jetstream_domains_do_not_split_a_supercluster.md`.

A gateway link does not just forward subjects — it makes the connected clusters
one **supercluster**, and inside one NATS account a JetStream stream name is
unique across that whole supercluster. `za` and `au` both create a stream named
`SHIPPING`, both KV buckets named `ships` / `container` / `meta`, and both
against the same accounts. The moment a gateway comes up, the second cell's
`CreateOrUpdateStream` **finds the first cell's stream and updates it** instead
of creating a regional copy. There is no error. FR-4's R3 would then be three
replicas of one global stream, not two regional streams — the exact opposite of
what the drawing shows, and `Replicas: 3` cannot fix it.

The fix is a **JetStream domain on every cluster**: `domain: za`,
`domain: au`, `domain: hub` inside each conf's `jetstream {}` block, which
today reads `jetstream {}` with no domain in
`demos/01-dictionary/nats/nats.conf`. A domain gives each cluster its own
JetStream namespace, so `SHIPPING` in `za` and `SHIPPING` in `au` are two
different streams that happen to share a name. This is also the shape a later
mirror phase needs, because a mirror is defined as a source in another domain.

**All three, not just the two regional ones.** The collision D9 exists to stop
is a `za`-vs-`au` problem, so it is tempting to read this as "a domain per
region" and leave the hub alone. Do not. A server with no `domain` sits in the
unnamed default domain — legal, but it makes a supercluster where two clusters
are named and one is not, and nobody reading it later can tell whether that was
deliberate. More concretely, cross-domain access is expressed as a named API
prefix (`$JS.za.API.>`), so an unnamed hub cannot be addressed from anywhere
else — and reading a region's stream *from the hub* is the whole point of the
hub. FR-5 keeps business streams off it; it does not make it JetStream-free.

Consequences to accept with it:

- **Domains are the boundary FR-4 actually rests on.** With domains, no
  stream's replicas *can* cross a gateway; without them, FR-4 is unenforceable.
  FR-4a records this.
- **Service code is unaffected** as long as a service talks to its own regional
  cluster, which FR-6 already guarantees. A client with no domain configured
  uses its own server's domain. No `WithDomain` call is needed in Go for the
  ordinary path.
- **Cross-domain access is explicit and only the hub needs it.** Reading `za`'s
  stream from the hub means an API prefix (`$JS.za.API.>`), which is a
  hub-phase concern, not this one.
- **Verification changes shape.** `nats stream info SHIPPING` is now ambiguous
  and must be run per domain (`nats --domain za stream info SHIPPING`). 64e's
  checks are rewritten accordingly.

**D11 — A JetStream domain isolates the API, NOT the subject space. The
two-stream check fails once the gateways are up, and this needs a decision.**
*Found 2026-09-08 in 64d, and it is the most important finding in Phase 64.*

D9 established that a domain is mandatory so that `CreateOrUpdateStream` in one
cell cannot silently update the other cell's stream. That is true and it holds.
What D9 did not say — and what 64a could not detect, because there was no
gateway yet — is that the domain does nothing to the **subject space**.

Reproduced from a clean state, with the mesh up:

```
purge SHIPPING in za and in au         -> za = 0, au = 0
publish ONE message on the za cluster:
  evt.acme.shipping.ship.gwprobe.arrived
                                       -> za = 1, au = 1
```

One publish, two captured copies, in two clusters. The mechanism: a gateway
propagates **account interest** across clusters. `acme` is one account in one
operator's trust chain, present in all three clusters (D5). Both cells run a
`SHIPPING` stream whose filter is the same `evt.{context}.shipping.>`. So each
cluster's stream is a legitimate interested subscriber to the other cluster's
publish, and each captures it. The `za`/`au` domains kept the two streams
separately *addressable* — they did not keep them separate.

The consequence is bigger than a message count. Each cell also runs its own
`shipping-service` with its own durable consumer on its own stream, so an event
published in `za` is projected into **both** regions' Postgres and KV. That is
not replication — nothing chose it, nothing bounds it, and the two projections
would diverge the moment one cell was down.

Two ways out. This is a design decision, not a build step:

1. **Region-scoped subjects.** Give each region a distinct token so the two
   stream filters no longer overlap. Cheap and reliable, but it contradicts the
   existing rule that **region never appears in a subject token** (§ Subject
   families, and the `{context}` rule in CLAUDE.md), and it touches every
   publisher and consumer.
2. **One stream per supercluster, plus explicit mirrors.** Stop running the
   same stream in both cells. One `SHIPPING` lives in one cluster; the other
   region gets a JetStream **mirror** or sourced stream, which is a stated,
   directional, bounded copy. This matches what a supercluster is actually for
   and it makes 64e's replica work meaningful, but it is a real design phase,
   not a knob.

Option 2 is the recommendation: option 1 buys isolation by giving up a rule
that was chosen deliberately, and it still leaves two independent write sides
with no story for reconciling them.

Until this is settled, **64e should not start** — putting `R3` replicas under
two streams that are already double-capturing the same events makes the wrong
shape more durable, not more correct.

> **Option 3, from a Codex discussion, 2026-09-08 — noted, not decided.**
> *This is a record of input, not an approved design. Nothing below has been
> tested in this lab.*
>
> **An account per tenant-region — `acme-za`, `acme-au` — combined with
> selective mirroring or sourcing.** The claim is that options 2 and 3 are not
> alternatives: the account boundary supplies the isolation, and a mirror
> supplies the deliberate copy. Under it, each cell keeps identical
> application subjects inside its own account, streams are explicitly placed
> in the intended cluster, and exchange happens only through named
> cross-account sources or mirrors with a defined processing owner.
>
> How it maps onto the three options above:
>
> - **Option 1 (region in subjects)** — NATS permits geographic subject
>   tokens but the guidance prefers encoding business intent over deployment
>   detail. Valid only if region is genuinely part of the business addressing
>   model, and it still contradicts our own subject convention.
> - **Option 2 (mirrors)** — documented for exactly this job, distributing
>   stored events across locations and unreliable links. But a mirror alone
>   does not establish independent regional ownership.
> - **Option 3 (account per tenant-region)** — the account is the documented
>   isolation boundary, and its scope is left to the designer. It is the fit
>   for cells that must reuse subject names without receiving each other's
>   publications.
>
> Cited as supporting evidence: the official NATS leaf-node demo recommends
> roughly one account per application and then moves stream data across an
> account boundary; Synadia's e-commerce example puts a West Coast app and an
> East Coast warehouse in **separate accounts** and shares orders through a
> mirror; and NATS documentation says most cross-account JetStream cases
> should mirror or source into the receiving account rather than let it reach
> into the originating stream directly.
>
> **The stated qualification matters as much as the recommendation:**
> geography alone is not a reason to split accounts. The NATS JWT guide ties
> an account to a team or an application, explicitly not to infrastructure. If
> `za` and `au` are two locations running one logical workload, a shared
> account with stream placement plus mirrors is simpler. Which of those two we
> are is the actual question, and it is not answered yet.
>
> No adoption statistics were found for any of the three; the recommendation
> is architectural judgement from the documentation, not a published NATS
> rule.
>
> **What this does to our own rules, unresolved:** tenancy in this repo is
> enforced by the account boundary and `{context}` is explicitly *not* the
> tenant and *not* the region (CLAUDE.md, § Subject families). Splitting
> `acme` into `acme-za` and `acme-au` makes the account carry region as well
> as tenant. That may be right, but it is a change to a settled rule and it
> reaches `shared/natstenants`, the `.creds` filenames that `SwitchTenant`
> scans, and every credential name.
>
> Sources given: NATS subject, mirroring, account and JWT guidance in
> `nats-io/nats.docs`; `nats-io/jetstream-leaf-nodes-demo`; Synadia's
> JetStream management guide; `synadia-labs/cross-account-jetstream-sourcing`.

**D10 — One data volume per NATS server, and one resolver directory per
server.** *Added 2026-09-08 after review.*
Today one volume, `nats-data`, is mounted at `/data`
(`deploy/cell/compose.infra.yaml`). Scaling that one service definition to three
containers would point **three servers at one folder**. JetStream keeps a
per-server store there and NATS's own resolver documentation says a resolver
directory must not be shared, so this is silent corruption, not a warning.

Each of the nine servers therefore gets its own named volume
(`nats-za-1-data`, `nats-za-2-data`, …) and its own resolver directory. That
rules out `deploy scale`-style replicas: the three servers in a cluster are
**three named service definitions** sharing one conf file (D2), not one service
with `replicas: 3`. D2 already assumed that shape; D10 makes the reason
explicit. Logs follow the same rule (`nats-za-1-logs`, …), since the log file
is opened for append by name.

#### Checklist

> Revised 2026-09-08. 64a now carries per-server volumes (D10) and the
> JetStream domain (D9); the former 64d ("one Compose project") is replaced by
> the gateway-network task (now 64c), which must land before the gateway
> blocks in 64d — a gateway cannot dial across Compose projects without it.

- [x] **64a — Cluster the `za` and `au` servers, with per-server storage and a
      domain.** *Done 2026-09-08 — see D2's refinement note for the two places
      the build differed from the design.* Add `cluster {}` with routes to a per-cluster conf (D2), set
      `domain` in that conf's `jetstream {}` block (D9), replace the single
      `nats` service in each cell with three named services, and give each its
      own data, logs and resolver volumes (D10). Verify `nats server list`
      shows three servers per cluster, `/routez` shows two peers each, and
      `nats --domain za stream info SHIPPING` and `--domain au` return **two
      different streams** with independent message counts.
- [x] **64b — Stand up the `hub` cluster.** Three more servers in their own
      `lb-hub` project, its own conf with `domain: hub`, no business service
      attached, no websocket listener (FR-5). *Done 2026-09-08.*
      `demos/01-dictionary/nats/nats-hub.conf` (cluster name and domain are
      literals — there is exactly one hub) and
      `deploy/global/compose.hub.yaml`, driven by the new
      `deploy/environments/local-mesh.env` (4422-4424 / 8422-8424, exactly
      D4's table). Verified: three servers, each routed to the other two;
      `nats --domain hub stream ls` answers and is **empty**, which is the
      correct state for a cluster with no business service on it; host ports
      9422-9424 are closed, so no websocket listener exists; and
      `nslookup nats` from inside `lb-hub` returns NXDOMAIN, proving the hub
      cannot reach a cell's `backend` network — the only thing that will ever
      cross a cluster boundary is the gateway link added in 64c.
- [x] **64c — The gateway network.** `docker network create lb-gateway`,
      declared `external: true` in all three projects, joined by the nine NATS
      containers only, with a stable `.gw` alias per server (D1, D3). Wrap the
      create plus the three `up` commands in one documented script (NFR-2).
      Verify each server resolves every other server's `.gw` alias and that
      **no Postgres or app container** is attached to `lb-gateway`.
      *Done 2026-09-08.* `lb-gateway` is a bridge on `172.23.0.0/16`, declared
      `external: true` in `cell/compose.infra.yaml` and
      `global/compose.hub.yaml`. `deploy/up-mesh.sh` creates it if absent
      (`docker network create` is not idempotent — it errors when the network
      exists, so the script checks with `docker network inspect` first) and
      then brings up all three projects; `./up-mesh.sh down` and
      `down -v` tear them down, and it deliberately does **not**
      `docker network rm lb-gateway`. Only `lb-za-1` gets
      `global/compose.control.yaml` — two provisioners writing one trust tree
      is the same writer twice, not HA. Verified: (1)
      `docker network inspect lb-gateway` lists exactly the nine NATS
      containers — no Postgres, no app, no frontend; (2) all nine `.gw`
      aliases resolve to identical IPs from `lb-za-1-nats-1-1`,
      `lb-au-1-nats-2-1` and `lb-hub-nats-3-1`; (3) each server's `/routez`
      shows exactly its own two cluster peers by `server_name`; (4)
      `grep -i "cluster name"` across all nine logs returns zero hits. **This
      task also uncovered a route-URL ambiguity that D3 did not anticipate —
      see D3's note for what broke and how it was fixed.** A dotted Docker
      network alias was proved to resolve before three compose files were
      written around the assumption. Also fixed a 64a loose end:
      `bootstrap-operator.sh`'s include-validation loop named
      `nats-za.conf`/`nats-au.conf`, which were never created, and skipped the
      regional conf that does exist — it now checks `nats.conf`,
      `nats-cluster.conf` and `nats-hub.conf`.
- [~] **64d — Gateway full mesh.** Add `gateway {}` to all three confs naming
      the other two clusters by `.gw` alias. Verify with
      `nats server report gateways` showing three clusters and, from a server
      in `za`, an inbound and outbound gateway to both `hub` and `au`. Then
      **re-run 64a's two-stream check with the gateways up** — this is the
      check that would have caught D9, and it is the single most important
      assertion in the phase.
      *Part done 2026-09-08. The mesh is built and verified; the two-stream
      check FAILED and needs a design decision — see D11 below.* The mesh:
      `gateway { name: …, port: 7222 }` in `nats-cluster.conf` and
      `nats-hub.conf`, with the remote list in one small file per cluster
      (`nats/gateway-remotes-{za,au,hub}.conf`) mounted onto a single
      in-container path. Verified: `nats server report gateways` run with
      `za`'s system creds reports **all nine servers in three clusters** —
      one `$SYS` space across the supercluster is itself the proof the mesh
      is up; every server holds an outbound gateway to both other clusters;
      inbound links are spread across each cluster's three servers, which is
      how NATS distributes them (so "inbound on this one server" is the wrong
      per-server assertion — assert it per cluster). 7222 is on no host port.
      **Open question 6 was NOT settled and could not be settled cheaply —
      see its own entry for why.**
- [ ] **64e — Make R3 real.** `NATS_STREAM_REPLICAS` knob, default 1, threaded
      to every stream, KV **and Object Store** creation site (D6). Verify
      `nats --domain za stream info SHIPPING` reports 3 replicas with a leader
      and two followers, that `OBJ_organizations-docs` reports 3, then **kill
      one server** and confirm the stream keeps quorum and stays writable.
      Also confirm the surviving Admin UI diagnostics still answer, since
      monitoring today points at one NATS node (see § 5 question 5).
- [ ] **64f — Document it.** Update `ARCHITECTURE-COMMUNICATIONS.md`'s topology
      section, the demo `README.md` port table (CLAUDE.md requires the new
      ports recorded there), and the ADR per D8. Record NFR-4 explicitly, so
      nobody later reads a sub-millisecond gateway hop as a real za-au figure.

---

## 5. Open Questions

1. **Who mints accounts in a mesh?** D5's contradiction. One `accounts-service`
      against the hub, writing into all three resolvers? One per cluster, each
      sovereign? This is the next control-plane phase and it blocks nothing in
      Phase 64. **It also settles the word `hub`** — see FR-5's pinned
      definition. That phase is where the control-plane band and this NATS
      cluster finally sit together, so it is where one of the two meanings has
      to be retired and `compose.control.yaml`'s header reworded.
2. ~~**Does the mesh project replace the cell band or sit beside it?**~~
      **Closed 2026-09-08** by the revised D1: there is no separate mesh
      project. The mesh *is* the existing three projects with one external
      gateway network attached, so the cell band is neither replaced nor
      forked.
3. **Which data crosses regions, and with what consistency?** § 1 questions 4-5,
      still unanswered. § 0 already recommends mirrors over gateway pub/sub;
      the phase after 64 turns that into a design.
4. **Is latency injection wanted later?** NFR-4 declines it for now. It becomes
      interesting the moment a mirror exists to measure lag on.
5. **Does Admin observability need a mesh-aware view?** *Raised 2026-09-08 in
      review.* The Admin UI's NATS monitoring and log panels each point at one
      node. On a nine-server mesh, losing that node leaves the diagnostics
      blind while the cluster itself is healthy. Not a Phase 64 blocker — the
      topology is verified from the CLI in 64a-64f — but it is the first thing
      that will feel broken afterwards.
6. **Do account JWTs really not cross a gateway?** D5 asserts they do not and
      treats it as settled. Review pushed back: NATS documents one operator as
      the authentication domain across clusters and gateways, and the `full`
      resolver as converging on the union of known JWTs. Cheap to settle —
      mint one tenant against `za` and try to connect as it on `au` and `hub`.
      Worth doing **inside 64d**, because a "no" and a "yes" lead to very
      different control-plane phases.
      *Attempted in 64d, 2026-09-08 — STILL OPEN, and the cheap test does not
      exist in this lab.* Every account in the lab arrives by the shared
      `resolver-preload.generated.conf`, which is bind-mounted into all nine
      servers. So `acme` already exists on `au` and `hub` whether or not a
      gateway propagates anything, and connecting as it proves nothing. A
      valid test needs an account minted **at runtime** on `za` only, then
      looked for on `au` — which means driving `accounts-service` (it runs in
      `lb-za-1` only) and then inspecting `au`'s resolver directory. That is a
      real test, not a five-minute one, and it writes to the trust tree. Left
      for the control-plane phase, which is where the answer is actually
      needed.
