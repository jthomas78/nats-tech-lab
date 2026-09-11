# CLAUDE.md — demos/02-multi-region

**This folder is a sealed unit. Read this file instead of the root `CLAUDE.md`
for anything inside it.**

The root file describes demo 01 — a Postgres-backed, multi-service, multi-
frontend POC. Demo 02 is six NATS servers and one small Go binary. Most of the
root file does not apply, and applying it here has already caused wrong work.

Two things still apply, repo-wide: the session memory rules and the general
preferences (stop if asked to do too much; don't read large docs whole;
delegate wide exploration).

## What this demo is

The question is **where a message goes**, not what shape a read model takes.
Demo 01 already answered the second one.

Two Compose projects, one per region, because a region is its own deployment:

| Project | Servers | File |
|---|---|---|
| `lb-za-1` | za-1, za-2, za-3 | `deploy/compose.za.yaml` |
| `lb-au-1` | au-1, au-2, au-3 | `deploy/compose.au.yaml` |

Three **external** Docker networks, created by `deploy/up.sh` and shared by both
projects: `lb-za` and `lb-au` carry routes (6222); `lb-wan` carries gateways
(7222) and nothing else.

Each server has a **different name per network** — `za-1.rt` on its regional
network, `za-1.gw` on the wan. A container on two networks resolves its plain
name to *either* one, and Docker does not promise which. Measured: with plain
names the routes came up on the wan. Do not "simplify" these aliases.

## Commands

```bash
cd demos/02-multi-region/deploy
./up.sh            # networks, trust chain if needed, six servers, lab2-* contexts
./up.sh down       # stop, keep the data and the networks
./up.sh down -v    # also drop the volumes, the networks and the lab2-* contexts
./contexts.sh      # re-register the contexts on their own
```

`up.sh` starts both regions with `up -d`. There is no `docker compose run`
anywhere in this demo any more.

### The CLI runs on your machine

**There is no toolbox container. Removed 2026-09-09 on the user's instruction:
demo 02 uses the `nats` and `nsc` binaries installed on the host.** Do not
reintroduce a container for them without asking.

What that costs, so nobody is surprised later: nothing pins the tool versions
now. Two machines on different `nats` or `nsc` versions can get different
answers from the same lab, and `nats/bootstrap.sh` can mint a slightly
different trust chain. If a bug ever looks machine-specific, check
`nats --version` and `nsc --version` first. Known good: **nats 0.4.0, nsc
2.15.0, jq** (all needed).

`deploy/contexts.sh` registers one context per file in `nats/creds/`. A new
credential needs no edit anywhere. `up.sh` calls it for you.

**Every context name starts `lab2-`.** That is not decoration. Demo 01 already
owns contexts called `sys` and `platform` in the same store
(`~/.config/nats/context/`), pointing at `localhost:4222` with demo 01's trust
chain. Unprefixed names here would silently overwrite them.

| Context | Account | Points at |
|---|---|---|
| `lab2-sys` | SYS | za, no JetStream domain — it crosses the gateway on its own |
| `lab2-linebooker-za` | LINEBOOKER_ZA | za, domain `lb` |
| `lab2-linebooker-au` | LINEBOOKER_AU | au, domain `lb` |
| `lab2-linebooker-shared-za` | LINEBOOKER (spans both) | za, domain `lb` |
| `lab2-linebooker-shared-au` | LINEBOOKER (spans both) | au, domain `lb` |
| `lab2-platform-shared-za` / `-au` | PLATFORM | one per side |

`-shared-` means the one account that spans both regions — the broken shape
`lab/00-the-problem.sh` demonstrates. `lab2-linebooker-za` and
`lab2-linebooker-shared-za` are **different accounts**, and that difference is
the demo.

Each context already carries all three of its region's servers and the one
shared JetStream domain, so a plain `nats --context lab2-linebooker-za stream
ls` is complete. **The domain is `lb` everywhere — see "One domain, not two"
below.**

### Addressing a region

```bash
# streams and KV -- a business account
nats --context lab2-linebooker-za stream ls
nats --context lab2-linebooker-au kv ls

# servers -- the sys account, which crosses the gateway
nats --context lab2-sys server list
nats --context lab2-sys server report jetstream --cluster au
```

`--cluster` exists on `server report` only -- not on `server list`, not on
`stream ls`. The `sys` account holds no streams; `stream ls` there returns
"no responders available", which is correct, not a fault.

**Never use a Docker name like `za-1` from the host.** Your machine cannot
resolve it. Host ports: za-1/2/3 = 4621/4622/4623, au-1/2/3 = 4721/4722/4723.
The contexts already hold these.

## ONE stream name, `ODOMETER`, and never two alive in one region

**Demo 02 has exactly one stream name: `ODOMETER`.** Every lab uses it. Do not
add a second name, and do not leave two streams alive in one region at the
same time. Set by the user 2026-09-09.

| Lab | Stream(s) | Account | Region |
|---|---|---|---|
| `00-the-problem.sh` | `ODOMETER` — ZA creates it, AU is refused (10058) | `LINEBOOKER` shared | za |
| `01-option-1-subjects.sh` | `ODOMETER_ZA`, `ODOMETER_AU` | `LINEBOOKER` shared | one each |
| `01-the-wall.sh` | none — plain pub/sub | — | — |
| `02-replicas.sh` | `ODOMETER`, **twice in sequence** (R1, delete, R3) | `LINEBOOKER_ZA` | za |
| `03-odometer.sh` | `ODOMETER` + KV `vehicles` | shared, then split | one each |

Two consequences worth keeping:

- `01-option-1-subjects.sh` needs **two** names because one account cannot hold
  one name twice across a supercluster. That is Option 1's price, and the
  suffix is the demonstration — it is not a second stream design.
- `02-replicas.sh` must not build an R1 and an R3 stream side by side. It runs
  `ODOMETER` at R1, reports, deletes it, then runs it at R3. Slower to read,
  but only one stream is ever alive.

The subject everywhere is `evt.odometer.vehicle.{vehicleID}.travelled`
(`01-option-1-subjects.sh` prefixes a region token, on purpose — that is what
it is measuring). Payload is `{"km":12.5}`.

`lab/streams.sh` lists every stream in every account, with its cluster. Use it
to check the rule holds:

```bash
cd demos/02-multi-region/lab
./streams.sh          # one look
./streams.sh watch    # redraw every second, in a second terminal
```

## `odometer/` is the only JetStream + CQRS example — keep it that way

One Go module, on the host, outside Docker. Listed in the root `go.work`.

| Thing | Value |
|---|---|
| Stream | `ODOMETER`, LimitsPolicy, replicas 3 |
| Subject | `evt.odometer.vehicle.{vehicleID}.travelled` |
| Payload | `{"km": 12.5}` |
| Read model | KV bucket `vehicles`, key `{vehicleID}` |

Three decisions made on purpose. Do not undo them without asking:

- **KV only, no Postgres.** Here the KV entry *is* the read model. In demo 01 KV
  is a cache in front of Postgres. Different job.
- **The subject omits `{context}`**, and so breaks `ARCHITECTURE-COMMUNICATIONS`
  § 2. The reason is in a comment at the top of `odometer/main.go`. Do not copy
  this shortened subject into a real service.
- **One example, not two.** Demo 02 is not the place for a second CQRS shape.

`lab/03-odometer.sh` runs it and prints the verdict. The question it answers is
**where does the number live**, not how big it is: with one shared account both
regions read **12.5 km** out of the **same** bucket in cluster `za`; with one
account per region ZA reads 12.5 and AU reads 0, each from its own local stream.

> **Correction, 2026-09-11.** This section used to say `25 km = captured twice`
> and called it a cross-region **double capture**. Wrong. The 25 came from
> `defer sub.Unsubscribe()` in `odometer/main.go` — on a **durable** pull
> consumer that DELETES the consumer, so every `project --once` replayed the
> stream from message 1 (measured: four runs over a stream holding **one**
> message read 12.5, 25, 37.5, 50; one region, one account, no gateway). Behind
> a gateway there is nothing to capture twice, because one account holds ONE
> `ODOMETER` and ONE `KV_vehicles`. Double capture is real in **hub-and-leaf**.
> Full write-up: `diagrams/gateway-double-capture-options-2.html`.

Three implementation details worth keeping: the projector writes with
`kv.Update(key, value, revision)`, not `Put`, so two projectors cannot both read
a value and both overwrite it; a failed apply is deliberately **not** acked; and
**nothing may call `Unsubscribe()` or `Drain()` on the projector's durable pull
subscription** — both delete the consumer server-side and cause the replay above.
`odometer/main.go` carries a dated comment saying so.

## Lab scripts

`lab/*.sh` each set up what they need and clean up after themselves in an EXIT
trap. **A lab that creates a stream must remove it in `cleanup`, not at the
start of the next run.** `02-replicas.sh` got this wrong once and left its two
streams behind between runs. If a lab stops a container, `cleanup` starts it
again *before* deleting streams — an R1 stream cannot be removed while its only
server is down.

## Where this demo's documents live

Everything about demo 02 lives under `demos/02-multi-region/`. Nothing about it
sits in demo 01 or in `.claude/plans/` any more (moved 2026-09-09).

| Folder | Holds |
|---|---|
| `README.md` | the intro the lab shell renders, and the three questions with their measured answers |
| `docs/Multi-Region-Plan.md` | the record of how the mechanics were worked out. Superseded in place — read it as history, not as instructions |
| `diagrams/` | `multi-region-pattern-cards.html` (the 10-card deck — every conclusion this demo reached, one card each), `gateway-vs-wan-cut.html`, `gateway-double-capture-options-2.html` (the corrected option comparison; `-options.html` next to it is the superseded original, kept for the record), and `multi-cluster-and-region/` (six topology drawings) |
| `odometer/README.md` | the one JetStream + CQRS example, and its business rule table |
| `lab/*.sh` | the runnable experiments |

**Put a new demo 02 document here, not in `.claude/plans/`.** That folder is
demo 01's phased plan and its two siblings.

**Session memory is the one exception.** Memory files stay in `.claude/memory/`
because Claude Code loads `.claude/memory/MEMORY.md` at session start and does
not look in subfolders. Demo 02's memories are tagged `[demo 02]` in that index
so you can tell them apart without opening them.

## Root rules that do NOT apply here

Ignore these inside this folder:

- **Frontend design system** (`shared/unifi-theme`, `AppShell.vue`, the 1920x1080
  viewport rule). There is no UI here. The dark UniFi palette still applies to
  any diagram under `diagrams/`.
- **Host port range 7100–7299.** Demo 02 uses 46xx and 47xx, listed above.
- **Ginkgo.** `odometer/` uses plain `go test`. It has no Postgres and nothing
  skips silently.
- **`BUSINESS_RULES-*.md`.** Demo 02's one rule (BR-OD01, km must be greater
  than 0) lives in the table in `odometer/README.md`. Do not add a demo 02 file
  to demo 01's business rules set.
- **`ARCHITECTURE*.md` and the ADR folder.** Those describe demo 01 and the
  proposed V3 platform. Demo 02 records its findings in its own `README.md` and
  in `.claude/memory/`.
- **`Main-POC-Plan.md` and its two siblings.** Demo 02 is not phased there.
- **`shared/natsconn`, hexagonal module layout, one module per bounded context.**
  `odometer/` is one small binary: `domain.go` (no I/O), `main.go` (the adapter).
  Keep it that way.

## Root rules that DO still apply

- **Storage naming.** Streams are `SCREAMING_SNAKE` (`ODOMETER`). KV buckets are
  `lowercase-kebab` (`vehicles`). Creds files too (`linebooker-za.creds`).
- **A bucket name is a stream name.** `vehicles` is really the stream
  `KV_vehicles`, and `stream ls` does not show it. Renaming a bucket orphans the
  old stream and makes an empty new one.
- **Every `nats.Connect` sets `nats.Name(...)`.** The odometer sets
  `nats.Name("odometer")`.
- **`MaxReconnects(-1)`** on a long-lived connection.
- **Credentials here are lab-only.** `nats/.gitignore` excludes `operator.jwt`,
  `resolver-preload.generated.conf`, `creds/`, `resolver/` and `.nsc-store/`.
  Never treat a minted credential from this lab as safe outside it.

## Measured facts — do not re-guess these

- **A WAN cut freezes JetStream MANAGEMENT only** (measured 2026-09-09). A
  gateway makes all six servers one JetStream meta group; majority of 6 is 4.
  Split 3/3 and neither side elects a meta leader (`leader=NONE`). What stops
  is `stream add/rm/edit` → `JetStream system temporarily unavailable (10008)`.
  What keeps working: core NATS pub/sub, reads from an existing stream, and
  **writes to an existing stream** (measured 10 → 15 messages, acked), because
  a stream's replicas all live inside one cluster. Plan a WAN cut as a change
  freeze, not an outage. Uneven regions (3+2) do **not** both freeze — the side
  holding 3 of 5 keeps its leader.
- **Never fake a WAN cut with `docker network disconnect lb-wan <container>`.**
  On a running container it also drops the published host ports, so ports 4621
  and 8621 go dead and every reading is a Docker artifact. An earlier run of
  this test wrongly reported "both regions died"; the giveaway was core NATS
  pub/sub also failing, which a meta split cannot cause. Lose majority
  honestly instead: `docker stop lab2-au-1 lab2-au-2 lab2-au-3`.
- **One supercluster is ONE JetStream namespace, so it takes ONE domain: `lb`.**
  A domain names a JetStream *system*. The NATS docs require the same domain
  name on every server of a cluster **and** of a supercluster; it may only
  change across a **leaf-node** link. A gateway is not a leaf link.
  Per-region domains (`za`, `au`) were configured here and **silently broke
  JetStream**: setting a domain suppresses JetStream traffic on the system
  account, so the clusters never learned each other's server names — za's meta
  leader listed the three au servers as `Server name unknown … offline: true`,
  au never elected a leader (`/jsz?meta=1` → `leader: null`), and
  `stream add --cluster au` failed with `no suitable peers for placement
  (10005)`. Fixed 2026-09-09. Now all six peers report `offline=false,
  current=true` and placement works. **Do not reintroduce a per-region domain.**
- **Separate two regions with PLACEMENT and ACCOUNTS, never a domain.**
  `--cluster za` / `--cluster au` on `stream add` (Go: `Placement:
  &nats.Placement{Cluster: …}`) decides where a stream lives, and it works.
  `odometer/main.go` sets it on both the stream and the KV bucket.
- **Inside one account a stream name is unique across the whole supercluster.**
  ZA takes `ODOMETER`; AU's request for `ODOMETER` is refused with `stream name
  already in use with a different configuration (10058)`, and `stream info` from
  AU shows **ZA's** stream — same cluster, same `created`. So one account across
  two regions is not two regions; AU is a second door into ZA's JetStream. That
  is what `lab/00-the-problem.sh` now measures. Two accounts
  (`lab2-linebooker-za` / `-au`) give two real streams with two `created` times.
- **A KV bucket IS a stream, so one shared account gives you ONE bucket.** Its
  writes publish on `$KV.<bucket>.<key>`, and `KV_vehicles` is subject to the
  same account-wide name check as any stream. With one `LINEBOOKER` account both
  regions read **12.5 km** — the same 12.5 km, out of the same bucket, held in
  cluster `za`; AU owns nothing and reads it over the WAN. With `LINEBOOKER_ZA` +
  `LINEBOOKER_AU` the answer is 12.5 km in ZA and 0 in AU, each local. That is
  the demo. (It used to be recorded here as "both regions read 25 km" — that was
  the replay bug, corrected 2026-09-11.)
- **`Routes 8` per server is normal** on NATS 2.14. It opens several route links
  per peer. It is not a fault.
