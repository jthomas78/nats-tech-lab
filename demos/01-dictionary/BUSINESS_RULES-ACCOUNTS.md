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

**Phase 19 — auth-service merged in.** Phase 15c's `auth-service` (browser
NATS credential minting, BR-UA01–UA09 below) started as its own Go module
and container because it predates the account-lifecycle work above; by the
time BR-AC01–BR-AC11 existed, its only real job was reading this service's
own `accounts.accounts` Postgres table over a second connection to the same
instance — a cross-service coupling with no independent lifecycle, no NATS
connection of its own, and no state beyond what `accounts.Store` already
holds. Phase 19 folded it into this binary as the sibling `accounts-service/auth/`
package: same `cmd/main.go`, same HTTP mux, same `accounts.Store` passed in
directly instead of read through a duplicate Postgres connection. Its three
routes (`GET /api/auth/connectInfo`, `GET /api/auth/tenants`,
`POST /api/auth/login`) stay ungated exactly as before — see
`auth/handler.go`'s `connectInfo` doc comment for why — while the
`/api/accounts/*` routes above stay `BasicAuth`-gated; `cmd/main.go` mounts
both handler sets on the one `http.ServeMux`. The `auth` package's own
tests migrated with it (`accounts-service/auth/*_test.go`), now reusing
`accounts.Migrate` for test schema setup instead of hand-duplicating its
`CREATE TABLE` statements the way a separate module had to.

### BR-AC01–BR-AC13 — Account lifecycle

- **BR-AC01:** An account name is unique; creating an account with a name
  that already exists is rejected (`409 Conflict`).
- **BR-AC02:** Creating an account mints both the account JWT (pushed to
  every server's resolver via `$SYS.REQ.CLAIMS.UPDATE`) and one user JWT for
  it, returning that user's `.creds` file content exactly once — it is never
  retrievable again through this API afterward.
- **BR-AC03:** Suspending an account (`POST /api/accounts/{name}/suspend`)
  revokes its account JWT at the resolver via `$SYS.REQ.CLAIMS.DELETE`, and
  best-effort removes its `.creds` file from the shared creds directory so
  `shipping-service`'s tenant selector stops offering it. **Corrected
  2026-08-03:** this entry previously claimed existing connections are not
  force-closed by the revoke — verified against NATS 2.11 on the running
  stack, that is false; the server evicts every connection on the account
  within a couple of seconds (see `ARCHITECTURE-ACCOUNTS.md` § 2t-a for the
  full runtime consequence, and BR-AC09/BR-031 for what reacts to it).
  Suspension is the only deactivation mechanism — there is no hard-delete
  endpoint, because tenant data spans multiple services and NATS
  account-scoped streams, and regulatory retention requirements in logistics
  make true deletion unsafe.
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
  their **lowercase tenant identity** — `platform`/`acme`/`globex` — if not
  already present, so the account list is complete without re-minting or
  overwriting their JWTs. Seeded rows have no signing key seed on record
  (this service never minted them) until the first time BR-AC04's
  reactivation establishes one for them.
  **Naming note (2026-07-28):** `bootstrap-operator.sh` names these
  accounts uppercase at the `nsc`/JWT level (`PLATFORM`/`ACME`/`GLOBEX` — that
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
- **BR-AC06:** The account names `PLATFORM` and `SYS` are reserved and can
  never be minted through `POST /api/accounts`, checked
  **case-insensitively** (`Platform`, `sys`, `SYS`, etc. are all rejected,
  `409 Conflict`) — not just the exact literal seeded at bootstrap. This
  matters because `shipping-service`'s tenant selector
  (`dictionary/internal/rest/tenant.go`) excludes switchable-tenant
  candidates by an exact match on the shared creds directory's `.creds`
  filename stems (`platform`, `sys`); a same-named account minted with
  different casing would produce a differently-cased `.creds` file that
  exact-match filter would miss, letting a reserved name masquerade as a
  switchable tenant. This rule is the primary enforcement point (refusing to
  ever create the problem); `tenant.go`'s own filter was separately hardened
  to compare case-insensitively as defense in depth, in case a reserved-named
  `.creds` file ever reaches that directory some other way (e.g. hand-placed
  outside this API).
- **BR-AC07 (Phase 16c):** An account name beginning with `_` is rejected
  (`400 Bad Request`) — that prefix is reserved for platform/system use
  across the whole subject/context taxonomy (`ARCHITECTURE-COMMUNICATIONS.md`
  § 2.3), not a concept this service owns itself. It matters here because, in
  the common case where a tenant has no company-group split (Main-POC-Plan.md
  Phase 16 decision 11), a tenant's own name doubles as its company
  `{context}` value — so an account named e.g. `_ops` would let that reuse
  silently claim the reserved `_platform` namespace the moment it's used as a
  context. refdata-service's own `ValidateContextName` (BR-D33,
  `BUSINESS_RULES-REFDATA.md`) is the primary enforcement point for context
  values specifically, since a context can be registered independently of any
  account; this rule closes the same gap one level up, where a tenant
  identity is minted in the first place. Only a *leading* underscore is
  rejected — `acme_northdiv` is unaffected.
- **BR-AC08 (Phase 16h):** After a create fully succeeds (resolver JWT
  pushed, `.creds` file written, Postgres row persisted), this service
  publishes `notify.accounts.account.created` — a context-free subject
  (this service has no `{context}` of its own; see
  `ARCHITECTURE-COMMUNICATIONS.md` § "Context-free services") — with the new
  tenant's name, over a second, PLATFORM-account NATS connection
  (`Handlers.NotifyNC`) dedicated to this one purpose. Exists so
  `shipping-service` can provision that tenant's resources reactively the
  instant it's minted, instead of only at its own next startup or the first
  time an operator switches the Admin UI to it — see
  `BUSINESS_RULES-SHIPPING.md`'s BR-030, the consumer side. Deliberately not
  published from the existing SYS-account connection (`Provisioner`'s own):
  core NATS pub/sub never crosses an account boundary, and
  shipping-service's subscriber listens on its own PLATFORM account.
  Best-effort — a publish failure doesn't fail the create request (the
  account is already fully committed by that point); `NotifyNC` is nil-safe,
  so this service still runs with the event simply never sent if
  `NATS_PLATFORM_CREDS_PATH` isn't configured. A rejected/failed create
  (duplicate name, minting error, etc.) never publishes anything — only a
  fully-committed account does.
- **BR-AC09 (Phase 16i):** After a suspend fully succeeds (resolver JWT
  revoked, Postgres row marked `suspended`), this service publishes
  `notify.accounts.account.suspended` — the mirror of BR-AC08, same
  context-free subject family, same `Handlers.NotifyNC` connection — with the
  suspended tenant's name. Exists so `shipping-service` can react promptly:
  BR-AC03's revoke force-evicts that tenant's connections, but eviction alone
  doesn't stop `nats.go`'s default reconnect logic from retrying forever
  against a `.creds` file this same suspend call has already deleted (see
  `BUSINESS_RULES-SHIPPING.md`'s BR-031, the consumer side, and
  `ARCHITECTURE-ACCOUNTS.md` § 2t-a for the full runtime behavior this
  closes). Same best-effort/nil-safe contract as BR-AC08: a publish failure
  doesn't fail the suspend request, since the revoke — the actual security
  boundary — has already happened by that point. A rejected/failed suspend
  (unknown account name) never publishes anything.
- **BR-AC10 (Phase 16j):** After a reactivate fully succeeds (resolver JWT
  re-pushed, signing key persisted, fresh `.creds` written, status back to
  `active`), this service publishes `notify.accounts.account.reactivated` —
  completing the lifecycle triple with BR-AC08/BR-AC09, same subject family
  and `Handlers.NotifyNC` connection. Without it, BR-AC09's teardown is a
  **one-way door**: `shipping-service` drops a suspended tenant's resources
  and nothing ever rebuilds them, so a reactivated tenant stays unusable
  until that process restarts or an operator manually switches the Admin UI
  to it (see `BUSINESS_RULES-SHIPPING.md`'s BR-032, the consumer side). The
  ordering matters and is asserted: the event must not fire until the fresh
  `.creds` file exists, because the consumer resolves the tenant by scanning
  that directory. Same best-effort/nil-safe contract as BR-AC08/BR-AC09; a
  rejected reactivation (unknown name, or an account that isn't suspended)
  never publishes anything.
- **BR-AC11 (2026-08-03):** Every lifecycle state change — create, suspend,
  reactivate — records an immutable row in `accounts.audit_events`: action,
  account name, actor, source IP, an outcome of `success`/`failed`, and a
  JSONB metadata payload. The table is append-only (no `UPDATE`, no
  `DELETE`), closing the gap the 2026-08-03 accounts architecture review
  flagged — previously the only trace of a lifecycle change was
  `accounts.updated_at`, which is in tension with BR-AC03's own
  regulatory-retention rationale for disallowing hard deletes. Actor is a
  placeholder until WorkOS-backed human auth lands: the shared basic-auth
  username (`admin`, see `BasicAuthUser`), overridable per request via an
  `X-Actor` header so a caller can self-identify in the interim — neither is
  authenticated identity, but both are strictly better than nothing and
  become real once WorkOS supplies an actual principal, behind the same
  column. Audit writes are best-effort: a failed insert is logged but never
  blocks or rolls back the lifecycle operation it's describing, and a
  request that fails validation before any resolver/Postgres mutation is
  attempted (bad name, reserved name, duplicate, "not suspended", etc.)
  writes nothing — there is no partial state yet worth recording. Once a
  handler has started mutating external state (minting at the resolver,
  writing a `.creds` file, a `Store` write), every further error on that
  request writes a `failed` row with the failing step and error message in
  `metadata`, directly documenting the partial-failure inconsistencies gap
  (open gap #4 from the same review) as it happens rather than only in a log
  line. See `ARCHITECTURE-ACCOUNTS.md` § "Audit trail" for the full sequence
  diagrams.
- **BR-AC12 (2026-08-03):** A tenant's JetStream resource limits may be
  updated at runtime via `POST /api/accounts/{name}/jslimits` with a body of
  `{jsMaxMem, jsMaxFile, jsMaxStreams, jsMaxConsumers}`. All four fields are
  required; all must be non-negative (negative values are rejected with
  `400 Bad Request`). An unknown account name is rejected with `404 Not Found`.
  No status gate — limits may be updated whether the account is `active` or
  `suspended`. The handler re-mints the account JWT with the new limits (via
  `Provisioner.UpdateAccountLimits`, which uses `newAccountClaims` + a unique
  `jslimits-<nanoseconds>` tag to defeat the resolver's deterministic-JWT
  no-op detection, + `$SYS.REQ.CLAIMS.UPDATE`), then persists the new values
  to Postgres via `Store.SetJSLimits`. If the account has no signing key on
  record (a never-reactivated seeded account), one is established on the fly
  exactly as BR-AC04 does. After both the resolver push and the Postgres
  persist succeed, this service publishes
  `notify.accounts.account.jslimits_updated` (same context-free subject
  family and `Handlers.NotifyNC` best-effort/nil-safe contract as
  BR-AC08–AC10). An audit row is written (BR-AC11 mechanic, `AuditActionJSLimitsUpdated`)
  with `metadata` carrying both `previous` and `requested` limits on success,
  or the failing step and error on failure. This rule closes gap #5 from the
  2026-08-03 accounts architecture review — limits set at mint time with no
  update path — directly motivated by the `acme` account exhausting its
  `js_max_streams=10` ceiling after two contexts were provisioned (each
  context requiring 4 KV-bucket streams).

- **BR-AC13 (2026-08-03):** Reserved accounts (`PLATFORM`, `SYS`, matched
  case-insensitively) can never be suspended. `POST /api/accounts/{name}/suspend`
  returns `409 Conflict` immediately — before any revocation or status update —
  when `name` matches a reserved account name. This applies regardless of whether
  a database row for that name exists; the guard fires on the name alone.
  Motivation: suspending `PLATFORM` would sever shipping-service's permanent
  JetStream connection and all cross-tenant infrastructure, leaving every tenant
  unreachable. Enforced in `accounts/handler.go`'s `suspendAccount` via the
  same `reservedAccountNames` map used by BR-AC06.

The lifecycle rules (BR-AC01, BR-AC03, BR-AC04, BR-AC06, BR-AC07, BR-AC13) are enforced in
`accounts/handler.go`'s `createAccount`/`suspendAccount`/`reactivateAccount`;
the JWT mechanics behind BR-AC02/BR-AC03/BR-AC04 are in
`accounts/provisioner.go`'s `CreateAccount`/`DeleteAccount`/
`ReactivateAccount`; BR-AC08 is `accounts/handler.go`'s
`publishAccountCreated`, called from `createAccount`; BR-AC09 is the same
file's `publishAccountSuspended`, called from `suspendAccount`; BR-AC10 is
`publishAccountReactivated`, called from `reactivateAccount`; BR-AC12 is
`updateJSLimits` and `publishAccountJSLimitsUpdated`, with `Provisioner.UpdateAccountLimits`
and `Store.SetJSLimits`. All four notify publishers share one nil-safe
`publishAccountEvent` helper. BR-AC11 is `accounts/audit.go`'s
`AuditLog` type (`Record`/`ListByAccount`, backed by the
`accounts.audit_events` table created in `store.go`'s `Migrate`), called from
all handlers via the shared `recordAudit`/`auditActor` helpers in
`handler.go`. Ginkgo coverage:
`provisioner_test.go` exercises the JWT
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
with connectable creds and a persisted signing key), plus BR-AC08's own two
specs (a rejected create publishes nothing; a successful create publishes
the tenant's name, and a create with a nil `NotifyNC` still succeeds) and
BR-AC09's mirrored two specs (a suspend of an unknown account publishes
nothing; a successful suspend publishes the tenant's name, and a suspend
with a nil `NotifyNC` still succeeds), plus BR-AC10's two (a failed
reactivation publishes nothing; a successful one publishes the tenant's name
**and** has already written the fresh `.creds` file by the time the event is
observable — the ordering its consumer depends on; and a reactivation with a
nil `NotifyNC` still succeeds), plus BR-AC11's two (a create → suspend →
reactivate sequence with a caller-supplied `X-Actor` header writes three
rows — one per action, newest first, each `success`, each carrying that
actor and a non-empty source IP; and severing the provisioner's NATS
connection mid-suspend writes a `failed` row naming `"revoke account"` as
the step, alongside the earlier successful create's own row), plus BR-AC12's
five (successful update reflected in GET response; negative-value rejection;
404 for unknown account; notify event fired with the tenant's name; audit row
written with `previous`/`requested` metadata and `AuditActionJSLimitsUpdated`),
plus BR-AC13's four (PLATFORM/platform/Platform all return 409; a real seeded
`platform` row's status is unchanged after the rejected request). The
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

### BR-AC14 (Phase 21) — Tenant account claims import only the declared PLATFORM cross-cutting contract

Every tenant account imports the four context-free local refdata service
subjects (`refdata.item.get.v1`, `refdata.type.list.v1`,
`refdata.item.get-versioned.v1`, and `refdata.locales.list.v1`), the fixed
`rpc._platform.refdata.context.list.v1` service, and the account-lifecycle
and refdata-change streams from PLATFORM. Each remapped service import names
the tenant's human-readable account name (e.g. `acme`) in the remote
`rpc.{tenantName}.refdata.*.v1` subject — readable in logs, traces, and the
admin UI's live subject view, rather than an opaque public key. Security is
still operator-enforced, not client-supplied: the import itself lives inside
an operator-signed account JWT, so a tenant cannot rewrite its own import to
substitute another tenant's name — doing so would require re-signing the
claim with the operator's private key, which the tenant never holds. Whenever
accounts-service re-signs a claim (startup signing-key establishment,
reactivate, or limits update), it preserves the prior JWT's `Exports` and
`Imports`; an account JWT update replaces the full claim and dropping either
would silently sever this contract.

- **Enforced in:** `nats/bootstrap-operator.sh`; `accounts/provisioner.go`.
- **Test:** `provisioner_claims_test.go`; shipping
  `internal/natsaccounts/isolation_test.go` import/isolation specs.
