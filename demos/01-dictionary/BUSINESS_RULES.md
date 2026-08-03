# Business Rules — Index

Split by domain so a rule add/edit only requires reading its own file:

- **[BUSINESS_RULES-SHIPPING.md](BUSINESS_RULES-SHIPPING.md)** — Ship + Container
  aggregates on the `SHIPPING` stream (BR-001–BR-022), plus guards, AIS status,
  container status tables, the Phase 15–18 `api.*`/`notify.*` transport rules
  (BR-023–BR-027), the Phase 17c Admin UI presentation rule (BR-028 —
  scoped to what the Connections/Services panels *display*, not the wire),
  the Phase 16g Sea Freight Flow presentation rule (BR-029 — Fleet
  Management, Ships at Port, and Terminal Yard panels show a loading state,
  not an empty one, mid tenant/context switch), and the Phase 16h
  reactive-provisioning rule (BR-030 — a tenant minted by accounts-service is
  immediately usable, no operator/restart needed; see BR-AC08, ACCOUNTS
  file, for the publishing side), and the Phase 16i reactive-teardown rule
  (BR-031 — a tenant suspended by accounts-service stops holding
  shipping-service resources open instead of reconnect-looping forever; see
  BR-AC09, ACCOUNTS file, for the publishing side).
  Rules live in `dictionary/internal/domain/` (BR-001–022),
  `dictionary/internal/browserrpc/` + `dictionary/internal/eventhandler/`
  (BR-023–024, 026–028), `internal/refdataconsumer/` (BR-025, 027),
  `frontend/seafreight-app/src/stores/port.js` (BR-029),
  `dictionary/internal/rest/tenant.go` + `dictionary/composition.go`
  (BR-030–031), and `frontend/seafreight-app/src/App.vue` (BR-031).
- **[BUSINESS_RULES-REFDATA.md](BUSINESS_RULES-REFDATA.md)** — Reference Data
  Service (BR-D01–BR-D28). Rules live in
  `backend/refdata-service/refdata/internal/domain/dictionary.go`.
- **[BUSINESS_RULES-ACCOUNTS.md](BUSINESS_RULES-ACCOUNTS.md)** — Accounts
  Service (BR-AC01–BR-AC09): NATS account provisioning, suspension,
  reactivation, reserved-name protection via decentralized JWTs, (BR-AC08)
  publishing `notify.accounts.account.created` so shipping-service can react
  to a newly-minted tenant immediately (see BR-030, SHIPPING file, for the
  consumer side), and (BR-AC09) the mirrored `notify.accounts.account.suspended`
  for a suspended tenant (see BR-031, SHIPPING file). Rules live in
  `backend/accounts-service/accounts/handler.go` and `provisioner.go`.

When CLAUDE.md's Quality Rule #4 says "update `BUSINESS_RULES.md`," it means:
add/edit the rule in whichever of the three files above matches the domain
the change touches. This index file itself should stay a pointer — don't add
rule detail here.
