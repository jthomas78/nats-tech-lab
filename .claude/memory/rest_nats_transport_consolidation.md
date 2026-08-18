---
name: rest-nats-transport-consolidation
description: Program to force all business comms onto rpc.*/api.* only, REST reserved for admin + infra health checks — Phases 31-34 done; Phase 35 (shared Go package extraction, adjacent cleanup) also done
metadata:
  type: project
---

Decided 2026-08-17: all business-domain communication (frontend or backend) must go over `rpc.*`/`api.*` NATS subjects only. REST + Swagger is reserved for admin operations and infra health checks (`/healthz`), never business CRUD — driven by wanting zero overlap between admin-app calls and business-app calls, and by discovering that most services already run NATS-native in parallel with REST (dual exposure, per `ARCHITECTURE-COMMUNICATIONS.md` §2.4) so removal is often just deleting redundant REST handlers, not building anything new.

**Phases (Main-POC-Plan.md), sequenced deliberately:**
- **Phase 31** — Consolidate to Shape B: retire Shapes A and C first, so every later phase inherits one shape's worth of read paths instead of three. See [phase31_shape_b_consolidation](phase31_shape_b_consolidation.md) — in progress.
- **Phase 32** — DONE (2026-08-17). `refdata-service` gained its own per-tenant + PLATFORM `api.*` adapter (it was the one service still relying on shipping-service as a REST relay); shipping-service's 5 refdata relay routes retired; `frontend/refdata` and the shared `useRefdataLabels.js`/`useL10nCopy.js` composables (used by admin + seafreight-app) all migrated off REST/SSE onto `api.*`/`notify.*`. See [tenants_manager_triplication](tenants_manager_triplication.md) and [phase32_refdata_platform_credential](phase32_refdata_platform_credential.md).
- **Phase 33** — Retire business REST across all four services (shipping, pricing, trading-partner, refdata); add the one missing `api.*.shipping.manifest.get.v1` subject first. PROPOSED, not started.
- **Phase 34** — DONE (2026-08-17). Enforce the boundary: every service's `Mount` returns `[]string`, asserted `ConsistOf` a hardcoded allowlist per service (BR-040/mirrors); `traceSpan.Requester` lifts `Nats-Requestor` onto the trace envelope, all 5 `natstrace` copies (BR-041); Admin UI's Traces panel gained subject-prefix (server-enforced) + requester (self-declared) filters; test-suite audit found zero business-over-REST violations. See [phase34_boundary_enforcement](phase34_boundary_enforcement.md).
- **Phase 35** — DONE (2026-08-18). Shared Go package extraction, adjacent to the REST-removal work but not itself part of it: `natstenants` (per-tenant connection manager, was 4 copies), `natstrace` (tracing, was 5 copies), `browserrpc` infra reply-tail (was 4 copies) — bundled together on user confirmation since all three shared the identical Go-module/Dockerfile-build-context blocker. Module-strategy decision (`go.work` + per-service `replace` directives) was sub-phase 35.1. See [phase35_shared_go_package_extraction](phase35_shared_go_package_extraction.md) and [tenants_manager_triplication](tenants_manager_triplication.md).

**Key design calls, don't re-litigate without new information:**
- Security boundary = NATS account/user permission grants (distinct browser vs admin tokens, `MintBrowserToken` never gets `rpc.>`). Subject naming and any `requester` header are for observability/filtering only — self-declared, never authoritative, since core NATS request/reply carries no server-attested caller identity.
- **`.v1` subject suffix stays.** Considered dropping it (would have meant `bootstrap-operator.sh --force` + full trust-chain/volume reset — 11 JWT-baked refs), then the user reverted that decision entirely — versioning strategy is a deliberately separate, not-yet-started exploration. Don't propose subject-versioning changes unless asked.
- Renumbering convention (now also in CLAUDE.md): never reuse an archived phase's number even after it's "freed" — new work goes to the next unused range. This is why the REST work is 31-34, not a reuse of the archived 23-30 block.
