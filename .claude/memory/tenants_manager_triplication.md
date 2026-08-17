---
name: tenants-manager-triplication
description: pricing-service and trading-partner-service each hand-rolled an identical per-tenant NATS connection manager; refdata-service needs the same thing next (Phase 32) — extraction into a shared package is a documented recommendation, not scheduled
metadata:
  type: project
---

`pricing-service` (`internal/tenants/tenants.go`, 288 lines) and `trading-partner-service` (`internal/tenants/tenants.go`, 368 lines) each independently implement: discover `.creds` files in a directory, open one `nats.Connect` per tenant, subscribe to `notify.accounts.account.{created,suspended,reactivated}` to provision/teardown connections reactively, close on shutdown. `shipping-service` has the same logic embedded in its larger `internal/rest/tenant.go` (577 lines, plus JetStream/KV provisioning). This is how a browser reaches any of these services directly under its own tenant's NATS account, without going through shipping-service as a relay.

**A real bug already came from the duplication, not a hypothetical one:** the `observability.creds` exclusion (a restricted PLATFORM user that must never be treated as a switchable tenant) was missing from two of the three copies, opening phantom connections that failed with subscription violations — found live via NATS server logs, fixed separately in each file at different times.

**`refdata-service` is the one holdout** — still a single PLATFORM-only connection, no `browserrpc`, no tenant manager — and per the [rest_nats_transport_consolidation](rest_nats_transport_consolidation.md) program (Phase 32) it needs this exact machinery next, to stop routing through shipping-service's REST relay.

**Extraction into a shared `natstenants` Go package is recommended and documented** (`ARCHITECTURE-ACCOUNTS.md` § "Three services now open per-tenant connections", with a diagram at `obsidian/.../images/tenants-manager-extraction.png`, editable source `demos/01-dictionary/diagrams/tenants-manager-extraction.html`) **but deliberately not scheduled as its own phase.** Blocker: the repo has 7 independent Go modules (one `go.mod` per service), no `go.work`, no `replace` directives anywhere, and each Dockerfile's build context is a single service directory — extraction is also the decision about how this repo shares Go code at all. Sequencing note in the doc: doing it before Phase 32 avoids writing a fourth copy into refdata-service; doing it after just means one more file to delete later. Either is fine — pasting a fourth copy and calling it done is the one outcome to avoid.
