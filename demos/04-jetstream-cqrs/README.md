# Demo 04 — JetStream as an Event Source, with CQRS

**One question, answered with a number:**

> Rehydrating an aggregate — how much does a snapshot buy you?

Demo 02 publishes to a stream and folds the result into a KV bucket. It has no
write side: nothing checks a command against the state the log already holds.

Demo 04 adds that write side, then rehydrates the same aggregate two ways —
from sequence 1, and from a snapshot plus the tail — and prints what each one
cost.

## The shape

```
              command  (register / travel / retire)
                 |
                 v
   +-------------------------------+
   |  WRITE SIDE                   |
   |  1. rehydrate the aggregate   |<---- KV  odometer-write   (snapshot)
   |  2. check the business rule   |
   |  3. append, if nobody won the |
   |     race first                |
   +-------------------------------+
                 |
                 v
        stream  ODOMETER     (LimitsPolicy — replay needs it)
             |          |
   write consumer    read consumer
             |          |
             v          v
   KV odometer-write   KV odometer-read  <---- query
        (snapshot)       (read model)
```

Two KV buckets, on purpose. The split is the demo, and you can see it in
`nats kv ls`.

The same thing drawn properly, with the two rehydration modes as two separate
diagrams:

![Event sourcing and CQRS in demo 04](diagrams/cqrs-blocks.png)

> Source: `diagrams/cqrs-blocks.html`. Re-export it with:
> `node ../01-dictionary/diagrams/export-html-png.mjs diagrams/cqrs-blocks.html diagrams/cqrs-blocks.png 1400`

## What is in here, and what is not

One NATS server, one Go binary, one Compose file, one stream, two KV buckets,
and a small Vue UI that watches all of it. Lesson 02 adds two more buckets,
but only once you run it.

No Postgres, no cluster, no gateway, no operator mode, no Temporal.
Multi-region is demo 02. Accounts and topologies are demo 03. Services are
demo 01.

## Ports

| Thing | Port | Needed for |
|---|---|---|
| NATS client | 4422 | the CLI |
| NATS monitor | 8422 | poking at the server |
| NATS WebSocket | 20403 | the UI reads through this |
| UI dev server | 20401 | the UI |
| Command API (`cqrs serve`) | 20402 | the UI writes through this |

Its own ports, so it runs beside demo 01 (4222) and demo 02 (4621+, 4721+).
The three 5-digit ones follow `20<demo number><increment>`, which keeps them
outside demo 01's 7100-7299 bands. `4422` and `8422` predate the scheme.

## How to run it

You need Docker and Go. Nothing else.

### The short way

One script starts everything — the NATS container, the command API on `20402`,
and both projectors. It waits until `/readyz` says the demo is ready, then
prints the two URLs.

```bash
demos/04-jetstream-cqrs/deploy/start.sh
```

Run it again any time. It reports what is already up and starts only what is
missing. To reverse it:

```bash
demos/04-jetstream-cqrs/deploy/stop.sh
```

The rest of this section is the same thing done by hand, one step at a time,
because the steps are the lesson.

### 1. Start the server

From `demos/04-jetstream-cqrs/`:

```bash
docker compose -f deploy/compose.yaml up -d
```

One NATS server, on port 4422. Wait a few seconds for it to be healthy.

### 2. Build the CLI

```bash
cd cqrs
go build -o cqrs .
```

Every command below takes `-url` if you moved the port. The default is
`nats://127.0.0.1:4422`.

### 3. Put a vehicle into service

```bash
./cqrs register -vehicle V1 -plate "CA 123-456"
```

It prints how it rebuilt the aggregate first, then the sequence it appended at.
The rebuild comes first on purpose: a command is checked against the log, not
against a row in a table.

### 4. Record some trips

```bash
./cqrs travel -vehicle V1 -km 12.5
./cqrs travel -vehicle V1 -km 7.5
```

### 5. Watch a rule refuse a command

```bash
./cqrs travel   -vehicle V1 -km 0            # BR-OD01 — km must be greater than 0
./cqrs register -vehicle V1 -plate "X"       # BR-OD03 — cannot register twice
./cqrs travel   -vehicle V2 -km 5            # BR-OD02 — V2 was never registered
```

Each one fails with the rule it broke. Nothing is written to the stream.

### 6. Run the two projectors

Each one blocks, so give each its own terminal, both in `cqrs/`.

```bash
./cqrs snapshotter
```

```bash
./cqrs projector
```

`snapshotter` keeps the write-side snapshot in KV `odometer-write`.
`projector` keeps the read model in KV `odometer-read`. They read the same
stream and do different jobs. That is CQRS.

### 7. Read the read side

```bash
./cqrs query -vehicle V1
```

One KV get. No replay, no rules. It prints the total km, the trip count, and
the last trip time.

### 8. Compare the two rehydrations

This is the measurement the demo exists for. See **The finding** below.

```bash
./cqrs seed      -vehicle V1 -n 10000
./cqrs rehydrate -vehicle V1 -snapshot=false
```

Now let `snapshotter` catch up — it is asynchronous, so give it ~20 seconds
for 10000 events — then stop it and run:

```bash
./cqrs rehydrate -vehicle V1 -snapshot=true
```

Both lines print events replayed and time taken. Put the two side by side.

### 9. Look at the storage (optional)

Needs the `nats` CLI on your machine.

```bash
nats context add lab4-odometer --server nats://127.0.0.1:4422
nats --context lab4-odometer stream ls
nats --context lab4-odometer kv ls
```

One stream, `ODOMETER`. Two buckets, `odometer-write` and `odometer-read`.
The split is the demo.

Run the worker pool (see **Lesson 02** below) and a second stream and three
more buckets appear: `ODOMETER_POOL`, `odometer-pool`, `odometer-pool-truth`
and `odometer-pool-workers`. They are lesson 02's, and they do not exist until
you run it.

### 10. Stop and clean up

```bash
docker compose -f deploy/compose.yaml down -v
```

`-v` deletes the JetStream volume. That wipes the stream and both buckets, so
the next run starts empty.

## Watch it in a browser

The CLI above is the whole demo. The UI shows the same demo moving: one log,
two KV buckets, and how far behind the head of the stream each side is.

You need the NATS server running (step 1) and Node 20 or newer.

### 1. Start the command API

The browser cannot publish to NATS on its own, and it is not allowed to decide
anything either. Every command goes through a thin HTTP shim that calls the
same `domain.go` the CLI calls. The shim holds no rules.

```bash
cd cqrs
go build -o cqrs .
./cqrs serve
```

It listens on `127.0.0.1:20402` and blocks. Give it its own terminal.

### 2. Start the two projectors

Same as step 6 above, each in its own terminal, both in `cqrs/`. Without them
the buckets stay empty and the screen has nothing to fold.

```bash
./cqrs snapshotter
```

```bash
./cqrs projector
```

### 3. Start the UI

```bash
cd frontend
npm install
npm run dev
```

Open <http://localhost:20401>.

The port is fixed on purpose. The command API's CORS list names
`http://localhost:20401` exactly, so a dev server on another port is refused
outright rather than half working.

### What is on the screen

The rail is a lesson index, not a list of data. Two rows, one per lesson; it never grows with the data.

| Rail row | What it shows |
|---|---|
| **01 · Stream + CQRS** | three tabs — Overview, Showcase, Performance |
| **02 · Scaling a consumer** | five tabs — Live, Starvation, Redelivery, 1 vs 4, odometer-pool |

Lesson 01 opens on **Overview**: a short summary, links to JetStream and CQRS
references, then this README and the existing diagrams.

**Showcase** watches `ODOMETER`: the write-side commands, the lag lane, both
KV stores side by side, then the newest events. Its vehicle picker narrows the
commands, lag and log; each bucket keeps its full key list below the selected
vehicle's document. The stream count travels with its bytes, and each storage
group prints the `nats` commands that reproduce it.

**Performance** holds Rehydrate, with its own live-vehicle picker and the
separate `ODOMETER_BENCH` fixture. The page heading and breadcrumb name the
lesson; vehicle selection belongs to the tab.

No button is ever greyed out by a rule. A trip of 0 km is sent, refused by
`domain.go`, and the refusal names the rule it broke. A rule the browser
enforced would be a rule you could not see working.

Nothing an accepted command returns is written into the panels. The command
gives back a sequence number; the screen learns what it meant from the read
path. That split is the demo.

A command typed in the terminal lands in the same stream and shows up on the
screen. The UI is a second door, not a second truth.

Rehydrate on lesson 01's **Performance** tab is the demo's headline question, measured while
you watch. Pick a vehicle, press **Run both**, and the write side rebuilds that
aggregate twice — once from sequence 1, once from the snapshot plus the tail —
and reports what each one cost. The three buttons are the whole control: **Run
both**, or either side on its own.

Five things about that tab are deliberate.

- **It never runs on its own.** A rebuild from sequence 1 reads every event that
  vehicle ever had. That must not happen because somebody clicked a tab.
- **The measurement reads and never writes.** It is a `GET`, it appends nothing,
  and you can press it as often as you like. The write-side command row is not
  on this tab — it is on Showcase, where pressing a button and watching the
  tables move is the point. Above a
  measurement it would only invite you to change the thing being measured.
- **The one thing that does write builds a different log.** The seed control at
  the top of the tab fills `ODOMETER_BENCH`, a throwaway stream, with 10 000,
  100 000 or 1 000 000 events, so the rebuild below has real work to do. It
  never touches `ODOMETER`, and it runs before any measurement of it exists —
  which is why it does not break the rule above. The button prints its own
  command (`cqrs bench -size 1000000`) so you can do the same thing in a
  terminal, and the panel reports what the fixture holds as **both a count and
  a size on disk**. A million events is about 2.2 seconds. `cqrs bench -rm`
  deletes the whole thing.
- **It needs one vehicle.** An aggregate is one vehicle, so "all vehicles" is
  not a thing you can rehydrate. The bench vehicles are not in the picker —
  they are in another bucket — so the fixture table hands them over itself.
- **It will not flatter itself.** If the two sides rebuild different states, the
  panel prints no speed-up at all — only the reason. Section *The number used to
  be wrong* below is why that rule is in the code.

## The finding

Measured 2026-09-16, on one NATS 2.14.3 server in Docker on a laptop, with
10001 events on one vehicle. Best of three runs each way.

| Rehydration | Events read | Time |
|---|---|---|
| From sequence 1, no snapshot | 10001 | **25 ms** |
| From the snapshot, then the tail | 0 | **1.1 ms** |

**About 23x, and it grows with the log.** The replay is linear: every command
on the write side reads all 10001 events again, and reads more tomorrow. The
snapshot read is one KV get and does not care how long the log is. Extend the
log to 100k events and the left-hand number goes up tenfold while the
right-hand one does not move.

### The number used to be wrong, and the reason is worth more than the number

This table read **8.2 s** and **about 600x** until 2026-09-16. Two things were
wrong with it, and the smaller one was the arithmetic.

The replay read the log with `Consumer.Next()`. On an **ordered** consumer that
call resets the consumer every time — the client deletes the server-side
consumer and creates a new one per message. The demo was making **one consumer
per event**. It was visible on the server, because an ordered consumer's name
carries a serial number:

```
5yL5cUQZHSfSpLY8WvMDXR_32469   delivering stream sequence 32478
```

So the 8.2 seconds was about 99% consumer bookkeeping and about 1% reading a
log. `Messages()` creates one consumer and pulls batches over it, which is what
the client's own documentation on `Next()` tells you to do.

The lesson survives the correction, and it is the same lesson: replay is linear
in the length of the log and a snapshot is constant. What changed is that the
number now measures that, and not a mistake in the demo.

Two things the number still does not say:

- **The snapshot is always behind.** The write consumer is asynchronous, so it
  trails the log. Rehydration reads the snapshot and then replays from
  `lastSeq + 1`. Measured mid-catch-up, the same command read 2900 events of
  tail in 12 ms. Code that trusts the snapshot and stops there is a bug.
- **The log is still the only source of truth.** Delete both KV buckets and
  everything comes back. Delete the stream and nothing does.

Reproduce it yourself with **How to run it**, step 8.

## Lesson 02 — in its own file

Scaling a consumer is a lesson of its own: one stream, many workers, the order
the fold loses, and what it costs. It lives in
[`docs/LESSON-02.md`](docs/LESSON-02.md), and the UI renders that file as the
Overview of the `02 · Scaling a consumer` screen.

Its mechanism is drawn, not just described: four figures in
[`diagrams/lesson-02-how-it-works.html`](diagrams/lesson-02-how-it-works.html)
— one stream with many workers, where the order is lost, redelivery, and what
`MaxAckPending` caps. They are the second Overview sub-tab, `How this works`
(04.11). They carry no measurements on purpose: the numbers belong to the run
you press, on the tabs beside them.

One lesson, one file (04.12). This intro is lesson 01 and the parts both
lessons share — ports, how to run it, the command list.

## The commands

| Command | What it does |
|---|---|
| `register -vehicle ID -plate P` | put a vehicle into service |
| `travel -vehicle ID -km N` | record one trip |
| `retire -vehicle ID -reason R` | take a vehicle out of service |
| `rehydrate -vehicle ID [-snapshot]` | rebuild the aggregate and report the cost |
| `seed -vehicle ID -n N` | write N trips, to make the replay worth timing |
| `snapshotter` | run the write-side projector (blocks) |
| `projector` | run the read-side projector (blocks) |
| `query -vehicle ID` | read the read store — one KV get, no replay |
| `serve` | run the command API the UI posts to (blocks) |
| `pool -workers N [-max-pending N] [-ack-wait D] [-kill-at N] [-drain]` | N workers racing on one durable consumer (blocks) |

## Status

Phases 04.1 to 04.6 are done. Phase 04.7 — the worker pool — is built and
runs; its four timings are not measured yet. See `docs/Demo-04-Plan.md`.

## Tests

```bash
cd demos/04-jetstream-cqrs/cqrs
ginkgo ./...
```
