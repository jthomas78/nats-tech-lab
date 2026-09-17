> Lesson 02 of demo 04. The demo's intro, its ports, how to run it and lesson
> 01 are in [`README.md`](../README.md), which the lab shell renders. One
> lesson, one file (04.12). The UI renders this file as
> **Overview → What it does** on the `02 · Scaling a consumer` screen.

## Lesson 02 — scaling a consumer

The rest of this demo folds one event at a time, in order, because a fold is a
fold. `cqrs pool` does it wrong on purpose: N workers bind to **one** durable
consumer and race each other. Throughput goes up. Order goes away.

Three facts decide everything that follows.

- **The position belongs to the consumer, not to a worker.** Adding workers
  adds hands, not places in the queue.
- **`MaxAckPending` is shared by the whole consumer.** Eight workers and a cap
  of three leaves five workers with nothing to do. That is a config answer, not
  a bug.
- **Demand order is not log order.** Worker 3 can be folding event 6 while
  worker 1 has already folded event 7.

### Lesson 02 has its own log

Since 2026-09-16 the pool reads `ODOMETER_POOL`, **not** `ODOMETER`. Its
subject is `evt.odometer-pool.>`, which does not overlap `evt.odometer.>`
because the SECOND token differs — a dot instead of the hyphen would put the
pool straight back inside the demo's own filter.

The split is not tidiness. The pool's consumer starves and redelivers on
purpose, and under `-kill-at` it abandons messages unacked. One press on
lesson 02 used to leave all of that on the log every other screen here is
drawn from.

Build it and fold it correctly in one step:

```bash
./cqrs pool -seed 10000
```

That writes 10 000 events to `ODOMETER_POOL` and folds them one at a time,
in order, into `odometer-pool-truth`. Nothing else reads that log.

The pool folds the same log into `odometer-pool`. That is the damaged
projection, kept apart from `odometer-pool-truth` on purpose: the pool is
deliberately wrong, and a demo that damaged the correct fold to show that
would have nothing left to compare against.

### What the damage looks like

`BR-OD08` is what turns reordering from a silence into a number. A fold refuses
an event whose sequence is behind its own position, terminates it, and counts
it. The run prints the count:

```
events      10001 handed out
folded      9994 acked
dropped     7 refused by BR-OD08 — out of order, terminated, gone
```

**The pool's total is short, never double.** A dropped event is a fact that is
gone: the fold's position never moves back, so nothing repairs it. Compare
`odometer-pool` against `odometer-pool-truth` and the difference is the cost
of the extra workers, in kilometres.

Both sides have to have folded the **same** log or the answer means nothing.
`odometer-read` folds `ODOMETER`, which lesson 02 never publishes to, so
subtracting it would report the pool's entire total as damage.

`BR-OD08` cannot fire while `-max-pending 1`. One message in flight is one
message in flight, whatever the worker count, so the pool behaves and proves
nothing.

### The four runs

Each one is the same pool under a different condition. They are the four tabs
in the UI.

```bash
./cqrs pool -seed 10000                                  # build the log first
./cqrs pool -workers 4 -max-pending 1000 -ack-wait 30s   # Live
./cqrs pool -workers 8 -max-pending 3                    # Starvation
./cqrs pool -workers 4 -ack-wait 30s -kill-at 94         # Redelivery
```

`-kill-at` is real fault injection, not a label. It counts the messages of
THIS RUN, not stream sequences — a re-seed leaves the stream numbering where
it stopped, so a fixed sequence stops existing. The worker that fetches that
sequence stops fetching and **never acks and never naks** — exactly what the
server sees when a process is killed. It must not nak: a nak redelivers at
once and hides the `AckWait` wait, which is the only thing the flag exists to
show. The event is redelivered after `AckWait`, the watermark refuses to
double-count it (`BR-OD07`), and the fold is stuck until then.

Each run blocks. Stop it with Ctrl-C.

### What the redelivery actually costs

Measured 2026-09-16, on one NATS 2.14.3 server in Docker on a laptop, four
workers. The pool keeps a kill clock in its own process, because nothing else
saw both ends: the server never reports a wait, only a delivery count, and the
worker that gets the event back is never the worker that lost it.

| AckWait | Waited | Handed to | Fold ran on | Outcome |
|---|---|---|---|---|
| `30s` | 30.02s | worker 3 → 4 | +29 events | dropped |
| `5s` | 5.001s | worker 4 → 1 | +30 events | dropped |

```bash
./cqrs pool -workers 4 -ack-wait 30s -kill-at 74040
./cqrs seed -vehicle truck-7 -n 40
./cqrs pool -workers 4 -ack-wait 5s -kill-at 74079
./cqrs seed -vehicle truck-7 -n 40
```

Those two runs were measured before lesson 02 got its own log, so they used
`cqrs seed` to top up `ODOMETER`. On `ODOMETER_POOL` the top-up is
`./cqrs pool -seed 40`, and the sequence numbers will be smaller. The
measurement stands; the commands to repeat it have moved.

Two things fall out of it.

The wait **is** `AckWait`, to within 20ms in both runs. The server is not
retrying after a failure — it is running a timer out. Set `AckWait` long and
you have chosen exactly how long the fold stays stuck.

And the wait **bought nothing**. The seed after each pool keeps the other
three workers folding, so by the time the event came back the watermark had
moved past it — and both redeliveries were dropped on arrival (`BR-OD07`). In
a pool, a redelivery after `AckWait` is not a recovery. The kilometres are
still gone.

The UI's **Redelivery** tab runs this fault itself and draws the redelivery
it caused. The two runs above are where the finding came from; the tab is how
you repeat it.

### Do the two projections agree — yes, and a rebuild is exact

The snapshotter folds into `odometer-write`; the projector folds into
`odometer-read`. Separate processes, separate durables, one log. Measured
2026-09-16: both buckets and both durables were deleted, then both consumers
folded all 74 135 events from sequence 1 again. The log was never touched.

| Vehicle | write `lastSeq` | read `lastSeq` | read `totalKm` |
|---|---|---|---|
| `V1` | 1 | 1 | 0 |
| `V2` | 28 | 28 | 37 |
| `rebuild-probe` | 74135 | 74135 | 25 |
| `truck-7` | 74109 | 74109 | 74327 |
| `ui-1` | 24 | 24 | 12.5 |
| `ui-demo-1` | 5 | 5 | 42 |

```bash
nats --context lab4-odometer kv del odometer-write -f
nats --context lab4-odometer kv del odometer-read -f
nats --context lab4-odometer consumer rm ODOMETER odometer-snapshotter -f
nats --context lab4-odometer consumer rm ODOMETER vehicle-projector -f
./cqrs snapshotter &
./cqrs projector &
```

Same position everywhere, and the same `status` and `plate`. **All ten KV
documents came back byte-identical to what they held before the wipe** —
`lastTripAt` included, to the nanosecond. That is the result worth having:
`project()` uses the event's own stream timestamp and never the wall clock, so
rebuilding the read model next year gives the answer it gave today.

They do **not** agree on `totalKm`, and must not. `Vehicle` — the write-side
aggregate — has no such field: `Travelled` leaves it unchanged, because no
command is ever judged against a distance. Ten thousand trips move the read
model and leave the aggregate byte-for-byte identical. That is why a write-side
snapshot stays tiny however long the log gets, and it is CQRS in one line.

### Does `MaxAckPending` starve workers — no

Measured 2026-09-16, on one NATS 2.14.3 server in Docker on a laptop, eight
workers, 74 109 events in the stream, `-drain` so every run reads the same log.

| MaxAckPending | Time | Events/s | Workers that acked | Folded | Dropped | Loss |
|---|---|---|---|---|---|---|
| **1** | 1m34.1s | 788 | **8 of 8** | 74109 | **0** | 0.0% |
| 3 | 50.0s | 1481 | **8 of 8** | 57529 | 16580 | 22.4% |
| 1000 | 33.2s | 2229 | **8 of 8** | 31912 | 42197 | 56.9% |

The cap of 3 was run twice — 54.6s and 16 722 dropped the second time — so
these are a race, not a constant.

```bash
./cqrs pool -workers 8 -max-pending 1 -drain
./cqrs pool -workers 8 -max-pending 3 -drain
./cqrs pool -workers 8 -max-pending 1000 -drain
```

**No worker starved, at any cap.** Not even at a cap of one: all eight acked,
within 2% of each other (`w1:9263 … w8:9264`). A worker acks, a slot frees,
the next fetch is served. `MaxAckPending` throttles the **consumer**; it does
not idle a worker.

What it really is, is the **loss dial**. A smaller cap is slower and drops
less, because there is less in flight to reorder. At a cap of 1 this pool
folds the whole log and drops **nothing** — same eight workers, same code, a
third of the speed.

The [nats.io worker-pool page][nats-wp] says a low cap "starves a large set of
workers". Both are true, about different things. At an **instant**, yes —
sampled while a capped pool ran, the consumer reported `num_ack_pending 3` and
`num_waiting 4`–`5` of the 8, every time:

```bash
nats --context lab4-odometer consumer info ODOMETER_POOL odometer-pool -j
```

Over a **run**, no — every worker gets a turn. Set the cap for the loss you
can accept, not to keep workers busy.

[nats-wp]: https://docs.nats.io/learn/jetstream/worker-pool

The UI's **Starvation** tab runs these four caps itself. The runs above are
where the finding came from; the tab is how you repeat it.

### Does it actually go faster — 1 vs 4

Measured 2026-09-15, on one NATS 2.14.3 server in Docker on a laptop, 10029
events in the stream, `-drain` so every run rebuilds from sequence 1 and
answers the same question.

| Workers | Time to drain | Events/s | Folded | Dropped |
|---|---|---|---|---|
| 1 | **23.5 s** | 426 | 10029 | **0** |
| 2 | **12.2 s** | 824 | 10025 | 4 |
| 4 | **6.3 s** | 1600 | 6947 | **3082** |
| 8 | **4.8 s** | 2100 | 4338 | **5691** |

**Yes, it goes faster. 3.7x at four workers.** And read the last column.

At four workers the pool threw away **31% of the log**. At eight, **57%**. The
kilometres in `odometer-pool` are not late, they are gone: a dropped event is
a fact the fold refused, and the fold's position never moves back.

One worker drops nothing, because one worker cannot race itself. That row is
the control, and it is why the table has four rows and not one.

**This is the whole lesson.** You are not buying throughput for free. You are
buying it with correctness, and the price is on the right-hand side of the
table. A pool is the right answer when the work per event is independent —
send an email, resize an image, call an API. It is the wrong answer for a
fold, because a fold is defined by order.

Reproduce it yourself:

```bash
./cqrs seed -vehicle truck-7 -n 10000
for w in 1 2 4 8; do ./cqrs pool -workers $w -drain; done
```

The UI's **1 vs 4** tab runs these four worker counts itself. The runs above
are where the finding came from; the tab is how you repeat it.

`-drain` is what makes the runs comparable: it rebuilds the pool's projection
from sequence 1 and stops when the consumer reports nothing left.

Your dropped counts will not match these exactly. Which worker gets which
message is a race, and a race is not repeatable. The shape is: one worker
drops nothing, and the count climbs hard with the worker count.

They will also be SMALLER than the table, and that is the fixture, not the
lesson. The runs above folded one vehicle's whole history, so every pair of
events was a collision waiting to happen. `ODOMETER_POOL` is seeded round
robin over three vehicles (2026-09-17 — it was ten until then, and ten left a
gap so wide the pool ran clean and taught nothing). Measured the same day on
10 000 events: one worker 0 dropped, four workers 13, eight workers 1 146.
