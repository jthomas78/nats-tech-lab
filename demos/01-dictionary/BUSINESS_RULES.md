# Business Rules — Index

Split by domain so a rule add/edit only requires reading its own file:

- **[BUSINESS_RULES-SHIPPING.md](BUSINESS_RULES-SHIPPING.md)** — Ship + Container
  aggregates on the `SHIPPING` stream (BR-001–BR-019), plus guards, AIS status,
  and container status tables. Rules live in `dictionary/internal/domain/`.
- **[BUSINESS_RULES-REFDATA.md](BUSINESS_RULES-REFDATA.md)** — Reference Data
  Service (BR-D01–BR-D21). Rules live in
  `backend/refdata-service/refdata/internal/domain/dictionary.go`.

When CLAUDE.md's Quality Rule #4 says "update `BUSINESS_RULES.md`," it means:
add/edit the rule in whichever of the two files above matches the domain the
change touches. This index file itself should stay a pointer — don't add rule
detail here.
