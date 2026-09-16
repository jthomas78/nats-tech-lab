# Business rules — demo 04 odometer

Every rule below is enforced in `cqrs/domain.go` and proved by one Ginkgo
`Context` in `cqrs/domain_test.go`. Nothing is enforced in `main.go`,
`write.go` or `read.go`.

BR-OD01..05 are **command** rules: they decide whether a fact may be appended
to the log. BR-OD06..09 are **fold** rules: they decide whether a fact that is
already in the log may be applied to a projection. Both kinds live in
`domain.go`, because both are answers the demo must give the same way every
time, in the CLI and in the browser alike.

| ID | Rule | Error | Enforced by |
|---|---|---|---|
| BR-OD01 | `km` must be greater than 0 | `ErrNonPositiveKm` | `Vehicle.Travel` |
| BR-OD02 | A vehicle must be registered before it travels | `ErrNotRegistered` | `Vehicle.Travel` |
| BR-OD03 | A vehicle cannot be registered twice | `ErrAlreadyRegistered` | `Vehicle.Register` |
| BR-OD04 | A retired vehicle refuses trips | `ErrRetired` | `Vehicle.Travel` |
| BR-OD05 | A vehicle cannot be retired unless it is registered | `ErrNotRegistered` / `ErrRetired` | `Vehicle.Retire` |
| BR-OD06 | A fold applies an event only when its stream sequence is ahead of the fold's position | — | `Fold.Next` |
| BR-OD07 | A sequence equal to the fold's position is a redelivery and is ignored | — | `Fold.Next` |
| BR-OD08 | A sequence behind the fold's position is out of order and is refused | `ErrOutOfOrder` | `Fold.Next` |
| BR-OD09 | An event this code cannot read is permanent: it is dropped, never retried | `ErrUndecodable` | `decode`, `vehicleIDFrom`, `Permanent` |

## Notes that are easy to lose

**BR-OD01 is why the total can only rise.** Without it, one corrupt event
lowers a total on every future replay, and a total that can fall is a total
nobody trusts.

**BR-OD03 also covers a retired vehicle.** A retired vehicle has been
registered once already, and "twice" is what the rule forbids. Re-registering
one returns `ErrAlreadyRegistered`, not a fresh registration.

**BR-OD02 and BR-OD04 are checked before BR-OD01.** A caller who is told
"vehicle is retired" and a caller who is told "km must be greater than 0" need
different fixes, so the lifecycle answer wins.

**No rule reads a total.** That is why `Vehicle` (the aggregate) has no
`TotalKm` and `Odometer` (the read model) does. If a future rule needs a total
— a service interval, say — it moves into the aggregate then, and not before.

**BR-OD06..08 split one comparison into three answers.** Before phase 04.7 the
snapshotter and the projector both wrote `if seq <= lastSeq { return nil }`.
That single line gave the same answer — silence — to two different situations,
and only one of them is harmless.

| Situation | Test | Answer | Why |
|---|---|---|---|
| A new fact | `seq > lastSeq` | apply it | BR-OD06 |
| The same fact again | `seq == lastSeq` | ignore it | BR-OD07 — JetStream delivers at least once |
| An older fact, after a newer one | `seq < lastSeq` | refuse it | BR-OD08 — the fold has already moved past it |

**BR-OD08 cannot fire while `MaxAckPending` is 1.** One message is in flight at
a time, so a redelivery always arrives before anything newer has been folded,
and `seq < lastSeq` is unreachable. That is why adding this rule changes
nothing about the snapshotter or the projector as they run today. It only
speaks when a **worker pool** is put in front of the same fold — and then it
says out loud what the old `<=` said with silence.

**A watermark makes redelivery safe. It does not make reordering safe — it
makes reordering silent.** BR-OD08 exists to end the silence. A fold that
returns `ErrOutOfOrder` is nak'd and retried; a fold that returns `nil` acks an
event it never applied, and the total is quietly short for ever.

**No fold rule reads a total either.** `Fold` holds a position, not a state.
It answers "may this event be applied", and the caller then applies it with
`Vehicle.Apply` or `Odometer.Apply` exactly as before.

**BR-OD09 is the difference between a bad EVENT and a bad MOMENT.** NATS being
unreachable is a bad moment: the event was always valid, and a retry gets it.
An event whose type nobody knows is a bad event: the bytes are in the log for
ever, and the tenth delivery reads exactly like the first. `Permanent(err)`
answers which one you have, so a consumer never has to match on the text of an
error message.

**It is easy to hit, and it used to hang the demo.** The stream filter is
`evt.odometer.>`, which is wider than the subject this code writes. Before this
rule, one command —

```bash
nats --context lab4-odometer pub evt.odometer.oops '{}'
```

— stopped **both** long-lived folds for good. `decode` failed, the consumer
nak'd, the server redelivered the same message, and `MaxAckPending: 1` meant
nothing behind it moved either. The only sign was one log line per `AckWait`.
Now the fold terminates that one message, says so on a `DROPPED` line, and the
rest of the log flows.

**There is deliberately no `MaxDeliver` on any consumer.** A `MaxDeliver` cap
looks like the same fix and is not: when the server gives up, the client is
never told, so a transient failure becomes a silent loss. Silent loss is the
exact thing BR-OD08 exists to end. In this demo a message is dropped by a fold
that says why, or it is not dropped at all.

**The pool counts BR-OD09 apart from BR-OD08.** `Dropped` on screen means "the
pool lost this event to racing", which is the lesson that panel teaches. An
unreadable event was never the pool's fault, so it is logged and not counted
there.
