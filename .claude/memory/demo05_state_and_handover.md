---
name: demo05-state-and-handover
description: "[demo 05] State as of 2026-09-30: folder set up, exercise 01 (open server, then user/password) built and measured 16/16 twice; exercises 02-06 planned only."
metadata:
  type: project
---

State of `demos/05-identity-and-permissions/` on 2026-09-30, branch
`poc/dictionary3.1`, not yet committed.

- Brief came from a Codex-written prompt the user pasted: showcase role,
  terminal → scripts → (later) lab shell, order-svc / analytics-reader
  scenario, scope limited to the two NATS docs pages authentication-basics and
  authorization. Accounts, JWTs, auth callout and TLS set-up are later demos.
- Exercise 01 = baseline + username/password. `lab/ex01-check.sh` ALL PASS,
  16 checks, 2 runs (nats-server v2.14.6, nats CLI 0.4.0).
- Exercises renumbered to 01–06 (Codex's 7 steps: steps 1 and 2 are both
  exercise 01).
- Not done: exercises 02–06, a stranger's walkthrough (`D05-R10`), pattern
  cards (`D05-R11`).

**Why:** the user will resume this over several sessions.
**How to apply:** start at the demo's `CLAUDE.md`, then `docs/THEORY.md` for
the plan of the next exercise; record results in `docs/OBSERVATIONS.md`.
See [[demo03-state-and-handover]] for the same shape of handover.
