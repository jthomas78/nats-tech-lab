---
name: nats_auth_terminology_and_demo_boundaries
description: Centralized auth = config mode (demo 05); decentralized auth = operator-mode JWT chain (demo 06); auth callout = demo 07, operator mode only, WorkOS. Hard scope lines.
metadata:
  type: feedback
---

**Canonical terms (set by the user 2026-10-08, checked against docs.nats.io
/learn/security, authentication-basics, decentralized-auth, auth-callout):**

- **Centralized authentication** = NATS **configuration mode**. The server
  config defines the accepted users, credentials and permissions. Docs: "the
  server holds the full list of users in its own config file".
- **Decentralized authentication** = NATS **operator mode**. The operator →
  account → user JWT chain delegates identity and permissions; the server
  config lists no user.
- The split is **config-managed users vs delegated JWT trust**. It is NOT
  "local files vs no files": operator mode has a config and stores JWTs on
  disk too.
- **Auth callout** delegates the authentication decision to an external
  service. Docs: it "works in either mode". The docs' worked example switches
  to config mode — **never copy that example here.**
- **NKeys alone do not mean operator mode.** An NKey listed in the server
  config is centralized authentication.

**Demo boundaries:**

- **Demo 05** (`demos/05-identity-and-permissions/`) — centralized auth +
  authorization. Config mode only; secrets may come from env vars. No operator
  mode, no JWT chain, no JWT resolver, no auth callout. Config-listed NKeys
  stay here.
- **Demo 06** (`demos/06-operator-trust-and-credentials/`) — operator mode and
  decentralized auth only. NATS-native credentials, trust checks, permission
  enforcement. No auth callout, no external IdP. Builds its own chain.
- **Demo 07** — decentralized auth with external auth callout. **Folder does
  not exist; lifecycle not started — do not create it unasked.** Operator mode
  only, keeping the NATS operator/account trust base. The callout service
  validates a WorkOS identity and maps it to NATS access through the signed
  NATS authorization request/response. An online external auth service does
  NOT make it "centralized configuration mode". Builds its own operator and
  chain; runs without demo 06.

**Shared:** every demo is one small, self-contained slice
([[demos_are_small_slices_not_demo01]]). Demos 06 and 07 build on each other
in ideas, never in files or running processes. Docs coverage alone never
widens an exercise list.

**Why:** the terms were drifting (demo 05 docs once sent operator mode to
demos 01/02), and the NATS callout example quietly drops operator mode.

**How to apply:** use these words in every demo doc, memory and deck. When a
request or a docs page pulls a feature across a line above, name the line and
ask. Each demo's own `CLAUDE.md` holds the detailed rule; this note is the
cross-demo summary.
