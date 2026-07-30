---
name: admin-ui-realtime-transport-options
description: Design discussion (undecided) — replacing per-panel SSE connections in the Admin UI with a single browser-side NATS WebSocket connection
metadata:
  type: project
---

**Original symptom (external chat, not this session):** opening a 2nd browser tab to
`http://localhost:7101/` (the Admin UI) leaves both tabs stuck in a "busy"/spinner state and
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
`docker-compose.yml`), subscribing to subjects like `evt.acme.shipping.>` directly client-side —
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
| Tenant scoping | Go handler injects current tenant context | Browser must know the tenant and build the right subject filter itself |

**The real tradeoff is the trust boundary.** Today the Go backend is a gatekeeper: it holds NATS
credentials, subscribes on the browser's behalf, and each SSE endpoint is scoped to show only what
it's designed to show. Moving subscriptions into the browser means the browser needs NATS
credentials directly and is trusted to subscribe only to the right subjects — acceptable for a
POC/lab, but in production this would need a read-only, tightly-scoped-per-tenant JWT rather than
a shared credential.

**Status:** This is a raw discussion note pasted in from elsewhere (two messages: the original
symptom + first 3 options, then the 4th-option follow-up) — not evaluated against this repo's
current architecture, not agreed to, and no implementation should start from this alone. Per
[[design-discussion-vs-implementation-signal]], treat this as the opening round of a multi-round
design conversation; wait for further comments/iteration before proposing a plan.

**Relevant existing context if this resurfaces:** the Admin UI's RPC panel already replays
`obs.rpc.*` traffic via JetStream + SSE (BR-D29/RPCTRACE) — see the `RpcPanel.vue` /
`watchRPCObs` work. Any NATS-WebSocket redesign would need to account for that stream's existing
JetStream-backed replay-then-live semantics, not just simple live pub-sub.
