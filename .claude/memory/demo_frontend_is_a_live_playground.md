---
name: demo-frontend-is-a-live-playground
description: A demo's frontend is first an interactive playground on the running feature, never a summary or replay of report data
metadata:
  type: feedback
---

A demo frontend should first strive to be a live playground: the user acts on
the running feature (freeze a cluster, publish, request leadership) and
watches what it actually does. It is not a summary, dashboard or replay of
report data. Reports and pattern cards keep the measured results; the page may
link to them (e.g. from a secondary Overview route).

**Why:** user, 2026-10-06, after demo 03's first mockup turned out to be an
evidence replay when they expected to click buttons and see behaviour. The
replay plan was superseded by `demos/03-multi-cluster-and-accounts/PLAYGROUND-PLAN.md`.

**How to apply:** when proposing or mocking up any demo page, make the
primary screen a control surface whose results appear as feedback to the
user's actions. If a mockup must use fake data, label it mock and make the
buttons drive it. Rule is written in `demo-playbook.html` stage 03 Activities
and root `CLAUDE.md`. Related: [[mockup-fidelity-functional-capability]].
