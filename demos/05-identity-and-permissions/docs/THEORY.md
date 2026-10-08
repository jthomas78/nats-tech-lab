# Demo 05 — theory, and the plan for every exercise

Written from two pages of the NATS docs, read 2026-09-30:

- **[AUTHN]** <https://docs.nats.io/learn/security/authentication-basics>
- **[AUTHZ]** <https://docs.nats.io/learn/security/authorization>

Everything below is **what the docs say**, in our own words. It is a
prediction until an exercise measures it. What was actually measured is in
[`EXERCISE_OBSERVATIONS.md`](../exercises/EXERCISE_OBSERVATIONS.md).

## The two questions

| | Authentication | Authorization |
|---|---|---|
| Asks | Who are you? | What may you do? |
| Checked | once, when the connection opens | on every publish and every subscribe |
| On failure | the connection is refused and closed | the one operation is refused; the connection stays open |
| Client sees | `Authorization Violation`, and the connect fails | a permissions error, **possibly later** than the call that caused it |

Note the naming trap: NATS reports a failed **authentication** as
`Authorization Violation`, and the config block for users is called
`authorization { }` even when it holds no permissions at all.

## Exercise 01 — who gets in? *(built, measured)*

- No `authorization` block → every client is admitted. [AUTHN]
- `authorization { users: [ {user, password} ] }` → one identity per entry.
- A wrong password, an unknown user and no credentials give the client the
  **same** error. The server log tells them apart. [AUTHN]
- The server warns at start-up when passwords are in plaintext. [AUTHN]
- Lab choice (not from the docs): passwords come from `$D05_...` environment
  variables, so no config file holds a secret.

## Exercise 02 — separate publish and subscribe limits *(built, measured)*

- Each user has a `permissions` block with independent `publish` and
  `subscribe` lists. [AUTHZ]
- Shape to build: `order-svc` may publish `orders.>` and nothing else;
  `analytics-reader` may subscribe `orders.>` and publish nothing.
- Denials to show: `analytics-reader` publishing an order; `order-svc`
  subscribing to orders.
- Positive control: the allowed path delivers in the same run.
- What to capture: the client's permissions error, the server log line, and
  that the denied message is **not delivered**. The docs say a denied publish
  is dropped and the connection kept. [AUTHZ]

## Exercise 03 — allow, deny, wildcards, defaults, empty lists *(in progress: 03a, 03b measured)*

Claims from [AUTHZ] to test one at a time:

1. Writing an `allow` list denies everything not on it.
2. When a subject matches both `allow` and `deny`, **deny wins**.
3. `*` matches one token, `>` matches one or more trailing tokens.
4. An **empty** list is read as *no restriction*, not *deny all*. To deny
   everything, write `deny: [">"]`.
5. `default_permissions` applies only to users with no `permissions` block
   of their own. A user's own block **replaces** the default; the two are not
   merged.
6. A literal subscription to a denied subject fails loudly. A **wildcard**
   subscription that overlaps a deny is accepted, and the denied subjects are
   filtered out at delivery, silently.

Claim 6 is the one most likely to surprise. It needs a positive control on a
sibling subject to prove the subscription is live.

## Exercise 04 — request / reply under limits *(drafted, not run)*

- A requester subscribes to a private inbox (`_INBOX.>`) before it sends the
  request. With subscribe denied, the reply has nowhere to go and the
  request **times out** — silently, not with an error. [AUTHZ]
- A responder needs permission to publish the reply. `allow_responses` gives
  exactly that, one reply per request, without a broad publish grant. [AUTHZ]
- Scenario: `analytics-reader` asks `order-svc` for an order summary on
  `orders.summary`.
- A timeout alone proves nothing. Show the same request succeeding with the
  inbox allowed, then failing with it denied, then read the server log.

## Exercise 05 — token and NKey, against the password *(drafted, not run)*

- **Token:** one shared secret for the whole server. No per-user identity, so
  no per-user permissions. [AUTHN]
- **NKey:** the server stores only a public key. The client signs a nonce
  (a one-time random value) with its private seed, so no secret crosses the
  wire. An NKey user entry cannot also carry a password. [AUTHN]
- Tools to verify on this machine: `nats auth nkey gen`, `nats auth nkey show`,
  `nats ... --nkey <file>`. Seeds go in `.run/`, never in Git.

## Exercise 06 — bcrypt, and what it does not do *(planned)*

- `nats server passwd` turns a password into a bcrypt hash (a one-way,
  deliberately slow scramble) for the config file. [AUTHN]
  Installed CLI: `-p/--pass`, `-c/--cost` (default 11), `-g/--generate`.
- The client still **sends the real password**. The server hashes what it
  receives and compares. So bcrypt protects the config file at rest, and
  nothing on the wire. [AUTHN]
- **TLS** protects the wire. This demo explains the split; it does not set up
  TLS. That is a later demo.
- To verify: the plaintext warning goes away; login still works; start-up and
  connect cost at different `--cost` values.

## Boundaries — for later demos

- **Accounts** — separate subject spaces on one server. Demo 03 uses them.
- **Operator mode / JWTs / `nsc`** — decentralized authentication: the
  operator → account → user JWT trust chain. Demo 06. Everything in demo 05
  is centralized authentication (configuration mode), NKeys included: an
  NKey listed in the server config is still a config-managed user.
- **Auth callout** — an external service decides who gets in. It works in
  either mode; this lab uses it in operator mode only. Demo 07 (WorkOS).
- **`no_auth_user`** — lets unauthenticated clients in as a named user. The
  docs warn it can undo a lock-down; worth one line in exercise 01's notes
  when it is measured. [AUTHN]

## Coverage of the NATS security docs

Demo 05 covers **part** of two pages: Authentication basics [AUTHN] and
Authorization [AUTHZ]. It does not cover the other pages under
<https://docs.nats.io/learn/security/>. Status as of 2026-10-07:

- **Measured** — recorded in `exercises/EXERCISE_OBSERVATIONS.md`.
- **Written, not run** — steps and a check script exist; every check in it is
  a prediction. `nats-server -t` (config validation) and `shellcheck` were run;
  neither says anything about runtime behaviour.
- **Planned** — an outline above, nothing built.
- **Not covered** — in the two pages, but no exercise is planned yet.
- **Out of scope** — kept for later demos (see Boundaries).

| Docs topic | Page | Exercise | Status |
|---|---|---|---|
| Central auth: the server holds the user list | AUTHN | 01 | measured |
| Giving a credential; connecting; refusal | AUTHN | 01 | measured |
| Token | AUTHN | 05a | written, not run |
| NKeys | AUTHN | 05b | written, not run |
| Storing passwords (bcrypt) | AUTHN | 06 | planned |
| Password in a URL or a saved context | AUTHN | — | not covered |
| `--user` sent as a token | AUTHN | — | not covered |
| A bcrypt-hashed token | AUTHN | — | not covered |
| `no_auth_user` | AUTHN | — | not covered |
| Subjects, publish and subscribe sides | AUTHZ | 02 | measured |
| Deny beats allow; `*` and `>` | AUTHZ | 03a | measured (by hand) |
| An empty allow list | AUTHZ | 03b | measured (by hand) |
| `default_permissions` replaced, not merged | AUTHZ | 03c | written, not run |
| Wildcard subscription overlapping a deny | AUTHZ | 03d | written, not run |
| Request / reply: the requester's inbox | AUTHZ | 04a | written, not run |
| `allow_responses` | AUTHZ | 04b | written, not run |
| Client exit codes on a denial | AUTHZ | 02–04 | not measured |
| Queue-group permissions | AUTHZ | — | not covered |
| Allowing `_INBOX.>` next to a deny | AUTHZ | — | not covered |
| Accounts, operator mode, JWTs, auth callout | both | — | out of scope (operator mode: demo 06; auth callout: demo 07) |
| TLS, OCSP and the other security pages | — | — | out of scope |
