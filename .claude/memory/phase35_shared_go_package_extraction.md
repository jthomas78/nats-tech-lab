---
name: phase35-shared-go-package-extraction
description: IMPLEMENTED 2026-08-18 — shared/natstenants, shared/natstrace, shared/browserrpc extracted from 4/5/4-way per-service duplication; go.work + per-service replace directives; first shared Go code in this repo
metadata:
  type: project
---

Phase 35 was the follow-through on the deferral Phase 32's BR-D40 recorded
explicitly ("paste the fourth copy rather than block on the unrelated
go.work migration") — see [[tenants_manager_triplication]] for the
duplication history this resolved.

**Module strategy (35.1), the actual deliverable, not a prerequisite.**
Repo-root `go.work` (workspace mode, for local `go build`/editor ergonomics)
**plus** an explicit `replace` directive in each consuming service's
`go.mod` — the belt-and-suspenders pin that's the real resolution mechanism,
since `go mod tidy` and Docker builds don't reliably honor `go.work`'s
implicit override once the replaced module has its own external
dependencies. Every affected Dockerfile's build context moved to the repo
root (`context: ../..` in `docker-compose.yml`) so each can
`COPY shared/<pkg> ./shared/<pkg>` before the service's own `COPY` — the
same pattern the frontend Dockerfiles already used for `shared/unifi-theme`.

**Three packages, three different consumer sets:**
- `shared/natstenants` (`Manager[R any]`, generic over each service's
  per-tenant resource type) — `pricing-service`, `trading-partner-service`,
  `refdata-service` consume the full `Manager`; `shipping-service` consumes
  only `Discover`/`SubscribeLifecycle` (connection lifecycle), keeping its
  JetStream/KV provisioning local since it already owned an equivalent
  per-tenant map (`Deps.TenantResources`) `Manager` would only have
  duplicated.
- `shared/browserrpc` (reply-tail plumbing: `ContextFromSubject`,
  `ResponderHeader`/`ResponderIdentity`, `Respond`/`RespondError`, the
  canonical `Reply`) — all four `browserrpc/adapter.go` files. Call-site
  signatures were **not** force-unified: pricing/trading-partner (already
  had a single `reply(req, result, err)` funnel) got a one-line delegating
  `reply`; refdata/shipping (no single funnel — handlers call
  `respond`/`respondError` directly at multiple early-exit points) kept
  their existing `(req, subject, correlationID, ...)` wrapper signatures
  with delegated bodies, to avoid a purely cosmetic ~90-call-site diff in
  refdata-service.
- `shared/natstrace` — all five services incl. `accounts-service` (which
  uses `HTTPMiddleware` instead of `Start`/`Middleware`, since its primary
  transport is REST not `rpc.*`/`api.*`).

**No compatibility shims anywhere** — every old per-service package/type
(`pricing/internal/tenants`, `tradingpartner/internal/tenants`'s old
content, `refdata/internal/tenants`, each service's local `errorResponse`/
`responderHeader`/`responderIdentity`/`contextFromSubject`) deleted outright,
including their own test files. Each shared package's own test suite
(`natstenants_test.go`, `natstrace_test.go`, new `browserrpc_test.go`, all
using an embedded `nats-server/v2/server`) is now the one suite every
consumer benefits from instead of one service's coverage staying
service-only.

**Recurring gotcha worth remembering if this pattern is reused:** a blind
`sed -i 's/contextFromSubject(/sharedbrowserrpc.ContextFromSubject(/g'`
(and similarly for `decode[`/`spanCtx(` in refdata-service) reliably also
mangles the identifier's own local `func` definition line into invalid Go
syntax (`func sharedbrowserrpc.ContextFromSubject(...)`), since the
definition line itself contains the literal string being renamed. Happened
identically in all four services; the fix each time was reading the
surrounding plumbing block and replacing it wholesale with the
shared-delegating version, not just patching the one mangled line.

**Verification.** All 10 workspace modules (`go build ./...` + `go vet
./...`, run per-module since `go build ./...` doesn't work from a
`go.work` root directly) clean; every service's full `ginkgo ./...` green.
Live: `docker compose down -v && up --build`, zero panics/fatals/auth
violations across every container's logs, Admin UI loaded and the
Request/Reply trace panel showed 45 live `api.*`/`rpc.*` traces at 0
errors — including `api.acme-atlantic-fleet.shipping.port.list.v1`
(shipping-service's refactored adapter) and multiple
`api._platform.refdata.type.list.v1` calls (refdata-service's), both
tracing correctly end-to-end through the new shared plumbing.

Full detail: [Main-POC-Plan-ARCHIVE.md](../plans/Main-POC-Plan-ARCHIVE.md)'s
Phase 35 section. Doc updates landed the same phase: `ARCHITECTURE-
ACCOUNTS.md`'s per-tenant-connections section (RECOMMENDATION →
IMPLEMENTED voice), `ARCHITECTURE-COMMUNICATIONS.md` § 6's `natstrace`
duplication passages, and `BUSINESS_RULES-REFDATA.md`'s BR-D40 (gained a
"Phase 35 amendment" paragraph).
