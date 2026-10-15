# CLAUDE.md — demos/07-auth-callout-workos

**This folder is a sealed unit. Read this file instead of the root `CLAUDE.md`
for anything inside it.**

Demo 07 is one bare `nats-server` on the host in **operator mode**, the host
`nsc` (the whole trust chain) and `nats` CLIs (client connections), one small
Go auth callout service, and shell scripts. The callout service checks a
**real WorkOS** identity. No Docker, no Postgres, no UI. Most of the root
file does not apply here.

**Why `nsc` and not `nats auth`** (unlike demo 06): `nats auth` CLI `0.4.0`
has no command to set an auth callout on an account; `nsc edit authcallout`
has. The user chose one tool for the whole chain over mixing two (decided
2026-10-09).

Three things still apply, repo-wide: the session memory rules, the general
preferences (stop if asked to do too much; don't read large docs whole;
delegate wide exploration; one command per `Bash` call), and **the life of a
demo** — `demo-playbook.html`, four stages ending in a pattern cards PDF.

**Demo 07 builds everything from nothing.** It makes its own operator,
accounts and users. It does not use, copy, start or wait for demo 06's files,
keys or server, and demo 06 never starts or feeds it. Demo 01 is background
reading only.

**Demo 07 is decentralized authentication with an external auth callout.**
Decentralized authentication = NATS **operator mode**: the operator →
account → user JWT chain is the trust base, and the server config lists no
user. The auth callout hands the *authentication decision* to a service we
run; that service answers through the signed NATS authorization
request/response. An online external auth service does **not** make this
"centralized configuration mode". In operator mode the callout settings live
in the **account JWT**, not in the server config.

**Never copy the NATS docs' worked callout example.** It switches to
configuration mode. Use the docs for facts, not for the setup.

## What this demo is

> **Can a NATS server in operator mode use a Go auth callout service to
> validate a real WorkOS identity, assign the correct account and
> permissions, and deny access when validation fails or the callout service
> is unavailable?**

- **Role: showcase + validation.** A person logs in with WorkOS in a
  terminal and sees NATS admit or refuse them. The refusal cases and the
  callout-down timeout are measured evidence for a platform choice.
- **Audience:** the author first; then a developer or architect following the
  walkthrough with no help.
- **Scenario, used everywhere:** account `ORDERS`, subjects `orders.*`. The
  WorkOS role decides access: `dispatcher` publishes and reads, `viewer` only
  reads.
- **WorkOS is real.** The preferred path is a real WorkOS environment. A local
  stand-in token issuer is allowed **only** where it helps testing (for
  example an expired or badly signed token). A stand-in result never counts
  as proof of the WorkOS integration, and every such result says so.
- **Sources,** read before any claim is written:
  <https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_callout>,
  <https://docs.nats.io/learn/security/decentralized-auth>, and the WorkOS
  docs for the token flow used. Every behavior claim is then checked against
  the installed tools and recorded in `exercises/EXERCISE_OBSERVATIONS.md`
  with the tool versions.

Progression: **terminal exploration → repeatable scripts**. A lab-shell page
comes later, only if needed (`demo-playbook.html`, stage `03`, Activities).

## What this demo is NOT

Mention a boundary in one line where it helps; do not build it here:

- configuration mode, config-listed users or NKeys — that is centralized
  authentication, **demo 05**
- re-measuring demo 06 (trust chain, account push, scoped signing keys,
  expiry and revocation of NATS-issued credentials) — cite demo 06's cards
- a second operator trusted by the server; multi-operator deployments
- clusters, gateways, leaf nodes, JetStream, TLS
- a frontend or a lab-shell menu entry (stage 03 decides, terminal first)

Do not modify any other demo from here.

## Ports, names, and run state

| Thing | Value |
|---|---|
| NATS client | `127.0.0.1:4822` (loopback only) |
| NATS monitor | `127.0.0.1:8822` (loopback only) |
| `server_name` | `d07-server` |
| Operator | `D07` |
| Accounts | `AUTH`, `ORDERS`, `SYS` |
| Users | `AUTH`: `callout-svc` (the callout's auth user), `sentinel`; `ORDERS`: none stored |
| WorkOS roles | `dispatcher`, `viewer` (and one test user with neither) |
| Env var prefix | `D07_` |
| WorkOS settings (gitignored, written by the user) | `.env.workos`: `D07_WORKOS_CLIENT_ID`, `D07_WORKOS_ORG_ID` (the one organization the callout admits) |
| Run state (gitignored) | `.run/` |

`4822` / `8822` appear nowhere else in the repo and nothing listened on them
(checked 2026-10-09).

**Every `nsc` command goes through `lab/nsc.sh`** and **every `nats` command
through `lab/nats.sh`** (demo 06 shows the idea — copy the idea, not the
file). `nsc.sh` points `nsc`'s config, data and key store into `.run/`, so
nothing lands in the author's real `~/.local/share/nats/nsc` or
`~/.nkeys`. `nats.sh` adds `--no-context`. Stage 03 measures that the real
stores did not change.

## Code

The Go callout service lives in `callout/`, its own Go module. It uses plain
`nats.go` + `github.com/nats-io/jwt/v2`, plus one small JWKS library for the
WorkOS token. It reads the authorization request and signs the response in
its own code, so a reader sees every step; the Synadia callout library is
not used (it hides the signing that `D07-R6` and `D07-R7` test). Keep it
small: one `main.go` if it fits.

## Secrets

- Claude never types a real WorkOS API key, client secret or password. The
  user writes them into the gitignored env file.
- `.run/` holds every seed and `.creds` file. It is in the root `.gitignore`.
  Never commit a key, seed, `.creds` file or the WorkOS env file.
- Never print a seed in a walkthrough (demo 06 found `nats auth … --json`
  does). Check the same for any `nsc` output used.
- A WorkOS access token is a live credential while it is valid. Never paste
  one into a committed file or an observation; record its claims, not the
  token.

## Process rules

- One server at a time on this demo's port. A start script refuses when the
  port is taken.
- Stop only the PID this demo wrote, after checking its command line names
  this folder. **Never `pkill nats-server`** — demos 03, 05 and 06 run bare
  servers on the same host. Same rule for the callout service.

## Three kinds of file: walkthrough, convenience, validation

- **Walkthrough** (`exercises/EXERCISE-NN-TERMINAL-STEPS.md`): direct
  `nats auth`, `nats-server` and `go run` commands, readable config and short
  explanations. Preparation is a section headed **Initial setup**. Exercise
  numbers start at `01`; `00` is invalid.
- **Convenience scripts** (`lab/`): thin wrappers around the same steps.
- **Validation scripts** (`exercises/exNN-check.sh`): assertions and
  deliberate failures, kept apart from the normal setup.

The Go callout service is part of the teaching path. Keep it small and
readable. No inline Python anywhere.

## Quality rules

- **Prediction is not a result.** Label it *prediction* until
  `exercises/EXERCISE_OBSERVATIONS.md` has the real output, date and versions.
- **A refusal proves nothing alone.** Show it next to a positive control on
  the same server, in the same run.
- **Client report and server enforcement are different evidence.** Record
  the client's error line and the server's log line, and the callout
  service's own log where it decided.
- **An admission must show its whole path.** Exercise 01 records visible
  evidence for each link, from one run: NATS sends the authorization request
  → Go validates the WorkOS token → Go returns a signed authorization
  response → NATS admits the user. The README's exercise 01 table lists the
  evidence per link. A client that "got in" without that chain is not a
  result. The callout logs claims and public keys, never a token or seed.
- **Authentication and authorization apart, every time.**
- **Real WorkOS and stand-in apart, every time.**

## Layout (planned)

| What | Where |
|---|---|
| Lab-shell intro, requirements, ports, exercises | `README.md` |
| WorkOS dashboard set-up and `.env.workos` (prerequisite for exercise 01) | `WORKOS-SETUP.md` |
| The Go callout service | `callout/` |
| Server configs | `exercises/config/exNN-nats-*.conf` |
| Numbered terminal steps (`NN` starts at `01`) | `exercises/EXERCISE-NN-TERMINAL-STEPS.md` |
| Measured results, with versions | `exercises/EXERCISE_OBSERVATIONS.md` |
| Check scripts | `exercises/exNN-check.sh` |
| Negative-test machinery (wrong signing key) | `exercises/` — named as such |
| Thin wrappers (`nsc.sh`, `nats.sh`, `up.sh`, `down.sh`) | `lab/` |
| Run state | `.run/` |

## Requirements and decisions

The `D07-R*` table lives in `README.md`. Every exercise and card points back
to an ID there. The platform choice this demo informs is the proposed
[ADR-057](../../obsidian/V3-Platform/Architecture/ADR/lab/ADR-057-lab-nats-external-identity-auth-callout.md);
stage 03 closes it.
