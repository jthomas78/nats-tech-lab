---
name: refdata-cross-tenant-stream-import
description: Every tenant imports evt.*.refdata.*.changed with an unbounded context wildcard, so tenants receive each other's refdata change events — unfixed as of 2026-08-11
metadata:
  type: project
---

**Found 2026-08-11 while reviewing the PLATFORM→tenant contract.** The stream import does not match the care taken on the service imports beside it.

- Export: `nsc add export --account PLATFORM --subject "evt.*.refdata.*.changed"` — `demos/01-dictionary/nats/bootstrap-operator.sh:120`
- Import in **every** tenant JWT: `stream("evt.*.refdata.*.changed")` — `demos/01-dictionary/backend/accounts-service/accounts/provisioner.go:200` (inside `tenantImports`)

The first token after `evt.` is `{context}`, and the wildcard is unbounded — so `acme` receives refdata change events for `globex`'s contexts and vice versa. Not values, but **context names, type keys and change timing**, which is enough to infer another tenant's business-unit structure and activity.

**Contrast with the four service imports directly above it** (`provisioner.go:194-197`), which are correctly scoped: the remote subject is `rpc.{tenantName}.refdata.item.get.v1` remapped to a context-free local subject, so a tenant can only reach its own namespace, and because the import lives in an operator-signed JWT it cannot remap itself to another tenant's. Four scoped imports sitting above one wildcard import reads as an oversight rather than a decision.

**How to apply:** fix by narrowing the **import** per tenant (not the export) — one entry per context the tenant owns. `accounts-service` already holds `accounts.business_units`, so it knows that set at mint time, and the code path that adds a BU would add the import. The wrinkle: the subject is keyed by *context*, not tenant, so the import list grows with business units instead of being one line — that is the accepted price of context-in-subject + tenancy-in-account ([[phase16_tenancy_taxonomy]]).

Note `handler_test.go:884` asserts `HaveLen(7)` on the acme import edges as "the complete tenantImports contract" — that test will need updating with the fix. Related: [[phase21_account_exports_imports]], [[v3_tenancy_axes_decision]].
