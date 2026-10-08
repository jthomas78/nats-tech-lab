# Demo 06 — Operator Trust and Credentials

**One question:**

> Can a NATS server admit a user it has never seen by name, only through a
> signed operator → account → user chain, and then refuse that user when its
> credential expires or is revoked?

In demo 05 the server config held a list of users. Here it holds **no
users at all**. It trusts one thing: an operator's public key. Every account
is signed by that operator, and every user by its account. A user proves who
it is by showing the chain and signing a challenge with its secret key.

**Role: showcase + validation.** You run it in a terminal and watch the chain
admit and refuse. The expiry and revocation timings are measured, so they can
back a real choice: how long a credential should live, and whether
revocation is fast enough to depend on.

**The scenario, used in every exercise:** one account, `ORDERS`. An order
service (`order-svc`) publishes order events on `orders.*`. An analytics
service (`analytics-reader`) reads them.

**This is decentralized authentication.** The server runs in NATS
**operator mode**. Trust is delegated through signed JWTs, so the server
config lists no user. Demo 05 was **centralized authentication**
(configuration mode): the config listed every user. The difference is who
manages users, not where files live. This demo has a config file and keeps
JWTs on disk too.

**Sources:** [Operator mode](https://docs.nats.io/learn/security/operator-mode)
and [Decentralized authentication](https://docs.nats.io/learn/security/decentralized-auth)
from the NATS docs. Auth callout (with WorkOS) is **demo 07**, also in
operator mode. Demo 07 builds its own chain; it does not need this demo.

## Requirements

| ID | Requirement | Exercise | State |
|---|---|---|---|
| `D06-R1` | The server config names no user and no account except `SYSTEM`. It trusts one operator JWT. | 01 | **measured** |
| `D06-R2` | A user whose account was pushed is admitted, and its messages are delivered. | 01 | **measured** |
| `D06-R3` | Refused: an account not yet pushed, no credentials, a chain from another operator (even after its account is pushed), and the right JWT with the wrong seed. | 01 | **measured** |
| `D06-R4` | The server stores no user. A user issued while the server runs is admitted with no restart and no push. | 01, 02 | **measured** |
| `D06-R5` | Publish and subscribe permissions travel inside the signed user JWT, and the server enforces them. | 02 | **measured** |
| `D06-R6` | A user signed with a scoped signing key gets the key's permissions, not what the user asked for. | 02 | **measured** |
| `D06-R7` | When a credential expires, new connections are refused and an open connection is cut. | 03 | **measured** |
| `D06-R8` | A revocation does nothing until the account is pushed. After the push, open connections are cut and new ones refused. | 03 | **measured** |
| `D06-R9` | Another person runs this README with no help and sees every exercise work. | all | not started |
| `D06-R10` | A pattern cards deck closes the demo (playbook stage 04). | — | not started |

**Demo 06 is not complete yet.** `D06-R9` (a walkthrough by another person)
and `D06-R10` (the pattern cards) are open.

**measured** = run, and recorded in
[`exercises/EXERCISE_OBSERVATIONS.md`](exercises/EXERCISE_OBSERVATIONS.md)
with the date and tool versions. Anything else in this README marked
"predict" is a question for you, not a result.

## What you need

- `nats-server` and the `nats` CLI on the host. Measured with
  `nats-server v2.14.6` and `nats` CLI `0.4.0` (it has `nats auth`; `nsc` is
  not used).
- `curl` and `lsof` (on macOS already).
- For the check scripts only: `jq`, and bash 5 for `ex03-check.sh`
  (`brew install jq bash`). Each script stops with a clear message if one
  is missing. The steps you type by hand need neither.
- Nothing else. No Docker, no Python.

## Ports and names

| Thing | Value |
|---|---|
| NATS client | `127.0.0.1:4922` |
| NATS monitor | `127.0.0.1:8922` |
| Server name | `d06-server` |
| Operator | `D06` |
| Account | `ORDERS` (and `SYSTEM`, made with the operator) |
| Users | `order-svc`, `analytics-reader`, `greedy-reader`; `admin` in `SYSTEM` |
| Keys, creds, resolver, log, PID file | `.run/` (not in Git) |

> **Run every `nats` command through `lab/nats.sh`.** It adds `--no-context`
> and keeps this demo's keys inside `.run/`. Plain `nats auth` would write
> them into your real key store, `~/.local/share/nats`.

## The exercises

| # | Question | State |
|---|---|---|
| 01 | Who gets in, when the server knows only the operator? | **built, measured** (by hand, then `ex01-check.sh`) |
| 02 | Where do permissions live, and who decides them? | **built, measured** (by hand, then `ex02-check.sh`) |
| 03 | What happens to a user when its credential expires, or is revoked? | **built, measured** (by hand, then `ex03-check.sh`) |

Each exercise has numbered steps in `exercises/EXERCISE-NN-TERMINAL-STEPS.md`.
Every check script **deletes `.run/` first** and builds a new chain, so run
the scripts after you finish the steps by hand, not in the middle.

```bash
exercises/ex01-check.sh
```

```bash
exercises/ex02-check.sh
```

```bash
exercises/ex03-check.sh
```

## Coverage of the operator-mode docs page

What this demo does with each topic on
[Operator mode](https://docs.nats.io/learn/security/operator-mode).
**Measured** = enforced behaviour, checked by a script. **Inspection** = a
claim or log line read, nothing enforced. **Not tested** = in scope, no
check. **Excluded** = out of scope on purpose. Nothing is planned beyond
this list.

| Docs topic | Exercise | Status |
|---|---|---|
| `operator add` makes `SYSTEM` and an operator signing key | 01 | inspection (`auth operator info`) |
| Account `Issuer` is the operator | 01 | inspection (I2) |
| `--defaults` puts a 1 MiB payload limit in the user JWT | 01 | inspection (I3); enforcement not tested |
| Boot log: Trusted Operators, issued, expires | 01 | inspection (I1) |
| `.creds` = user JWT + seed; the seed never leaves the client | 01 | measured (C12 wrong seed) |
| `.creds` file mode 600 | 01 | measured once (CLI writes 600) |
| No account JWT → `Authorization Violation` | 01 | measured (C1–C2) |
| `full` resolver, `system_account`, `resolver_preload` | 01–03 | measured (every run uses them) |
| `account push` with SYSTEM creds | 01 | measured (C3) |
| `account query` — the server's copy | 01, 03 | measured (Q1–Q2 in both) |
| User JWTs are never pushed; the server keeps no user | 01 | measured (C16, Q2) |
| An account edit has no effect until pushed | 03 | measured **for a revocation only**; a connection-limit edit not tested |
| Missing system account stops the server | — | measured once, by hand: removing only the config line does **not** stop it (the operator JWT supplies it). Absent from both: not tested |
| Revoke + re-push; credential expiry | 03 | measured (E1–E6, R1–R6) |
| Bearer JWTs | — | excluded |
| Memory, cache and URL resolvers; `allow_delete`, `limit` | — | excluded |
| A second account; exports / imports | — | excluded (one account only) |
| `nats server generate` scaffolding | — | excluded (`lab/trust-conf.sh` writes the same lines) |

## Reset

```bash
lab/down.sh --clean
```

This stops the server and deletes `.run/`: every key, credential and the
operator itself.
