# Demo 02 — Multi-Region Cluster Mechanics

Six NATS servers. Nothing else. No database, no services, no web pages.

This demo answers two questions and stops there.

## Question 1 — does a tenant wall hold between regions?

Two regions are joined by a **gateway**. A gateway lets one region hear what
the other region is interested in. So can South Africa's traffic reach
Australia?

The answer depends on one thing only: the **account**.

| Publisher | Subscriber | Same subject? | Arrives? |
|---|---|---|---|
| `linebooker-za` in ZA | `linebooker-za` in AU | yes | **yes** |
| `linebooker-za` in ZA | `linebooker-au` in AU | yes | **no** |

The subject was the same both times. Only the account changed. That is the
point: a NATS account is a **hard wall**, checked by the server on every
message. It does not depend on anyone naming subjects correctly.

## Question 2 — what does `Replicas: 1` cost when a server dies?

A cluster here has three servers. A stream stores its messages on the number
of servers **you ask for**. The default is **one**.

So three servers is not three copies.

| Stream | Replicas | Stop the server holding it | Result |
|---|---|---|---|
| `ODOMETER` | 1 | yes | `stream is offline (10118)` — data stuck |
| `ODOMETER` | 3 | yes | new leader in about a second — data fine |

Same stream name twice, because this demo has only one. `lab/02-replicas.sh`
runs it at 1, deletes it, then runs it at 3.

The cost of 3 is real: every write goes to three servers, so it is slower and
uses three times the disk. Choose it on purpose, per stream.

## Question 3 — is the double capture real? Ask the odometer.

A truck drives **12.5 km, once**, in South Africa. Two regions. What does the
odometer say afterwards?

| Accounts | Odometer in ZA | Odometer in AU | The fleet believes |
|---|---|---|---|
| one `LINEBOOKER`, both regions | 25 km | 25 km | **50 km** |
| `LINEBOOKER_ZA` + `LINEBOOKER_AU` | 12.5 km | 0 km | **12.5 km** |

Same subject. Same code. Same publish. Only the account changed.

A message count cannot tell you the business is wrong. A total can. 25 km for
a 12.5 km trip is not a rounding question — it is proof the event was
captured twice. This is the number that pays for one account per region.

The JetStream `domain` does not save the first row, and it cannot. A gateway
joins the two clusters into one **supercluster**, and a supercluster is one
JetStream system with one domain name. Regions are separated by **accounts**,
and streams are pinned to a region by **placement** (`--cluster za` /
`--cluster au`).

```bash
../lab/03-odometer.sh
```

Design and code: `odometer/README.md`. It is the **only** JetStream + CQRS
example in this demo, on purpose — demo 01 already compared the read-model
shapes, and this demo is about where a message goes.

## One stream name

This demo has exactly **one** stream name: `ODOMETER`, on subject
`evt.odometer.vehicle.{vehicleID}.travelled`. Every lab uses it, and no region
ever holds two streams at once.

The one exception proves the rule. `lab/01-option-1-subjects.sh` must create
`ODOMETER_ZA` and `ODOMETER_AU`, because one account cannot hold one name twice
across a supercluster. That suffix **is** the cost of Option 1.

See what is there right now:

```bash
../lab/streams.sh
```

## The whole thing on paper

Every conclusion in this demo, one card each — namespace, placement, accounts,
replicas, ack cost, WAN cuts, and the three measurements that were wrong the
first time.

```
diagrams/multi-region-pattern-cards.html
```

PDF: `obsidian/V3-Platform/Architecture/Dictionary-POC/NATS Multi-Region — Pattern Cards.pdf`

## The shape

```
  cluster za                  gateway                 cluster au
  ┌──────────────────┐       ◄────────►       ┌──────────────────┐
  │ za-1  za-2  za-3 │                        │ au-1  au-2  au-3 │
  │  one JetStream, one domain `lb`, across all six servers   │
  │  a stream is pinned to a region by --cluster za|au        │
  └──────────────────┘                        └──────────────────┘
```

Two kinds of link, easy to mix up:

- A **route** (port 6222) joins servers **inside one** cluster. It carries
  everything.
- A **gateway** (port 7222) joins **two** clusters. It carries interest, and
  it never crosses an account.

Accounts in the lab: `SYS`, `LINEBOOKER_ZA`, `LINEBOOKER_AU`, `PLATFORM`.
A tenant is a **marketplace**, so the tenant accounts are named after
marketplaces — not after customers.

## One region, one project

Each region is its own Docker Compose project, because in the real world each
region is its own deployment.

| Project | Servers | File |
|---|---|---|
| `lb-za-1` | za-1, za-2, za-3 | `deploy/compose.za.yaml` |
| `lb-au-1` | au-1, au-2, au-3 | `deploy/compose.au.yaml` |

That split is what lets you take one region off the air on its own:

```bash
cd deploy
docker compose -f compose.au.yaml down    # Australia is gone, ZA keeps running
```

Three Docker networks join them, and all three are shared, so `up.sh` creates
them:

| Network | Carries | Who is on it |
|---|---|---|
| `lb-za` | routes, 6222 | za-1, za-2, za-3 |
| `lb-au` | routes, 6222 | au-1, au-2, au-3 |
| `lb-wan` | gateways, 7222 | all six servers, **no client** |

A server has a different name on each network — `za-1.rt` on `lb-za`,
`za-1.gw` on `lb-wan` — so each kind of traffic can only take its own road.
Without that, Docker picks a network for the plain name `za-1` and does not
tell you which; measured, the routes ended up on the wan.

## Run it

```bash
cd deploy
./up.sh
```

The first run mints the trust chain (operator, accounts, credentials) using
the `nsc` on your own machine, then registers one `nats` context per
credential. Every context name starts `lab2-`, so it cannot clash with demo
01's `sys` and `platform`.

You need three tools on your Mac:

```bash
brew install nats-io/nats-tools/nats nats-io/nats-tools/nsc jq
```

Then run the labs:

```bash
../lab/00-the-problem.sh        one account across two regions is ONE JetStream
../lab/01-option-1-subjects.sh  a region token in the subject -- it works, at a price
../lab/01-the-wall.sh           does an account boundary hold across a gateway?
../lab/02-replicas.sh           what does Replicas 1 vs 3 cost when a node dies?
../lab/03-odometer.sh           is the double capture real? (also needs Go)
../lab/streams.sh               show every stream, and which region it sits on
```

All five put everything back when they finish. Run them as often as you
like.

Look around by hand — your own `nats` CLI, one context per credential:

```bash
nats --context lab2-sys server list
nats --context lab2-linebooker-za stream ls
nats --context lab2-linebooker-au kv ls
```

Each context already holds all three of its region's ports and the one shared
JetStream domain (`lb`), so there is nothing else to type. `lab2-linebooker-shared-za`
is the *other* account — the one that spans both regions, which is the broken
shape stage 0 shows.

Re-register them at any time:

```bash
./contexts.sh            # add or refresh
./contexts.sh remove     # delete every lab2-* context
```

Stop it:

```bash
./up.sh down       # keep the data
./up.sh down -v    # drop the data and remove the three networks
```

## Ports

Chosen so this runs beside demo 01.

| Server | Client | Monitor |
|---|---|---|
| za-1 / za-2 / za-3 | 4621 / 4622 / 4623 | 8621 / 8622 / 8623 |
| au-1 / au-2 / au-3 | 4721 / 4722 / 4723 | 8721 / 8722 / 8723 |

## Not in this demo

- The cross-region load handoff (a load moving ZA → AU). That needs business
  rules we do not have yet.
- The gateway hub (a third cluster). Not needed yet.
- A second CQRS example. The odometer is the only one, and one is enough to
  answer question 3.
- Postgres. The odometer's KV entry is the read model, not a cache.
- The reference-data push, any roll-up to a global view.

Demo 01 covers the application. This one covers the plumbing under it.
