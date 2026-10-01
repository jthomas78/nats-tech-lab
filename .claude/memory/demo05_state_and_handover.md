---
name: demo05-state-and-handover
description: "[demo 05] State as of 2026-10-01: exercises 01 (16 checks) and 02 (21 checks) built, measured, with check scripts; exercises 03-06 planned only."
metadata:
  type: project
---

State of `demos/05-identity-and-permissions/` on 2026-10-01, branch
`poc/demo05`.

- Brief came from a Codex-written prompt the user pasted: showcase role,
  terminal → scripts → (later) lab shell, order-svc / analytics-reader
  scenario, scope limited to the two NATS docs pages authentication-basics and
  authorization. Accounts, JWTs, auth callout and TLS set-up are later demos.
- Everything one exercise needs lives flat in `exercises/`: configs, the
  `EXERCISE-NN-TERMINAL-STEPS.md`, `exNN-check.sh`, and
  `EXERCISE_OBSERVATIONS.md` (moved there from `docs/` on 2026-10-01).
- Exercise 01 = baseline + username/password. `exercises/ex01-check.sh` ALL
  PASS, 16 checks, 3 runs (nats-server v2.14.6, nats CLI 0.4.0). Not re-run
  since the move into `exercises/`.
- Exercise 02 = permissions. The user walked Steps 1–15 by hand on 2026-10-01.
  Then `exercises/ex02-check.sh` was written from it: 21 checks, ALL PASS, 3
  runs. Finding: a side a user does not list is open, so list both sides
  (`deny: ">"` on the unused one).
- Exercises renumbered to 01–06 (Codex's 7 steps: steps 1 and 2 are both
  exercise 01).
- Not done: exercises 03–06, an `ex02-check.sh`, a stranger's walkthrough
  (`D05-R10`), pattern cards (`D05-R11`).
- An untracked `demos/05-identity-and-permissions/user.nk` (59 bytes, looks
  like an NKey seed) is not ours and must not be committed.

**Why:** the user will resume this over several sessions.
**How to apply:** start at the demo's `CLAUDE.md`, then `docs/THEORY.md` for
the plan of the next exercise; record results in
`demos/05-identity-and-permissions/exercises/EXERCISE_OBSERVATIONS.md`.
See [[demo03-state-and-handover]] for the same shape of handover.
