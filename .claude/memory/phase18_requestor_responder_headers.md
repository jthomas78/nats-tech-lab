---
name: phase18-requestor-responder-headers
description: Phase 18 (Nats-Requestor/Nats-Responder identity headers) DONE 2026-08-01 — BR-D37/BR-027; also fixed a micro.Config.Name/nats.Name mismatch found while implementing
metadata:
  type: project
---

Phase 18 — Requestor/Responder Identity Headers — implemented 2026-08-01.
Follow-on to Phase 17a (BR-D36/BR-026, which made a message's real headers
visible in the Admin UI's Request/Reply panel): that made headers visible,
but no header actually said *who* sent or answered a call, since NATS
doesn't attach caller/responder identity to a message on its own.

**What shipped:**
- `Nats-Requestor` set by the caller — `refdataconsumer.requestRPC`
  (shipping-service's `rpc.*` caller) uses the connection's own
  `nats.Name(...)`; `useNatsConnection.js`'s `request()` (browser's `api.*`
  caller) uses the fixed string `"seafreight-app"` (tenant deliberately
  excluded — it's already the NATS account boundary).
- `Nats-Responder` set by the answering adapter on every reply (success and
  error) as `"<service's nats.Name>/<micro.Service instance ID>"` — new
  info is the *instance*, since the subject alone already identifies which
  *service* answers in this repo (no fan-out today).
- New rule **BR-D37** (`BUSINESS_RULES-REFDATA.md`) + mirror **BR-027**
  (`BUSINESS_RULES-SHIPPING.md`); new Ginkgo tests in both services'
  natsrpc_test.go / browserrpc_test.go.

**Found and fixed while implementing — worth remembering:** each adapter's
`micro.Config.Name` (`refdata-rpc`, `shipping-api` — family-derived) didn't
match its own connection's `nats.Name` (`refdata-service`,
`shipping-service`). Left alone, `Nats-Requestor` and `Nats-Responder` would
have shown two different names for the same physical service in the panel —
looking like the requestor and responder were different entities. Renamed
both `Config.Name` values to match `nats.Name` exactly.

**How to apply:** if a third service ever registers a `micro.Service`,
give it the same `Config.Name` as its own `nats.Name(...)` — don't pick a
family/protocol-derived name (`"foo-rpc"`) — or the same requestor/responder
mismatch will resurface. This is the second time in this codebase that
verifying "what name does this actually report" (not what a comment or
prior assumption claims) caught a real inconsistency — see
[[verify_before_resuming_offloaded_work]] for the general pattern.

**Follow-up (2026-08-15):** the newer Phase 28 `natstrace` tracing spans
(a separate wire format from this phase's `obsEnvelope`/RpcPanel headers)
reintroduced the same class of bug — `Nats-Requestor` was captured but
silently dropped before publish. See
[[phase28_trace_detail_request_response_split]].
