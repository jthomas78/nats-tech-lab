---
name: nats-tower-operator-mode-tradeoff
description: NATS Tower needs the server in decentralized JWT operator mode to show real metrics — resolved 2026-07-28 when Phase 14a converted the shared server to operator mode; Tower itself still needs sys.creds added through its own UI (manual, not yet done)
metadata:
  type: project
---

NATS Tower (an observability/admin tool evaluated alongside `nats-ui`/`nui` under `demos/01-dictionary/nats/`) requires the target NATS server to be bootstrapped in decentralized JWT **operator mode** to pull real accounts/users/metrics. It needs a `resolver: { type: full, dir: ... }` block plus an included `operator.conf` so Tower has accounts to query.

Our lab's `nats.conf` is a bare, auth-free server (no operator/accounts/resolver) — a client URL against that gives Tower nothing to query, hence "Error loading data" everywhere except the raw URL/identifier field.

Converting to operator mode would be a **structural change to the one shared `nats` service** that `shipping-service`, `refdata-service`, `nats-ui`, and `nui` all connect to, and would require issuing new accounts/creds for each of those services.

**Why:** this came up while trying to get real metrics into NATS Tower for the dictionary POC's NATS tooling evaluation. The user was unfamiliar with operator-mode JWT auth (operator → account → user JWT chain, signed with Ed25519, validated via the resolver) and asked for background before deciding how to proceed.

**Resolved 2026-07-28 (Phase 14a):** option 3 — the shared `nats` service converted to decentralized-JWT operator mode, driven by Phase 13/14's own need for dynamic tenant provisioning (accounts-service), not by this Tower question directly. `shipping-service`/`refdata-service` now connect via `.creds` files (`nats/creds/*.creds`, minted by `nats/bootstrap-operator.sh` or `accounts-service` at runtime); `nats-ui`/`nui` are unaffected (they register connections through their own UI, not env vars). Tower itself has **not yet** been pointed at the new SYS account — a user who wants real Tower metrics now adds `nats/creds/sys.creds` through Tower's own "add installation" flow (same pattern `nui` already uses); this is a manual follow-up, not automated in docker-compose.

**How to apply:** when NATS admin/observability tooling (Tower or similar) reports auth/metrics errors against this lab's NATS server, check whether it needs `sys.creds` registered through its own UI before assuming a config bug.

See [[dev_machine_toolchain]] for the Mac/Linux Docker split relevant to running any additional NATS server.
