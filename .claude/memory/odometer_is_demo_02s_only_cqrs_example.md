---
name: odometer_is_demo_02s_only_cqrs_example
description: 2026-09-09 — demo 02 gets exactly ONE JetStream+CQRS example, the odometer (KV only, no Postgres, no {context} in the subject); its total is the decisive proof of the cross-region double capture (12.5 km = captured once, 25 km = twice)
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

A stream message count cannot tell you the business is wrong. A total can.

**Measured 2026-09-09, one publish of 12.5 km in ZA, projector run in both regions:**

| Accounts | odometer ZA | odometer AU | fleet total |
|---|---|---|---|
| one `LINEBOOKER` in both regions | 25 km | 25 km | **50 km** |
| `LINEBOOKER_ZA` + `LINEBOOKER_AU` | 12.5 km | 0 km | **12.5 km** |

Same subject, same code, same publish, JetStream `domain` set in both rows. Only the
account changed. So **the double capture is real** and the region boundary has to be an
account boundary — this is the number that pays for option 3. See
[[gateway-double-capture-and-option3]] and [[multi-region-lives-in-demo-02]].

Note both regions read 25, not 12.5: with one shared account the KV bucket is itself a
stream in that account, so each projector's write lands in *both* regions' buckets.

`lab/03-odometer.sh` runs both stages and prints the verdict. It resets both regions
first — a total carried over from the last run means nothing.

## Two implementation details worth keeping

- The projector writes with `kv.Update(key, value, revision)`, not `Put`. Two projectors
  on one bucket would both read 12.5, both write 25, and lose a trip. A stale revision
  fails, the message is not acked, and it comes back.
- A failed apply does **not** ack. Redelivery is the retry, and a permanent failure stays
  visible instead of disappearing.
