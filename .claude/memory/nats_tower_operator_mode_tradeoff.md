---
name: nats-tower-operator-mode-tradeoff
description: NATS Tower needs the server in decentralized JWT operator mode to show real metrics; our lab's nats.conf is auth-free, which is why Tower shows "Error loading data" everywhere except the raw URL/identifier
metadata:
  type: project
---

NATS Tower (an observability/admin tool evaluated alongside `nats-ui`/`nui` under `demos/01-dictionary/nats/`) requires the target NATS server to be bootstrapped in decentralized JWT **operator mode** to pull real accounts/users/metrics. It needs a `resolver: { type: full, dir: ... }` block plus an included `operator.conf` so Tower has accounts to query.

Our lab's `nats.conf` is a bare, auth-free server (no operator/accounts/resolver) — a client URL against that gives Tower nothing to query, hence "Error loading data" everywhere except the raw URL/identifier field.

Converting to operator mode would be a **structural change to the one shared `nats` service** that `shipping-service`, `refdata-service`, `nats-ui`, and `nui` all connect to, and would require issuing new accounts/creds for each of those services.

**Why:** this came up while trying to get real metrics into NATS Tower for the dictionary POC's NATS tooling evaluation. The user was unfamiliar with operator-mode JWT auth (operator → account → user JWT chain, signed with Ed25519, validated via the resolver) and asked for background before deciding how to proceed.

**How to apply:** when NATS admin/observability tooling (Tower or similar) reports auth/metrics errors against this lab's NATS server, check whether it's this same operator-mode requirement before assuming a config bug. As of this writing the user had **not yet decided** how to proceed — options on the table were: (1) leave the shared server as-is (auth-free, Tower stays read-only/limited — recommended default), (2) spin up a second, isolated NATS server just for Tower to manage in operator mode, or (3) convert the shared `nats` service itself to operator mode (higher blast radius — touches all four dependent services). Don't assume a direction was chosen; confirm with the user first.

See [[dev_machine_toolchain]] for the Mac/Linux Docker split relevant to running any additional NATS server.
