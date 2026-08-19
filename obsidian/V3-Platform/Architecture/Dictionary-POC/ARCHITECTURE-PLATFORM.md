# Tech Lab Operator — Platform Entry Point

Entry point for anything related to **Tech Lab Operator**'s design — the
`frontend/refdata` app (rebranded from "Dictionary," `.claude/plans/
Main-POC-Plan.md` Phase 36) that carries operator/tenant-facing tasks:
reference-data setup, configuration, registration of users, companies,
transporters, and whatever else lands in it as the app grows. This doc owns
the app's **nav taxonomy and cross-feature design** — what belongs in it,
how it's organized, how one feature's design relates to another's. It does
not re-derive the backend/schema detail specific to any one feature that
already has its own architecture doc; those docs remain the owners of their
own depth, and are linked from here.

> **Status:** Phase 36 is approved at the design gate (mockups pending,
> implementation not started as of 2026-08-19). This doc currently records
> the nav shape and feature map as decided in the plan; it will grow
> section-by-section as each feature actually ships, the same way
> `ARCHITECTURE-ADMIN.md` grew alongside the Admin UI's own panels.

---

## 1. What Tech Lab Operator is

The operator-facing counterpart to `frontend/admin` (which stays focused on
cross-tenant/system-level NATS observability and platform administration —
see [ARCHITECTURE-ADMIN.md](ARCHITECTURE-ADMIN.md)). Tech Lab Operator is
scoped to a single tenant's day-to-day operational tasks: setting up and
maintaining that tenant's own reference data, configuration, users,
companies, and trading partners.

## 2. Nav taxonomy

As of Phase 36's design-gate approval:

```
Operations
  - Reference Data          (Phase 36.1 — rebrand + nav restructure)
```

Planned, not yet scheduled as their own phase:

```
Platform
  - Trading Partners          (Phase 36.2 — migrating in from admin's
      - Shippers                Platform group; see below)
      - Transporters
```

Additional operator tasks named in Phase 36's goal — registration of users,
companies, etc. — are not yet mapped to nav entries; they'll be added here
as their own design decisions land, each getting its own sub-phase the same
way 36.1/36.2 did.

## 3. Feature docs owned elsewhere

- [ARCHITECTURE-DICTIONARY.md](ARCHITECTURE-DICTIONARY.md) — owns
  `refdata-service`'s own backend architecture: seeding, Postgres
  schema/ER diagram, data access paths (Postgres/REST/KV), and
  cross-service consumption. The "Reference Data" nav entry above is Tech
  Lab Operator's UI surface over that service — a subset of this document,
  not a replacement for it. When a Reference Data UI decision depends on
  backend behavior, cite ARCHITECTURE-DICTIONARY.md rather than
  re-describing the backend here.
- [ARCHITECTURE-COMMUNICATIONS.md](ARCHITECTURE-COMMUNICATIONS.md) — owns
  the `rpc.*`/`api.*` subject taxonomy and `{context}` rules Tech Lab
  Operator's frontend-to-service calls follow, same as every other app in
  this repo.
- [ARCHITECTURE-ACCOUNTS.md](ARCHITECTURE-ACCOUNTS.md) — owns the tenant
  account model Phase 36.2's Trading Partners migration has to reconcile
  `refdata`'s lighter `context` concept against (see that phase's "Open
  risk" design decision in `Main-POC-Plan.md`).

## 4. UI design system

Same shared system as every other app in this repo — no exception carved
out for Tech Lab Operator. See `shared/unifi-theme/LAYOUT.md` for the
slot API, nav (`NavList.vue`) conventions, and the "Panel top tabs" contract
(`Tabs`/`TabList`/`Tab`/`TabPanels`/`TabPanel`) that Phase 36.1's new
tabbed info panel follows, copying `admin`'s `RpcPanel.vue` as the concrete
reference implementation.
