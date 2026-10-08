# CLAUDE.md — demos/06-operator-trust-and-credentials

**This folder is a sealed unit. Read this file instead of the root `CLAUDE.md`
for anything inside it.**

Demo 06 is one bare `nats-server` on the host in **operator mode**, the host
`nats` CLI (`nats auth`), and shell scripts. No Docker, no Go, no Postgres,
no UI, no `nsc`. Most of the root file does not apply here.

Three things still apply, repo-wide: the session memory rules, the general
preferences (stop if asked to do too much; don't read large docs whole;
delegate wide exploration; one command per `Bash` call), and **the life of a
demo** — `demo-playbook.html`, four stages ending in a pattern cards PDF.
Demo 06's deck will be `docs/demo-06-pattern-cards.html` and `.pdf`. It does
not exist yet.

**Demo 06 builds everything from nothing.** It does not use, copy or start
demo 01's stack, keys or `nats/` folder. Demo 01 is background reading only.

**Demo 06 is decentralized authentication, and only that.** Decentralized
authentication = NATS **operator mode**: the operator → account → user JWT
trust chain delegates identity and permissions, so the server config lists
no user. Centralized authentication = **configuration mode**, where the
server config lists every user and permission (demo 05, NKeys included).
The split is "users managed in the server config" against "trust delegated
through signed JWTs" — not "files on disk". This demo also has a config
file and stores JWTs on disk, and is still decentralized.

Scope: NATS-native credentials, trust checks and permission enforcement.
No auth callout and no external identity provider. Demo 07 builds on this
demo's ideas, never on its files: demo 07 makes its own operator and
chain, and demo 06 never starts, feeds or waits for demo 07.

## What this demo is

> **Can a NATS server admit a user it has never seen by name, only through a
> signed operator → account → user chain, and then refuse that user when its
> credential expires or is revoked?**

- **Role: showcase + validation.** A person follows the README in a terminal
  and sees the chain admit and refuse. The expiry and revocation results are
  measured evidence for a choice: how long a credential should live, and
  whether revocation is fast enough to rely on.
- **Audience:** the author first; then a developer or architect following the
  walkthrough with no help.
- **Scenario, used everywhere:** one account `ORDERS`; an order publisher
  (`order-svc`) and an analytics reader (`analytics-reader`), on `orders.*`.
- **Sources:** only these two pages, read before any claim is written:
  <https://docs.nats.io/learn/security/operator-mode> and
  <https://docs.nats.io/learn/security/decentralized-auth>.
  Every behavior claim is then checked against the installed tools and
  recorded in `exercises/EXERCISE_OBSERVATIONS.md` with the tool versions.

Progression: **terminal exploration → repeatable scripts**. A lab-shell page
comes later, only if needed (`demo-playbook.html`, stage `03`, Activities).

## What this demo is NOT

Mention a boundary in one line where it helps; do not build it here:

- auth callout, WorkOS, or any external identity provider — **demo 07**
  (operator mode only; it does not exist yet)
- configuration-mode users, passwords, tokens or config-listed NKeys — that
  is centralized authentication, **demo 05**
- a second account, exports / imports, cross-account — one account only
- bearer tokens, operator signing-key rotation — follow-up only if a real
  question needs them
- multi-operator deployments (a server trusting more than one operator).
  **Allowed:** one throwaway, untrusted operator (`ROGUE`, exercise 01) whose
  only job is to be refused — a negative trust-chain test. The server never
  trusts it.
- `nsc`, account servers, the `memory` / URL resolvers — the `full` resolver only
- clusters, gateways, leaf nodes, JetStream, TLS
- a frontend or a lab-shell menu entry

Do not modify any other demo from here.

## Ports, names, and run state

| Thing | Value |
|---|---|
| NATS client | `127.0.0.1:4922` (loopback only) |
| NATS monitor | `127.0.0.1:8922` (loopback only) |
| `server_name` | `d06-server` |
| Operator | `D06` (a throwaway second operator `ROGUE` for one refusal test) |
| Account | `ORDERS` (plus the generated `SYSTEM`) |
| Users | `order-svc`, `analytics-reader`, `greedy-reader`; SYSTEM user `admin` |
| Env var prefix | `D06_` |
| Run state (gitignored) | `.run/` |

`4622` / `8622` would follow demo 05's `4522`, but demo 02's `za-2` holds
them. `4922` / `8922` are free and appear nowhere else in the repo
(checked 2026-10-08).

**Every `nats` command goes through `lab/nats.sh`.** It adds `--no-context`
(another demo's context is often selected) and points `XDG_DATA_HOME` and
`XDG_CONFIG_HOME` into `.run/`. Without that, `nats auth` writes this demo's
operator into the author's real store, `~/.local/share/nats/nsc`. Measured
2026-10-08: with the wrapper, nothing under `~/.local/share/nats` or
`~/.config/nats` changed.

## Secrets

- `.run/xdg-data/nats/nsc/keys/` holds every seed: operator, accounts,
  signing keys, users. `.run/creds/*.creds` hold a user JWT **and** its seed.
  The CLI writes all of them mode 600 (measured).
- `.run/` is in the root `.gitignore`. Check it is still there before adding
  any generated file. Never commit a `.creds` file or a seed.
- `lab/down.sh --clean` deletes `.run/`, which deletes the operator. The next
  run builds a new chain with new keys; old `.creds` files then fail.

## Process rules

- One server at a time. `lab/up.sh` refuses to start when `4922` is taken.
- `lab/down.sh` stops only the PID in `.run/nats-server.pid`, and only after
  checking that PID's command line names this folder's config. **Never
  `pkill nats-server`** — demos 03 and 05 run bare servers on the same host.
- `lab/up.sh` runs the server with the demo folder as its working directory,
  because the config's resolver `dir` is relative to it.

## Three kinds of file: walkthrough, convenience, validation

The goal is a **transparent** demo, in the NATS docs' style: explicit
commands, readable config, minimal helper logic. A reader must understand
and repeat the setup without reading a scripting framework. Keep three
kinds of file apart:

- **Walkthrough** (`exercises/EXERCISE-NN-TERMINAL-STEPS.md`): direct
  `nats auth` and `nats-server` commands, readable configuration and short
  explanations. This is the primary learning experience.
- **Convenience scripts** (`lab/`): thin wrappers around those same steps.
  They save typing, but never hide how the trust chain, credentials and
  resolver are configured.
- **Validation scripts** (`exercises/exNN-check.sh`): automated assertions
  and deliberate failures. These may need extra manipulation (for example
  the wrong-seed `.creds` file), but that machinery stays separate from the
  normal setup.

Prefer a native NATS command (`nats auth … info`, `account query`, the
monitor endpoints) to decoding a JWT. Where decoding is needed, keep it
small and say that decoding a JWT does not verify it. Never use
`nats auth … --json` in a walkthrough: it prints seeds (measured
2026-10-08). Use no inline Python in new work. Add another tool (for
example `jq`) only where it makes a needed step simpler, and check for it
explicitly.

## Layout

| What | Where |
|---|---|
| Lab-shell intro, requirements, walkthrough | `README.md` |
| Everything one exercise needs, in one flat folder | `exercises/` |
| — server configs | `exercises/config/exNN-nats-*.conf` |
| — numbered terminal steps (`NN` starts at `01`; `00` is invalid) | `exercises/EXERCISE-NN-TERMINAL-STEPS.md` |
| — measured results, with versions | `exercises/EXERCISE_OBSERVATIONS.md` |
| — the exercise's check script | `exercises/exNN-check.sh` |
| — negative-test machinery (wrong-seed `.creds`) | `exercises/wrong-seed.sh` |
| Shared tools (lib, nats wrapper, trust-conf, up, down) | `lab/` |
| Run state: key store, creds, resolver, log (gitignored) | `.run/` |

## Quality rules

- **Prediction is not a result.** A README "expected" line is labelled
  *prediction* until `exercises/EXERCISE_OBSERVATIONS.md` has the real output,
  the date and the versions.
- **A refusal proves nothing alone.** Every refusal is shown next to a
  positive control that got through, on the same server, in the same run.
- **Two refusals can look the same.** The client always prints only
  `Authorization Violation`. Tell causes apart by the server log (for
  example: an account fetch that timed out, versus an instant refusal).
- **Client report and server enforcement are different evidence.** Record
  both: the client's error line and exit code, and the server's log line.
- **Authentication and authorization apart, every time.** Authentication =
  the connection is admitted or refused. Authorization = a publish or
  subscribe is allowed or denied.
- **New connection and open connection apart, every time.** Expiry and
  revocation are measured on both. The docs describe new connections; never
  assume an open one behaves the same.

## Completion — not yet

The demo is not complete until, under the playbook:

1. exercises 01–03 are built and recorded as measured, each with a check script;
2. **another person** runs the README with no help (the showcase exit test);
3. the pattern cards deck exists (`pattern-cards` skill, stage 04).
