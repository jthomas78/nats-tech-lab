# Demo 04 — JetStream as an Event Source, with CQRS

**Status:** Phases 04.1-04.5 DONE — rules signed off 2026-09-14, finding in
`README.md`. Phase 04.6 (a UI) is PROPOSED 2026-09-15 and awaits approval.
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
