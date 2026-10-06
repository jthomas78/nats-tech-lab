---
name: demo05-state-and-handover
description: "[demo 05] State as of 2026-10-06: exercises 01 (16 checks) and 02 (24 checks) done with check scripts; ex 03 parts 03a+03b measured by hand, 03c+03d not run; 04-06 planned only."
metadata:
  type: project
---

State of `demos/05-identity-and-permissions/` on 2026-10-06, branch
`poc/demo05`.

- Brief came from a Codex-written prompt the user pasted: showcase role,
  terminal → scripts → (later) lab shell, order-svc / analytics-reader
  scenario, scope limited to the two NATS docs pages authentication-basics and
  authorization. Accounts, JWTs, auth callout and TLS set-up are later demos.
- Everything one exercise needs lives flat in `exercises/`: configs (in
  `exercises/config/`), the `EXERCISE-NN-TERMINAL-STEPS.md`, `exNN-check.sh`,
  and `EXERCISE_OBSERVATIONS.md`.
- Exercise 01 = baseline + username/password. `exercises/ex01-check.sh` ALL
  PASS, 16 checks, 3 runs (nats-server v2.14.6, nats CLI 0.4.0).
- Exercise 02 = permissions. `exercises/ex02-check.sh`: 24 checks, ALL PASS.
  Finding: a side a user does not list is open, so list both sides
  (`deny: ">"` on the unused one).
- Exercise 03 = allow/deny/wildcards/defaults/empty lists, 4 parts. Steps
  written. 03a (deny wins; `*` one token, `>` one or more) and 03b (empty
  allow list = no restriction) measured by hand 2026-10-01. 03c
  (`default_permissions`) and 03d (wildcard sub overlapping a deny) not run.
  No `ex03-check.sh` yet. 03a added user `audit-observer` and
  `D05_AUDIT_OBSERVER_PASSWORD`.
- Exercises 04 (request/reply, `_INBOX`, `allow_responses`), 05 (token and
  NKey), 06 (bcrypt) are planned only, in `docs/THEORY.md`.
- Not done: a stranger's walkthrough (`D05-R10`), pattern cards (`D05-R11`).

**Why:** the user will resume this over several sessions.
**How to apply:** start at the demo's `CLAUDE.md`, then `docs/THEORY.md` for
the plan of the next exercise; record results in
`demos/05-identity-and-permissions/exercises/EXERCISE_OBSERVATIONS.md`.
See [[demo03-state-and-handover]] for the same shape of handover.
