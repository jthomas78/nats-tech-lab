---
name: accounts-service-plan
description: User confirmed intent to build a separate accounts service + admin UI page for dynamic tenant provisioning, using decentralized JWTs (nsc/NKeys) for NATS account creation and WorkOS for human auth
metadata:
  type: project
---

**Decision (confirmed 2026-07-28):** build an **accounts service** (separate backend, per [[tenant-service-separation-decision]]) with an **accounts page in the admin UI** to manage tenant provisioning dynamically.

Architecture layers agreed in discussion:
- **M2M / NATS account provisioning** — decentralized JWTs (`nsc` / NKeys / `resolver: full`) so creating a new tenant = minting an account JWT and pushing it to the resolver, no `nats.conf` edit or server restart. This replaces the static `accounts{}` block from Phase 13a/b's spike.
- **Human auth for the accounts UI** — WorkOS (SSO federation), protecting the admin frontend/REST boundary. WorkOS authenticates the person; the accounts service (itself an M2M NATS client) performs the actual provisioning action.
- **Auth callout (optional layer)** — can sit on top of decentralized JWTs to dynamically determine which connecting user/service maps to which account, backed by Postgres. Auth callout assigns to *existing* accounts; it does not create them — that's the decentralized JWT layer's job.

Flow: `Human → (WorkOS session) → Accounts frontend → (authz check) → Accounts service → (nsc/NKey mint) → NATS resolver`

**Why:** `nats.conf` is too static and restrictive for N-tenant scaling — confirmed after Phase 13a/b proved account isolation works but left credentials hardcoded. Auth callout alone can't create new accounts dynamically; decentralized JWTs are required for that.

**How to apply:** this is now Phase 14 in `.claude/plans/Main-POC-Plan.md` (renumbered 2026-07-28 from its earlier placeholder slot at Phase 20; sub-phases 14a/14b/14c), plan-approved but not yet implemented — awaiting explicit go-ahead. See [[nats-tower-operator-mode-tradeoff]] — converting to operator mode (which decentralized JWTs require) was previously flagged as a structural change; this decision now commits to that direction.
