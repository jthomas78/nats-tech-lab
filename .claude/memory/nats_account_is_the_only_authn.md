# The NATS account boundary is this repo's only authentication

Established while implementing Phase 38c-ii (2026-08-20). Verified by search
across every service, not inferred.

- **Nothing in this repo verifies a JWT.** `accounts-service`
  (`auth/token.go`) only *mints* NATS user credentials — `MintBrowserToken`,
  `MintAdminToken`, `MintRefdataAdminToken` — and `accounts/provisioner.go`
  mints account/user JWTs. There is no `VerifyJWT`, no middleware, no
  `golang-jwt` dependency anywhere.
- Every caller authenticates by **connecting to a NATS account**, enforced
  server-side. Services read the tenant off their own connection, never off a
  request body (see `browserrpc`'s package doc in each service); `{context}`
  comes from the subject by position. There is no per-user identity at all —
  no subject/user claim is read, and no principal is threaded into command
  handlers.
- **Consequence: any new HTTP ingress has nothing to reuse for authn.** This
  is the trap — a design doc saying an endpoint needs "its own auth" reads as
  wiring and is actually a from-scratch build. It cost real design time in
  38c-ii before the gap was found.
- **The adopted answer is a capability ticket, not a second auth system**
  (BR-TP41, `tradingpartner/internal/filetickets`): the browser asks for a
  single-use, short-lived grant over its already-authenticated NATS
  connection, and the HTTP call carries only that token, in a header. Tenant,
  context and target are read back off the redeemed ticket, so nothing the
  HTTP request claims can widen it. Reach for this shape again rather than
  introducing JWT verification for a couple of routes.
- Rejected alternative: sending the browser's NATS user JWT as a bearer
  token. It is weaker — a bare JWT has no proof-of-possession, unlike the
  nkey challenge the NATS connection itself performs.

Related: [[phase38_document_object_store]], [[nats_scoped_signing_keys]],
[[phase34_boundary_enforcement]].
