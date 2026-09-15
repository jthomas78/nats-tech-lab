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
and a small Vue UI that watches all of it.

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

| Panel | What it shows |
|---|---|
| **How it works** | the first rail entry — this README, and the diagrams |
| **Command bar** | register, record a trip, retire |
| **Lag lane** | the stream head, and how far each side trails it |
| **Two buckets** | the same vehicle stored twice, for two different jobs |
| **Log** | every event, newest first, marked as each side folds it |

No button is ever greyed out by a rule. A trip of 0 km is sent, refused by
`domain.go`, and the refusal names the rule it broke. A rule the browser
enforced would be a rule you could not see working.

Nothing an accepted command returns is written into the panels. The command
gives back a sequence number; the screen learns what it meant from the read
path. That split is the demo.

A command typed in the terminal lands in the same stream and shows up on the
screen. The UI is a second door, not a second truth.

## The finding

Measured 2026-09-14, on one NATS 2.14.3 server in Docker on a laptop, with
10001 events on one vehicle.

| Rehydration | Events read | Time |
|---|---|---|
| From sequence 1, no snapshot | 10001 | **8.2 s** |
| From the snapshot, then the tail | 0 | **0.6 ms** |

**About 600x, and it grows with the log.** The replay is linear: every command
on the write side would pay that 8 seconds again, and pay more tomorrow. The
snapshot read is one KV get and does not care how long the log is.

Two things that number does not say:

- **The snapshot is always behind.** The write consumer is asynchronous, so it
  trails the log. Rehydration reads the snapshot and then replays from
  `lastSeq + 1`. With the snapshotter stopped and 4 new trips appended, the
  same command read exactly those 4 events in 4.7 ms. Code that trusts the
  snapshot and stops there is a bug.
- **The log is still the only source of truth.** Delete both KV buckets and
  everything comes back. Delete the stream and nothing does.

Reproduce it yourself with **How to run it**, step 8.

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

## Status

All six phases are done — see `docs/Demo-04-Plan.md`.

## Tests

```bash
cd demos/04-jetstream-cqrs/cqrs
ginkgo ./...
```
