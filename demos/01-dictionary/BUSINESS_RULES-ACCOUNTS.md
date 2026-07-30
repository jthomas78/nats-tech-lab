# Business Rules — Accounts Service (`backend/accounts-service/`)

> Split out of `BUSINESS_RULES.md` to keep per-domain reads small. See that
> file's index for the Shipping (BR-001–BR-019) and Reference Data
> (BR-D01–BR-D28) domain rules.

Phase 14, [Main-POC-Plan.md](../../.claude/plans/Main-POC-Plan.md). A
separate service, separate Postgres schema (`accounts.accounts`), no
datastore shared with `shipping-service` or `refdata-service` — see
`tenant_service_separation_decision.md`. Provisions NATS accounts at runtime
via decentralized JWTs (`github.com/nats-io/jwt/v2` + `nkeys`), replacing
`nats/bootstrap-operator.sh`'s one-shot `nsc` invocation with a live
`$SYS.REQ.CLAIMS.UPDATE`/`DELETE` round trip. This service has no separate
`internal/domain/` package (flat structure, matching `refdata-service`'s own
"no monolith abstraction" bootstrap pattern) — rule enforcement lives in
`accounts/handler.go`, with the JWT-minting mechanics themselves in
`accounts/provisioner.go`.

### BR-AC01–BR-AC06 — Account lifecycle

- **BR-AC01:** An account name is unique; creating an account with a name
  that already exists is rejected (`409 Conflict`).
- **BR-AC02:** Creating an account mints both the account JWT (pushed to
  every server's resolver via `$SYS.REQ.CLAIMS.UPDATE`) and one user JWT for
  it, returning that user's `.creds` file content exactly once — it is never
  retrievable again through this API afterward.
- **BR-AC03:** Suspending an account (`POST /api/accounts/{name}/suspend`)
  revokes its account JWT at the resolver via `$SYS.REQ.CLAIMS.DELETE` —
  existing connections are not force-closed, but no new connection under that
  account can succeed afterward — and best-effort removes its `.creds` file
  from the shared creds directory so `shipping-service`'s tenant selector
  stops offering it. Suspension is the only deactivation mechanism — there is
  no hard-delete endpoint, because tenant data spans multiple services and
  NATS account-scoped streams, and regulatory retention requirements in
  logistics make true deletion unsafe.
- **BR-AC04:** Only a suspended account may be reactivated
  (`POST /api/accounts/{name}/reactivate`); calling it on an account that is
  not currently suspended is rejected (`409 Conflict`), and an unknown
  account name is rejected (`404 Not Found`). Reactivation restores the
  account under its **original public key and JetStream limits** — it does
  not mint a new account identity — by re-signing and re-pushing an account
  JWT built from the account's own signing key seed and limits, and **always**
  ends with a fresh, working user `.creds` file, returned once in the
  response exactly like BR-AC02's create flow, since the account's previous
  `.creds` file was removed by BR-AC03's suspend. When the account has no
  signing key on record yet — every seeded pre-existing account
  (`default`/`acme`/`globex` — see BR-AC05) starts this way — reactivation
  establishes one on the fly (mints a fresh signing keypair, re-signs the
  account claims with it, persists the seed via `Store.SetSigningKeySeed`)
  before minting the user, rather than restoring the account at the resolver
  level only and leaving it with no way to ever produce working creds again.
  **This closes a real incident (2026-07-28):** `acme`/`globex` were cycled
  through suspend→reactivate, came back `active`, but — under the prior
  behavior, which skipped creds-minting whenever no signing key was on
  record — were left with no `.creds` file anywhere and no way to get one,
  which silently emptied `shipping-service`'s tenant dropdown (nothing but
  the excluded `default` had a working credential). The incident also
  surfaced a second bug in the recovery itself — see BR-AC05's naming note.
  Reactivation's re-signed
  claims always carry a unique `reactivated-<nanoseconds>` tag: NATS account
  JWTs sign deterministically (Ed25519, no nonce), so claims rebuilt from
  identical inputs (same public key, signing key, and limits) would
  otherwise encode to the exact same JWT the account had before suspension —
  which the server's resolver treats as a no-op update, leaving the
  account's in-memory expired state from BR-AC03's revocation unresolved
  even though the JWT is valid again on disk.
- **BR-AC05:** At startup, the three accounts pre-existing from
  `nats/bootstrap-operator.sh` are seeded into this service's Postgres under
  their **lowercase tenant identity** — `default`/`acme`/`globex` — if not
  already present, so the account list is complete without re-minting or
  overwriting their JWTs. Seeded rows have no signing key seed on record
  (this service never minted them) until the first time BR-AC04's
  reactivation establishes one for them.
  **Naming note (2026-07-28):** `bootstrap-operator.sh` names these
  accounts uppercase at the `nsc`/JWT level (`DEFAULT`/`ACME`/`GLOBEX` — that
  is what the resolver JWT filenames and the account claims' own `Name`
  field say), but every other place a tenant is identified —
  `shipping-service`'s `.creds` filenames, NATS subjects, KV bucket
  names — has only ever used the lowercase form. Seeding originally used the
  uppercase nsc name verbatim as the Postgres row's `name`, which is a
  *different string* from the tenant's real identity everywhere else; this
  surfaced live when reactivating `ACME`/`GLOBEX` (BR-AC04's incident) wrote
  `.creds` files as `ACME.creds`/`GLOBEX.creds`, which briefly appeared as
  brand-new, empty tenant contexts distinct from the real `acme`/`globex`
  data. Fixed by seeding under the lowercase name directly, with
  `Store.RenameIfExists` migrating any already-seeded uppercase row
  in place (preserving its public key, status, and history) — safe to call
  every startup, since after the first successful rename the uppercase name
  no longer exists and it's a no-op.
- **BR-AC06:** The account names `DEFAULT` and `SYS` are reserved and can
  never be minted through `POST /api/accounts`, checked
  **case-insensitively** (`Default`, `sys`, `SYS`, etc. are all rejected,
  `409 Conflict`) — not just the exact literal seeded at bootstrap. This
  matters because `shipping-service`'s tenant selector
  (`dictionary/internal/rest/tenant.go`) excludes switchable-tenant
  candidates by an exact match on the shared creds directory's `.creds`
  filename stems (`default`, `sys`); a same-named account minted with
  different casing would produce a differently-cased `.creds` file that
  exact-match filter would miss, letting a reserved name masquerade as a
  switchable tenant. This rule is the primary enforcement point (refusing to
  ever create the problem); `tenant.go`'s own filter was separately hardened
  to compare case-insensitively as defense in depth, in case a reserved-named
  `.creds` file ever reaches that directory some other way (e.g. hand-placed
  outside this API).

The lifecycle rules (BR-AC01, BR-AC03, BR-AC04, BR-AC06) are enforced in
`accounts/handler.go`'s `createAccount`/`suspendAccount`/`reactivateAccount`;
the JWT mechanics behind BR-AC02/BR-AC03/BR-AC04 are in
`accounts/provisioner.go`'s `CreateAccount`/`DeleteAccount`/
`ReactivateAccount`. Ginkgo coverage: `provisioner_test.go` exercises the JWT
round trip directly against an embedded operator-mode NATS server (mint →
connect → revoke → reject → reactivate → connect again, with JetStream
limits verified via `AccountInfo`); `handler_test.go` exercises the full HTTP
flow end to end (create → list → get → suspend → reactivate → reject
double-reactivate → reject unknown-name reactivate → reject reserved names
in every casing) against a real disposable Postgres container, mirroring
`refdata-service`'s container-per-suite test pattern, plus a dedicated
regression spec for the 2026-07-28 incident (a real minted account inserted
with an empty signing key seed and `suspended` status, mimicking a seeded
account, reactivated through the HTTP endpoint and asserted to come back
with connectable creds and a persisted signing key). The
`shipping-service`-side defense in depth is covered separately by
`dictionary/internal/rest/tenant_discovery_test.go`'s
`TestDiscoverTenantsExcludesReservedNamesCaseInsensitively`, a plain Go test
against `discoverTenants` directly (no NATS server needed for this one).
BR-AC05's rename migration is covered by `store_test.go`'s
`RenameIfExists` spec (migrates an existing row, preserves the rest of it,
and is a no-op once the old name is gone).

---

### BR-UA01–BR-UA09 — User accounts

User accounts represent human operators within a tenant (NATS account).
Identity and authentication are delegated to WorkOS; this service owns
domain attributes (tenant association, role, permissions) and NATS credential
minting. WorkOS is the source of truth for *"who is this person and are they
authenticated"*; the app DB is the source of truth for *"what can this person
do in our system"*; NATS user credentials are a downstream artifact derived
from both — never a source of truth themselves.

#### Provisioning

- **BR-UA01:** User provisioning follows a **WorkOS-first, JIT-provision
  downstream** pattern. An admin creates an invitation record in the app DB
  (email, target tenant, role) — no WorkOS user is created at this point.
  The invited person signs up through WorkOS (email/password, SSO, or MFA —
  this service never sees or stores a password). On the first successful
  authentication callback from WorkOS, the backend JIT-provisions the domain
  user: mints a NATS user JWT + NKey under the tenant's account signing key,
  writes the domain user record to the app DB (tenant association, role,
  WorkOS user ID), and returns a short-lived NATS JWT + refresh token. Until
  the person actually authenticates through WorkOS, no domain user record or
  NATS credential exists.
- **BR-UA02:** Enterprise SSO users are provisioned through the same JIT
  path. When a tenant uses SAML/OIDC via WorkOS, Directory Sync (SCIM)
  provisions users from the customer's IdP. The WorkOS authentication
  callback fires identically regardless of whether signup was
  email/password or SAML — the backend's provisioning logic does not
  distinguish between the two.

#### Session & token lifecycle

- **BR-UA03:** On each login, the backend mints a **short-lived NATS user
  JWT** (15–30 min TTL) from the tenant's stored account signing key, and
  issues a longer-lived **refresh token** (7–30 days) alongside it. The NATS
  JWT is the access credential presented to NATS on connect; the refresh
  token is used to obtain a fresh NATS JWT without re-authenticating through
  WorkOS.
- **BR-UA04:** When a NATS user JWT expires, the client uses the refresh
  token to request a new one from the backend. The backend validates the
  refresh token, checks the user's status in the app DB (active, not
  suspended), and mints a fresh NATS JWT. If the refresh token itself has
  expired, the user must re-authenticate through WorkOS to obtain both a new
  NATS JWT and a new refresh token.
- **BR-UA05:** The NKey seed (private key) used to sign NATS user JWTs is
  **never sent to the browser**. It remains server-side; the client receives
  only the signed JWT. The browser holds the NATS JWT (for connecting) and
  the refresh token (for renewal), nothing else.

#### Revocation

- **BR-UA06:** When an admin revokes a user from the app's UI, the backend:
  (1) marks the domain user as suspended in the app DB, (2) revokes the
  user's refresh token server-side so no new NATS JWT can be obtained, and
  (3) calls WorkOS to delete or deactivate the user. The user's last
  short-lived NATS JWT is **not** actively killed — it expires naturally
  within its 15–30 min TTL window. This is an accepted tradeoff: the blast
  radius is bounded by the JWT TTL, and active NATS connection termination
  would require heavier machinery (NATS user JWT revocation lists) that is
  disproportionate for the logistics domain's session-kill latency
  requirements.
- **BR-UA07:** When an IdP-initiated revocation occurs (enterprise customer
  removes an employee from their directory), WorkOS fires a
  `dsync.user.deleted` webhook to the backend, which follows the same
  suspension path as BR-UA06: mark suspended in app DB, revoke refresh
  token, let the NATS JWT expire naturally.
- **BR-UA08:** Revocation order is **app DB first, then WorkOS**. If the
  WorkOS call fails, the user is still locked out of the system (cannot
  obtain new NATS credentials), and the WorkOS cleanup can be retried. The
  reverse order (WorkOS first, app DB second) would leave a window where the
  user cannot log in via WorkOS but still holds a valid refresh token.

#### Authentication mode

- **BR-UA10:** SSO is a **per-tenant configuration option**, not a global
  requirement. Large enterprise tenants connect their own IdP (Okta, Azure
  AD, Google Workspace, etc.) via WorkOS SSO + Directory Sync; smaller
  tenants without an IdP use WorkOS's built-in email/password
  authentication. The logistics industry spans large carriers with
  established identity infrastructure and small freight forwarders that have
  none — requiring SSO globally would block the latter from onboarding.
  WorkOS abstracts both paths behind the same authentication callback, so
  the backend's provisioning and session logic (BR-UA01–UA04) does not
  branch on which mode the tenant uses; only the tenant's WorkOS
  organization configuration differs.

#### Logout

- **BR-UA09:** On explicit logout, the backend revokes the refresh token
  server-side and clears it client-side (httpOnly cookie cleared by the
  response, or local storage entry removed). The short-lived NATS JWT is not
  revoked — it expires naturally within its TTL. A copy of a refresh token
  that was stolen before logout (XSS, proxy log) must not remain usable —
  server-side revocation is the enforcement point, not client-side deletion
  alone.
