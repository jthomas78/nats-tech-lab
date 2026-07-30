---
name: accounts-service-plan
description: Accounts service for dynamic tenant provisioning via decentralized JWTs (jwt/v2+nkeys); implemented Phase 14 (2026-07-28); suspend/reactivate lifecycle complete; WorkOS human auth deferred
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

**How to apply:** this is Phase 14 in `.claude/plans/Main-POC-Plan.md` (renumbered 2026-07-28 from its earlier placeholder slot at Phase 20; sub-phases 14a/14b/14c) — **implemented and verified 2026-07-28** (same day, on explicit go-ahead). `nats/bootstrap-operator.sh` (nsc CLI) mints the operator/SYS/DEFAULT/ACME/GLOBEX artifacts checked into `nats/`; a new `accounts-service` (Go, own Postgres on 5434) mints/revokes accounts at runtime via `github.com/nats-io/jwt/v2` + `nkeys` and `$SYS.REQ.CLAIMS.UPDATE`/`DELETE` (not the `nsc` CLI — that's bootstrap-only); shipping-service's tenant dropdown now scans the shared `nats/creds/` directory instead of a hardcoded map, so a minted account appears without a restart. Verified live: minted a tenant through the admin UI's new "Platform > Accounts" page, switched to it, and suspended it — each step reflected immediately with no service restart. WorkOS-backed human auth is still deferred; accounts-service currently sits behind a single shared HTTP Basic Auth secret (spike-only). See [[nats-tower-operator-mode-tradeoff]] — this closes that previously-open question by committing to operator mode.

**Reactivation (implemented 2026-07-28):** suspension was initially one-way; reactivation was added the same day. `reactivateAccount` re-signs the account JWT from the stored signing key seed (or establishes one on the fly for seeded accounts that never had one), pushes via `$SYS.REQ.CLAIMS.UPDATE` with a unique tag (to avoid no-op resolver dedup), mints fresh `.creds`, and returns them once — same one-time pattern as create. See BR-AC04 in `BUSINESS_RULES-ACCOUNTS.md` for the full lifecycle including the 2026-07-28 incident (seeded accounts without signing keys).

**No hard-delete (decided 2026-07-29):** the REST surface explicitly avoids `DELETE` — suspension (`POST /api/accounts/{name}/suspend`) is the only deactivation mechanism. Rationale: tenant data spans multiple services and NATS account-scoped streams; regulatory retention in logistics makes true deletion unsafe. The endpoint was renamed from `DELETE /api/accounts/{name}` to the explicit action verb to avoid implying data destruction. See BR-AC03.
