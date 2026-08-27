---
name: admin-ui-realtime-transport-options
description: Admin browser NATS topology — one PLATFORM WebSocket; the Phase 23 tenant connection was removed after observability centralized
metadata:
  type: project
---

**Current topology (2026-08-26):** `frontend/admin` has exactly one browser
NATS WebSocket, `usePlatformConnection.js`, authenticated into PLATFORM.
Centralized trace/pub-sub feeds arrive from observability-service's PLATFORM
KV projections. The Phase 23 `admin-tenant` connection was removed after its
direct `obs.api.>` and raw tenant notify consumers had already been retired.
UI copy/context bootstrap now use three exact read-only refdata subjects added
to `MintAdminToken`; cross-account Stream/KV contents remain backend-mediated
REST snapshots with an account parameter. Admin never calls
`connectInfo?tenant=` and should not appear as an `acme`/`globex` connection.

**Historical Phase 23 shape:** the original SSE→WebSocket migration used a
dual-connection model: PLATFORM plus a selected tenant. Keep the discussion
below as the reasoning trail, not as current topology.

**Scope correction found mid-implementation:** `watchRefdata`/`GET /api/refdata-watch` was NOT migrated or deleted — it backs `shared/refdata/useRefdataLabels.js`'s UI-text/label refresh, used by every frontend (admin, seafreight-app, refdata), not just the four admin-specific panels Phase 23 targeted (dictionary watch, KV inspector, JetStream watch, RPC panel). The original Phase 23 plan's `notify.*` bullet list conflated this shared SSE endpoint with the four in-scope ones — caught before landing; it now lives untouched in its own `rest/refdata_watch.go`. **Correction to this note's earlier text below**: there is no `DEFAULT` account in this system — RPCTRACE/REFDATA both already live on `PLATFORM` (confirmed via `bootstrap-operator.sh` and the old `sse.go`'s doc comments during Phase 23 design). See [[phase16_tenancy_taxonomy]] for account topology.

**Remaining before this is fully done:** `nats/bootstrap-operator.sh --force` + `docker compose down -v && up --build` (needed — `shipping-admin` gained `notify._platform.>` publish permission, which requires regenerating creds) and a live multi-tab/connection-indicator/four-panel check. Not run yet — destructive (invalidates every existing JWT/creds file), needs an explicit go-ahead.

**Resolution (Phase 15, DONE 2026-07-31 — scope: `frontend/seafreight-app` only):** option 4 below
was built, not just discussed. `useNatsConnection.js` holds one `nats.ws` WebSocket per tab;
`auth-service` (Phase 15c) mints short-lived, permission-restricted browser JWTs
(`api.>`/`notify.>`, never `rpc.>` — see [[phase16_tenancy_taxonomy]] point 8); shipping-service
exposes commands/queries over `api.*` request/reply (Phase 15a/16b, `browserrpc/` adapter) and
publishes projected-entity changes on `notify.*` after each projection write (Phase 15b). REST +
SSE were fully removed from `seafreight-app`'s command/query/live-update path. **`frontend/admin`
and `frontend/refdata` deliberately did NOT get this treatment** — they remain on REST + SSE; the
multi-tab connection-exhaustion problem this note originally describes is unresolved for those two
apps. If it resurfaces there, this note plus the trade-off table below is the starting context —
but confirm with the user before assuming the same NATS-WebSocket approach should extend to them.

**Original symptom (external chat, not this session):** opening a 2nd browser tab to
`http://localhost:7100/` (the Admin UI) leaves both tabs stuck in a "busy"/spinner state and
never finishes loading. Root cause: the Admin UI opens ~4-5 long-lived SSE streams per tab
(dictionary watch, KV inspector, JetStream watch, RPC watch/obs). Chrome caps concurrent
HTTP/1.1 connections per origin at 6; one tab already uses several of those slots, so a second
tab pushes the origin over the limit and all other HTTP requests (page resources, API calls, even
the HTML document) queue forever behind the permanently-open SSE connections.

Three fixes were proposed first, in order of practicality:
1. **HTTP/2** — multiplexes all requests over one TCP connection, no per-origin limit. Vite's dev
   server doesn't serve H2 by default; nginx in Docker can. Framed as "the real fix for
   production."
2. **Close SSE on `visibilitychange`** — background tabs close their `EventSource`s and reopen on
   foreground, freeing slots for the active tab. Framed as the lightest change.
3. **Shared SSE via `BroadcastChannel`** — one tab owns the SSE connections and relays events to
   sibling tabs. More complex, but eliminates duplicate connections entirely.

The user then asked about a **4th option**: replace all per-panel SSE with **one NATS WebSocket
connection** from the browser directly to `nats.ws` (already exposed on port 9222 in this repo's
`docker-compose.yml`), subscribing to subjects like `evt.{context}.shipping.>` directly client-side (where
`{context}` is the company/business-unit token — the tenant is the NATS account the
browser authenticates into, never a subject token; see Phase 16) —
sidestepping the connection-count problem entirely rather than working around Chrome's limit.

Comparison surfaced in that discussion:

| | SSE (current) | NATS WebSocket |
|---|---|---|
| Connections/tab | 1 per panel (4-5 streams) | 1 total |
| Multi-tab | Multiplies (the actual problem) | 1/tab, or shareable via SharedWorker |
| Server cost | Go handler per SSE endpoint (goroutine + NATS sub + HTTP conn each) | None — handlers deleted, browser subscribes directly |
| Latency | Event → NATS → Go handler → SSE → browser | Event → NATS → browser (one fewer hop) |
| Backend code | ~5 SSE handlers to maintain | Deleted entirely |
| Auth | Inherits HTTP proxy's auth | Needs its own NATS creds/JWT — which account does the browser connect as? |
| Subject filtering | Go handler decides what browser sees | Browser can subscribe to anything the account permits — relies on NATS account-level permissions |
| Tenant scoping | Go handler injects current tenant context | Browser authenticates into the tenant's own NATS account — tenancy is the account boundary, so the browser does **not** encode a tenant in any subject (it only needs the `{context}` company/business-unit token). See Phase 16. |

**The real tradeoff is the trust boundary.** Today the Go backend is a gatekeeper: it holds NATS
credentials, subscribes on the browser's behalf, and each SSE endpoint is scoped to show only what
it's designed to show. Moving subscriptions into the browser means the browser needs NATS
credentials directly and is trusted to subscribe only to the right subjects — acceptable for a
POC/lab, but in production this would need a read-only, tightly-scoped-per-tenant JWT rather than
a shared credential.

**Historical status (pre-Phase-15):** the sections above this point are the original raw discussion
note (symptom + 3 SSE-side fixes + the 4th NATS-WebSocket option), kept as-is for the reasoning
trail. Superseded by the Resolution note at the top for `seafreight-app`'s scope — still accurate
background if the same question comes up for `frontend/admin`/`frontend/refdata`.

**Historical Phase 23 RPCTRACE implementation:** it once combined a PLATFORM
RPCTRACE replay/notify path with a direct tenant `obs.api.>` subscription.
Phase 28g retired both in favor of centralized `obs.trace.*`; Phase 43 added
the parallel centralized `obs.pubsub.*` projection. Neither current feed uses
a tenant browser connection.
