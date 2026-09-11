# The odometer

One small Go binary. It is the only JetStream + CQRS example in demo 02, on
purpose — this demo is about **where a message goes**, not about how many
shapes a read model can take. Demo 01 already compared the shapes.

## The domain, in one line

A vehicle drives. It reports how far. The odometer adds it up.

```
event   evt.odometer.vehicle.{vehicleID}.travelled   {"km": 12.5}
rule    km must be greater than 0
stream  ODOMETER   LimitsPolicy, replicas 3
read    KV bucket `vehicles`, key {vehicleID}, value {"totalKm":..,"trips":..}
```

**KV only. No Postgres.** In demo 01 the KV entry is a cache in front of
Postgres, and Postgres is the truth. Here there is no Postgres, so the KV
entry IS the read model. That is the honest shape for this demo: the point
being measured is whether the *number* is right after a message crosses a
gateway, and a second datastore would only add a place for it to go wrong.

## Why the total is the test

The stream count tells you how many messages were stored. It does not tell
you whether your business number is wrong. The odometer does.

Drive **once**, 12.5 km, then read it from both regions:

| Accounts | ZA reads | AU reads | What it means |
|---|---|---|---|
| one `LINEBOOKER` | `12.5` | `12.5` | **one** bucket, in `za`. AU reads it over the WAN. |
| `LINEBOOKER_ZA` + `LINEBOOKER_AU` | `12.5` | `0` | two real buckets, one per region. Correct. |

Row one looks right and is the trap. Inside **one** account a stream name is
unique across the **whole supercluster**, so AU's `stream add ODOMETER` is
refused with `stream name already in use (10058)` — and the client keeps
working, because a stream is reachable from either side of a gateway. Ask
`where` (below) to see which cluster really holds it.

A total that is **higher** than the distance driven is not a cross-region
problem. It is a replay. See the correction note.

> **Correction, 2026-09-11.** This page used to say `total 25` meant the event
> was captured twice, and called that a gateway **double capture**. It was
> wrong. The 25 came from `defer sub.Unsubscribe()` in `main.go`. On a
> **durable** pull consumer that DELETES the consumer on the server, and the
> durable is the only record of what the projector already added. So every
> `project --once` replayed the stream from message 1. Measured: four runs
> over a stream holding **one** message read 12.5, 25, 37.5, 50 — one region,
> one account, no gateway. The line is gone now. Do not add `Unsubscribe()`
> or `Drain()` back on that subscription. Full write-up:
> `../diagrams/gateway-double-capture-options-2.html`.

## Business rules

Demo 02 has one, and it has a test (`domain_test.go`).

| Rule | Statement | Where |
|---|---|---|
| **BR-OD01** | `km` must be greater than 0. | `domain.go`, `Travelled.Validate` |

It is checked on the **write side**, before the event is published. A bad
event that reaches a `LimitsPolicy` log is permanent, and a total that can go
down is a total nobody trusts. The odometer only counts up.

The projector checks it again on the way in. That is not belt-and-braces for
its own sake — a replay reads whatever the log holds, including events written
before a rule existed.

## Commands

Run from `demos/02-multi-region/odometer/`. The binary talks to the published
host ports, so the stack must be up (`../deploy/up.sh`).

```bash
go run . setup     --region za --account linebooker-za
go run . travelled --region za --account linebooker-za --vehicle V1 --km 12.5
go run . project   --region za --account linebooker-za --once
go run . show      --region za --account linebooker-za
go run . where     --region za --account linebooker-za
go run . reset     --region za --account linebooker-za
```

`project --once` drains what is waiting and exits, which is what a script
wants. Without `--once` it runs until you stop it, which is what a service
would do.

`where` prints one word: the name of the cluster that really holds the
`ODOMETER` stream. Use it whenever a region reports a number, because a
client can read a stream from **either** side of a gateway. "I can read it
from AU" does not mean AU owns it.

`setup` says so too. If the stream already belongs to the other region it
prints a `WARNING` and ends with `usable from here, but NOT owned here`
instead of `ready`.

The lab script `../lab/03-odometer.sh` runs all of this for you and prints
the verdict.
