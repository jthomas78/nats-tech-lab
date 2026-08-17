---
name: rest-nats-transport-consolidation
description: Program to force all business comms onto rpc.*/api.* only, REST reserved for admin + infra health checks — Phases 31-34, in progress
metadata:
  type: project
---

Decided 2026-08-17: all business-domain communication (frontend or backend) must go over `rpc.*`/`api.*` NATS subjects only. REST + Swagger is reserved for admin operations and infra health checks (`/healthz`), never business CRUD — driven by wanting zero overlap between admin-app calls and business-app calls, and by discovering that most services already run NATS-native in parallel with REST (dual exposure, per `ARCHITECTURE-COMMUNICATIONS.md` §2.4) so removal is often just deleting redundant REST handlers, not building anything new.

**Phases (Main-POC-Plan.md), sequenced deliberately:**
- **Phase 31** — Consolidate to Shape B: retire Shapes A and C first, so every later phase inherits one shape's worth of read paths instead of three. See [phase31_shape_b_consolidation](phase31_shape_b_consolidation.md) — in progress.
- **Phase 32** — DONE (2026-08-17). `refdata-service` gained its own per-tenant + PLATFORM `api.*` adapter (it was the one service still relying on shipping-service as a REST relay); shipping-service's 5 refdata relay routes retired; `frontend/refdata` and the shared `useRefdataLabels.js`/`useL10nCopy.js` composables (used by admin + seafreight-app) all migrated off REST/SSE onto `api.*`/`notify.*`. See [tenants_manager_triplication](tenants_manager_triplication.md) and [phase32_refdata_platform_credential](phase32_refdata_platform_credential.md).
- **Phase 33** — Retire business REST across all four services (shipping, pricing, trading-partner, refdata); add the one missing `api.*.shipping.manifest.get.v1` subject first.
- **Phase 34** — Enforce the boundary: per-service REST-mux admin-allowlist tests, a client-supplied (non-authoritative) `requester` header for the Admin UI's trace filter, admin subjects namespaced separately from business subjects (e.g. `api.*.refdata.admin.*` vs `api.*.refdata.item.*`) so permission grants can scope by subject prefix.

**Key design calls, don't re-litigate without new information:**
- Security boundary = NATS account/user permission grants (distinct browser vs admin tokens, `MintBrowserToken` never gets `rpc.>`). Subject naming and any `requester` header are for observability/filtering only — self-declared, never authoritative, since core NATS request/reply carries no server-attested caller identity.
- **`.v1` subject suffix stays.** Considered dropping it (would have meant `bootstrap-operator.sh --force` + full trust-chain/volume reset — 11 JWT-baked refs), then the user reverted that decision entirely — versioning strategy is a deliberately separate, not-yet-started exploration. Don't propose subject-versioning changes unless asked.
- Renumbering convention (now also in CLAUDE.md): never reuse an archived phase's number even after it's "freed" — new work goes to the next unused range. This is why the REST work is 31-34, not a reuse of the archived 23-30 block.
