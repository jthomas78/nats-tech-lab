# Business Rules — Index

Split by domain so a rule add/edit only requires reading its own file:

- **[BUSINESS_RULES-SHIPPING.md](BUSINESS_RULES-SHIPPING.md)** — Ship + Container
  aggregates on the `SHIPPING` stream (BR-001–BR-022), plus guards, AIS status,
  container status tables, and the Phase 15 `api.*`/`notify.*` transport rules
  (BR-023–BR-024). Rules live in `dictionary/internal/domain/` (BR-001–022) and
  `dictionary/internal/browserrpc/` + `dictionary/internal/eventhandler/`
  (BR-023–024).
- **[BUSINESS_RULES-REFDATA.md](BUSINESS_RULES-REFDATA.md)** — Reference Data
  Service (BR-D01–BR-D28). Rules live in
  `backend/refdata-service/refdata/internal/domain/dictionary.go`.
- **[BUSINESS_RULES-ACCOUNTS.md](BUSINESS_RULES-ACCOUNTS.md)** — Accounts
  Service (BR-AC01–BR-AC06): NATS account provisioning, suspension,
  reactivation, and reserved-name protection via decentralized JWTs. Rules
  live in `backend/accounts-service/accounts/handler.go` and
  `provisioner.go`.

When CLAUDE.md's Quality Rule #4 says "update `BUSINESS_RULES.md`," it means:
add/edit the rule in whichever of the three files above matches the domain
the change touches. This index file itself should stay a pointer — don't add
rule detail here.
