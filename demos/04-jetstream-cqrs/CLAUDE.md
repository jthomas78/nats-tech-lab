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

- no frontend
- no Postgres
- no cluster, no gateway, no supercluster, no JetStream domain
- no operator mode (a plain server, no `nsc` trust chain)
- no Temporal
- no `{context}` subject token

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
| `nats` CLI context | `lab4-odometer` |

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

Two buckets, not one. The split is the demo — you can see it in `nats kv ls`.

## Quality rules

Same as the root file, scaled down:

1. Every business rule has a Ginkgo spec, written before the implementation.
2. `ginkgo ./...` from `cqrs/` is green before a phase is done.
3. Business rules live in `domain.go`. No rule enforcement in `main.go`,
   `write.go` or `read.go`.
4. `BUSINESS_RULES-ODOMETER.md` and `docs/Demo-04-Plan.md` change in the same
   commit as the rule.

## Two mechanics that are easy to get wrong

**LimitsPolicy, not InterestPolicy.** This demo replays from sequence 1.
InterestPolicy discards a message once every consumer has acked it, and the
replay then returns nothing — with no error.

**The snapshot is always stale.** The write consumer is asynchronous, so the
snapshot trails the stream. Rehydration reads the snapshot and then replays the
tail from `lastSeq + 1`. Code that stops at the snapshot is a bug, not a
shortcut.
