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

  > **Follow-on to Phase 30 (2026-08-17):** the Admin UI's Streams and KV
  > Buckets panels (`observability-service`, `JetStreamPanel.vue` and
  > `KvInspector.vue`) surface every known account's status (`GET
  > /api/accounts`'s `status` field, via `AccountsClient.TenantStatuses`) as
  > a dimmed dot on that account's group header — replacing an earlier,
  > unrelated client-side signal (Streams compared the group's name against
  > the browser's own connected tenant; KV Buckets just hardcoded "gray iff
  > PLATFORM"). Because a suspended account's connections are force-evicted
  > (this entry's own correction above), its cross-account `$JS.API` access
  > via BR-AC32's `monitor.{tenant}.js` remap always fails "no responders" —
  > `listStreams`/`listKVBuckets` (`streams.go`/`kv.go`) treat that as
  > per-account and non-fatal (log + skip that account's rows) rather than
  > aborting the whole response the way they used to, and both handlers
  > additionally report every account (name + status) in a dedicated
  > `accounts` field independent of whether any of its streams/buckets were
  > listable, so a suspended tenant still gets a (empty, dimmed) group in
  > both panels instead of vanishing entirely. **Test:**
  > `TestTenantStatusesReturnsStatusPerTenantExcludingPlatformAndSys`,
  > `TestIntrospectableAccountsTagsPlatformAndTenantStatus`,
  > `TestListStreamsSkipsUnreachableAccountRatherThanFailingWholeResponse`,
  > `TestListKVBucketsSkipsUnreachableAccountRatherThanFailingWholeResponse`
  > (`observability-service/observability/internal/rest/`).
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
  overwriting their JWTs. **Amended 2026-08-06 (BR-AC19):** seeded rows now
  take their signing key seed from the one `bootstrap-operator.sh` exported
  for that account, so the account's identity is stable across a
  `docker compose down -v`. This entry previously said seeded rows have no
  signing key seed on record until BR-AC04's reactivation establishes one —
  that stopped being true when startup establishment (Phase 15c's
  `ensureSigningKey`) was added, and minting a *fresh random* key on each
  wiped boot is precisely the defect BR-AC19 fixes.
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

  > **Phase 28e amendment (BR-037):** all four lifecycle publishers (BR-AC08/
  > BR-AC09/BR-AC10, plus `publishAccountJSLimitsUpdated`, BR-AC12) now go
  > through `publishAccountEvent`, which mints a `natstrace` outbound span
  > (continuing whatever span `natstrace.HTTPMiddleware` attached to the
  > originating HTTP request's `ctx`, or a fresh root span if none) and
  > attaches its `Traceparent` header to the `notify.accounts.*` publish —
  > this is what lets a trace initiated by an Admin UI create/suspend/
  > reactivate/jslimits-update action continue into the reactive-provisioning
  > work it causes on shipping-service's (and pricing-/trading-partner-
  > service's) side. Also, per BR-AC30, this happens on the PLATFORM
  > (`NotifyNC`) connection — the same one already required for the notify
  > itself — never a tenant connection.
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
  `js_max_streams=10` ceiling after two contexts were provisioned. Prior to
  Phase 20b, each context required 4 dedicated KV-bucket streams; Phase 20b
  collapsed per-context buckets to one shared bucket per tenant role so
  adding a context no longer consumes additional streams, but the limit
  enforcement remains relevant for the streams each tenant does use.

- **BR-AC12b (2026-08-03):** Live JetStream usage for all tenant accounts is
  readable via `GET /api/accounts/usage`. The response is an array of
  per-account objects, each carrying four `{used, limit}` counters: `streams`,
  `consumers`, `mem`, and `file`. Live values are fetched from the NATS
  server's monitoring endpoint (`/jsz?accounts=true&account-details=true&streams=true`),
  keyed by account public key; Postgres-stored limits are joined in from the
  same `accounts` table that BR-AC12 writes. Accounts present in Postgres but
  not yet seen by the NATS server (zero JetStream activity) appear with
  `used = 0`. Consumer count is reported as 0 — the `/jsz` endpoint only
  exposes consumers when `?consumers=true` is set (a separate expensive query
  omitted for performance). When `NATS_MONITOR_URL` is not configured, the
  handler returns `503 Service Unavailable`. The endpoint requires Basic Auth
  (same shared secret as other accounts-service admin endpoints).
  - **Enforced in:** `accounts/jsusage.go` (`UsageFetcher.FetchAll`);
    `accounts/handler.go` (`listJSUsage`).
  - **UI surface:** the Admin UI Accounts panel shows a `Streams` column
    (used / limit, color-coded green / amber / red at 0–79% / 80–99% / ≥100%)
    and an Edit Limits dialog wired to BR-AC12's update endpoint.

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
  WorkOS. **Partially realized (2026-08-07):** the 15–30 min TTL target is now
  enforced and configurable for the browser/admin credentials — see
  [[BR-AC20]] (durable, Admin-UI-configurable TTL, default 15 min) and
  [[BR-AC21]] (the hard 15–30 min envelope). The refresh-token half remains
  unbuilt; browser tabs reconnect on expiry rather than renewing in place.
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
would silently sever this contract. BR-AC19 extends the same treatment to
`SigningKeys`, which this rule did not cover and which was in fact being
dropped on every re-sign.

- **Enforced in:** `nats/bootstrap-operator.sh`; `accounts/provisioner.go`.
- **Test:** `provisioner_claims_test.go`; shipping
  `internal/natsaccounts/isolation_test.go` import/isolation specs.

## BR-AC15 — Business unit registration

Every business unit has two fields, not one (Phase 22b, BR-AC26): a free-text
English **name** (`Pacific Fleet`) and an immutable, subject-safe **context**
slug (`acme-pacific-fleet`) — the token refdata-service's context tree, the
NATS subject taxonomy, and KV key prefixes all actually use. When a create
request omits `context`, it is derived from `name` (BR-AC26). Registration
persists a row in `accounts.business_units (account_id, name, context,
visible, is_default)` and fires a best-effort call to `POST
/api/refdata/admin/contexts` — sending `name` and `context` as the distinct
values they are — to register the context there as well. A context is
rejected at the API level unless it passes `ValidateContext` (BR-AC27); this
implicitly also blocks a leading `_`, since that character never appears in
anything `ValidateContext` accepts.

- **Enforced in:** `accounts/handler.go` (`createBusinessUnit`);
  `accounts/slug.go` (`ValidateContext`, `DeriveContext`).
- **Test:** `accounts/slug_test.go`; `accounts/handler_test.go`
  (`Describe("Business units (BR-AC26/27/28/29)")`).

## BR-AC16 — Auto-create a default business unit row

Every newly created account receives its own default business unit row
automatically — name `Default`, context `{tenant}-default`, `is_default: true`
— created immediately after `Store.Insert` persists the account. This
guarantees that `ListBusinessUnits` always returns at least one entry even
before any real business units are registered. The auto-creation is
best-effort: a failure is logged but does not fail the create request, since
the account is already fully minted at that point.

Before 2026-08-13 (Phase 22) this was a single literal `_default_bu` row
shared by every account with none of its own. That collapsed two tenants'
data onto the same refdata-service `(context, type_key, code)` rows the
moment both resolved to it, which is the defect Phase 22b's per-tenant
default (BR-AC28) exists to remove.

- **Enforced in:** `accounts/handler.go` (`createAccount` — auto-inserts
  the default row after the `Store.Get` reload, using `DefaultContext` from
  `accounts/slug.go`). Also replayed by `cmd/main.go`'s
  `seedPreexistingAccounts` for every non-reserved seeded account (acme,
  globex): that path inserts account rows directly via `Store.SeedIfMissing`
  rather than going through `createAccount`, so without this it would seed
  accounts that never satisfy the "always at least one BU" invariant — which
  is exactly what happened before 2026-08-13 (globex showed "No business
  units yet" while a freshly-registered account always showed the
  placeholder).
- **Test:** `accounts/handler_test.go` (BR-AC16/BR-AC28 spec — auto-created
  default carries its own tenant-prefixed slug, never the retired shared
  literal).

## BR-AC17 — Business unit visibility toggle semantics

Setting a business unit's `visible` flag to `false` hides it from the context
selector in the shipping and refdata UIs (`ListByTenant` filters by
`visible = true`). It does not delete the BU row or any refdata items seeded
under that context — those remain queryable directly. The Admin UI prompts
the operator to hide the account's default business unit when they register
their first real BU (since real BUs make the placeholder redundant), but does
not do so automatically because the default may already hold demo or
migration data. Visibility can be toggled back at any time via `PATCH
/api/accounts/{name}/business-units/{context}`.

- **Enforced in:** `accounts/handler.go` (`updateBusinessUnit`);
  `refdata/internal/postgres/context_repository.go` (`ListByTenant`).
  `cmd/main.go`'s `seedDemoBusinessUnits` replays this same hide step for
  acme immediately after seeding its two real demo BUs, so acme's Business
  Units table matches what a real operator would see after registering an
  account and adding its first real BU — an always-visible reserved row
  would otherwise never occur through the normal create-then-add-BU flow.
- **Test:** `accounts/handler_test.go` (BR-AC28 spec covers the toggle
  surviving on the default row specifically).

## BR-AC18 — Admin token minting is isolated from the tenant lifecycle

`MintAdminToken` mints a NATS user JWT under `PLATFORM` directly from its own
signing key material (established at startup for every seeded account by
`cmd/main.go`'s `ensureSigningKey`, same mechanism as a tenant's), independent
of `accounts.Store`'s `Status`/`SigningKeySeed`/reactivation state machine —
that machine governs tenant accounts only, and PLATFORM has no
suspend/reactivate lifecycle to gate on. `GET /api/auth/adminConnectInfo`
looks up the fixed `"platform"` row directly rather than going through
`connectInfo`'s tenant-shaped, `Status`-gated lookup. The minted JWT carries
subscribe-only permissions (no `$JS.API.>`, `$KV.>`, or publish grants at
all — `Pub.Deny` is set to `>` explicitly, since an unset Allow list means
"allow everything" in NATS permission semantics, not "allow nothing") scoped
to `notify.accounts.account.>`, the REFDATA `notify.*` subject Phase 23 adds
(`notify._platform.refdata.>`), and `notify._platform.kv.traces.>` (Phase
28g, the trace waterfall/messages panel). `notify._platform.rpctrace.>`
(Phase 23) was retired in Phase 28g along with the RPCTRACE stream and its
notify bridge — nothing publishes there anymore.
This is the Admin UI's PLATFORM-account browser connection
(`frontend/admin/src/nats/usePlatformConnection.js`) — opened once at boot,
never reconnected on tenant/BU switch, and the one connection the topbar's
connection indicator is driven by for exactly that reason.

- **Enforced in:** `auth/token.go` (`MintAdminToken`); `auth/handler.go`
  (`adminConnectInfo`).
- **Test:** `auth/token_test.go` (`MintAdminToken` — asserts the exact
  `Sub.Allow` set, `Pub.Allow` empty, `Pub.Deny` contains `>`); `auth/
  handler_test.go` (`GET /api/auth/adminConnectInfo` — 200 with a signing key
  on record, 404 when PLATFORM isn't seeded, 409 with no signing key).

## BR-AC19 (2026-08-06) — Seeded accounts adopt a stable signing key, and a claim re-sign never drops one

Two halves of one invariant: **an account's signing key is stable across a
`docker compose down -v`, and re-signing an account claim never invalidates a
credential that was valid a moment earlier.**

`bootstrap-operator.sh` exports each seeded account's signing key seed to
`nats/keys/{platform,acme,globex}-signing-key.nk`, alongside the operator's
own. At startup `accounts-service` adopts that seed for any seeded account
with none on record, after verifying its public key is listed in that
account's resolver JWT — a seed the resolver doesn't trust is a startup
error, not a warning, since persisting it would mint user JWTs the server
rejects. When no seed is exported (a stack bootstrapped before this rule),
the previous behaviour stands: mint one and re-sign the claim.

Separately, every claim re-sign — startup key establishment, reactivate, or
limits update — **accumulates** signing keys rather than replacing the list,
exactly as BR-AC14 already requires for `Exports`/`Imports` and for the same
reason: an account JWT update replaces the whole claim. Re-signing is not a
revocation operation; revoking an account's credentials is BR-AC03's suspend
(`$SYS.REQ.CLAIMS.DELETE`), which removes the account JWT outright.

**This closes a real incident (2026-08-06).** `globex` could not connect at
all — `nats: authorization violation` on every `shipping-service` tenant
connection — after nothing more than `docker compose down -v && docker
compose up`. The chain: `bootstrap-operator.sh` deletes its `nsc` keystore,
so the seeded accounts' signing keys were never exported and their Postgres
rows started with none (BR-AC05). `ensureSigningKey` therefore minted a
**fresh random key on every boot with an empty accounts Postgres**, and the
re-sign replaced the account's whole signing key list. That was harmless for
as long as every shipped `.creds` file was signed by its account's *identity*
key — which is why `acme` never broke, and why the assumption went unnoticed
in `ensureSigningKey`'s own doc comment. But BR-AC04's reactivation had
rewritten `globex.creds` as a **signing-key**-signed credential
(`CreateUser` always signs with the account signing key, and the shared
`./nats/creds` mount is writable by `accounts-service`), and that file was
committed. From then on each wiped boot dropped the one key `globex.creds`
was signed by, with a different random key each time — which is why it
presented as intermittent. The repo was inconsistent at rest too:
`nats/resolver/GLOBEX.jwt` listed one signing key while the committed
`globex.creds` was signed by another, so even a fresh clone would fail for
`globex`.

- **Enforced in:** `nats/bootstrap-operator.sh` (seed export);
  `accounts/signingkeys.go` (`ResolveSeededSigningKey` — verification);
  `cmd/main.go` (`seedPreexistingAccounts`, `ensureSigningKey` — adoption);
  `accounts/provisioner.go` (`newAccountClaims` — signing key accumulation).
- **Test:** `accounts/signingkeys_test.go` (adoption, lowercase lookup,
  absent-seed fallback, and each rejection path);
  `accounts/provisioner_claims_test.go`
  (`TestNewAccountClaimsPreservesPriorSigningKeys`).
- **Operational note:** adopting the exported seeds requires regenerating the
  trust chain once (`nats/bootstrap-operator.sh --force`, then
  `docker compose down -v && docker compose up --build`), because the
  existing accounts' signing keys were never exported and cannot be
  recovered — `nsc`'s keystore holding them was deleted at bootstrap time.

## BR-AC20 (2026-08-07) — Browser/admin JWT expiry TTL is a durable, configurable system setting

The TTL stamped on the short-lived NATS user JWTs that `auth.MintBrowserToken`
and `auth.MintAdminToken` issue to browser WebSocket connections is a
**durable, platform-global system setting**, not a compile-time constant. It
is stored in a singleton `accounts.system_config` row (one row, guaranteed by
a `BOOLEAN PRIMARY KEY … CHECK (singleton)`), edited from the Admin UI's
**System → Settings** screen, and read fresh on **every** mint so a change
takes effect on the next browser (re)connect without a service restart.

Two values are configurable: the **TTL value** actually issued
(`token_ttl_minutes`) and the **operational range** it must sit within
(`token_ttl_min_minutes`, `token_ttl_max_minutes`). Defaults, seeded by
`Migrate` and returned by `DefaultTokenTTLConfig`: **value 15 min, range
15–30 min.** A config read failure at mint time falls back to that default
rather than failing the connect — issuing a short-lived credential on the
default TTL is strictly safer than refusing to connect the browser.

This supersedes the previous behaviour (a hardcoded `const tokenTTL =
5 * time.Minute` in `auth/token.go`) and is the POC's first concrete step
toward BR-UA03's 15–30 min target — the refresh-token half of BR-UA03/UA04
is still not built, so a tab open past the TTL still reconnects rather than
renewing in place (see `ARCHITECTURE-ACCOUNTS.md` § "Runtime — browser JWT
expiry & reconnect").

- **Where:** `accounts/config.go` (`TokenTTLConfig`, `DefaultTokenTTLConfig`,
  `Store.GetTokenTTLConfig`/`SetTokenTTLConfig`); `accounts/store.go`
  (`Migrate` — `system_config` table + seed); `accounts/handler.go`
  (`GET`/`PUT /api/accounts/system-config`, BasicAuth-gated);
  `auth/handler.go` (`tokenTTL` — read per mint); `auth/token.go`
  (`Mint*` now take a `ttl` argument).
- **Test:** `accounts/config_test.go` (default + duration, always runs — no
  Postgres); `accounts/handler_test.go` (GET default, PUT round-trip, auth
  required); `auth/handler_test.go` (end-to-end: `connectInfo`'s minted JWT
  expiry reflects the configured TTL); `auth/token_test.go` (the `ttl`
  argument flows through to `Expires`).

## BR-AC21 (2026-08-07) — TTL value and range are bounded by the hard 15–30 minute envelope

`TokenTTLConfig.Validate` enforces, and the `PUT /api/accounts/system-config`
handler rejects with **HTTP 400** any update that violates:

1. the configured range must lie within the **hard `[MinTTLMinutes,
   MaxTTLMinutes]` = `[15, 30]` envelope** — these are code constants because
   they *are* BR-UA03's rule ("all JWT expiry must be between 15 and 30
   minutes"); widening the envelope is a change to the rule and must go
   through code + a spec, never a runtime toggle;
2. the range minimum must not exceed the maximum; and
3. the issued value must fall within the configured range.

Because a valid range is envelope-bounded, a valid value is transitively
guaranteed to sit inside the 15–30 minute window. The configurable range
therefore only ever lets an operator **narrow** the window within the
envelope, never escape it. The `GET`/`PUT` responses expose the envelope
(`envelopeMinMinutes`/`envelopeMaxMinutes`) read-only so the Admin UI can
constrain its editors without hardcoding the bounds.

- **Where:** `accounts/config.go` (`TokenTTLConfig.Validate`, `MinTTLMinutes`,
  `MaxTTLMinutes`); `accounts/handler.go` (`updateSystemConfig`).
- **Test:** `accounts/config_test.go` (`DescribeTable` over the envelope,
  range-inversion, and value-out-of-range cases); `accounts/handler_test.go`
  (400 for a range outside the envelope, 400 for a value outside the range).

## BR-AC22 (2026-08-12) — A declared import is only "healthy" if a matching export actually exists

`GET /api/accounts/topology` used to report every account's declared
imports as if they were live traffic, with no check that anything on the
exporter's side actually satisfies them. An import is only satisfiable when
the account it names (`Import.Account`) declares an export of the same type
(`service`/`stream`) whose subject **contains** the import's subject —
`jwt.Subject.IsContainedIn`, the same wildcard-aware containment nsc itself
uses to validate an import against an export (mirroring, with an added type
check, `jwt.Exports.HasExportContainingSubject`). An import with no such
export is reported with status `no-export`, never silently dropped —
omitting it is exactly what made this class of misconfiguration invisible.
A known exporter whose own claims lookup failed is treated the same as
`no-export` rather than invented as a third "can't tell" state, since either
way no export can be confirmed.

- **Where:** `accounts/topology.go` (`matchExport`, `importStatus`,
  `listTopology`).
- **Test:** `accounts/topology_test.go` (`TestMatchExport`,
  `TestImportStatus` — table-driven, including the real wildcarded PLATFORM
  export shapes from `nats/bootstrap-operator.sh`); `accounts/handler_test.go`
  (BR-AC22/BR-AC23 spec — matched vs. no-export over a live pushed export).

## BR-AC23 — An export nobody imports is reported as unconsumed, separately from the edge list

Alongside `edges`, the topology response carries `unconsumedExports`: every
export, on any known account, that no import (from any known account)
currently matches. This is lower severity than an unmatched import — unused
capability, not breakage — so it's reported as its own list rather than as a
graph edge; an export has no importer endpoint to draw a line to. An export
is "consumed" the moment any import matches it by subject+type, independent
of whether that import also satisfies BR-AC24's token requirement.

- **Where:** `accounts/topology.go` (`listTopology`'s second pass over
  `claims.Exports`, gated on the `consumed` map built while walking imports).
- **Test:** `accounts/handler_test.go` (BR-AC22/BR-AC23 spec — an export with
  no importer appears in `unconsumedExports`; a consumed export does not).

## BR-AC24 — An import matching an export that requires a token, without one, is reported distinctly

An export may set `TokenReq`, meaning an importer must present an activation
token to actually use it. An import whose subject+type matches such an
export but carries no `Token` is reported as `token-required` — distinct
from both `matched` (usable as declared) and `no-export` (no contract exists
at all): here a contract exists but isn't actually usable as declared, which
is a different failure to diagnose than either of those.

- **Where:** `accounts/topology.go` (`importStatus`).
- **Test:** `accounts/topology_test.go` (`TestImportStatus`);
  `accounts/handler_test.go` (BR-AC24 spec).

## BR-AC25 — An import naming an account outside this deployment is reported as unknown-account, never dropped

If `Import.Account` isn't a public key any known account (`Store.List`)
holds, the edge is still reported — with `status: "unknown-account"` and
`fromAccount` set to the raw public key rather than a resolved name — instead
of being silently omitted. This was already accounts-service's stated intent
(`imp.Account` falling back to the raw pubkey "rather than drop the edge")
before the Admin UI's Topology panel had anywhere to render it; BR-AC25 is
what makes that intent an enforced, tested contract.

- **Where:** `accounts/topology.go` (`importStatus`, `listTopology`).
- **Test:** `accounts/topology_test.go` (`TestImportStatus`);
  `accounts/handler_test.go` (BR-AC25 spec — an import naming a freshly
  generated, never-registered account pubkey).

## BR-AC26 (2026-08-13) — A business unit's name and its context slug are distinct, and the slug is immutable once created

A business unit carries two independently-mutable-or-not fields: `name`, a
free-text English label an operator may rename at will, and `context`, the
subject-safe slug refdata-service and every NATS subject/KV-key actually use.
Before this rule (Phase 22) they were one field — an operator naming a unit
had to type its eventual subject token directly, and the Admin UI displayed
that token as if it were the label.

`context` is immutable from the moment a business unit is created. This is
not a preference but a hard constraint: none of refdata-service's data
tables (`dictionary_items`, `dictionary_localizations`, `dictionary_references`,
`dictionary_locales`) carry a foreign key back to `refdata.contexts` — they
hold the context value as a bare column. Renaming a slug would silently
orphan every row keyed under the old value, plus the `refdata-{context}` KV
bucket, the versioned corpus buckets, and the already-immutable
`evt.{context}.…` JetStream history recorded under it. `PATCH
/api/accounts/{name}/business-units/{context}` accepts a `name` field and has
no `context` field at all — there is no code path that could rename a slug,
by construction, not by a check that could be bypassed.

When a create request omits `context`, `DeriveContext(tenant, name)` proposes
one: the tenant name, then the slugified display name, skipping the tenant
prefix when the name already leads with it (so "Acme Pacific Fleet" under
tenant `acme` still derives `acme-pacific-fleet`, not
`acme-acme-pacific-fleet`). The Admin UI sends the derived value back
explicitly rather than relying on server-side derivation, so the operator
sees and can edit it before committing to something that can never change.

- **Enforced in:** `accounts/slug.go` (`DeriveContext`, `Slugify`);
  `accounts/handler.go` (`createBusinessUnit` — derives when omitted;
  `updateBusinessUnit` — `name` is the only mutable field a `PATCH` body can
  carry); `frontend/admin/src/components/AccountsPanel.vue` (Add Business
  Unit dialog — Context auto-follows Name until hand-edited, then stops).
- **Test:** `accounts/slug_test.go` (`TestDeriveContext`);
  `accounts/handler_test.go` (BR-AC26 specs — derived vs. explicit context,
  rename preserves the slug unchanged).

## BR-AC27 (2026-08-13) — A context slug must be a legal subject token and is globally unique, not just unique per account

`ValidateContext` rejects anything that isn't lowercase letters, digits and
hyphens, starting and ending alphanumeric, at most 48 characters
(`MaxContextLen`) — stricter than refdata-service's own `ValidateSubjectToken`
(`^[A-Za-z0-9_-]+$`, BR-D22) in one deliberate way: no uppercase. NATS subject
tokens are case-sensitive, so `Acme` and `acme` address two different
subjects and two different KV buckets while reading as the same business unit
to a human — exactly the mismatch that surfaces as "the dropdown is populated
but every lookup returns nothing." Validation runs in accounts-service at the
point of write, not left to refdata-service's own check: the call into
refdata-service is best-effort, so before this rule a business unit named
`west coast` persisted locally and then failed *silently* downstream, leaving
a row that could never resolve to anything.

`context` is also unique across every account, not merely within one:
`accounts.business_units` carries a global `UNIQUE (context)` index, even
though `UNIQUE (account_id, name)` (display names) stays per-account.
refdata-service's own `contexts.context` is a primary key, and its `Register`
upserts on conflict — so before this constraint, two accounts registering the
same slug would let the second silently overwrite the first's context row,
including its `name` and `tenant` ownership metadata, with no error surfaced
to anyone. `POST /api/accounts/{name}/business-units` now returns `409` on a
global slug collision.

- **Enforced in:** `accounts/slug.go` (`ValidateContext`); `accounts/store.go`
  (`business_units_context_key` unique index); `accounts/handler.go`
  (`createBusinessUnit` — 400 on an invalid slug, 409 on a collision).
- **Test:** `accounts/slug_test.go` (`TestValidateContext`,
  `TestValidateContextLength`); `accounts/handler_test.go` (BR-AC27 specs —
  rejects an illegal slug; rejects a cross-account slug collision).

## BR-AC28 (2026-08-13) — Every account's default business unit is its own, tenant-owned, and its identity is readonly

Each account's auto-created default business unit (BR-AC16) has context
`{tenant}-default` — an ordinary tenant-owned slug, deliberately *not* the
Phase 22 shared literal `_default_bu`. Two tenants can no longer collide by
both resolving to the same context: `acme`'s default is `acme-default`,
`globex`'s is `globex-default`, each satisfying `ValidateContext` (BR-AC27)
with no special-case exception.

The default is identified by an explicit `is_default BOOLEAN` column, never
by comparing a name or slug against a reserved literal — Phase 22's code had
roughly seven places (three in accounts-service, four across the frontends)
that string-matched `_default_bu` directly, every one of which a per-tenant
slug would have silently broken.

"Readonly" covers identity only: a default business unit cannot be renamed
(`updateBusinessUnit` returns `409` for any `name` change once
`bu.IsDefault` is true) and there is no endpoint to create one directly or
delete any business unit at all. `visible` stays toggleable — BR-AC17's
hide-once-a-real-BU-exists flow is exactly this toggle, and disabling it
there would break that flow.

`ListBusinessUnits` sorts the default first, ahead of every real business
unit by name, rather than wherever it falls alphabetically — it is the one
row guaranteed to exist and reads as the list's anchor.

- **Enforced in:** `accounts/slug.go` (`DefaultContext`); `accounts/store.go`
  (`ListBusinessUnits`'s `ORDER BY bu.is_default DESC, bu.name`);
  `accounts/handler.go` (`updateBusinessUnit` — rejects a rename when
  `IsDefault`).
- **Test:** `accounts/slug_test.go` (`TestDefaultContext` — two tenants never
  collapse to the same value); `accounts/handler_test.go` (BR-AC28 specs —
  rename rejected, visibility toggle still succeeds, list ordering).

## BR-AC29 (2026-08-13) — A tenant's default business unit context is provisioned to inherit the platform template, gated on that template actually being ready

Registering an account's default business unit's *context* in
refdata-service is more than one call: `RegisterContext` (parented to
`_default_bu`, the platform-owned template — see BUSINESS_RULES-REFDATA.md's
amended BR-D38), `AddLocale` for en/es/af-za (locales are **not** covered by
corpus inheritance — they sit on refdata-service's flat, non-inheriting read
path, so a context with none of its own has no effective default locale even
though its items inherit correctly), then `CreateDraft` + `Publish` so the new
context's corpus actually contains `_default_bu`'s (and therefore
`_platform`'s) inherited items rather than nothing.

Every step is gated on `RefdataClient.WaitForPublishedAncestor(ctx,
"_default_bu")` succeeding first. This has to tolerate two distinct failure
modes as the same "not ready" signal: refdata-service's `CreateDraft` silently
skips an ancestor with no published corpus — a draft created one instant too
early inherits nothing and still reports success — and, since
accounts-service and refdata-service are independent containers with no
startup ordering guarantee between them, the very first call in the sequence
is just as likely to hit "connection refused" as "context not found yet" on a
cold `docker compose up`. Both are retried identically, up to 30 attempts at
one-second intervals.

This runs off the HTTP request path for a live `createAccount` call (a
detached goroutine with its own 45s timeout) so a slow or cold
refdata-service never turns tenant registration itself into a slow request —
but runs synchronously during `cmd/main.go`'s own startup seeding, where
blocking is expected and acceptable.

- **Enforced in:** `accounts/refdata.go` (`RefdataClient.ProvisionDefaultContext`,
  `WaitForPublishedAncestor`); `accounts/handler.go` (`createAccount` — fires
  provisioning in a detached goroutine); `cmd/main.go`
  (`seedPreexistingAccounts` — same provisioning, run synchronously at
  startup).
- **Test:** integration against the full stack — verified live
  (`docker compose down -v && up --build`) that `acme-default` and
  `globex-default` both register with `parent: _default_bu` and their own
  locales, and that the versioned corpus endpoint returns every `_platform`
  currency item under `acme-default` with `sourceContext: "_platform"`.
  Not yet covered by an automated Go test — the accounts-service test suite
  doesn't stand up a live refdata-service, so `RefdataURL` is unset there and
  every call in this rule is a no-op by `configured()`'s design (BR-AC26's
  "best-effort" contract). A dedicated integration test would need its own
  refdata-service fixture.

## BR-AC30 (Phase 28) — Minted account JWTs carry `allow_trace: true` on service exports and stream imports, plus a per-tenant `obs.trace.>` stream export imported into PLATFORM

Without an explicit grant, distributed tracing (`BUSINESS_RULES-SHIPPING.md`'s
BR-036/BR-037) stops dead at the account boundary: a tenant account's
`rpc.*` call into PLATFORM's `refdata-service` crosses accounts today only
because specific service exports/imports are already declared for `rpc.*`
and `api.*` — `obs.trace.*` needs the identical treatment or a
cross-account trace simply has no way to reach the PLATFORM-side trace store
(BR-036's KV/JetStream projection, Phase 28f) at all. Every tenant account
JWT minted by `MintTenantAccount` therefore gains `allow_trace: true`
alongside its existing service exports and stream imports, and a per-tenant
`obs.trace.>` stream export is declared and imported into PLATFORM the same
way `rpc.*`'s stream import already is. This is additive to every existing
export/import declaration — no existing `Sub.Allow`/`Pub.Allow` entry is
narrowed or removed.

**Critically, no browser-facing JWT gains this grant.** `MintBrowserToken`
(the credential seafreight/admin tenant connections use) is unaffected —
`obs.trace.>` is a service-to-service and PLATFORM-only concern
(BR-036), so a tenant browser credential must never carry `Sub.Allow` for
it, the same way it is never granted `rpc.>` at all.

- **Enforced in:** `accounts/jwt.go`'s `MintTenantAccount` (adds
  `allow_trace: true` and the `obs.trace.>` stream export/import
  declarations); `accounts/jwt.go`'s `MintBrowserToken` (unchanged — no new
  grant added here, asserted by omission in the test below).
- **Test:** a minted tenant account JWT decodes with `allow_trace: true` and
  an `obs.trace.>` stream export/import present; a minted browser JWT's
  `Sub.Allow` list does **not** contain `obs.trace.>` (mirrors the existing
  `rpc.>`-exclusion assertion for browser tokens).

  > **Phase 28f amendment:** this grant is account-level (a NATS
  > Export/Import declaration on the account claims, resolved server-side —
  > not a user-JWT `Sub.Allow`/`Pub.Allow` permission entry the way `rpc.>`
  > is), so it lives in `accounts/provisioner.go`, not
  > `accounts/jwt.go`, and there is no `MintTenantAccount` function — the
  > actual entry point is `Provisioner.CreateAccount`. Concretely:
  > `newAccountClaims`'s cross-account branch adds a Stream **export** of
  > `obs.trace.>` to the new tenant's own claims via the new
  > `tenantExports()` helper (no `allow_trace` here — `jwt.Export.Validate`
  > rejects that flag on anything but a Service export, and this is a Stream
  > export); `CreateAccount` then calls the new `addPlatformTraceImport`,
  > which looks up PLATFORM's own current claims (`LookupAccountClaims`),
  > idempotently appends a matching Stream **import** — `{Account:
  > <new tenant's pubkey>, Subject: "obs.trace.>", Type: Stream,
  > AllowTrace: true}` — skipping the append if PLATFORM already imports that
  > exact `(Account, Subject)` pair, and re-signs/re-pushes PLATFORM's claims
  > via the same `pushClaimsUpdate` ($SYS.REQ.CLAIMS.UPDATE) mechanism every
  > other claims mutation in this file uses. This is the one leg
  > `tenantImports`'s doc comment doesn't cover: every other declaration in
  > this file grants a tenant account access to something PLATFORM exports;
  > this is the reverse — PLATFORM importing a stream *from* each tenant —
  > which NATS's decentralized JWT model has no wildcard shorthand for, so it
  > has to be minted per tenant at `CreateAccount` time. `MintBrowserToken`'s
  > exclusion premise is unaffected: the browser-facing user JWT (`auth/
  > token.go`'s `MintBrowserToken`/`MintAdminToken`) never gains `obs.trace.>`
  > in its `Sub.Allow`/`Pub.Allow` lists, since this whole mechanism operates
  > one layer up, on the account claims those user JWTs are issued under.
  > Covered by `TestNewAccountClaimsAddsTenantImportsAndPreservesPriorCrossAccountWiring`
  > (unit, tenant-side export) and `TestAddPlatformTraceImportIsIdempotent`
  > (integration, against an embedded operator-mode NATS server — PLATFORM-side
  > import + idempotent re-push), both in `accounts/provisioner_claims_test.go`
  > / `accounts/provisioner_test.go`.

## BR-AC31 (Phase 30a) — Minted account JWTs carry a `$SRV.>` Service export back to PLATFORM, imported per tenant with a tenant-scoped local remap, so cross-account service discovery can reach every tenant account

Without this grant, `$SRV.PING`/`$SRV.INFO`/`$SRV.STATS` discovery — the
same `nats.go/micro` control protocol `nats micro stats` uses, and what
`observability-service`'s Services panel (Phase 30f) broadcasts to find
every registered service — stops dead at the account boundary exactly the
way distributed tracing did before BR-AC30: a tenant account's own
registered services (shipping/pricing/trading-partner's `browserrpc`
adapters) are invisible to a PLATFORM-only connection unless the tenant
explicitly exports its `$SRV.>` control subjects and PLATFORM imports them.
Every tenant account JWT minted by `CreateAccount` therefore gains a
`$SRV.>` **Service** export on its own claims, and PLATFORM's claims gain a
matching per-tenant **Service** import, remapped to a tenant-scoped local
subject (`monitor.{tenantName}.srv.>`) so a caller on PLATFORM can address
one tenant's discovery traffic without colliding with another's — the same
shape `tenantImports`'s `service()` helper already uses for the
PLATFORM-to-tenant refdata imports, just with exporter and importer
reversed. This is additive to every existing export/import declaration —
no existing grant is narrowed or removed.

**This is a Service export, not a Stream export like BR-AC30's
`obs.trace.>`, and it needs `ResponseType: Stream`, not the library default
of `Singleton`.** `$SRV.STATS` is a broadcast: every registered service
instance in the account replies independently to the same request. A
`Singleton` response type (`jwt.Export`'s documented "a service that sends a
single response only") is written for classic 1:1 request/reply — the
`rpc.*.refdata.*` exports use it correctly, since exactly one reply is
expected per request. `ResponseType: Stream` ("a service that will send
multiple responses") is what keeps the cross-account reply route open long
enough for more than one instance's reply to cross back; the exact
mechanics of a multi-repliers-to-one-broadcast pattern specifically (as
opposed to one responder streaming several messages) are unproven in this
codebase — no `$SRV` subject has ever crossed an account boundary here
before this rule — and are exercised for the first time in this rule's own
Ginkgo coverage plus Phase 30i's live verification, not assumed correct in
advance.

**Critically, no browser-facing JWT gains this grant**, the same premise as
BR-AC30: `MintBrowserToken`/`MintAdminToken` (`auth/token.go`) are
unaffected — `$SRV.>` cross-account discovery is a PLATFORM-only,
service-to-service concern, so a tenant browser credential must never carry
it.

- **Enforced in:** `accounts/provisioner.go`'s `tenantExports()` (adds the
  `$SRV.>` Service export, `ResponseType: jwt.ResponseTypeStream`, to every
  tenant's own claims — no `AllowTrace`, that flag is `obs.trace.>`'s alone)
  and a new `addPlatformMonitorImport` (PLATFORM-side, mirrors
  `addPlatformTraceImport`'s re-sign-via-`pushClaimsUpdate` mechanism and its
  idempotency check, but keyed on `(Account, Subject)` for a Service import
  carrying `LocalSubject: monitor.{tenantName}.srv.>` instead of a bare
  Stream import); called from `CreateAccount` alongside
  `addPlatformTraceImport`, gated on the same `platformPublicKey != ""`
  condition.
- **Test:** a freshly-minted tenant's own claims decode with a `$SRV.>`
  Service export whose `ResponseType` is `Stream`; PLATFORM's claims, after
  `CreateAccount`, decode with a matching per-tenant Service import
  (`Subject: "$SRV.>"`, `LocalSubject: "monitor.{tenantName}.srv.>"`),
  accumulating correctly across multiple tenants without disturbing each
  other's entries and without duplicating on a retried call. Covered by
  `TestNewAccountClaimsAddsTenantMonitorExport` (unit, tenant-side export) and
  the "re-signs PLATFORM's own claims to import each new tenant's `$SRV.>`
  service discovery" spec (integration, against an embedded operator-mode
  NATS server), both in `accounts/provisioner_claims_test.go` /
  `accounts/provisioner_test.go`. The browser-exclusion premise needs no new
  test of its own: `auth/token_test.go`'s existing `ConsistOf` assertions on
  `MintBrowserToken`/`MintAdminToken`'s exact `Sub.Allow`/`Pub.Allow` lists
  already fail on any unlisted subject, `auth/token.go` is untouched by this
  rule, and the suite stays green — so `$SRV.>`/`monitor.*.srv.>` staying out
  of a browser JWT is a pre-existing, still-enforced guarantee, not a gap
  this rule had to newly close. Live cross-account reply routing itself —
  the part this rule's design note above flags as unproven by claims-shape
  tests alone — is exercised at Phase 30i's live `docker compose`
  verification, not by this rule's own (claims-only) test coverage.

## BR-AC32 (Phase 30b, extended 30i) — Minted account JWTs carry seven narrow, explicit `$JS.API` Service exports back to PLATFORM, imported per tenant with a tenant-scoped local remap, for read-oriented JetStream/KV introspection

The third tenant-to-PLATFORM export (after `obs.trace.>`, BR-AC30, and
`$SRV.>`, BR-AC31), needed so `observability-service`'s JetStream/KV
introspection panels (Phase 30e — Streams, KV Buckets, KV Entries, Replay)
can reach every tenant account without a raw per-tenant connection. Traced
directly against the exact call chain in `dictionary/internal/rest/{kv,
replay}.go` and the `$JS.API` subject constants in the pinned
`nats.go@v1.52.0` (Phase 30's own Design section has the full trace) —
**not** a blanket `$JS.API.>` export, which would grant stream
*management* (create, delete, purge), not just visibility
(ARCHITECTURE-ACCOUNTS.md:87–101). Every tenant account JWT minted by
`CreateAccount` gains exactly seven Service exports on its own claims, and
PLATFORM's claims gain seven matching per-tenant Service imports, each
remapped to `monitor.{tenantName}.js.<same-suffix>` so a caller on PLATFORM
can address one tenant's introspection traffic without colliding with
another's — same shape as BR-AC31's `$SRV.>` grant, applied to seven
subjects instead of one wildcard, since `STREAM.LIST`, `STREAM.INFO.*`,
`CONSUMER.CREATE.*`, `CONSUMER.CREATE.*.*`, `CONSUMER.CREATE.*.*.>`,
`CONSUMER.MSG.NEXT.*.*`, and `CONSUMER.DELETE.*.*` have different wildcard
arities and cannot be merged into a single pattern without either
overreaching or excluding one of them.

> **Phase 30i amendment — `CONSUMER.CREATE.*.*.>` added as a seventh
> subject, caught only once this ran against a real multi-account
> deployment.** `CONSUMER.CREATE.*.*` (no filter) covers a plain named
> consumer create, but nats.go's `CreateOrUpdateConsumer` embeds a
> `FilterSubject` directly into the *published* `$JS.API` subject rather
> than the request body whenever one is set
> (`apiConsumerCreateWithFilterSubjectT`, `"CONSUMER.CREATE.%s.%s.%s"`) —
> and `jetstream.KeyValue.WatchAll` (the KV Buckets panel's live-entries
> view, `kv.go`'s `kvBucketEntriesOnce`) always sets one, filtered to the
> bucket's own `$KV.<bucket>.>` subject. The literal wire subject is
> therefore `$JS.API.CONSUMER.CREATE.<stream>.<ephemeral-name>.$KV.<bucket>.>`
> — a variable-length tail no fixed two-wildcard pattern can match. Unit
> tests never exercise this (an embedded test server has no account
> permissions to violate), so it surfaced only once observability-service
> ran under BR-AC31/BR-AC32's real cross-account grants — the entire
> premise of Phase 30i's live-verification step. Applies equally to a
> tenant's own KV buckets (once one exists — a fresh docker-compose stack's
> ACME/GLOBEX have none until a ship or container is first registered) as
> to PLATFORM's own (`refdata-service`'s KV caches, `trace-request-reply`),
> which is why this is a `jsAPIExportSubjects` addition (both sides of the
> tenant-to-PLATFORM grant) rather than a PLATFORM-native-only fix — see
> `BUSINESS_RULES-SHIPPING.md`'s Phase 30i amendment to the trace-store rule
> for the PLATFORM-native counterpart (`nats/bootstrap-operator.sh`'s
> `observability` user), which needed the identical addition for the exact
> same reason.

**Response types are not uniform across the seven** — each is set to match
what the underlying `$JS.API` operation actually does, not copied
blindly from BR-AC31:

- `STREAM.LIST`, `STREAM.INFO.*`, `CONSUMER.CREATE.*`,
  `CONSUMER.CREATE.*.*`, `CONSUMER.CREATE.*.*.>`, `CONSUMER.DELETE.*.*` —
  library default `Singleton` (exactly one reply per request: a list page,
  a stream's info, a created consumer's info — filtered or not, still one
  reply — a delete acknowledgement).
- `CONSUMER.MSG.NEXT.*.*` — **`Stream`**, for the same reason BR-AC31's
  `$SRV.>` needed it: a single pull request with a batch size greater than
  one yields *multiple* individual JetStream messages delivered back to the
  requester, not one reply. This is a second, independent instance of the
  same not-yet-proven-in-this-codebase mechanism BR-AC31 flagged — whether a
  NATS service import correctly keeps the cross-account reply route open for
  more than one message per request — so `CONSUMER.MSG.NEXT`'s live
  behavior is exercised at Phase 30i's live verification alongside
  BR-AC31's, not assumed correct from BR-AC31 having been implemented.

**`CONSUMER.DELETE.*.*` carries the same residual risk BR-AC32's design
predecessor flagged and this rule does not attempt to close at the JWT
layer**: the wildcard matches any consumer name on any stream in the tenant
account, not just one `observability-service` itself created. That stays an
application-layer invariant (`observability-service` must only ever call
delete with a name it just received from its own preceding create, in the
same request — Phase 30's own checklist item 30b requires a code-level
test/lint for this, not just a claims-shape spec), never something this
account-JWT rule can enforce by narrowing the subject further — NATS
wildcards match by token position, not by "who created this."

**Critically, no browser-facing JWT gains this grant** — same premise as
BR-AC30/BR-AC31; `MintBrowserToken`/`MintAdminToken` are unaffected.

- **Enforced in:** `accounts/provisioner.go`'s `jsAPIExportSubjects` (the
  seven-subject list, shared by both `tenantExports()` and
  `addPlatformJSAPIImport` so they can't drift apart), `tenantExports()`
  (adds them, `ResponseType` set per-subject as above, to every tenant's
  own claims), and `addPlatformJSAPIImport` (PLATFORM-side, mirrors
  `addPlatformMonitorImport`'s re-sign/idempotency mechanism, looping the
  seven `(remote, local)` subject pairs); called from `CreateAccount`
  alongside `addPlatformTraceImport`/`addPlatformMonitorImport`, gated on
  the same `platformPublicKey != ""` condition. `nats/bootstrap-operator.sh`
  carries the day-0 nsc equivalent for the pre-seeded ACME/GLOBEX tenants.
- **Test:** a freshly-minted tenant's own claims decode with all seven
  `$JS.API` Service exports present, `CONSUMER.MSG.NEXT.*.*`'s
  `ResponseType` equal to `Stream` and every other one of the seven left at
  the library default `Singleton`; PLATFORM's claims, after `CreateAccount`,
  decode with seven matching per-tenant Service imports
  (`LocalSubject: monitor.{tenantName}.js.<suffix>`), accumulating correctly
  across multiple tenants without disturbing each other's entries and
  without duplicating on a retried call; a negative assertion that no
  mutating `$JS.API` subject (`STREAM.CREATE`/`DELETE`/`PURGE`/`UPDATE`/
  `RESTORE`, `STREAM.MSG.GET`, any `CONSUMER.CREATE`/`DELETE` form beyond
  the two/one listed) is present on either side. Covered by
  `TestNewAccountClaimsAddsTenantJSAPIExports` (unit, tenant-side export) and
  the "re-signs PLATFORM's own claims to import each new tenant's `$JS.API`
  introspection subjects" spec (integration, against an embedded
  operator-mode NATS server), both in `accounts/provisioner_claims_test.go` /
  `accounts/provisioner_test.go`. As with BR-AC31, the browser-exclusion
  premise is covered by `auth/token_test.go`'s pre-existing `ConsistOf`
  assertions, not a new test — `auth/token.go` is untouched by this rule.
  Live cross-account reply routing for `CONSUMER.MSG.NEXT` and the
  `DeleteConsumer`-name-provenance invariant are both exercised outside this
  rule's own claims-only coverage — see the design note and Phase 30b's
  checklist item, respectively.

### BR-AC33 (Phase 34) — This service's mirror of `BUSINESS_RULES-SHIPPING.md`'s BR-040 mux allowlist rule

accounts-service mounts two independent route sets onto one mux, each
covered by its own allowlist test since they live in separate packages:

- `accounts/handler.go`'s `Handlers.Mount(mux, authSecret)` returns
  `[]string` — the 13 `BasicAuth`-gated `/api/accounts*` routes (account
  create/list/get, usage, topology, suspend/reactivate, jslimits,
  system-config get/put, business-unit list/create/update).
- `auth/handler.go`'s `Handlers.Mount(mux)` returns `[]string` — the 5
  deliberately ungated `/api/auth/*` routes (connectInfo,
  adminConnectInfo, refdataAdminConnectInfo, tenants, login). Every route in
  both sets is inherently admin/bootstrap — accounts-service administers the
  tenant axis itself and has no business domain to separate REST from, so
  unlike the other five services there is no "business route" category this
  allowlist is guarding against, only future scope creep beyond
  account/tenant lifecycle.

- **Enforced in:** `accounts/handler.go`'s `Mount`, `auth/handler.go`'s
  `Mount`.
- **Test:** `accounts/handler_allowlist_test.go` —
  `TestAccountsMountRoutesMatchAdminAllowlist`; `auth/handler_allowlist_test.go`
  — `TestAuthMountRoutesMatchAdminAllowlist`. Each asserts its `Mount`'s
  returned route list `ConsistOf` its 13- or 5-entry allowlist above.

### BR-AC34 (Phase 67a, PROPOSED — not yet implemented) — `tenantExports()` gains a second Stream export, `obs.pubsub.>`, mirrored into PLATFORM the same way `obs.trace.>` is

`tenantExports()` (`accounts/provisioner.go:315`) gains a second entry
alongside the existing `obs.trace.>` export: `{Subject: jwt.Subject("obs.pubsub.>"), Type: jwt.Stream}` — no `AllowTrace`, no `ResponseType`, the same shape as `obs.trace.>`'s export and unlike `$SRV.>`/`$JS.API.*`'s Service exports (BR-AC31/32), because a Stream export needs no per-tenant `LocalSubject` remap: the importing account boundary itself, not a subject remap, is what disambiguates which tenant a given imported stream message came from (see BR-AC30's rationale, which applies identically here).

PLATFORM's side gains a new `addPlatformPubsubImport`, mirroring `addPlatformTraceImport` (`accounts/provisioner.go:463`) exactly: same idempotency-by-`(Account, Subject)` scan over `claims.Imports` before adding, same `jwt.Import{Account: tenantAccountPub, Subject: "obs.pubsub.>", Type: jwt.Stream, AllowTrace: true}`, same re-sign-and-push via `$SYS.REQ.CLAIMS.UPDATE`. Called from `CreateAccount` alongside `addPlatformTraceImport`, gated the same way on `platformPublicKey != ""`.

- **Enforced in:** not yet — Phase 67a.
- **Test:** not yet written — pending Phase 67a implementation.
