---
name: odometer_is_demo_02s_only_cqrs_example
description: 2026-09-09, CORRECTED 2026-09-11 — demo 02 gets exactly ONE JetStream+CQRS example, the odometer (KV only, no Postgres, no {context} in the subject); its total proves WHERE the data lives, not a double capture — one account = ONE stream + ONE KV bucket supercluster-wide, so both regions read the same 12.5 km out of cluster za
metadata:
  type: project
---

Agreed with the user and built on 2026-09-09, in `demos/02-multi-region/odometer/`
(one Go module, in `go.work`, run on the host with `go run .`).

## The shape

```
event   evt.odometer.vehicle.{vehicleID}.travelled   {"km": 12.5}
rule    BR-OD01 -- km must be greater than 0
stream  ODOMETER   LimitsPolicy, replicas 3
read    KV bucket `vehicles`, key {vehicleID}, value {"totalKm":..,"trips":..}
```

**Exactly one CQRS example, deliberately.** Demo 01 already compared the read-model
shapes; demo 02 is about *where a message goes*. Do not add a second one.

**KV only, no Postgres.** In demo 01 the KV entry is a write-through cache and Postgres
is the truth. In demo 02 the KV entry IS the read model, because the thing being
measured is whether a number survives a gateway, and a second datastore only adds a
place for it to go wrong.

**The subject has no `{context}` and that breaks `ARCHITECTURE-COMMUNICATIONS` § 2 on
purpose.** `{context}` is the company/business unit; this demo has no companies, only
regions, and § 2 is most emphatic that a region is never a subject token. A placeholder
context would either invite someone to write `za` there — the exact mistake the rule
exists to stop — or teach nothing. The reason is written in a comment at the top of
`odometer/main.go`. Demo 01 carries the full six-token form and is the reference.

## Why the total is the decisive test

A stream message count cannot tell you the business is wrong. A total can. But a total
that looks **right** can still be reading the other region's stream, so always ask which
cluster holds it (`go run . where --region au --account linebooker`).

**Re-measured 2026-09-11, one publish of 12.5 km in ZA, projector run in both regions:**

| Accounts | odometer ZA | odometer AU | who owns AU's data |
|---|---|---|---|
| one `LINEBOOKER` in both regions | 12.5 km | 12.5 km | **nobody — the SAME bucket, in `za`** |
| `LINEBOOKER_ZA` + `LINEBOOKER_AU` | 12.5 km | 0 km | **`au`, its own local stream** |

Same subject, same code, same publish, JetStream `domain` set in both rows. Only the
account changed. Row one is the trap: the number is right and means nothing. A KV bucket
IS a stream (`KV_vehicles`), and inside one account a stream name is unique across the
**whole supercluster**, so AU's `stream add` is refused with `10058`, AU reads ZA's
bucket across the WAN, and AU loses it entirely if cluster `za` dies. That is what pays
for option 3. See [[gateway-double-capture-and-option3]] and
[[multi-region-lives-in-demo-02]].

## CORRECTION 2026-09-11 — the 25/50 row was wrong

This note used to record `25 km | 25 km | 50 km` and call it a cross-region **double
capture**. Two separate mistakes:

1. **The 25 was a replay.** `defer sub.Unsubscribe()` in `odometer/main.go` ran on a
   **durable** pull consumer. `Unsubscribe()` (and `Drain()`) DELETE the consumer
   server-side, and the durable is the only record of what the projector already
   applied. So every `project --once` restarted at sequence 1. Measured: four runs over
   a stream holding **one** 12.5 km message read 12.5, 25, 37.5, 50 — one region, one
   account, no gateway anywhere. **Never call `Unsubscribe()` or `Drain()` on that
   subscription.** `main.go` now carries a dated comment saying so.
2. **The 50 was double counting by the lab script.** It added ZA's reading to AU's when
   both read the same bucket.

There was never a second stream to capture with. Double capture IS real in a
**hub-and-leaf** topology (two JetStream systems, neither able to see the other's
subjects, so neither can refuse the overlap) — see
`demos/03-multi-cluster-and-accounts/diagrams/meta-quorum-options.html`. Full write-up:
`demos/02-multi-region/diagrams/gateway-double-capture-options-2.html`.

`lab/03-odometer.sh` runs both stages and prints the verdict. It resets both regions
first — a total carried over from the last run means nothing.

## Two implementation details worth keeping

- The projector writes with `kv.Update(key, value, revision)`, not `Put`. Two projectors
  on one bucket would both read a value and both overwrite it, losing a trip. A stale
  revision fails, the message is not acked, and it comes back.
- **Nothing may `Unsubscribe()` or `Drain()` the projector's durable pull subscription.**
  See the correction above. This is the single easiest way to break this demo again.
- A failed apply does **not** ack. Redelivery is the retry, and a permanent failure stays
  visible instead of disappearing.
