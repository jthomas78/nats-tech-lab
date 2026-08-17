---
name: phase33-refdata-admin-rest-exemption
description: /api/refdata/admin/* REST stays permanently — accounts-service calls it server-to-server with no NATS equivalent; not un-migrated business REST
metadata:
  type: project
---

Phase 33 retired business REST platform-wide (shipping/pricing/trading-partner/refdata-service), but refdata-service's `/api/refdata/admin/*` routes could not be deleted like the plan originally assumed.

**Why:** `accounts-service/accounts/refdata.go`'s `RefdataClient` calls these routes server-to-server (`RegisterContext`, `SetContextVisible`, `AddLocale`, `CreateDraft`, `PublishCorpus`, etc.), wired into live account/BU-creation handlers (`accounts/handler.go`) and startup seeding (`cmd/main.go`). refdata-service's NATS surface has no write/admin equivalent — `internal/natsrpc` is read-only, and Phase 32's `browserrpc` adapter ([[phase32_refdata_platform_credential]]) only covers browser-facing traffic, not this service-to-service link.

**Resolution (owner decision, not a Phase 32 gap fix):** keep `/api/refdata/admin/*` as a permanent, deliberately-documented REST exemption — same category as the plan's existing "structurally exempt" bootstrap class — rather than building a new `rpc.*` admin-write path. Only the business (browser-facing) reads under `/api/refdata/*` were deleted; those already had full `api.*` parity via `browserrpc`. Documented as BR-D43 in `BUSINESS_RULES-REFDATA.md`.

**Pattern to remember:** every phase-32-style "we already moved X onto NATS" claim should be checked for *all* callers, not just the browser one — a service-to-service HTTP client can hide behind a REST route that looks purely browser-facing at a glance. If a future phase revisits this, the actual work needed is giving `accounts-service` an `rpc.*` path for these admin writes, not just deleting the REST route.

See [BUSINESS_RULES-REFDATA.md](../../demos/01-dictionary/BUSINESS_RULES-REFDATA.md) BR-D43.
