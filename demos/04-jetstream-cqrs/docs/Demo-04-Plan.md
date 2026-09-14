# Demo 04 — JetStream as an Event Source, with CQRS

**Status:** DONE — rules signed off 2026-09-14; all phases complete, finding in `README.md`.
**Depends on:** Demo 02's `odometer/` (the domain is lifted from it).

---

## 0. Purpose

Demo 02 publishes to a stream and folds the result into a KV bucket. It has no
write side at all: nothing validates a command against the state the log
already holds.

Demo 04 adds that write side, and answers one question with a number:

> **Rehydrating an aggregate — how much does a snapshot buy you?**

Everything in this demo exists to make that number honest.

---

## 1. Scope

**In:** one Go binary, one Compose file, one NATS server, one stream, two KV
buckets, a CLI.

**Out (agreed 2026-09-14):** no frontend, no Postgres, no cluster, no gateway,
no operator mode, no Temporal, no `{context}` token.

This demo is sealed the same way demo 02 is. Nothing here is imported by
demo 01, and nothing from demo 01's service layout is copied in.

---

## 2. Layout

```
demos/04-jetstream-cqrs/
  README.md              intro text rendered by the lab shell
  docs/Demo-04-Plan.md   this file
  deploy/compose.yaml    one NATS server, JetStream on
  cqrs/                  the Go module (added to go.work)
    domain.go            aggregate + read model. No NATS, no I/O.
    domain_test.go       Ginkgo specs, one Context per business rule
    names.go             stream / bucket / subject names, in one place
    codec.go             event <-> subject + JSON body
    codec_test.go        Ginkgo specs for the codec
    nats.go              connect, ensure stream, ensure bucket
    write.go             rehydrate, optimistic append, command retry
    snapshotter.go       the durable write-side projector
    read.go              read consumer, read store
    seed.go              the timing fixture
    main.go              CLI
```

---

## 3. The stream

`ODOMETER`, **LimitsPolicy** — InterestPolicy discards a delivered message and
this demo replays from sequence 1, so the retention choice is load-bearing.

```
evt.odometer.vehicle.{id}.registered
evt.odometer.vehicle.{id}.travelled
evt.odometer.vehicle.{id}.retired
```

Stream filter `evt.odometer.>`. First token is the fixed literal `evt`, never a
wildcard — an open first token textually overlaps `$SYS.>` and `$JS.API.>`, and
JetStream refuses such a stream without NoAck.

**No `{context}` token**, for the same reason demo 02 leaves it out: this demo
has no companies, and inventing a constant that means nothing teaches nothing.
Demo 01 carries the full six-token form and remains the reference for it.

---

## 4. Write side

1. A **command** arrives — `register`, `travel`, or `retire`.
2. **Rehydrate** the aggregate:
   - snapshot ON — read KV `odometer-write` key `vehicle.{id}` for
     `{state, lastSeq}`, then replay only events after `lastSeq`;
   - snapshot OFF — replay every event for that vehicle from sequence 1.
3. **Check the rule** against the rehydrated state. Reject, or continue.
4. **Append** the event with
   `Nats-Expected-Last-Subject-Sequence: <lastSeq>` and
   `Nats-Expected-Last-Subject-Sequence-Subject: evt.odometer.vehicle.{id}.>`.
   The scoped form needs NATS 2.11+; the repo runs 2.14.3.
   If another writer got there first the append fails, and the command retries
   from step 2.
5. A **write consumer** folds new events into the KV snapshot in the
   background.

### 4.1 The snapshot is always stale — on purpose

Because the write consumer is asynchronous, the snapshot trails the stream.
Step 2 therefore replays the tail from `lastSeq + 1`. A demo that read the
snapshot and stopped there would be showing a bug, not a pattern.

### 4.2 Why the aggregate has a lifecycle

`km > 0` needs no state, so an aggregate that enforced only BR-OD01 would do no
work and prove nothing. `register` / `retire` gives the aggregate one state
field that a command must be checked against.

---

## 5. Read side

A **read consumer** folds every event into KV `odometer-read`, key `vehicle.{id}`:

```json
{ "totalKm": 0, "trips": 0, "status": "registered", "lastTripAt": "" }
```

The **query** command reads that key and prints it. No replay, no rules, no
rehydration. That asymmetry against § 4 is the point of CQRS, and keeping the
two stores in separate buckets keeps it visible in `nats kv ls`.

---

## 6. Business rules — SIGNED OFF 2026-09-14

| ID | Rule |
|---|---|
| BR-OD01 | `km` must be greater than 0 (carried in from demo 02) |
| BR-OD02 | A vehicle must be registered before it travels |
| BR-OD03 | A vehicle cannot be registered twice |
| BR-OD04 | A retired vehicle refuses trips |
| BR-OD05 | A vehicle cannot be retired unless it is registered |

Each rule gets one Ginkgo `Context` with one or more `It`s, written before the
implementation. Summary file: `demos/04-jetstream-cqrs/BUSINESS_RULES-ODOMETER.md`.

---

## 7. Proving the snapshot claim

```bash
cqrs seed -vehicle V1 -n 10000
cqrs rehydrate -vehicle V1 -snapshot=false
cqrs rehydrate -vehicle V1 -snapshot=true
```

Each `rehydrate` prints events replayed and elapsed time. Without those two
numbers side by side, "snapshots are faster" is a claim, not a finding.

---

## 8. Phases

- [x] **04.1** Scaffold — folder, `go.work` entry, `deploy/compose.yaml`, README.
- [x] **04.2** Domain — `domain.go` + Ginkgo specs for BR-OD01..05. Red first.
- [x] **04.3** Write side — rehydrate (both modes), optimistic append, write consumer.
- [x] **04.4** Read side — read consumer, read store, `query`.
- [x] **04.5** `seed` + `rehydrate` timing, and write the finding into the README.
