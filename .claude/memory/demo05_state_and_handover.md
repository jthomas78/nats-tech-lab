---
name: demo05-state-and-handover
description: "[demo 05] State as of 2026-10-07: ex 01, 02 done; ex 03 half measured; ex03/04/05 scripts NEVER run; ex04 rewritten (S/D/C + hook), not run."
metadata:
  type: project
---

State of `demos/05-identity-and-permissions/` on 2026-10-06, branch
`poc/demo05`.

- Brief came from a Codex-written prompt the user pasted: showcase role,
  terminal → scripts → (later) lab shell, order-svc / analytics-reader
  scenario, scope limited to the two NATS docs pages authentication-basics and
  authorization. Accounts, JWTs, auth callout and TLS set-up are later demos.
  Demo 05 = centralized auth (config mode), config-listed NKeys included;
  see [[nats_auth_terminology_and_demo_boundaries]].
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
  03a added user `audit-observer` and `D05_AUDIT_OBSERVER_PASSWORD`.
- 2026-10-06 the user chose: write steps + scripts for 03, 04, 05 FIRST, one
  commit per exercise, then run them all by hand. So `ex03-check.sh` (36) and
  `ex04-check.sh` (22) exist but were NEVER RUN; `prediction:` checks are docs
  claims. Exercise 04 = request/reply: 04a inbox allowed/denied, 04b
  no-publish vs `allow_responses` (source v2.14.6: responses beat deny ">").
- Exercise 05 drafted too: 05a token, 05b NKey; `lab/nkeys.sh` (seeds in
  `.run/nkeys/`, 600), `D05_TOKEN` added to `lab/secrets.sh`.
  `ex05-check.sh` (32) NEVER RUN. Its two "rejected"
  configs were confirmed by `nats-server -t` only.
- 2026-10-07 Codex review (pasted by the user): `lab/secrets.sh` now adds
  missing vars and keeps passwords; `--rotate` only on explicit request.
  ex05 A10/B13 use a named probe connection + jq (empty list cannot pass).
  Every unrun check is tagged `[pred]` / `[pred·hand]` / `[cfg]` / `[rig]`
  (ex03 now 38 checks). Coverage map added to `docs/THEORY.md`. ex04
  rewritten after a second review (36 checks, NOT run): S/D/C groups,
  `.run/ex04/` captures, `exercises/ex04-responder-hook.sh` via
  `nats reply --command` as responder-receipt evidence, per-case verdicts
  gated on positive controls. Open assumptions listed in
  EXERCISE_OBSERVATIONS.md. Do not add coverage gaps; do not run or rotate unasked.
- Next: the user runs 03c/03d, 04 and 05 by hand, then the three scripts;
  record results in EXERCISE_OBSERVATIONS.md. 06 (bcrypt) stays planned.
- Not done: a stranger's walkthrough (`D05-R10`), pattern cards (`D05-R11`).

**Why:** the user will resume this over several sessions.
**How to apply:** start at the demo's `CLAUDE.md`, then `docs/THEORY.md` for
the plan of the next exercise; record results in
`demos/05-identity-and-permissions/exercises/EXERCISE_OBSERVATIONS.md`.
See [[demo03-state-and-handover]] for the same shape of handover.
