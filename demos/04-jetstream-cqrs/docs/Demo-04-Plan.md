# Demo 04 — JetStream as an Event Source, with CQRS

**Status:** Phases 04.1-04.6 DONE — rules signed off 2026-09-14, finding in
`README.md`. Phase 04.7 (a worker pool) was APPROVED 2026-09-15 and is in
progress.
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

**Out (agreed 2026-09-14):** no Postgres, no cluster, no gateway, no operator
mode, no Temporal, no `{context}` token.

**Changed 2026-09-15:** "no frontend" and "no services" are lifted for phase
04.6 only — a Vue UI and one thin HTTP shim over the existing domain code. See
section 9. Everything else in "Out" still holds.

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
- [x] **04.6** A UI for the demo — see section 9.
- [ ] **04.7** A worker pool in front of the fold — see section 10. APPROVED 2026-09-15, in progress.

---

## 9. Phase 04.6 — a UI for the demo (DONE)

**Status:** DONE 2026-09-15. Option B was approved and built inside this
folder: a Vue app on `20401` reading NATS directly over WebSocket `20403`,
and writing through a thin HTTP shim on `20402` that holds no rules.

This reverses two lines in section 1. The demo now gains a frontend and one
service. Everything else in "Out" still holds: no Postgres, no cluster, no
gateway, no operator mode, no Temporal, no `{context}` token.

### 9.1 Why

The demo's finding is a number, and a terminal prints it one line at a time.
The thing that is hard to see in a terminal is **lag** — the write snapshot and
the read model each sit at a different point behind the stream head, and they
move independently. A screen can draw all three positions at once.

Mockups: `diagrams/ui-mockups.html` (Option A and Option B side by side).
Option B was chosen.

### 9.2 Design decisions

**D1 — the browser never decides anything.** Every accept and every refusal
comes from `domain.go`. The UI renders the answer and the rule code. No
business rule is reimplemented in JavaScript. This is the whole reason a shim
exists rather than a browser-side write path.

**D2 — the shim is a transport, not a layer.** `cqrs serve` is a new subcommand
in the existing module. It parses JSON, calls the same functions the CLI calls,
and maps a domain error to an HTTP status plus the error name. It holds no
state and adds no rules. If a change to it needs a new rule, the rule goes in
`domain.go` and gets a Ginkgo spec first.

**D3 — reads go straight to NATS, not through the shim.** The browser opens its
own WebSocket to the demo's NATS server and watches both KV buckets and the
stream. The shim is write-only. This keeps the CQRS split visible in the
network traffic itself: commands go one way, reads come the other.

**D4 — the terminal still works.** The CLI is unchanged. A command typed in the
terminal and a command sent from the screen land in the same stream, and the UI
labels which one it was. Demoing both at once is the point.

**D5 — house style, not a new one.** The frontend imports `@unifi-theme` and
`@ui-shell` the way demo 01's four apps do. No new palette. This is the one
place demo 04 reaches outside its folder, alongside the `go.work` entry.

**D6 — NATS needs a config file now.** A `websocket { }` block cannot be set by
a command-line flag, so `deploy/compose.yaml` gains a mounted `nats.conf`. The
existing flags move into it. No behaviour changes.

### 9.3 Ports

New scheme for this demo: `20<demo number><increment>`.

| Port | What |
|---|---|
| `20401` | frontend dev/serve |
| `20402` | command API (`cqrs serve`) |
| `20403` | NATS WebSocket (browsers cannot speak the NATS TCP protocol) |

`4422` / `8422` are unchanged. Nothing here touches the 7100-7299 bands, which
belong to demo 01.

### 9.4 Command API surface

Three endpoints, one per command, all `POST`, all JSON.

```
POST /commands/register  {"id":"truck-7","plate":"CA 41-208"}
POST /commands/travel    {"id":"truck-7","km":42.0}
POST /commands/retire    {"id":"truck-7"}
```

Success: `200` with `{"seq":129}`. Refusal: `409` with
`{"rule":"BR-OD04","error":"ErrRetired","message":"..."}`. The rule code is
carried so the screen can show *which* rule refused, not just that one did.

CORS is open to the frontend origin only. There is no auth — this demo has no
accounts, and adding them belongs to demo 03.

### 9.5 Business rules

**No new business rules.** BR-OD01..05 are unchanged and the shim adds none.
`BUSINESS_RULES-ODOMETER.md` needs no edit for this phase. Confirm before code.

### 9.6 Layout

```
demos/04-jetstream-cqrs/
  deploy/nats.conf       NEW - jetstream + websocket block
  cqrs/serve.go          NEW - the HTTP shim
  cqrs/serve_test.go     NEW - Ginkgo specs: status codes, rule codes, CORS
  frontend/              NEW - Vue app, imports @unifi-theme and @ui-shell
```

### 9.7 Tasks

- [x] 04.6.1 `deploy/nats.conf` + compose change, websocket on `20403`. Verified:
      `/varz` reports the listener, a browser handshake on `20403` returns
      `101 Switching Protocols`, and `ODOMETER` + both KV buckets survived the
      container recreate.
- [x] 04.6.2 `cqrs/serve.go` + specs. Red first. Error mapping is the spec.
      45 specs green, 0 skipped. The specs run without NATS by injecting a fake
      `commandRunner` that calls the REAL decide function, so they still fail if
      a rule is ever reimplemented in the shim. Verified live against `4422`:
      all five rules return 409 with their BR code, and the three accepted
      commands landed on the stream carrying the optimistic-concurrency headers.
- [x] 04.6.3 Frontend scaffold on `20401`, theme wired, AppShell consumed.
      Vue 3 + Vite, no Pinia — this UI's state is a projection of two KV
      buckets and one stream, not its own. Imports `@unifi-theme` and
      `@ui-shell` via the two aliases in `vite.config.js`; the only files
      outside this folder are the `go.work` entry and the `demo04-odometer`
      entry in `.claude/launch.json`. Verified at 1920x1080: topbar, rail,
      the shared collapse control (`Collapse sidebar`) and the dark palette
      all render; no console errors, no dev-server errors, `vite build`
      succeeds and `eslint .` is clean.
- [x] 04.6.4 Read path — WebSocket connect, KV watch on both buckets, stream tail.
      `src/nats/` — `subjects.js` and `model.js` are pure and specced (25
      vitest specs); `useOdometer.js` is the plumbing and is verified against
      the live server instead, because a mocked WebSocket would only prove
      the mock works. Nothing in JavaScript decides anything (D1): the
      buckets and the log are read, never judged.
      The stream tail starts at `head - 199`, not at sequence 1 — `seed`
      writes 10 000 trips and a browser holding all of them would be
      measuring the browser.
      Two defects found and fixed while verifying: `connect()` set its guard
      after the first `await`, so a hot reload opened two connections; and
      the ordered consumer had a fixed `name_prefix`, so two viewers asked
      for the SAME consumer and the second took the first one's messages —
      an empty log with no error anywhere. The prefix is now unique per
      viewer, with a 30s `inactive_threshold` so a closed tab does not leave
      a consumer behind.
      Verified live at 1920x1080: 14 events in the log table, both buckets
      watched, the rail listing vehicles as they appear, a terminal command
      showing up on the screen (D4), read-side lag drawn honestly (write at
      seq 13, read at seq 8, `5 behind`) after stopping the projector, two
      tabs both working, and a `docker compose restart nats` recovering to
      Connected with all three subscriptions still live. No console errors.
- [x] 04.6.5 The lag lane, the two bucket panels, the log table.
      50 vitest specs green. The lane geometry and every bit of formatting are
      pure and specced in `src/view/` (`lane.js`, `format.js`) — the Vue files
      draw, they do not decide. Two honesty defects were found while verifying
      and fixed: a single key was being measured against the head of the WHOLE
      stream, so a vehicle parked since seq 1 read as "16 behind" when nothing
      about it had happened (fixed with a per-vehicle head, and an axis label
      that says which head is being drawn); and Go's zero time was drawn as a
      clock instead of `(never)`. A third defect was structural — the ordered
      consumer's name was a module-level constant, so a Vite hot reload gave
      the new component the same consumer name as the one still being torn
      down and the stream tail silently stopped; the name is now minted per
      `useOdometer()` call. Verified live at 1920x1080 with the projector
      stopped — global lane `write 21 / read 17 · 4 behind`, `truck-7`
      `0 / 4 events behind`, `V1` `both caught up`, bucket headers
      `folded to seq 21 · 0 behind` and `folded to seq 17 · 4 behind`, log
      rows 18-21 marked `write` only while 17 and below show `write · read` —
      and restarted, everything back to caught up at 21. Two tabs at once, no
      console errors, columns stack to one below 1100px, no horizontal scroll,
      `vite build` clean.
- [x] 04.6.6 Command bar + refusal display, wired to `20402`.
      64 vitest specs green (14 new). The write path is `src/commands/api.js`
      — pure, specced, and holding no rule: `bodyFor` sends an empty km box as
      `0` on purpose, because `0` is exactly what BR-OD01 exists to refuse, and
      no button is ever disabled by a vehicle's state. The only thing that
      disables a button is a command already in flight, plus an empty id, which
      is serve.go's own 400 and not a rule. An answer becomes one of three
      kinds — `accepted` (with the sequence it was appended at), `refused` (a
      rule code, the Go error name, the domain's message), `broken` (the
      plumbing; a rule code is never carried here, matching serve.go). Verified
      live against `cqrs serve` on 20402, all five rules refused from the
      screen: BR-OD01 `ErrNonPositiveKm` (0 km), BR-OD02 `ErrNotRegistered`
      (trip before register), BR-OD03 `ErrAlreadyRegistered` (register twice),
      BR-OD04 `ErrRetired` (trip after retire), BR-OD05 `ErrNotRegistered`
      (retire an unknown vehicle); and three accepted, appended at seq 22, 23
      and 24, each appearing in the log below with `write · read`. Stopping
      `cqrs serve` gave `broken · Unreachable` naming the port and the command
      to start, with no rule code. D4 confirmed: `cqrs travel` typed in the
      terminal appeared on screen as seq 25 and the command panel correctly did
      not claim it. Two tabs, no console errors on a fresh one, no horizontal
      scroll at 1920, `vite build` clean.
- [x] 04.6.7a "How it works" — the first rail entry, explaining the demo from
      the two files that already explain it. Notes are `README.md` rendered
      with `marked`; the class and sequence diagrams are
      `diagrams/demo04-jetstream-cqrs.html` in a frame. Both are imported
      `?raw`, so nothing is copied, nothing is fetched, and neither can drift
      from the file it came from. The frame gets `sandbox="allow-same-origin"`
      and no scripts — same-origin only so the panel can read its content
      height and grow to fit, which keeps one scrollbar on the page instead of
      two. Selecting it hides every live panel: a reader learning what CQRS
      means here should not have to read past a moving lag lane. Verified at
      1920: first nav item, notes render 8 sections with the README's own
      image resolved through Vite, frame sized to 6539px with no inner scroll,
      9 SVGs, frame background `rgb(20,23,27)` matching the app, no horizontal
      scroll, no live panel present. `npx eslint .` clean, `npx vitest run`
      68 passed (5 files), `npx vite build` clean.
- [x] 04.6.7 Update `README.md` with a port table and how to run the UI. The
      port table now carries all five ports with what each one is for, and
      names the `20<demo><increment>` scheme. "What is in here" no longer says
      "no frontend". A new "Watch it in a browser" section runs the command
      API, the two projectors and the dev server in order, and says why the
      port is fixed (the CORS list is exact-match). A panel table says what is
      on the screen, and the section repeats the one rule that matters: no
      button is greyed out by a business rule, and nothing a command returns is
      written into the panels. `serve` added to the command table; Status is
      now six phases.

      Also this task: the two block diagrams the user asked to drop — "The
      snapshot is always stale" and "Two commands at once" — were removed from
      `diagrams/demo04-jetstream-cqrs.html`, the intro line corrected from
      three block diagrams to one, and the PNG re-exported. The page is now 7
      figures and 7 SVGs; the About frame resized itself to 4469px with no
      inner scrollbar.

---

## 10. Phase 04.7 — a worker pool in front of the fold (PROPOSED)

**Status:** PROPOSED 2026-09-15. No code, no `nats.conf` change and no Vue
file until this section is approved.

Source: <https://docs.nats.io/learn/jetstream/worker-pool>. The scope is that
page and nothing else. Subject mapping and `partition()` are a different page
and are **out** — see D7.

This phase adds nothing to section 1's "Out" list. No Postgres, no cluster, no
gateway, no operator mode, no Temporal, no `{context}` token.

### 10.1 Why

A worker pool is the standard JetStream answer to "this consumer is too slow":
many workers bind to **one** durable consumer, and the server hands each stored
message to exactly one of them. It is the right tool for work that is
independent — send an email, resize an image, call an API.

This demo's two consumers are not independent work. They are **folds**. A fold
is order-dependent by definition, which is why `snapshotter.go` and `read.go`
both set `MaxAckPending: 1` and say so in a comment.

So the phase answers a question the demo cannot currently answer:

> **What does a worker pool actually buy, and what does it actually cost, when
> the work is a fold?**

The answer is a pair of numbers on one screen: `4 workers ≈ 2.9× faster` beside
`totalKm is short by 278 km`. Neither number means much alone.

### 10.2 Design decisions

**D1 — the two existing consumers are not touched.** `snapshotter` and
`projector` keep `MaxAckPending: 1` and keep their behaviour exactly. The pool
is a **third**, separate consumer (`odometer-pool`) writing to a **third**
bucket. If this phase changes a number in `odometer-write` or `odometer-read`,
it has failed.

**D2 — the pool is deliberately wrong, and says so.** The point is not to ship
a fast fold. It is to show, with the repo's own code and the repo's own
numbers, why the `MaxAckPending: 1` comment is there. A demo that only shows
the safe configuration cannot teach why it is safe.

**D3 — the silence ends. BR-OD08 turns a dropped event into an error.**
Today both folds write `if seq <= lastSeq { return nil }`. That treats a
redelivery and a reordering as the same thing and acks both. Under a pool the
second case is permanent, silent loss. BR-OD06..08 split the test in three, and
`seq < lastSeq` now returns `ErrOutOfOrder`.

**This cannot fire while `MaxAckPending` is 1**, so the change is inert for the
two existing consumers. That is what makes it safe to add.

**D4 — an out-of-order event is terminated, not retried.** A nak redelivers the
same sequence, and the fold's position never moves backwards, so the retry
fails again for ever. `ErrOutOfOrder` therefore calls `msg.Term()`: the event is
still lost, but it is **counted, logged and drawn**. Loud loss instead of silent
loss is the entire improvement.

**D5 — the UI reads a heartbeat bucket, not consumer info.** Each worker writes
its own key into a new KV bucket `odometer-pool-workers` — what it holds, how
many it has acked, how many it refused. The browser watches that bucket exactly
the way it already watches `odometer-write` and `odometer-read`, so no new data
path and no new NATS permission is needed. `$JS.API.CONSUMER.INFO` was the
alternative; it was rejected because it reports the **consumer**, and this
phase's whole subject is the **workers**.

**D6 — the panel is a viewer, not a control room.** Worker count,
`MaxAckPending`, `AckWait` and the kill are all CLI flags. The browser starts
nothing and stops nothing. The demo's two interaction paths stay genuinely
different: the CLI **does**, the UI **shows**. Every tab prints the command
that produced it, so nothing on screen is unreproducible.

**D7 — partitioning is out of scope.** A partitioned pool — subject mapping
with `{{partition(n, 1)}}`, one durable per shard — would restore per-vehicle
order and make the pool correct. It is real, but it is documented on
`/nats-concepts/subject_mapping`, not on the worker-pool page, and it needs a
`nats.conf` mapping block plus N consumers. It is a candidate for a later
phase, named here so nobody thinks it was missed.

**D8 — `-kill-at` is real fault injection, not a label.** The worker that
fetches that sequence stops fetching and never acks and never naks. That is
precisely what the server observes when a process is killed. It must **not**
nak: a nak redelivers at once and hides the `AckWait` wait that the tab exists
to show.

### 10.3 Business rules

**Three new fold rules. `BUSINESS_RULES-ODOMETER.md` is updated in the same
commit as the code.**

| ID | Rule | Error | Enforced by |
|---|---|---|---|
| BR-OD06 | A fold applies an event only when its stream sequence is ahead of the fold's position | — | `Fold.Next` |
| BR-OD07 | A sequence equal to the fold's position is a redelivery and is ignored | — | `Fold.Next` |
| BR-OD08 | A sequence behind the fold's position is out of order and is refused | `ErrOutOfOrder` | `Fold.Next` |

`Fold` is a small type in `domain.go` holding one `uint64` position. It holds
no state and reads no total; it answers "may this event be applied". The caller
then applies it with `Vehicle.Apply` or `Odometer.Apply`, unchanged.

`snapshotter.go` and `read.go` both drop their hand-written `<=` line and call
`Fold.Next` instead, so the rule is enforced in one place, in `domain.go`, for
every fold in the demo.

### 10.4 Storage

| Kind | Name | Role | New? |
|---|---|---|---|
| Stream | `ODOMETER` | unchanged | no |
| KV | `odometer-write` | unchanged, still `MaxAckPending: 1` | no |
| KV | `odometer-read` | unchanged, still `MaxAckPending: 1` | no |
| KV | `odometer-pool` | the pool's own fold — the damaged one | **yes** |
| KV | `odometer-pool-workers` | one key per worker: heartbeat, counters | **yes** |
| Consumer | `odometer-pool` | one durable, many workers bound to it | **yes** |

Both new buckets are `lowercase-kebab`, per the demo's storage rule. The
consumer shares its name with its bucket on purpose — one pool, one position,
one damaged projection.

`odometer-pool-workers` gets a short TTL so a worker that dies stops appearing
on the screen without anything having to delete its key.

### 10.5 CLI surface

One new subcommand in `main.go`'s switch, beside `snapshotter` and `projector`:

```
cqrs pool -workers N [-max-pending N] [-ack-wait D] [-kill-at SEQ] [-drain]
```

| Flag | Default | What it does |
|---|---|---|
| `-workers` | `4` | how many workers bind to `odometer-pool` |
| `-max-pending` | `1000` | `MaxAckPending` on the consumer — shared by all workers |
| `-ack-wait` | `30s` | `AckWait` on the consumer |
| `-kill-at` | off | the worker holding that sequence goes silent (D8) |
| `-drain` | off | stop when the consumer reports 0 pending, and print the elapsed time |

`-drain` is what makes the "1 vs 4" numbers comparable: every run answers the
same question against the same 10 000 event seed.

### 10.6 The UI

#### 10.6.1 The rail becomes a lesson index — option B, approved 2026-09-15

The demo teaches two things and the second only makes sense after the first, so
the rail says exactly that and nothing else:

```
GUIDE
  How it works
LESSONS
  01 · Stream + CQRS
  02 · Scaling a consumer
```

**Three rows, and it never grows** — not for a new vehicle, not for a new
bucket, not for a third lesson. Today's rail mixes four kinds of thing (a
guide, domain data, a lesson, four storage objects) as peers, and adding the
pool would have made it eleven rows and counting. Option A — numbered lesson
groups with the vehicles and buckets indented underneath — was drawn and
rejected; option B was chosen.

Mockups: `diagrams/nav-grouping-option-b.html` (chosen) and
`diagrams/nav-grouping-proposal.html` (option A, for the record).

**D9 — what leaves the rail becomes a control in the panel.** The vehicle list
becomes a picker in the pagehead. The storage objects become a tab strip. Both
lessons therefore carry a tab strip, and both use the repo's one tab style — a
real PrimeVue `Tabs` carrying `class="panel-tabs"`, the same as
`AboutPanel.vue`. Never a chip or pill toggle; chips stay reserved for filters.

| Lesson | Tabs |
|---|---|
| 01 · Stream + CQRS | Overview · ODOMETER · odometer-write · odometer-read |
| 02 · Scaling a consumer | Live · Starvation · Redelivery · 1 vs 4 |

**D10 — lesson 01's Overview tab must show both buckets side by side.** This is
not a layout preference, it is the demo. `CLAUDE.md` says *"Two buckets, not
one. The split is the demo — you can see it in `nats kv ls`."* The per-bucket
tabs exist for browsing keys; the Overview tab is where the split is argued,
and a change that leaves only one bucket visible there has broken the demo.

**D10a — `odometer-pool-workers` gets no tab of its own.** It leaves the rail
and does not reappear as a bucket tab, because lesson 02's Live tab already
draws its contents: one worker card per key. A bucket viewer beside that would
show the same data twice in the same panel.

**D11 — the breadcrumb carries the lesson.** `Demo 04 / 02 · Scaling a
consumer / Worker pool`, so a reader who lands deep still knows which half of
the demo they are in.

#### 10.6.2 Lesson 02 — the worker pool panel

Four tabs — the repo's one tab style, a real PrimeVue `Tabs` carrying
`class="panel-tabs"`, the same as `AboutPanel.vue`. Never a chip or pill toggle.

| Tab | Shows | Command it prints |
|---|---|---|
| Live | the log strip, the consumer, the worker cards, fold damage | `cqrs pool -workers 4 -max-pending 1000 -ack-wait 30s` |
| Starvation | events acked per worker, eight workers on a cap of three | `cqrs pool -workers 8 -max-pending 3` |
| Redelivery | one message's timeline across an `AckWait` | `cqrs pool -workers 4 -ack-wait 30s -kill-at 94` |
| 1 vs 4 | time to drain 10 000 events at 1, 2, 4 and 8 workers | `cqrs seed -events 10000` then `cqrs pool -workers N -drain` |
| odometer-pool | every key in the pool's bucket, beside `odometer-read` | `nats kv ls odometer-pool` |

**A fifth tab, added 2026-09-15 on the user's finding.** The first four are
conditions; this one is the read-only view, the same `nats kv ls` view lesson
01 gives `odometer-write` and `odometer-read`. Every bucket this demo folds
into can be browsed, because a fold with no read-only view is a number you
have to trust. It is drawn beside `odometer-read` because the pool's damage is
per vehicle: a single total says kilometres are missing, this says WHICH
vehicle lost them. `odometer-pool-workers` still gets no tab (D10a) — the Live
tab already draws its contents as worker cards.

Tabs, not four rail rows: the pool is one subject under four conditions, not
four subjects. Under option B the rail holds lessons only, so there was never a
row available for a condition.

Mockup: `diagrams/worker-pool-ui-mockup.html` (+ `.png`). Before/after of the
consumer topology: `diagrams/worker-pool-proposal.html` (+ `.png`).

`LagLane.vue` takes exactly two markers today (`writeSeq`, `readSeq`). It is
generalised to N markers so the pool's position can be drawn beside the other
two. The geometry stays pure and specced in `src/view/lane.js`; the Vue file
draws, it does not decide.

### 10.7 Layout

```
demos/04-jetstream-cqrs/
  cqrs/domain.go              EDIT - Fold + ErrOutOfOrder (BR-OD06..08)
  cqrs/domain_test.go         EDIT - one Ginkgo Context per new rule, red first
  cqrs/snapshotter.go         EDIT - call Fold.Next instead of its own `<=`
  cqrs/read.go                EDIT - call Fold.Next instead of its own `<=`
  cqrs/pool.go                NEW  - the pool subcommand, workers, heartbeats
  cqrs/pool_test.go           NEW  - Ginkgo specs: flags, Term on out-of-order
  cqrs/main.go                EDIT - one `pool` case in the switch
  deploy/nats.conf            NO CHANGE - nothing to permit, see 04.7.7
  frontend/src/App.vue        EDIT - sections becomes a 3-row lesson index (D9)
  frontend/src/components/
    PoolPanel.vue             NEW  - lesson 02, the four tabs
    PoolPanel.spec.js         NEW  - incl. the its that hold "1 vs 4" to the run
    StreamCqrsPanel.vue       NEW  - lesson 01, the four tabs, Overview first
    StreamCqrsPanel.spec.js   NEW  - the D10 acceptance test
    VehiclePicker.vue         NEW  - what the rail's Vehicles group used to be
    VehiclePicker.spec.js     NEW  - all vehicles is a real option
    BucketPanel.vue           NO CHANGE - its props already took a subject, 04.7.9a
    BucketKeys.vue            NO CHANGE - its props already took a subject, 04.7.9a
    LagLane.vue               EDIT - two markers become N
    PoolStrip.vue             NEW  - 04.7.4b, the chip strip + the consumer bar
    PoolStrip.spec.js         NEW  - 11 specs
  frontend/src/view/lessons.js      NEW  - the rail rows, the tabs, the crumb
  frontend/src/view/lessons.spec.js NEW  - written first, 14 specs
  frontend/src/view/lane.js   EDIT - laneRows(), the N-row geometry
  frontend/src/view/pool.js       NEW  - lesson 02's arithmetic, pure
  frontend/src/view/pool.spec.js  NEW  - written first, 22 specs
  frontend/src/view/drain.js      NEW  - 04.7.4's four runs, recorded as data
  frontend/src/view/drain.spec.js NEW  - written first, 8 specs
  frontend/src/view/strip.js      NEW  - 04.7.4b, chip states and the window
  frontend/src/view/strip.spec.js NEW  - written first, 16 specs
  frontend/src/view/redelivery.js NEW  - 04.7.5's two runs, recorded as data
  frontend/src/view/redelivery.spec.js NEW - written first, 11 specs
  frontend/src/view/commands.spec.js NEW - every printed command vs main.go
  frontend/src/view/starvation.js NEW  - 04.7.6's four runs, recorded as data
  frontend/src/view/starvation.spec.js NEW - written first, 14 specs
  RedeliveryTimeline.vue      NEW  - 04.7.5, the mockup's Tab 3 time line
  RedeliveryTimeline.spec.js  NEW  - written first, 6 specs
  cqrs/pool.go                EDIT - 04.7.5 kill clock, 04.7.6 PoolShare
  cqrs/pool_test.go           NEW  - written first, 4 + 5 specs
  cqrs/main.go                EDIT - 04.7.6, printPool prints busy/idle/spread
  frontend/src/config.js      EDIT - POOL_KV, POOL_WORKERS_KV
  frontend/src/styles/sides.css EDIT - --d4-lost, for loss only
  frontend/src/view/lane.js   EDIT - N markers, still pure, still specced
  frontend/src/nats/
    useOdometer.js            EDIT - watch BOTH pool buckets, optionally
    model.js                  EDIT - poolWorker(), one heartbeat to a row
    model.spec.js             EDIT - 4 specs for poolWorker
    subjects.js               EDIT - workerFromKey(), reverses `worker.{02d}`
  BUSINESS_RULES-ODOMETER.md  EDIT - BR-OD06..08
  README.md                   EDIT - port table unchanged, pool section added
```

No new host port. The pool is a CLI process, like `snapshotter` and
`projector`.

### 10.8 Tasks

- [x] 04.7.1 `domain.go` — `Fold` + `ErrOutOfOrder`, BR-OD06..08. Specs first,
      red, then green. `BUSINESS_RULES-ODOMETER.md` in the same commit.
- [ ] 04.7.2 `snapshotter.go` and `read.go` call `Fold.Next`. Prove the two
      existing consumers behave identically: same `lastSeq`, same `totalKm`,
      after a full `seed` + rebuild.
- [x] 04.7.3 `cqrs/pool.go` — the subcommand, N workers on one durable,
      heartbeats into `odometer-pool-workers`, `Term()` on `ErrOutOfOrder`.
- [x] 04.7.4 `-drain` and the 1/2/4/8 measurement. The real numbers are now in
      the README and on the "1 vs 4" tab, replacing the mockup's placeholders.

      Measured 2026-09-15, one NATS 2.14.3 server in Docker on a laptop, one
      seed of 10 000 trips on `truck-7`, 10029 events in the stream. `-drain`
      rebuilds the pool's projection from sequence 1, so all four runs answer
      the same question against the same log and the order they were run in
      does not matter (they were run 1, 2, 8, 4).

      | Workers | Drain | Events/s | Speed | Folded | Dropped |
      |---|---|---|---|---|---|
      | 1 | 23.5 s | 426 | 1.0x | 10029 | 0 |
      | 2 | 12.2 s | 824 | 1.9x | 10025 | 4 |
      | 4 | 6.3 s | 1600 | 3.7x | 6947 | 3082 (31%) |
      | 8 | 4.8 s | 2100 | 4.9x | 4338 | 5691 (57%) |

      **The phase's answer, in one line: it goes 3.7x faster at four workers,
      and it throws away 31% of the log to do it.** The one-worker run is the
      control and the only row that folds everything — one worker cannot race
      itself. The times reproduce; the dropped counts will not match exactly,
      because a race is a race, and the README and the UI both say so.

      The numbers are kept as DATA, in `frontend/src/view/drain.js`, not in
      markup — `MEASURED_AT`, `DRAIN_SOURCE` (the commands, one per line) and
      `DRAIN_RUNS`, with `drainRows()` deriving speed-up, loss share and bar
      width. Specced before use (8 its), including that each row accounts for
      every event it was handed, so a typed number fails rather than renders.

      The tab follows the mockup (`diagrams/worker-pool-ui-mockup.html`, Tab
      4) rather than the table first written here: bars for the speed, then
      two cards — what the pool bought, what it cost — then the terminal that
      produced them. Two cards, because the trade IS the lesson and a single
      table lets a reader take the fast number and walk away.

      This tab is the ONE place on lesson 02 that draws a number the page did
      not watch arrive, and `view/drain.js` says so in its header. A row may
      not be added there before the run has been made. The two guard its in
      `PoolPanel.spec.js` that used to assert "no timing appears" are now
      inverted: they assert the drawn figures match `DRAIN_RUNS`, cell for
      cell, so a drifted number fails the suite.

      Also aligned while here: the tab's header command was
      `cqrs seed -events 10000 && cqrs pool -workers N -drain`, which matched
      neither the seed flags actually used nor the terminal block below it.
      160 vitest specs green, `npm run build` clean, checked at 1920x1080.
- [x] 04.7.4a A caught-up pool explains itself, 2026-09-16. Worker heartbeats
      have a 10-second TTL (`PoolWorkerTTL`), so the three live tabs are blank
      unless a pool is running — that is correct and was reported as a
      regression, which means the screen was not saying it. Worse, a pool that
      has folded to the head draws `waiting · 0 acked` on every card and every
      starvation bar at zero: the same picture a broken page would draw.

      `poolCaughtUp(head, foldSeq, health)` in `view/pool.js` (5 specs) names
      that state. The signal is the FOLD POSITION against the head, not the ack
      counters — counters are per process and reset to 0 on every restart, so
      they would flicker; the fold position lives in KV and does not. Live and
      Starvation each gained a hint carrying `SEED_CMD` from `view/lessons.js`,
      and Redelivery gained a line saying to stop any running pool first, since
      every pool joins the same durable consumer. 169 vitest specs green,
      `npm run build` clean, checked at 1920x1080 against a live 4-worker pool.
- [x] 04.7.4b The chip strip, 2026-09-16. The mockup's Live-tab hero — the log
      drawn as one chip per sequence, with the pool's watermark on it and the
      single durable consumer as a bar underneath — had never been built; the
      Live tab opened on `LagLane` instead. The lane answers "how far behind is
      each worker" and has nowhere to put the one event about to be thrown
      away, which is the lesson. `PoolStrip.vue` draws it, `view/strip.js` holds
      the state maths (16 specs, written first).

      Five chip states, each tied to something the page already watches:
      `folded`, `mark` (lastSeq — the watermark, and it belongs to the
      CONSUMER), `flight`, `doomed` (held AND at or behind the watermark), and
      `pending`. `doomed` is the whole point: same shape as `flight`, one
      colour apart, because nothing about the event changed — only where the
      watermark got to.

      **The window is anchored on the action, not on the head.** A head-pinned
      window was built first and was wrong in the only case that matters: a
      seed puts the head 14 000 events ahead, the strip draws eight `pending`
      chips, and the watermark sits off-screen. It now anchors on the oldest
      event still held (the one at risk), keeps one folded chip for context,
      and `headGap()` prints `+N more · head #M` so the log never appears to
      stop where the chips do. Caught live at 1920x1080 against a running
      4-worker pool: `#72024 lastSeq`, three `flight` chips named by worker,
      four `pending`, `+1,998 more`.

      Not drawn, deliberately: the mockup's MaxAckPending / AckWait figures on
      the consumer bar. The heartbeat bucket does not carry the consumer's
      limits, so the panel does not know them, and a plausible `1000` is
      exactly the kind of number this demo does not put on screen. The props
      exist and a spec covers the blank. Also not drawn: the mockup's lifelines
      from the bar down to the worker cards — the lane sits between them in the
      real panel, so the lines would connect nothing.

      196 vitest specs green, `npm run build` clean.
- [x] 04.7.5 The redelivery measurement, 2026-09-16. The tab said the wait
      "is not measured on this screen" and it was right — nothing in the system
      knew. The server never reports a wait; it reports a delivery count of 2,
      and the worker that receives the event was never the worker that lost it.
      So `cqrs pool` grew a `killClock`: one clock for the whole pool, first
      kill wins, specced before it was written (`cqrs/pool_test.go`, 4 its).
      The worker that gets a `NumDelivered > 1` asks it how long the silence
      was and logs the answer. `NumDelivered == 1` now guards the kill itself,
      so a redelivered event is not killed a second time.

      Two runs, not one, because one run cannot tell a rule from a
      coincidence — kept in `frontend/src/view/redelivery.js` under the same
      rule `drain.js` has: a row may only be added after the run was made, and
      the command that made it is written beside it.

      | AckWait | waited | handed to | fold ran on | outcome |
      |---|---|---|---|---|
      | `30s` | 30.02s | worker 3 → 4 | +29 events | dropped |
      | `5s` | 5.001s | worker 4 → 1 | +30 events | dropped |

      **The finding, and it is harder than the tab expected.** The wait is
      `AckWait` and nothing else, to within 20ms — the server is not reacting
      to a failure, it is running a timer out. And the wait bought nothing:
      the other three workers kept folding during the silence, so both
      redelivered events arrived behind the watermark and were dropped on
      arrival (BR-OD07). In a pool, a redelivery after `AckWait` is not a
      recovery. The kilometres are still gone.

      That is drawn on the Redelivery tab as the mockup's Tab 3 time line
      (`RedeliveryTimeline.vue`), then the table, then the terminal that
      produced both. The line is the mockup's shape with one change: the
      mockup starts at the FIRST delivery, and the pool has no such number —
      its clock starts at the kill. So the line starts at the silence, and
      every point on it is a figure the run produced. It also says the thing
      the table cannot: the drop was decided while AckWait was still counting,
      by the 29 events the other workers folded meanwhile.

      The guard its in `PoolPanel.spec.js` were inverted the same way 04.7.4
      inverted its pair: they now fail if a figure on screen drifts from
      `view/redelivery.js`.

      **On the user's instruction, the printed commands are now a test.**
      `view/commands.spec.js` reads `cqrs/main.go` — where the one shared
      FlagSet and the subcommand switch both live — and holds every command
      the UI prints to it: the two recorded blocks, `SEED_CMD`, and every tab
      `cmd` read through `tabsFor`. A renamed flag now fails the suite instead
      of failing in front of a reader. Proven to bite: breaking one flag to
      `-kill-att` fails it. It is a text check, not an execution — a unit
      suite that needed a NATS server would simply be skipped, and the flags
      are what drift, not the server. 236 vitest specs, 62 Ginkgo specs.
- [x] 04.7.6 `-max-pending 3` and the starvation measurement — **the tab was
      wrong, and the measurement is what proves it.**

      The task was written to demonstrate starvation, because the
      [nats.io worker-pool page](https://docs.nats.io/learn/jetstream/worker-pool)
      says a low cap "starves a large set of workers", and the Starvation tab
      repeated it: *"Eight workers on a cap of three. Start it and watch five
      of them do nothing."* Nothing in the pool could check that claim —
      `PoolResult` carried only totals, and one worker doing everything looks
      exactly like eight sharing it evenly.

      So `PoolShare` / `shareOf()` were added to `pool.go` (specs first, 5 its
      in `pool_test.go`) and `printPool` now prints busy / idle / spread. Then
      four `-drain` runs at 8 workers over the same 74 109 events:

      | MaxAckPending | Time | Events/s | Acked | Folded | Dropped | Loss |
      |---|---|---|---|---|---|---|
      | 1 | 1m34.1s | 788 | **8 of 8** | 74109 | **0** | 0.0% |
      | 3 | 50.0s | 1481 | **8 of 8** | 57529 | 16580 | 22.4% |
      | 3 (repeat) | 54.6s | 1357 | **8 of 8** | 57387 | 16722 | 22.6% |
      | 1000 | 33.2s | 2229 | **8 of 8** | 31912 | 42197 | 56.9% |

      **No worker starved, at any cap.** At a cap of 1 the spread was
      `w1:9263 … w8:9264` — under 2% apart. A worker acks, a slot frees, the
      next fetch is served. `MaxAckPending` throttles the CONSUMER; it does
      not idle a worker.

      What it is, is the **loss dial**. Smaller cap = slower and drops less,
      because there is less in flight to reorder. At a cap of 1 the pool folds
      the whole log and drops nothing — same eight workers, same code, a third
      of the speed. Read beside 04.7.4 (which varied worker count at one cap),
      the two halves meet: the same trade appears on the knob you would
      actually reach for.

      **On the user's challenge, the cap was confirmed on the server, not in
      our code.** `nats consumer info ODOMETER odometer-pool -j` reports
      `max_ack_pending 3`, `ack_policy explicit`. Sampled 12 times during a
      live capped run it held at `num_ack_pending 3` with `num_waiting` 4–5.

      That also reconciles the two claims: the doc is describing an INSTANT
      (5 of 8 parked, which `num_waiting` confirms), and the tab was reading
      it as a RUN. Both sentences are on the tab now, labelled as such.

      Runs kept in `view/starvation.js` (spec-first, 14 its in
      `view/starvation.spec.js`), drawn on the Starvation tab as a table with
      a time bar and the cap-1 control marked, then the terminal that produced
      it. The guard its in `PoolPanel.spec.js` were inverted the same way
      04.7.4 and 04.7.5 inverted theirs — including one that fails if the
      words "do nothing" ever come back. 257 vitest specs, 67 Ginkgo specs.
- [x] 04.7.7 `deploy/nats.conf` — **no change needed, and that is the finding.**
      The task was written expecting a permission to add. There is none:
      `deploy/nats.conf` holds `server_name`, `port`, `http_port`, a
      `jetstream` block and a `websocket` block, and **no `authorization`,
      `accounts`, `users` or `permissions` block anywhere** — confirmed by
      grep across `nats.conf` and `compose.yaml`. This server grants
      everything to everyone by design (it is a single-server demo with no
      secrets, and its own comment says so). So the browser can already watch
      `KV_odometer-pool-workers` the moment the bucket exists, exactly as D5
      assumed. Do not invent a config change to close this task.
- [x] 04.7.8 `LagLane.vue` + `lane.js` — two markers become N. Specs first.
      `lane.js` gained `laneRows({ head, rows })` and `ROW`; a row is
      `{ id, text, seq, tone, kind }` and `kind: 'log'` draws full width.
      `lanePoints` is now three rows (write, log, read) on top of it, and a
      spec proves the three-row case lands on exactly the old coordinates —
      y 18/44/70, axis 88, viewBox height 104 — so nothing about lesson 01's
      drawing moved. `LagLane.vue` takes a `rows` prop for the general form
      and keeps `writeSeq`/`readSeq` for the CQRS form. `styles/sides.css`
      gained `--d4-lost` for a killed worker or a refused event; a worker
      takes the read colour, because a worker folds into a read model and is
      not a third side of CQRS. 78 vitest specs green.
- [x] 04.7.9a The rail becomes a lesson index (D9). `sections` drops to three
      rows; `VehiclePicker.vue` and lesson 01's tab strip take what left the
      rail; `BucketPanel.vue` and `BucketKeys.vue` read their subject from a
      tab. D10 is the acceptance test: both buckets still side by side on
      Overview. Verified at 1920x1080.
      **Done.** The rail's three rows, both lessons' tab tables and the D11
      breadcrumb are data in `frontend/src/view/lessons.js`, specced first in
      `lessons.spec.js` (14 specs) — so "the rail never grows", "no
      `odometer-pool-workers` tab" (D10a) and "every tab prints a command" are
      machine-checked without mounting Vue. `App.vue` now holds two separate
      refs, `view` (the lesson) and `vehicle`, where it used to hold one
      `view` string; picking a vehicle and picking a lesson could not both be
      true while they were one value. `StreamCqrsPanel.spec.js` is the D10
      acceptance test and it mounts the panel for real.
      **`BucketPanel.vue` and `BucketKeys.vue` needed NO change, and the plan
      line above said EDIT.** Both already took their subject from props
      (`side`, `bucket`, `keyName`/`rows`, `doc`, `head`); nothing in either
      read the rail. The task was written expecting an edit; there was none.
      **One real bug, found in the browser, not by a spec.** The picker came
      up blank. `null` is what every panel above it means by "all vehicles",
      but PrimeVue reads a null model value as "nothing is selected" and falls
      back to the placeholder. The picker now uses an internal sentinel and
      still emits `null` outwards; `VehiclePicker.spec.js` pins both halves.
      104 vitest specs green, `npm run build` clean, checked at 1920x1080.
      `PoolPanel.vue` exists as a STUB so lesson 02's row renders — its tab
      strip and commands are real, its tab contents are 04.7.9b's job. It
      draws no numbers, because the numbers are not measured yet (04.7.4).
- [x] 04.7.9b `PoolPanel.vue` — the four tabs, each printing its command.
      Verified at 1920x1080.

      The arithmetic went into `view/pool.js` first, specced before it existed
      (22 its), so the panel only draws. Transport was the cheap part: both
      pool buckets are watched exactly like the other two, so there is no new
      consumer-info call and no HTTP. The watches are OPTIONAL — `cqrs pool`
      may never have been run, and an absent bucket is a state of the world,
      not an error.

      Two findings, recorded rather than smoothed over:

      A heartbeat does not say "the last sequence I acked" (`WorkerState`,
      `cqrs/pool.go`), only what a worker is holding right now. So an idle
      worker is left OFF the lane instead of being drawn at sequence 0, which
      would claim it is the whole log behind when it is merely idle. A spec
      pins this.

      Two of the four tabs have no measurement yet and say so in words:
      **1 vs 4** draws nothing at all and prints the `-drain` commands, because
      04.7.4 has not been run; **Redelivery** shows what the server is doing
      but reports no duration, because nothing here measures one. Live and
      Starvation are fully live off the two pool buckets. Two its in
      `PoolPanel.spec.js` exist only to fail if a later change fills the
      empty tab in with plausible numbers.

      **Superseded 2026-09-15 by 04.7.4** for the 1-vs-4 tab only: the runs
      were made, so the tab now draws them and those two its were inverted to
      hold the drawing to the run.

      **Superseded 2026-09-16 by 04.7.5** for the Redelivery tab: the runs were
      made, so that tab now draws them too. Nothing on lesson 02 is unmeasured
      any more.

      **Corrected 2026-09-15**, on the user's finding: lesson 02 had no
      read-only bucket view at all. Lesson 01 gives every bucket it folds into
      a `nats kv ls` tab and lesson 02 folds into one too, so a fifth tab was
      added — `odometer-pool`, drawn beside `odometer-read`. On the live
      screen it shows real damage: truck-7 at 181.0 km / 11 trips in the pool
      against 247.0 km / 17 trips in the read model. The footer's "watching"
      line was wrong in the same way, naming three objects when the page had
      subscribed to four; it now names only the buckets that actually exist.

      148 vitest specs green, `npm run build` clean, checked at 1920x1080.
- [x] 04.7.10 README — a pool section and the four numbers.

      The section is written: what the pool is, the three facts that decide
      how it behaves, what BR-OD08 damage looks like, the three condition
      runs, and the `-drain` comparison.

      The four numbers were NOT in it. The 1-vs-4 table was present with every
      cell dashed and a line saying plainly that it had not been measured,
      matching the UI's "1 vs 4" tab. A table of times this demo never ran
      would break the only promise it makes.

      **Filled in 2026-09-15 by 04.7.4**, README and UI in one edit, as
      planned.

      Two other corrections while in the file: the UI panel table described
      the old rail and now describes the three-row lesson index (D9), and the
      Status line claimed all phases were done.

### 10.9 What would make this phase a failure

- A number in `odometer-write` or `odometer-read` changed (D1).
- A business rule enforced anywhere but `domain.go`.
- A button in the browser that starts, stops or configures a worker (D6).
- A tab that shows a number without the command that produced it.
- `partition()` or a `nats.conf` subject mapping appearing anywhere (D7).
- A rail row that is not a lesson or the guide (D9).
- Lesson 01's Overview tab showing one bucket instead of two (D10).
