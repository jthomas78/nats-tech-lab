---
adr: 57
title: An Auth Callout in Operator Mode Turns a WorkOS Identity into NATS Access
status: Proposed
date: 2026-10-09
scope: lab
context: nats
decision: A Go auth callout service, registered on a dedicated AUTH account in operator mode, validates the WorkOS access token on every new connection and returns a signed NATS user for the mapped account (ORDERS) and role permissions. Proposed until demo 07's exercises validate it.
why: A person's identity and role live in WorkOS, which NATS cannot read. Auth callout is the agreed direction because it decides at connect time from the live token, stores no NATS user, and keeps operator mode as the trust base.
related: []
---

# ADR-057: An Auth Callout in Operator Mode Turns a WorkOS Identity into NATS Access

**Status:** **Proposed 2026-10-09** — the design is the agreed direction.
Demo 07 (`demos/07-auth-callout-workos/`) validates it; its stage 03 moves
this ADR to Accepted, or records what failed.
**Date:** 2026-10-09
**Deciders:** Jeremy (repo owner)
**Related:** `demos/07-auth-callout-workos/README.md` (requirements
`D07-R1`–`D07-R8`); demo 06's pattern cards
(`demos/06-operator-trust-and-credentials/docs/demo-06-pattern-cards.pdf`)
for the operator-mode trust chain this builds on.

## Context

People log in with **WorkOS** (AuthKit). Their organization and role live
there. NATS runs in **operator mode**: the operator → account → user JWT
chain is the trust base, and the server config lists no user (demo 06).

NATS cannot read WorkOS. Something has to turn "this WorkOS user, with this
role" into "this NATS account, with these publish and subscribe rights".
Configuration mode is out of scope: the platform keeps operator mode.

## Decision

Use an **auth callout in operator mode**:

- A dedicated account `AUTH` carries the callout settings in its account
  JWT: the callout service's own user (`callout-svc`) is the only user that
  skips the callout, and `ORDERS` is the only account it may place users in.
- A client connects with its WorkOS access token. The server sends a signed
  authorization request to the callout service.
- The Go service validates the token against WorkOS's signing keys, maps
  the WorkOS role to permissions (`dispatcher`: publish and read
  `orders.*`; `viewer`: read only), and answers with a NATS user JWT for
  `ORDERS`, signed by the `AUTH` account key. Or it refuses.

## What demo 07 must show before this is Accepted

| Requirement | Claim under test |
|---|---|
| `D07-R1` | The callout settings live in the account JWT; the server config lists no user. |
| `D07-R2`, `D07-R8` | A real WorkOS identity is admitted into `ORDERS`, with evidence for every link of the path. |
| `D07-R3` | The WorkOS role sets the permissions, and the server enforces them. |
| `D07-R4` | No token, a bad token, an expired token and an unknown role are refused. |
| `D07-R5` | With the service down, connections are refused after the timeout. |
| `D07-R6` | Only `callout-svc` skips the callout; every other user goes through it. |
| `D07-R7` | An answer signed by the wrong key is refused. |

## Background — not evaluated here

Another known shape is a **token exchange** service that takes the WorkOS
token once and hands out a short-lived NATS `.creds` file, after which the
client connects the ordinary operator-mode way. It keeps the service off the
connect path, but leaves a NATS credential alive outside WorkOS's control
until it expires or is revoked (demo 06 measured both mechanisms). Demo 07
does not test this alternative, so this ADR draws no comparison from it.

## Consequences

Written when the ADR is accepted.

## Action Items

1. Build and measure demo 07 exercises 01–04.
2. Accept this ADR with the evidence, citing the `D07-R*` IDs, or record
   which claim failed.
