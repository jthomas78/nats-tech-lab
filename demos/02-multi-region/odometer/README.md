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

Drive **once**, 12.5 km:

- total `12.5` — the event was captured once. Correct.
- total `25`   — the event was captured twice. The double capture is real.

One number, no interpretation needed.

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
```

`project --once` drains what is waiting and exits, which is what a script
wants. Without `--once` it runs until you stop it, which is what a service
would do.

The lab script `../lab/03-odometer.sh` runs all of this for you and prints
the verdict.
