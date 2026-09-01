# Phase 5 lifecycle and health — COMPLETE 2026-09-01, verified live

Designed 2026-08-31 (14 questions, all A except Q8 B), built and verified 2026-09-01. **This file
previously said "do not begin code" — that was the planning-only state and no longer applies.**

Source of truth: `.claude/plans/Application-Shell-Microfrontend-Plan.md` Phase 5 (5a–5e all ticked)
and `demos/01-dictionary/BUSINESS_RULES-APP-SHELL.md` BR-AS52–65 with an implemented matrix.

As built: explicit static/dynamic class, legacy backfilled static, class edits after reload. Static
disable offers reload; dynamic operator disable and signed publisher unregister withdraw live.
Withdrawal keeps the module and the row; an unchanged return restores without a second activation.
An occupied URL tombstones in place behind a `beforeEach` guard, the route record staying
registered. Slot-owner withdrawal suspends placements, never blames the contributor. Security
revocation still outranks everything; a degraded read never ordinary-withdraws. No scope disposal.

Health is decoration: its own subject, own hint, own reply shape, carrying no revision or signed
bytes. Frontend and backend stay two signals. 5s interval, 2s timeout, 2 failures, 15s freshness;
ageing happens at READ time on both sides, so there is no timer to leak. `shared/natsready` is a new
module — every ask runs the real check, because presence is not readiness.

**The lesson worth keeping, from the live run:** all three suites were green while three deployment
defects sat in the stack — a Dockerfile missing a COPY for two new shared modules, credentials that
predated the grant change (`bootstrap-operator.sh --force` is required, per BR-AC34), and a shell
timer that repainted without re-reading. A unit spec asserts what the code does; none of these were
questions about the code. See [[app-shell-deployment-gaps]].
