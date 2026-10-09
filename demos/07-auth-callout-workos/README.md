# Demo 07 — Auth Callout with WorkOS

**One question:**

> Can a NATS server in operator mode use a Go auth callout service to
> validate a real WorkOS identity, assign the correct account and
> permissions, and deny access when validation fails or the callout service
> is unavailable?

In demo 06 every user carried a NATS credential signed by its account. Here
a person carries a **WorkOS identity** instead. When they connect, the NATS
server asks a small service of ours — the **auth callout** — "may this
person in?". The service checks the WorkOS identity, then answers with a
signed NATS user that says which account and which permissions they get.

**Role: showcase + validation.** You log in with WorkOS in a terminal and
watch NATS admit or refuse you. The refusal cases and the "callout service
is down" timeout are measured, so they can back a real platform choice.

**The scenario, used in every exercise:** one account, `ORDERS`, with
subjects `orders.*`. The WorkOS role decides access:

- `dispatcher` may publish and read `orders.*`.
- `viewer` may only read `orders.*`.

**This is still decentralized authentication.** The server runs in NATS
**operator mode**: the operator → account → user JWT chain is the trust
base, and the server config lists no user. The callout settings live in an
account JWT. Running an outside auth service does not turn this into
centralized configuration mode (that was demo 05).

**WorkOS is real.** The main path uses a real WorkOS environment. A local
stand-in token issuer is used only where it helps a test. A stand-in result
does not prove the WorkOS integration, and it is always labelled.

**Sources:** [Auth callout](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_callout)
and [Decentralized authentication](https://docs.nats.io/learn/security/decentralized-auth)
from the NATS docs, plus the WorkOS docs for the token flow. Demo 07 builds
its own operator and chain; it does not need demo 06.

## Requirements

| ID | Requirement | Exercise | State |
|---|---|---|---|
| `D07-R1` | Operator mode only. The server config lists no user. The callout settings live in the account JWT, not in the server config. | 01 | not started |
| `D07-R2` | A real WorkOS identity is checked by the Go callout service and admitted into `ORDERS`. | 01 | not started |
| `D07-R3` | The WorkOS role sets the NATS permissions, and the server enforces them. | 02 | not started |
| `D07-R4` | Refused: no token, a bad token, an expired token, and a valid token with an unknown role. | 03 | not started |
| `D07-R5` | When the callout service is down, the client is refused after the timeout. The time is measured. | 04 | not started |
| `D07-R6` | Only the callout service's own user skips the callout. | 01 | not started |
| `D07-R7` | A callout response signed by the wrong key is refused. | 04 | not started |
| `D07-R8` | At least one admission uses a real WorkOS token. A stand-in result is labelled "does not prove WorkOS". | 01 | not started |
| `D07-R9` | Another person runs this README with no help and sees every exercise work. | all | not started |
| `D07-R10` | A pattern cards deck closes the demo (playbook stage 04). | — | not started |

**Demo 07 is in stage 02** (the rig is designed, nothing is built yet).

## What you will need

- `nats-server` and the `nats` CLI on the host (designed against
  `nats-server v2.14.6`).
- `nsc` (designed against `2.15.0`). This demo builds the whole operator →
  account → user chain with `nsc`, because the `nats auth` CLI (`0.4.0`) has
  no command to set an auth callout on an account. `nsc edit authcallout` has.
- Go (designed against `1.27.0`) for the callout service.
- A **WorkOS** staging environment with AuthKit and **CLI Auth** turned on,
  the roles `dispatcher` and `viewer`, and three test users: one
  `dispatcher`, one `viewer`, and one with neither role (for the "unknown
  role" refusal). You write the WorkOS client ID into `.env.workos` (not in
  Git). CLI Auth is a public client: it needs the client ID only, no API key
  and no secret ([WorkOS CLI Auth](https://workos.com/docs/authkit/cli-auth)).
- Network access to `api.workos.com` (log-in and the signing keys).
- No Docker, no Python.

## Ports and names

| Thing | Value |
|---|---|
| NATS client | `127.0.0.1:4822` |
| NATS monitor | `127.0.0.1:8822` |
| Server name | `d07-server` |
| Operator | `D07` |
| Accounts | `AUTH` (holds the callout service), `ORDERS` (where people land), `SYS` |
| Users in `AUTH` | `callout-svc` (skips the callout), `sentinel` (no rights; see below) |
| Users in `ORDERS` | none stored — the callout service makes one per connection |
| WorkOS keys | `.env.workos` (not in Git), variable `D07_WORKOS_CLIENT_ID` |
| Keys, creds, resolver, logs, PID files | `.run/` (not in Git) |

`4822` / `8822` appear nowhere else in the repo and nothing listened on them
(checked 2026-10-09).

## How it works (the design, not yet measured)

![Demo 07 rig: the nats CLI connects with sentinel creds and a WorkOS token to one bare operator-mode nats-server on 127.0.0.1:4822; the server sends an authorization request to the Go callout service, which verifies the token against WorkOS's JWKS and returns a response signed by the AUTH key; the server then admits a per-connection user into ORDERS.](diagrams/d07-topology.png)

Editable source: [`diagrams/d07-topology.html`](diagrams/d07-topology.html).
Re-export from the repo root with
`node demos/01-dictionary/diagrams/export-html-png.mjs demos/07-auth-callout-workos/diagrams/d07-topology.html demos/07-auth-callout-workos/diagrams/d07-topology.png 1024 --clip=".wrap"`.
Check it first with `node demos/01-dictionary/diagrams/audit-svg-layout.mjs demos/07-auth-callout-workos/diagrams/d07-topology.html`.

1. You log in to WorkOS from the terminal (the OAuth device flow). You get a
   WorkOS **access token**, a JWT with your `role` and an expiry.
2. You connect to NATS with that token (`nats --token`).
3. The NATS server does not decide. It sends a signed **authorization
   request** to the account `AUTH`, where the Go service `callout/` listens.
4. The service checks the token against WorkOS's public signing keys
   (`https://api.workos.com/sso/jwks/<client_id>`), reads the role, and
   answers with a signed NATS user for account `ORDERS`, with the role's
   permissions. Or it refuses.
5. The server checks that the answer is signed by the `AUTH` account's key,
   then admits or refuses you.

*Prediction, to be proved or disproved in exercise 01:* in operator mode the
client must also present a `sentinel` user from `AUTH`. It has no rights. It
only tells the server which account's callout to use.

## The exercises (planned)

| # | Question | Requirements | State |
|---|---|---|---|
| 01 | Does NATS use the Go auth callout to validate a real WorkOS identity and admit the user into `ORDERS`? | R1, R2, R6, R8 | not started |
| 02 | Does the WorkOS role set the permissions (`dispatcher` against `viewer`)? | R3 | not started |
| 03 | Who is refused: no token, a bad token, a real expired token, an unknown role? | R4 | not started |
| 04 | What if the callout fails: the service is down, or its answer is signed with the wrong key? | R5, R7 | not started |

**Exercise 01 must show the full path, one visible piece of evidence per
link, from the same run.** An admission alone is not enough:

| Link | Evidence (where it is seen) |
|---|---|
| 1. NATS sends the authorization request | the callout service logs the request it received: server ID, client info, the `sentinel` user's public key — never the token |
| 2. Go validates the WorkOS token | the callout service logs the token's claims it checked: issuer, `sub`, `role`, `exp`, and that the signature matched a key from the WorkOS JWKS |
| 3. Go returns a signed authorization response | the callout service logs the response it signed: the issued user's account (`ORDERS`), its permissions, and the signing key's public key |
| 4. NATS admits the user | the client connects and a publish is delivered; the server's connection list on the monitor port (`/connz`) shows the connection in account `ORDERS` |

Which server log line (if any) marks steps 1 and 4 is not known yet. Stage
03 finds it and adds it here.

**Exercise 01 also proves the bypass rule (`D07-R6`)**, in the same run:

| Check | Expected | Evidence |
|---|---|---|
| `callout-svc` connects with its own `.creds` | admitted with **no** callout | the callout service logs **no** request for it; `/connz` shows it in `AUTH` |
| Any other `AUTH` user (`sentinel`) connects with no WorkOS token | goes **through** the callout, and is refused | the callout service logs a request for it, then a refusal; the client gets `Authorization Violation` |
| Any other `AUTH` user connects with a valid WorkOS token | goes **through** the callout, and lands in `ORDERS`, not `AUTH` | the full-path evidence above |

**What would prove the design wrong:** a client with no valid WorkOS token
gets in, or a client with a valid one lands in the wrong account or with the
wrong permissions.

**How each break is made** (designed before the happy path):

| Break | How | Real WorkOS? |
|---|---|---|
| No token | connect with `sentinel` only, no `--token` | — |
| Bad token | a random string, and a real token with one changed character | partly |
| Expired token | a real WorkOS token, used after **its own** `exp` claim. Read `exp` from the token; never assume a lifetime | yes |
| Unknown role | the third WorkOS test user | yes |
| Service down | stop the callout service, connect, time the refusal | yes |
| Wrong signing key | the service answers with a key that is not `AUTH`'s (negative-test machinery in `exercises/`, never in the normal setup) | stand-in |

## Reset (planned)

```bash
lab/down.sh --clean
```

It will stop this demo's server and callout service (by their PID files
only) and delete `.run/`.
