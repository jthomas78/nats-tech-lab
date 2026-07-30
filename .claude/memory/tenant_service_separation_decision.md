---
name: tenant-service-separation-decision
description: Agreed decision to keep a future hard-multi-tenancy tenant-service separate from refdata-service, merging them only at the Admin UI layer, not the backend/service layer
metadata:
  type: project
---

**Decision (agreed 2026-07-27):** if/when hard tenant isolation (NATS Account per tenant) is built, it belongs in its own service (e.g. `tenant-service`) — **not** merged with `refdata-service` into a single "platform" service, even though both are admin-facing and crosscutting.

Reasoning that led to this:
- Dictionary/refdata is a **data-plane** concern — crosscutting because many services *consume* the same reference-data read model, same shape as any shared reference data.
- Tenant/account management under hard isolation is a **control-plane / security-boundary** concern — it would own operator signing keys, NATS Account JWT minting/revocation, the resolver directory, tenant onboarding/offboarding. Much higher privilege than serving dictionary values.
- Bundling them increases blast radius (a bug in the larger, lower-trust dictionary REST/admin surface would sit in the same process as credential-minting code), couples unrelated lifecycles (rare ops-triggered tenant provisioning vs. high-frequency dictionary reads), and a "platform" name invites scope creep (billing, flags, quotas, etc. all landing in one god-service).

**What does merge:** the **Admin UI only** — one frontend surfacing both tenant management and dictionary management screens is fine (matches how this repo's admin frontend already talks to multiple backend services). The split is backend/service-layer only.

**Why:** came out of a design discussion following [[nats_tower_operator_mode_tradeoff]] — the user asked whether a `platform` service made sense given both concerns are "global and crosscutting at the admin level," and agreed with keeping them as separate services after this tradeoff was laid out.

**How to apply:** Phase 13 (`.claude/plans/Main-POC-Plan.md`, formerly Phase 18) was the static-server-config-accounts spike this note originally anticipated — it completed 2026-07-27. The actual hard-isolation tenant-management service, Phase 14 (`accounts-service`, formerly tracked as Phase 20), **implemented and verified 2026-07-28** as its own service/bounded context exactly per this decision — separate Go module, separate Postgres (port 5434), no shared datastore with `refdata-service`; the two are wired together only in the admin frontend's sidebar ("Platform" section next to refdata's own screens), never in shared backend code. This is a discussion-stage agreement, not yet captured as a formal DD in the System Design doc — do that when the user is ready to record it.
