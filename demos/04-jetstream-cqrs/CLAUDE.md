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

## Storage names

Root rule, and it holds here: **streams are `SCREAMING_SNAKE`, KV buckets are
`lowercase-kebab`.**

| Kind | Name | Role |
|---|---|---|
| Stream | `ODOMETER` | the log — the only source of truth |
| KV | `odometer-write` | write-side snapshot: `{state, lastSeq}` |
| KV | `odometer-read` | read model, denormalised |
| KV | `odometer-pool` | lesson 02 — a THIRD fold, deliberately wrong |
| KV | `odometer-pool-workers` | lesson 02 — one key per worker |
| Stream | `ODOMETER_BENCH` | the rehydrate fixture, disposable |
| KV | `odometer-bench-write` | the fixture's snapshots |

Two buckets, not one. The split is the demo — you can see it in `nats kv ls`.

`odometer-pool` is kept apart from `odometer-read` on purpose: the pool is
deliberately wrong, and a demo that damaged the read model to show that would
have nothing correct left to compare against.

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

It applies to `ODOMETER` and `ODOMETER_BENCH`, and to nothing else for now.
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
npx vitest run                      # 326 specs, 22 files
npx eslint src --ext .js,.vue       # 0 errors; 7 PoolPanel.vue warnings are the baseline
npm run build
```

`frontend/src/view/commands.spec.js` is a live guard, not a unit test. It
parses `cqrs/main.go` for the subcommands and flags that actually exist, and
fails when the UI prints a command the binary would reject. Do not weaken it
to make a screen pass.

## The design gate

A phase entry in `docs/Demo-04-Plan.md` stays **PROPOSED** until the user
approves it. No tasks, no tests, no code before that. An entry marked PROPOSED
is a request for a decision, not a backlog item to pick up.

Nothing is PROPOSED right now. 04.7.18 (Performance gets one target picker and
one seed button) was approved and completed 2026-09-16.

## Two mechanics that are easy to get wrong

**LimitsPolicy, not InterestPolicy.** This demo replays from sequence 1.
InterestPolicy discards a message once every consumer has acked it, and the
replay then returns nothing — with no error.

**The snapshot is always stale.** The write consumer is asynchronous, so the
snapshot trails the stream. Rehydration reads the snapshot and then replays the
tail from `lastSeq + 1`. Code that stops at the snapshot is a bug, not a
shortcut.
