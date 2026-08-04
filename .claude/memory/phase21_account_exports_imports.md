---
name: phase21-account-exports-imports
description: Plan approved 2026-08-03 to replace cross-account "second .creds connection" pattern with NATS-native exports/imports; two-account partitioning (PLATFORM cross-cutting, tenant data-plane); not yet implemented
metadata:
  type: project
---

**Decision (plan approved 2026-08-03):** move cross-account communication from "open a second connection authenticated as the other account" to NATS's own account-JWT-declared exports/imports. Today `accounts-service` holds SYS+PLATFORM and `shipping-service` holds PLATFORM + every tenant account — the boundary is enforced only by which `.creds` file happens to be mounted, not by anything declared in the JWTs. This was flagged and deferred twice already: Phase 13's completion note ("refdata-service excluded — needs exports/imports for cross-tenant sharing — deferred") and `ARCHITECTURE-ACCOUNTS.md`'s "Production-scale fix" sketch. No phase number existed for it until now — it's Phase 21 in `Main-POC-Plan.md`.

**Target partitioning, user-specified:** PLATFORM holds cross-cutting services (`accounts-service`, `refdata-service`) and declares exports; tenant accounts (`acme`, `globex`, runtime-minted) hold the data plane (`shipping-service`'s per-tenant `SHIPPING` stream + KV, the browser) and declare matching imports.

**Four export/import declarations:**
1. Stream export `notify.accounts.account.*` (accounts-service → tenant), no remap
2. Service export ×4, `rpc.*.refdata.{op}.v1` with a subject remap — tenant imports as a bare local subject (`refdata.item.get.v1` etc.); the server, not the caller, stamps the tenant's own account identity into the subject
3. Fixed-subject service import `rpc._platform.refdata.context.list.v1` — deliberately NOT remapped, this one endpoint is intentionally cross-tenant/admin-facing
4. Stream export `evt.*.refdata.*.changed` (REFDATA's business-path change feed), no remap

**Bonus, not just aesthetic:** the remap closes a real gap — today `refdataconsumer` interpolates `{context}` from caller-held application state, so nothing stops a client connected as `acme` from asking for `globex`'s data by constructing the subject. After the remap, that subject cannot exist in `acme`'s imported subject space at all — enforced by the server via the account's own import declaration, not by the caller behaving.

**What does NOT collapse to imports** (confirmed via exploration, not assumed):
- `$SYS.REQ.CLAIMS.*` is system-account-internal, not exportable — `accounts-service` keeps its SYS connection unchanged.
- `$SRV.>` micro-service discovery does not cross accounts via export/import at all.
- A stream export delivers live core messages only, not `$JS.API` access to the exporter's stream — so JetStream *replay* of another account's stream isn't obtainable this way.

**shipping-service still keeps a second PLATFORM connection** (user's explicit choice, not full elimination) — narrowed to a new permission-restricted user (`shipping-admin`, not the existing unrestricted `platform` user) scoped to just `$SRV.>` + narrow `$JS.API` for `REFDATA`/`RPCTRACE`, used only by the admin/observability endpoints (`nats_ops.go`'s Connections/Services panels, `sse.go`'s RPC-trace replay). The Services panel and the RPCTRACE 10-minute replay are the two things that don't survive a full move to imports.

**How to apply:** full design + checklist is Phase 21 in `.claude/plans/Main-POC-Plan.md` (added 2026-08-03, same session as this note) — read that before implementing, it has the exact file:line call sites for every change (`accounts/provisioner.go`'s `newAccountClaims` needs to *preserve* existing `Exports`/`Imports` across a re-push, which it doesn't do today — a real gap independent of this phase; `refdataconsumer/consumer.go`'s four `fetch*ViaRPC` methods; `bootstrap-operator.sh`'s new `nsc add export`/`nsc add import` calls). Not yet implemented as of this note.
