---
name: phase16-tenancy-taxonomy
description: Phase 16 decision record and a-f sub-phase status — context vs tenant vs region, 5-family subject taxonomy, reserved-`_`-prefix enforcement, refdata's real context tree
metadata:
  type: project
---

**Trigger:** Phase 15 exposed that `{context}` meant two different things across services —
refdata-service documented it as tenant/region scope (`emea-acme`), shipping-service implemented
it as a tenant-agnostic company/business-unit qualifier. Phase 16 settled one model and migrated
both services to match. Full decision record and sub-phase detail: `Main-POC-Plan.md` §
"Phase 16 (16a/16b/16c/16d)" (search that heading — the section also covers 16e/16f).

**The model (13-point decision record, 2026-07-31), condensed:**
- Tenancy = **NATS account only** — hard, server-enforced boundary. Subject-prefix separation
  inside one shared account is NATS's own documented *legacy/weaker* pattern and isn't used here.
- `{context}` = **company/business-unit** — soft, app-layer partition, hyphenated into one token
  (`acme-atlantic-fleet`), never dot-separated (fixed subject arity), value is opaque.
- Region = a **separate deployment axis** (its own regional NATS stack), never a subject token.
- Full hierarchy: `region → tenant (NATS account) → company/group → business unit ({context})`.
- `_`-prefixed context/account values are **reserved for platform use** (`_platform`).
- Five subject families — Core: `evt.*`, `rpc.*` (service-to-service), `api.*`
  (browser-to-service), `notify.*` (change notification). Supportive: `obs.rpc.*`/`obs.api.*`
  (debug side-channel, deliberately isolated from business subjects). `cmd.*` reserved, unused.
- `rpc.*`/`api.*` are strictly separate grants — a browser JWT never gets `rpc.>`; backend code
  never calls `api.>`.
- `auth-service`/`accounts-service` carry no `{context}` (they administer the tenant axis itself);
  `refdata-service` does carry it (its data is company-scoped even though it's a platform service).
- Both services use the **fully-qualified** context form everywhere (`acme-atlantic-fleet`, not
  a locally-minimal `atlantic-fleet`) — refdata has no account boundary to disambiguate whose
  corpus a request concerns, so the company qualifier has to live in the value itself.
- A context may carry an optional `tenant` link — **governance metadata only, not enforced**
  (refdata runs on one shared account with no caller identity to check it against).

**Sub-phase status (2026-07-31):**
| Phase | Status | Summary |
|---|---|---|
| 16a | DONE | Docs-only realignment (`ARCHITECTURE-COMMUNICATIONS.md`, `ARCHITECTURE-DICTIONARY.md`, `CLAUDE.md`, both `BUSINESS_RULES-*.md`, `Refdata-Versioning-Tenancy-Design.md`, `Multi-Region-Plan.md`). |
| 16b | DONE | `rpc.*`→`api.*` migration for the browser path: shipping-service `natsrpc/`→`browserrpc/`, `auth-service` drops `rpc.>`, frontend subject builders/tests updated. |
| 16c | DONE | Reserved `_`-prefix enforcement in both `accounts-service` (BR-AC07) and `refdata-service` (BR-D33 — the primary point, since context is refdata's own independently-registrable resource). |
| 16d | DONE | Real refdata context tree `_platform → acme → acme-atlantic-fleet` (retiring `emea-acme`), `tenant` column (BR-D34), `RegisterPlatformRoot` bootstrap exception, seed data demonstrating BR-V06/BR-V07 inheritance end-to-end, parent-first idempotent corpus publish, two live-consumer fixes. Fully verified (all Go suites green, clean rebuild, curl-level inheritance proof, live browser checks). |
| 16e | DONE (2026-07-31) | Renamed shipping-service's own context literals to the fully-qualified form: `global`→`acme`, `atlantic-fleet`→`acme-atlantic-fleet`, `pacific-fleet`→`acme-pacific-fleet`, across Go source/tests, Swagger docs, and both frontends' `CONTEXTS` arrays. Required a `docker compose down -v` (no bucket-rename mechanism exists — confirmed, not built). **Investigated but not built**: tenant-aware context seeding — shipping's Postgres schema has no tenant column at all (`seedDefaultPorts` runs once, pre-tenant-connection), so this was out of reach without a bigger schema change; documented as a known limitation in `migrate.go`, not silently dropped. |
| 16f | DONE (2026-07-31) | refdata-service gained `ListByTenant` (BR-D35, both REST `?tenant=` and `rpc._platform.refdata.context.list.v1`). shipping-service added `refdataconsumer.ListContexts` + `GET /api/refdata/contexts` (BR-025), and replaced the hardcoded `refdataContext="acme"` constant with `refdataCompanyContext(tenant)` (decision 11: tenant name doubles as company context). Both frontends' `CONTEXTS` arrays are now just the offline fallback — a `loadContexts()` action fetches the real list on tenant init/switch. **Bug found and fixed during verification**: the fetched list includes refdata's `_platform` reserved root, which is meaningless as a *fleet* context (no ship/container ever belongs to it) — selecting it wasted the tenant's JetStream stream quota (`MaxStreams: 10`) on empty KV buckets and caused live SSE 500s. Fixed by filtering `_`-prefixed values out of both frontends' `loadContexts()`. **Known gap, documented not fixed**: `refdataCompanyContext` resolves via shipping's REST-side `Deps.Tenant` (Admin UI's `SwitchTenant`), which Sea Freight Flow's NATS-authenticated tenant (Phase 15d) never drives — the two coincide by default but aren't the same signal; a real fix needs an explicit tenant threaded through the shared `useRefdataLabels`/`useL10nCopy` composables (pre-existing Phase 15 scope boundary, not introduced by 16f). |

**How to apply:** When touching subject construction, context validation, or tenant/account code
anywhere in this repo, this model is authoritative — `{context}` is never tenant or region. Before
starting 16e or 16f, re-read `Main-POC-Plan.md`'s Phase 16 section directly (this note is a
summary, not a substitute) since 16e specifically requires a KV data migration that needs care.
See also [[shipping_domain_overview]] (subject taxonomy as it applies to the Ship/Container
domain) and [[tenant_service_separation_decision]] (why tenancy lives in its own service).
