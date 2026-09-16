# CLAUDE.md — demos/04-jetstream-cqrs

**This folder is a sealed unit. Read this file instead of the root `CLAUDE.md`
for anything inside it.**

The root file describes demo 01 — a Postgres-backed, multi-service, multi-
frontend POC. Demo 04 is one NATS server and one small Go binary. Most of the
root file does not apply here.

Two things still apply, repo-wide: the session memory rules, and the general
preferences (stop if asked to do too much; don't read large docs whole;
delegate wide exploration).

## Everything for this demo lives in this folder

Plan, business rules, diagrams, Compose file, Go module, README. **Do not put a
demo 04 file anywhere else** — not in `.claude/plans/`, not in `obsidian/`, not
in a shared `diagrams/` directory.

| What | Where |
|---|---|
| Plan and phases | `docs/Demo-04-Plan.md` |
| Business rules | `BUSINESS_RULES-ODOMETER.md` |
| Diagrams (HTML + exported PNG) | `diagrams/` |
| Lab shell intro text | `README.md` |
| UI layout mockups | `diagrams/*.html` (dark UniFi palette) |
| Go module | `cqrs/` |
| Compose | `deploy/compose.yaml` |

The one exception is `go.work` at the repo root, which must list `cqrs/` for
the module to build.

## What this demo is

One question, answered with a number:

> **Rehydrating an aggregate — how much does a snapshot buy you?**

Demo 02's odometer has no write side: it publishes, then folds the result into
KV. Demo 04 adds the write side — a command that is checked against the state
the log already holds — and then shows the same rehydration with and without a
snapshot.

## What this demo is NOT

Agreed with the user 2026-09-14. Do not add any of it:

- no Postgres
- no cluster, no gateway, no supercluster, no JetStream domain
- no operator mode (a plain server, no `nsc` trust chain)
- no Temporal
- no `{context}` subject token

**Changed 2026-09-15 — a UI is now in scope.** "no frontend" and "no services"
were lifted for phase 04.6: a Vue app plus one thin HTTP shim (`cqrs serve`)
in front of the existing `domain.go`. The shim holds no rules. The rest of the
list above is unchanged. Read `docs/Demo-04-Plan.md` section 9 before touching
`frontend/`, `cqrs/serve.go` or `deploy/nats.conf`.

Multi-region belongs to demo 02. Accounts and topologies belong to demo 03.
Services, Postgres and UIs belong to demo 01.

## Relationship to demo 02

The domain is **lifted** from `demos/02-multi-region/odometer/`, then given a
lifecycle. The two are independent copies on purpose — demo 02 is sealed too,
and a shared package would couple two demos that are meant to be read alone.

Both demos name their stream `ODOMETER` and both run on their own NATS server.
That is not a clash, because they never share a server. Do not "fix" it by
renaming.

## Ports and contexts

| Thing | Value |
|---|---|
| NATS client | `4422` |
| NATS monitor | `8422` |
| NATS WebSocket | `20403` |
| Frontend | `20401` |
| Command API (`cqrs serve`) | `20402` |
| `nats` CLI context | `lab4-odometer` |

This demo's host ports follow `20<2-digit demo><2-digit increment>`, so they
never collide with demo 01's `7100-7299` bands. `4422` / `8422` predate the
scheme and stay.

**Every context name starts `lab4-`.** Demo 01 owns `sys` and `platform`, and
demo 02 owns `lab2-*`, in the same store (`~/.config/nats/context/`). An
unprefixed name here would silently overwrite one of theirs.

## The shim's routes

`cqrs serve` (port `20402`) is the only HTTP surface. It holds no rules — it
calls the same functions the CLI calls. Reads do NOT come through it: the
browser watches NATS over the WebSocket on `20403`.

| Route | Method | What it does |
|---|---|---|
| `/rehydrate` | POST | rehydrates one vehicle, with and without a snapshot |
| `/bench` | GET | the `ODOMETER_BENCH` fixture's size, in messages AND bytes |
| `/bench/seed` | POST | fills the fixture — 10 000, 100 000 or 1 000 000 |
| `/pool` | GET | lesson 02's state: stream size, buckets, whether a run is going |
| `/pool/run` | POST | runs the pool once, and answers with what that run measured |
| `/pool/stop` | POST | ends a run that is going (only Live runs open-ended) |
| `/pool/seed` | POST | fills `ODOMETER_POOL` |
| `/pool/rm` | POST | deletes `ODOMETER_POOL` and its three buckets |
| `/commands/` | POST | register, travel, retire — lesson 01's write side |

**Lesson 02 runs itself** (04.9). Every tab on the pool screen has a Run
button, and every number it shows comes from a run the reader just made.
There are no recorded measurements left in `frontend/src/view/` — deleting
them was 04.9.9, and `frontend/src/view/recorded.spec.js` fails if any come
back. `cqrs/docs_test.go` reads the routes out of `serve.go` and fails if
this table falls behind.

A run outlives the request that started it (D11), so `/pool/run` answers when
the run ENDS. `/pool/stop` is the only way to end an open-ended one. The gate
in `serve.go` allows one run at a time.

## Storage names

Root rule, and it holds here: **streams are `SCREAMING_SNAKE`, KV buckets are
`lowercase-kebab`.**

| Kind | Name | Role |
|---|---|---|
| Stream | `ODOMETER` | the log — the only source of truth |
| KV | `odometer-write` | write-side snapshot: `{state, lastSeq}` |
| KV | `odometer-read` | read model, denormalised |
| Stream | `ODOMETER_POOL` | lesson 02's OWN log — nothing else reads it |
| KV | `odometer-pool` | lesson 02 — a THIRD fold, deliberately wrong |
| KV | `odometer-pool-truth` | lesson 02 — the same log folded correctly |
| KV | `odometer-pool-workers` | lesson 02 — one key per worker |
| Stream | `ODOMETER_BENCH` | the rehydrate fixture, disposable |
| KV | `odometer-bench-write` | the fixture's snapshots |

Two buckets, not one. The split is the demo — you can see it in `nats kv ls`.

`ODOMETER_POOL` and its three buckets belong to lesson 02 and to nothing else.
Its subject is `evt.odometer-pool.>`, which does not overlap `evt.odometer.>`
because the second token differs — a DOT instead of the hyphen would put the
pool inside the demo's own filter, the same trap `ODOMETER_BENCH` avoids the
same way. Build it with `cqrs pool -seed N`.

The split exists because the pool MISBEHAVES on purpose: its consumer starves
and redelivers, and under `-kill-at` it abandons messages unacked. Before
04.8 that happened on `ODOMETER`, the log every other screen is drawn from.

Careful: `ODOMETER` is a PREFIX of `ODOMETER_POOL`, so a half-copied name
still reads as plausible. A check for "does this name appear" must match on a
boundary — the frontend spec uses `/ODOMETER(?!_POOL)/`.

`odometer-pool` is kept apart from `odometer-pool-truth` on purpose: the pool
is deliberately wrong, and a demo that damaged the correct fold to show that
would have nothing left to compare against. The damage is measured against
`odometer-pool-truth` and never against `odometer-read` — that bucket folds a
different log.

`ODOMETER_BENCH` is a fixture, not the demo. Its subject is
`evt.odometer-bench.>`, which does not overlap `evt.odometer.>` because the
second token differs — a DOT instead of the hyphen would put the fixture
inside the demo's own filter. Build it with `cqrs bench -size N`, delete it
with `cqrs bench -rm`. Nothing else may be added to it, and nothing on the
other tabs may read from it.

## A count is never shown without its bytes

Standing rule, set by the user 2026-09-16. Anywhere this demo reports a
stream's message count — on screen, in the CLI, in a JSON body — it reports
the bytes that count consumes as well. A length is a number nobody can price:
100 000 000 events sounds reasonable right up to the moment you learn it is
7.6 GB.

It applies to `ODOMETER`, `ODOMETER_BENCH` and `ODOMETER_POOL`, and to
nothing else for now.
`frontend/src/view/format.js` has `formatBytes()`; `useOdometer.js` already
carries `bytes` beside `messages`.

## Quality rules

Same as the root file, scaled down:

1. Every business rule has a Ginkgo spec, written before the implementation.
2. `ginkgo ./...` from `cqrs/` is green before a phase is done.
3. Business rules live in `domain.go`. No rule enforcement in `main.go`,
   `write.go` or `read.go`.
4. `BUSINESS_RULES-ODOMETER.md` and `docs/Demo-04-Plan.md` change in the same
   commit as the rule.

The frontend has its own three, and all three must pass before a UI task is
done. Run them from `frontend/`:

```bash
npx vitest run                      # 431 specs, 29 files
npx eslint src --ext .js,.vue       # 0 errors; 3 warnings are the baseline
npm run build
```

Three live guards, not unit tests. Do not weaken one to make a screen pass —
adding to the list a guard checks is the right move.

- `frontend/src/view/commands.spec.js` parses `cqrs/main.go` for the
  subcommands and flags that actually exist, and fails when the UI prints a
  command the binary would reject.
- `frontend/src/view/recorded.spec.js` walks `src/` and fails if a deleted
  recorded-measurement file or constant comes back.
- `cqrs/docs_test.go` reads `serve.go`'s routes and fails if this file does
  not list one.

## The design gate

A phase entry in `docs/Demo-04-Plan.md` stays **PROPOSED** until the user
approves it. No tasks, no tests, no code before that. An entry marked PROPOSED
is a request for a decision, not a backlog item to pick up.

Nothing is PROPOSED right now.

**04.9 is COMPLETE** (2026-09-17) — lesson 02 runs itself: a Run button on
every tab, results from a real run instead of recorded constants, and a
progress bar for the whole of a multi-run set (D12, the condition it was
approved on). See `docs/Demo-04-Plan.md` section 12 and
`diagrams/lesson-02-run-buttons.html`.

**04.8 is COMPLETE** (2026-09-16) — lesson 02 got its own log,
`ODOMETER_POOL`, so the pool consumer stops misbehaving on the demo's own
stream. See `docs/Demo-04-Plan.md` section 11, decisions D8 to D12.

04.7.18 (Performance gets one target picker and one seed button) was approved
and completed 2026-09-16.

## Two mechanics that are easy to get wrong

**LimitsPolicy, not InterestPolicy.** This demo replays from sequence 1.
InterestPolicy discards a message once every consumer has acked it, and the
replay then returns nothing — with no error.

**The snapshot is always stale.** The write consumer is asynchronous, so the
snapshot trails the stream. Rehydration reads the snapshot and then replays the
tail from `lastSeq + 1`. Code that stops at the snapshot is a bug, not a
shortcut.
