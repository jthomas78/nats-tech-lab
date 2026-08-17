---
name: phase21-account-exports-imports
description: IMPLEMENTED 2026-08-03 (extended Phase 28f 2026-08-xx) — PLATFORM/tenant two-account partitioning via NATS-native account-JWT exports/imports, replacing the old "second .creds connection" pattern
metadata:
  type: project
---

**Status: IMPLEMENTED 2026-08-03, `ginkgo ./...` green in both `accounts-service` and `shipping-service`.** Replaced the cross-account "open a second connection authenticated as the other account" pattern with NATS-native account-JWT-declared exports/imports. This was flagged and deferred twice before landing (Phase 13's completion note, `ARCHITECTURE-ACCOUNTS.md`'s "Production-scale fix" sketch).

**Partitioning:** PLATFORM holds cross-cutting services (`accounts-service`, `refdata-service`) and declares exports; tenant accounts (`acme`, `globex`, runtime-minted) hold the data plane (`shipping-service`'s per-tenant `SHIPPING` stream + KV, the browser) and declare matching imports.

**Forward leg — PLATFORM exports / tenant imports (`nats/bootstrap-operator.sh` at day-0, `accounts/provisioner.go`'s `tenantImports`/`tenantExports` at runtime for accounts minted after boot):**
1. Service export ×4, `rpc.*.refdata.{item.get,type.list,item.get-versioned,locales.list}.v1` — tenant imports each with a subject remap to a bare local subject (`refdata.item.get.v1` etc.); the remote subject carries the tenant's own human-readable name (`rpc.acme.refdata.item.get.v1`), stamped server-side, not client-supplied.
2. Fixed-subject service import `rpc._platform.refdata.context.list.v1` — deliberately NOT remapped, this one endpoint is intentionally cross-tenant/admin-facing.
3. Stream export `notify.accounts.account.*` (accounts-service → tenant), no remap.
4. Stream export `evt.*.refdata.*.changed` (REFDATA's business-path change feed), no remap. See [[refdata_cross_tenant_stream_import]] — this one is still unbounded/unscoped by tenant, a known open gap distinct from this phase.

**Reverse leg — Phase 28f, tenant exports / PLATFORM imports:** each tenant exports its own `obs.trace.>` spans (stream export, no `AllowTrace` — that flag is rejected on stream exports); PLATFORM imports each tenant's `obs.trace.>` with `--allow-trace`, feeding PLATFORM's cross-account trace store (`dictionary/composition.go`'s TRACES consumer). Wired both in `bootstrap-operator.sh` (day-0) and `provisioner.go`'s `addPlatformTraceImport` (runtime, for accounts minted after boot).

**Enforcement:** import declarations live inside the operator-signed account JWT — a tenant cannot rewrite its own import to substitute another tenant's name, since that requires re-signing with the operator's private key, which the tenant never holds. The remap closes a real gap: pre-Phase-21, `refdataconsumer` interpolated `{context}` from caller-held state, so nothing stopped a client connected as `acme` from constructing a subject to read `globex`'s data. After the remap, that subject doesn't exist in `acme`'s imported subject space at all.

**Preservation on re-sign (real gap this phase had to close):** account JWT updates replace the entire claim wholesale, so `provisioner.go`'s `newAccountClaims` explicitly copies `prior.Exports`/`prior.Imports` forward verbatim on every re-sign (suspend, reactivate, limits change) — without this, any account mutation after initial creation would silently drop the Phase 21 wiring. A recovery path rebuilds imports from scratch only when a prior claim has zero imports (stale pre-Phase-21 JWT, or first-time mint).

**What does NOT collapse to imports** (confirmed via exploration, not assumed):
- `$SYS.REQ.CLAIMS.*` is system-account-internal, not exportable — `accounts-service` keeps its SYS connection unchanged.
- `$SRV.>` micro-service discovery does not cross accounts via export/import at all.
- A stream export delivers live core messages only, not `$JS.API` access to the exporter's stream — so JetStream *replay*/listing of another account's stream isn't obtainable this way.

**shipping-service still keeps a second PLATFORM connection** (user's explicit choice, not full elimination) — narrowed to a permission-restricted `shipping-admin` user (allow-list of only the ordered-consumer subjects for REFDATA replay, no `$JS.API.>`), used only by admin/observability endpoints. A *third*, unrestricted `platform` full-JS connection also exists for the Admin UI's stream-listing panels (`$JS.API.STREAM.LIST` isn't covered by `shipping-admin`'s allow-list) — listing needs `PlatformFullJS`, replay works on either.

**How to apply:** full design + checklist is Phase 21 in `.claude/plans/Main-POC-Plan.md`; exact call sites: `nats/bootstrap-operator.sh` (day-0 `nsc add export`/`nsc add import`), `accounts/provisioner.go`'s `newAccountClaims`/`tenantImports`/`tenantExports`/`addPlatformTraceImport` (runtime), `refdataconsumer/consumer.go`'s four `fetch*ViaRPC` methods (consume the remapped local subjects).
