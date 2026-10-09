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
| `D07-R1` | Operator mode only. The server config lists no user. The callout settings live in the account JWT, not in the server config. | stage 02 | not started |
| `D07-R2` | A real WorkOS identity is checked by the Go callout service and admitted into `ORDERS`. | stage 02 | not started |
| `D07-R3` | The WorkOS role sets the NATS permissions, and the server enforces them. | stage 02 | not started |
| `D07-R4` | Refused: no token, a bad token, an expired token, and a valid token with an unknown role. | stage 02 | not started |
| `D07-R5` | When the callout service is down, the client is refused after the timeout. The time is measured. | stage 02 | not started |
| `D07-R6` | Only the callout service's own user skips the callout. | stage 02 | not started |
| `D07-R7` | A callout response signed by the wrong key is refused. | stage 02 | not started |
| `D07-R8` | At least one admission uses a real WorkOS token. A stand-in result is labelled "does not prove WorkOS". | stage 02 | not started |
| `D07-R9` | Another person runs this README with no help and sees every exercise work. | all | not started |
| `D07-R10` | A pattern cards deck closes the demo (playbook stage 04). | — | not started |

**Demo 07 is in stage 01.** Ports, names, the exercise list and the rig
come in stage 02.
