---
name: phase17-request-reply-panel
description: Phase 17 (Request/Reply panel v2) DONE 2026-08-01 — obs envelope extended (BR-D36/BR-026), RpcPanel.vue rebuilt; admin app has no Vitest infra
metadata:
  type: project
---

Phase 17 — Request/Reply Panel v2 — implemented 2026-08-01 (both 17a and
17b). Design was the bottom detail split with paired Request | Reply panes
plus subject token-facet filtering (chosen over a DevTools-style right
drawer and a Datadog-style facet rail), built to match the approved
reference `demos/01-dictionary/frontend/admin/request-reply-reference.html`.

**Why:** the paired panes mirror the obs channel's two-correlated-messages
structure, and token-click filtering exploits the fixed-arity subject
taxonomy ([[phase16-tenancy-taxonomy]]) instead of adding a second sidebar.

**What shipped:**
- 17a: `obsEnvelope` (both `natsrpc/adapter.go` and `browserrpc/adapter.go`)
  gained `headers`/`timestamp`/`payloadBytes`, additive/optional. New rule
  **BR-D36** (`BUSINESS_RULES-REFDATA.md`) + mirror **BR-026**
  (`BUSINESS_RULES-SHIPPING.md`) — note BR-D31 was already taken (enum
  namespace, Phase 12.14), so check the BR file before assuming a number is
  free. `respondError` in both adapters now attaches real
  `Nats-Service-Error`/`-Code` headers to the actual wire reply, not just
  the observability copy.
- 17b: `RpcPanel.vue` rebuilt per the reference; `SubjectPath.vue` gained an
  opt-in `clickable` prop + `token-click` emit (additive, `StreamView.vue`'s
  existing usage unaffected).

**How to apply:** if touching this panel again, the reference file is the
source of truth for layout — don't redesign from scratch. **The admin
frontend has no Vitest/test infra at all** (no `test` script, no vitest
devDependency, and none of its other panels have component tests) —
Phase 17b's checklist item for Vitest specs was deliberately left undone to
match this existing (imperfect) convention rather than introduce new test
tooling unrequested; verification for that layer was live-browser only.
If a future task wants real coverage here, standing up vitest for admin is
its own decision to raise explicitly, not something to bundle into an
unrelated feature.
