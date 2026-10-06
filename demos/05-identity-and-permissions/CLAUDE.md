# CLAUDE.md — demos/05-identity-and-permissions

**This folder is a sealed unit. Read this file instead of the root `CLAUDE.md`
for anything inside it.**

Demo 05 is one bare `nats-server` on the host, the host `nats` CLI, and shell
scripts. No Docker, no Go, no Postgres, no UI, no `nsc`, no operator mode.
Most of the root file does not apply here.

Three things still apply, repo-wide: the session memory rules, the general
preferences (stop if asked to do too much; don't read large docs whole;
delegate wide exploration; one command per `Bash` call), and **the life of a
demo** — `demo-playbook.html`, four stages ending in a pattern cards PDF.
Demo 05's deck will be `docs/demo-05-pattern-cards.html` and `.pdf`. It does
not exist yet.

## What this demo is

> **Can we admit a client while limiting exactly what it may publish and
> subscribe to?**

- **Role: showcase.** A person follows the README in a terminal and sees
  NATS authentication and authorization work, and fail, on purpose.
- **Audience:** the author learning first; then another developer or
  architect following the walkthrough with no help.
- **Scenario, used everywhere:** an order publisher (`order-svc`) and an
  analytics reader (`analytics-reader`), on `orders.*` subjects.
- **Sources:** only these two pages, read before any claim is written:
  <https://docs.nats.io/learn/security/authentication-basics> and
  <https://docs.nats.io/learn/security/authorization>.
  Every behavior claim is then checked against the installed tools and
  recorded in `exercises/EXERCISE_OBSERVATIONS.md` with the tool versions.

Progression: **terminal exploration → repeatable scripts → (later) the lab
shell**. All three stay in this folder. Terminal is the first usable form.
The rule, its reason and its exceptions are in `demo-playbook.html` (repo
root), stage `03`, Activities.

## What this demo is NOT

Kept for later demos. Mention a boundary in one line where it helps; do not
build it here:

- accounts, and sharing across accounts (export / import) — demo 03 has some
- operator mode, JWTs, `nsc`, resolvers — demo 01 and demo 02 use them
- auth callout
- full TLS exercises (exercise 06 explains TLS; it does not configure it)
- clusters, gateways, leaf nodes, JetStream
- a frontend or a lab-shell menu entry — **not in this task**. When one comes,
  it reuses `shared/unifi-theme/` and `shared/ui-shell/`, and reuses the
  checks already verified in `exercises/` and the shared tools in `lab/`. It
  never re-implements an exercise.

Do not modify any other demo from here.

## Ports, names, and run state

| Thing | Value |
|---|---|
| NATS client | `127.0.0.1:4522` (loopback only) |
| NATS monitor | `127.0.0.1:8522` (loopback only) |
| `server_name` | `d05-server` |
| Users | `order-svc`, `analytics-reader` |
| Env var prefix | `D05_` |
| `nats` context prefix (if ever used) | `lab5-` |
| Run state (gitignored) | `.run/` |

`4522` / `8522` follow demo 04's `4422` / `8422`. Both bind `127.0.0.1`,
because exercise 01a runs with **no authentication at all**.

**Every `nats` command passes `--no-context`.** The author's selected context
is another demo's (`lab4-odometer` on 2026-09-30). Without the flag, the CLI
silently adds that context's credentials, and an exercise measures the wrong
user. This is the easiest mistake to make in this demo.

## Secrets

- Passwords are **never in a config file and never in Git**. A config says
  `password: $D05_ORDER_SVC_PASSWORD`; `nats-server` reads the value from the
  environment when it starts. With the variable unset, it refuses to start
  (measured: `variable reference for 'D05_ORDER_SVC_PASSWORD' ... can not be
  found`). That is fail-closed, and it is the point.
- `lab/secrets.sh` writes random passwords to `.run/secrets.env`, mode 600.
  NKey seeds (exercise 05) go in `.run/nkeys/`, mode 600, written by
  `lab/nkeys.sh`; public keys go in `.run/nkeys.env`. The CLI writes seeds
  mode 644, so the script sets 600.
- `.run/` is in the root `.gitignore`. Check it is still there before adding
  any generated file.

## Process rules

- One server at a time. `lab/up.sh` refuses to start when `4522` is taken.
- `lab/down.sh` stops only the PID in `.run/nats-server.pid`, and only after
  checking that PID's command line names this folder's config. **Never
  `pkill nats-server`** — demo 03 runs bare servers on the same host and an
  unprefixed kill has already killed a live lab twice.
- A foreground server started by hand in a terminal is stopped with Ctrl-C.

## Layout

| What | Where |
|---|---|
| Lab-shell intro, requirements, walkthrough | `README.md` |
| Everything one exercise needs, in one flat folder | `exercises/` |
| — server configs | `exercises/config/exNN-nats-*.conf` |
| — numbered terminal steps (`NN` starts at `01`; `00` is invalid) | `exercises/EXERCISE-NN-TERMINAL-STEPS.md` |
| — measured results, with versions | `exercises/EXERCISE_OBSERVATIONS.md` |
| — the exercise's check script | `exercises/exNN-check.sh` |
| Shared tools (secrets, up, down, lib) | `lab/` |
| Theory, from the two source pages | `docs/THEORY.md` |
| Run state, secrets, logs (gitignored) | `.run/` |

## Quality rules

- **Prediction is not a result.** A README "expected" line is labelled
  *prediction* until `exercises/EXERCISE_OBSERVATIONS.md` has the real output, the date and
  the versions. Never mark an exercise verified that was not run.
- **A silent subscriber proves nothing.** Every denial is shown next to a
  positive control that did get through, on the same server, in the same run.
- **Client report and server enforcement are different evidence.** Record
  both: the client's error line and exit code, and the server's log line.
  Permission errors (exercise 02 on) arrive **asynchronously** — the publish
  call can return before the error does — so a check reads the server log and
  the actual delivery, not only the client's exit code.
- **Name authentication and authorization apart every time.** Authentication
  = who are you (the connection is admitted or refused). Authorization = what
  may you do (a publish or subscribe is allowed or denied).
- Exercises not yet built stay marked **planned — not implemented, not
  verified** in the README.

## Completion — not yet

The demo is not complete until, under the playbook:

1. every exercise 01–06 is implemented and recorded as measured;
2. **another person** runs the README walkthrough with no help (the showcase
   exit test);
3. the pattern cards deck exists (`pattern-cards` skill, stage 04).

Exercises 01 and 02 are built and measured, each with a check script
(`exercises/ex01-check.sh`, `exercises/ex02-check.sh`). Exercise 03 is in
progress: 03a and 03b measured by hand; `exercises/ex03-check.sh` written
before the 03c / 03d hand run and not yet run. Exercise 04 is drafted
(steps, four configs, `exercises/ex04-check.sh`) and not yet run. So is
exercise 05 (steps, four configs, `lab/nkeys.sh`, `exercises/ex05-check.sh`).
