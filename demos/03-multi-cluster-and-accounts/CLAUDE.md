# CLAUDE.md — demos/03-multi-cluster-and-accounts

**This folder is a sealed unit. Read this file instead of the root `CLAUDE.md`
for anything inside it.**

Every demo folder in this repo is sealed and owns one of these. Demo 01 is a
Postgres-backed multi-service POC; demo 02 is six NATS servers in Docker plus a
small Go binary; demo 04 is one NATS server, a Go binary and a Vue app. **Demo
03 is none of those.** It is bare `nats-server` processes on the host, plain
password auth, and no application code at all. Most of the root file does not
apply here, and applying it has already caused wrong work.

Two things still apply, repo-wide: the session memory rules, and the general
preferences (stop if asked to do too much; don't read large docs whole;
one command per `Bash` call; delegate wide exploration).

This file is the demo's **stage 02 — Design the rig**. See
[`../../demo-playbook.pdf`](../../demo-playbook.pdf) for what that means, and
[`README.md`](README.md) for stage 01, the question and the requirement IDs.

## What this demo is

**Role: validation only. Not a showcase.** It exists to produce measured
evidence for a topology decision, not to show a feature working.

The one question, from `README.md`:

> In a two-region logistics platform, when one region goes dark, who is still
> alive to take a JetStream write?

### What is held still, and what is the one variable

| | |
|---|---|
| **The variable** | the **topology** — T1 to T5. How many clusters, what links them, where the JetStream meta vote lives. |
| Held still | the domain slice (`ODOMETER`, KV `t7-vehicles`), the two regions (ZA, AU), the three accounts, `nats-server 2.14.6`, the host `nats` CLI, plain password auth, `127.0.0.1` only. |

**This is the opposite of demo 02.** Demo 02 holds the topology still (one
gateway, always six servers) and varies the *account model*. Demo 03 varies the
topology itself. That is why this demo needs 6, 7 or 9 servers depending on the
run, and why its configs are hand-written per topology instead of one Compose
file.

## The rig

Six `nats-server` processes started **by hand on the host**. No Docker. No
`nsc`, no operator mode, no `.creds` files, no `nats` contexts. Accounts and
users are plain `user`/`password` pairs written straight into the `.conf` files.

Never use Docker to cut a region — see "Measured facts" below.

### The six committed configs = T2, the baseline

`za-1.conf`, `za-2.conf`, `za-3.conf`, `au-1.conf`, `au-2.conf`, `au-3.conf`.
Two clusters of three, joined by **one gateway**, with **no `jetstream.domain`
anywhere**. That is topology T2, also called Figure A.

Every other topology is a variation of these six files:

| ID | Figure | Shape | Servers | Meta group |
|---|---|---|---|---|
| **T1** | — | one cluster per region, **no link** | 6 | 3 + 3, quorum 2 each |
| **T2** | **A** | one cluster per region, **gateway** | 6 | 6, majority 4 |
| — | **B** | T2 + a **domain per cluster** | 6 | 6, half-blind — **does not work** |
| **T3** | **C** | T2 + a **1-instance** arbiter site | 7 | 7, majority 4 |
| **T4** | — | T2 + a **3-instance** arbiter cluster | 9 | 9, majority 5 |
| **T5** | **D** | 3-instance **hub** + a **leaf** cluster per region | 9 | 3 + 3 + 3, quorum 2 each |

T1 is the same six files with the `gateway {}` block deleted. T4 was never
built; its row in the matrix is marked `inferred` and must stay that way until
somebody builds it.

## The lab — every measurement, re-runnable

**`lab/` is the rig. It is the demo's stage 03 deliverable, and it is the only
thing that keeps this demo honest.** Added 2026-09-17, after the folder was
found to have no scripts at all: every finding here had been produced by hand
in a terminal and never written down as anything runnable, which fails the
playbook's own exit test for a validation demo.

```bash
cd demos/03-multi-cluster-and-accounts/lab
./run-all.sh              # all five topologies, then write ../REPORT.md
./run-all.sh report       # re-render the report from the last run
./01-gateway.sh           # or run one topology on its own
```

| Script | Topology | Servers |
|---|---|---|
| `00-islands.sh` | T1 — no link | 6 |
| `01-gateway.sh` | T2 / A — one gateway | 6 |
| `02-domain-over-gateway.sh` | B — a domain per cluster over a gateway | 6 |
| `03-arbiter.sh` | T3 / C — gateway + 1-node arbiter | 7 |
| `04-hub-and-leaf.sh` | T5 / D — hub + two leaf clusters | 9 |
| `_common.sh` | the shared harness — config builders, freeze/thaw, the checks |
| `render-report.py` | turns `run/results.tsv` into `REPORT.md` |

Rules for anything added here:

- **The scripts do not use the six committed `.conf` files.** They write their
  own `t-*.conf` into `lab/run/` and delete them afterwards. See the `t-` prefix
  rule below — it is what stops a stray `pkill` reaching your real lab.
- **`lab/run/` is gitignored and thrown away on every run.** Nothing in it is
  evidence; `REPORT.md` is.
- **A check has an expected answer and passes only on an exact match.** If you
  cannot say in advance what the right answer is, record it as a `note`
  instead. A note is a measurement on the record, not a claim.
- **A freeze must prove it froze.** `wait_dark` polls the monitor port and hard
  errors if a supposedly-dark server still answers. This exists because an
  early version recorded the wrong process id, so nothing was ever frozen, and
  the lab happily reported healthy behaviour that looked exactly like a finding.
- **Never read `/jsz?meta=1` once and believe it.** The `leader` field stays
  stale for roughly a minute after a majority dies. Use `wait_no_leader` /
  `wait_live_leader`.
- **Only `nats-server`, `nats`, `jq`, `curl` and `python3` are needed.** Do not
  add Docker, `nsc` or a `nats` context to this folder.

`REPORT.md` is generated. Do not hand-edit it — change the scripts, or the
prose in `render-report.py`, and re-run.

## Ports

All on `127.0.0.1`. The root file's 7100–7299 band is a per-demo *application*
band and does **not** apply here.

| Server | Client | Monitor | Route | Gateway |
|---|---|---|---|---|
| za-1 | 4231 | 8231 | 6231 | 7231 |
| za-2 | 4232 | 8232 | 6232 | 7232 |
| za-3 | 4233 | 8233 | 6233 | 7233 |
| au-1 | 4241 | 8241 | 6241 | 7241 |
| au-2 | 4242 | 8242 | 6242 | 7242 |
| au-3 | 4243 | 8243 | 6243 | 7243 |

Used by the extra topologies:

| Thing | Port |
|---|---|
| arbiter client (T3, T4) | 4540 |
| arbiter route (T3, T4) | 6540 |
| hub leaf-node listeners (T5) | 7560, 7561, 7562 |

Keep a new port inside these families. Do not borrow demo 02's 46xx / 47xx or
demo 04's 20402.

## Accounts

Three business accounts, all with JetStream on, plus `$SYS`. Same three in
every config file.

| Account | User / password | What it is for |
|---|---|---|
| `LB` | `lb` / `lb` | the **shared** account that spans both regions — the broken shape |
| `LB_ZA` | `za` / `za` | ZA owns its own streams |
| `LB_AU` | `au` / `au` | AU owns its own streams |
| `$SYS` | `admin` / `admin` | server and JetStream administration |

`LB` and `LB_ZA` are **different accounts**, and that difference is the demo.
These passwords are lab-only and worthless outside `127.0.0.1`.

Because there are no contexts, every call names its server and user:

```bash
nats --server nats://127.0.0.1:4231 --user za --password za stream ls
nats --server nats://127.0.0.1:4241 --user lb --password lb stream info ODOMETER
```

## Naming

- Streams are `SCREAMING_SNAKE` — `ODOMETER`.
- KV buckets are `lowercase-kebab` — `t7-vehicles`. A bucket is really the
  stream `KV_t7-vehicles`, so it obeys every account-wide stream rule.
- The subject slice is the odometer slice lifted from demo 02.

## The `t-` prefix rule — read this before you write any script

**Every scratch config file, and every scratch `server_name`, starts `t-`.**

This is not decoration. A tidy-up `pkill -f "nats-server -c za-"` once matched
the user's own identically named files and **killed the live lab — twice**.
With the prefix, the only safe kill pattern is:

```bash
pkill -f "nats-server -c t-"
```

and it cannot reach the six committed configs. Generated configs belong in a
gitignored scratch folder, never beside the committed six.

## Reading the answer

Read the **meta group**, not the logs. The logs will not tell you who won a
vote.

```bash
curl -s "localhost:8231/jsz?meta=1" | jq '{leader:.meta_cluster.leader, size:.meta_cluster.cluster_size}'
```

Quote the `?` — zsh globs it. Also useful: `/varz`, `/routez`, `/gatewayz`,
`/leafz`, `/accountz`, `/jsz?accounts=1`.

Two streams can share a `created` timestamp and still be different streams. Tell
them apart with **Placement Cluster** and **message count**, never `created`.

## Cutting a region honestly

```bash
kill -STOP <pid>    # the region goes dark
kill -CONT <pid>    # the region comes back
```

Stopping the far region's **processes** is the only honest cut in this demo.
Never `docker network disconnect` — it also drops published host ports, and
every reading afterwards is a Docker artifact.

After a leaf-link change, wait about **5 seconds** for interest to spread. A
2-second wait once read `0 received` and nearly got written up as a failure;
the same test at 5 seconds read `3 of 3`.

## Where this demo's documents live

Everything about demo 03 lives under `demos/03-multi-cluster-and-accounts/`.

| Path | Holds |
|---|---|
| `README.md` | stage 01 — the question, the role, the five topologies, requirements `D03-R1`…`D03-R9` |
| `CLAUDE.md` | stage 02 — this file, the rig |
| `diagrams/combination-matrix.html` | every combination wired, one row each; figures T1–T5; placement findings C1–C6 |
| `diagrams/meta-quorum-options.html` | *"Who is still alive to take a write?"* — figures A–D, the scoreboard, five config traps |
| `lab/*.sh` | stage 03 — the runnable rig, one script per topology |
| `REPORT.md` | stage 03 — the findings, generated by `lab/run-all.sh` |
| `*.conf` | the six committed T2 configs |
| `js/` | JetStream store directories — **gitignored, throwaway** |

Both diagram pages cover demos 02 **and** 03. Demo 03 owns figures T1–T5,
C1–C6, and matrix rows 5, 6 and 7.

Rows say `measured`, `inferred` or `unmeasured`. Believe the labels, and do not
promote one without a run.

Stage 04 — the pattern cards deck — is **not written yet**. See the
`pattern-cards` skill before starting it.

## Root rules that do NOT apply here

- **Frontend design system** (`shared/unifi-theme`, `AppShell.vue`, the
  1920x1080 viewport rule). There is no UI in this demo. The dark UniFi palette
  still applies to anything under `diagrams/`.
- **Host port range 7100–7299.** Demo 03 uses the families listed above.
- **Ginkgo, `go build`, `go test`.** There is no Go code here.
- **`BUSINESS_RULES-*.md`, the `ARCHITECTURE*.md` set, the ADR folder,
  `.claude/plans/`, `shared/natsconn`, hexagonal layout.** Those describe demo
  01 and the proposed V3 platform.
- **Demo 02's plumbing.** No Docker Compose, no `up.sh`, no `nsc` trust chain,
  no `.creds`, no `lab2-*` contexts. Do not copy demo 02's `_common.sh`
  wholesale — its context helper has nothing to bind to here.

## Root rules that DO still apply

- Session memory in `.claude/memory/`, and the `MEMORY.md` hook line.
- One command per `Bash` call — no `&&` chains.
- The dark UniFi palette for any generated diagram or report.
- Storage naming, above.
- The demo playbook. This demo declares its role and gives every requirement an
  ID, because stage 04 needs something to point back at.

## Measured facts — do not re-guess these

All measured 2026-09-11 on `nats-server 2.14.6` unless stated.

- **A domain cannot split a supercluster.** A domain name may only change across
  a **leaf-node** link, and a gateway is not one. Setting a domain per cluster
  over a gateway gives you a shared meta group **and** a broken link: one of the
  two clusters never elects a leader, and placement onto that cluster fails with
  `no suitable peers for placement (10005)`. Use the same domain everywhere, or
  none. This is Figure B, and it is the second deliberate reproduction of a
  demo 02 finding.
- **WHICH cluster goes blind under Figure B is a coin toss** (measured
  2026-09-17, `lab/02-domain-over-gateway.sh`). Demo 02 recorded `au` as the
  blind side and so did this demo's first run. A later run of the *same* configs,
  minutes apart with nothing changed, left **`za`** blind instead. It is a
  start-up race, not a property of a region. Never write a test — or a runbook —
  that names the side. Ask only that exactly one of the two elects, and record
  the winner as an observation.
- **A WAN cut is a change freeze, not an outage.** With `au` stopped in T2: core
  pub/sub worked, reads worked, and a publish to the existing `ODOMETER` took it
  from 1 to 2 messages. Only `stream add` failed, with `JetStream system
  temporarily unavailable (10008)`. Three of six is below the majority of four.
- **The meta group is not per account.** Measured with 3 JetStream accounts
  live: `accounts with JS: 3, meta groups: 1, leader: za-2, size: 6`. An account
  is a wall for **data**, not for **availability**. A freeze hits every account
  at the same moment.
- **Inside one account, a stream name is unique across the whole supercluster.**
  A second `ODOMETER` from the far side gives `stream name already in use with a
  different configuration (10058)`. The nastier half: `stream info` from the far
  side then **succeeds** and hands back a working handle to the *other region's*
  stream. It looks local. It is not.
- **Accounts are the only hard wall.** `ODOMETER` in `LB_ZA` and `LB_AU` both
  create cleanly, with different Placement Cluster and independent message
  counts.
- **Placement decides where bytes sit, not who survives.** `--cluster za|au` is
  about storage. It never changes quorum.
- **Replicas never cross a region.** Every replica of a stream lives inside one
  cluster. `--replicas 3` means three copies in `za`, not one in each region. An
  arbiter does not give you a third copy of your data.
- **A consumer lives where the stream lives.** A durable pull consumer created
  from AU for a ZA-held stream reported `Cluster Information: Name: za`. AU held
  no part of it, and paid the WAN on every pull.
- **A new stream or KV bucket lands where the client is**, with no placement
  flag. So a shared-account projection can split in half: its consumer in ZA,
  its KV write side in AU.
- **The arbiter is not automatically safe.** It votes, but it also **stores**.
  Eight R1 streams created from a ZA client all landed in `za` — placement
  prefers the client's local cluster, which is the good news. But an explicit
  `--cluster arb`, or any client connecting straight to `4540`, creates a real
  R1 stream on the arbiter: unreplicated data in the smallest, least protected
  site. `--replicas 3 --cluster arb` fails with `10005`, because one node cannot
  hold R3. Keep clients off the arbiter's client port, or fence it with
  placement.
- **Every JetStream cluster member needs at least one route, including the
  seed.** A routeless seed with JetStream on dies: `Can't start JetStream:
  JetStream cluster requires configured routes or solicited leafnode for the
  system account`. One route is enough — gossip finds the rest. A one-node
  arbiter still has a `cluster` block, so point it at itself:
  `routes: [ nats://127.0.0.1:6540 ]`.
- **A one-node hub must have NO `cluster` block at all.** Given the arbiter's
  self-route trick it believes in a peer that does not exist —
  `leader=None, cluster_size=2, num_routes=0`. Delete the whole block. A
  three-node hub is a normal cluster and is better anyway.
- **Every region server lists ALL of the hub's leaf ports.** This is what makes a
  hub-node failure self-heal: four orphaned links moved to another hub node by
  themselves. One URL makes one hub node a single point of failure.
- **A cross-domain mirror needs the API prefix.** Without
  `external: { api: "$JS.au.API" }` the mirror looks only in its own domain and
  sits at 0 messages forever, **with no error**. Measured over a **leaf** link
  only.
- **`Duplicate Route` in the log is gossip finding a peer twice.** It is normal.
- **Demo 02's `gateway-double-capture-options.html` is superseded**
  (correction, 2026-09-11). Over a gateway a per-cluster domain protects
  nothing, and one account holds ONE stream for the whole supercluster, so
  nothing can be stored twice. Real double capture happens in **hub-and-leaf**
  (Figure D / T5) instead — two separate JetStream systems, neither able to see
  the other's subjects, so neither can refuse the overlap.

## Still open — do not claim these are settled

- **`D03-R8`** — two accounts sharing a subject **on purpose**, via explicit
  export / import. The one unmeasured claim in the matrix.
- **`D03-R7`** — whether `source`/`mirror` survives across two **gateway**-joined
  clusters with different domains. Placement already fails there with `10005`,
  so do not trust it without a test. Mirror catch-up time at production volume
  is also unmeasured.
- The hub as a **real JetStream store**, not a pass-through (T5).
- Leaf reconnect behaviour with **many** regions, not two.
- **T4** — the three-instance arbiter was never built. Every T4 row is
  `inferred`.
