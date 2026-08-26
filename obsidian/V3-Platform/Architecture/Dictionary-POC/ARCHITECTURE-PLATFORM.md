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

## 1. Where Tech Lab Operator sits

The target platform splits into three planes. Tech Lab Operator is the UI of
the **marketplace operations plane** — plane 2 in the diagram, drawn there as
"marketplace admin" — which sits between the global control plane above it and
the per-tenant product plane below it.

![Three-plane, multi-region V3 logistics platform. A global SaaS control plane holds eight areas in two rows: region registry and deployment, cluster and container fleet, the NATS operator trust root, and plans/entitlements/tenant billing; then tenant catalogue and placement, the AuthN and AuthZ model, the module catalogue, and the app harness contract. One provisioning rail leaves tenant catalogue and placement and reaches two independent regional cells, ZA and AU, each carrying a marketplace operations plane (marketplace admin plus regional platform services for accounts, auth, refdata and observability), a product/business data plane of isolated per-tenant cells each holding its own NATS account, and a regional runtime of a three-node NATS HA cluster and per-service Postgres. Cross-region disaster recovery and replication are marked TBD. A second figure defines the tenant axis: a regional cell hosts many tenants, a tenant is one marketplace and one NATS account containing organisations, business unit/context is an application naming scope, and users reach the tenant through organisation memberships.](images/v3-logistics-platform-overview.png)

Three things in that picture constrain what belongs in this app:

- **The control plane is not this app.** Region and cluster provisioning,
  the NATS operator trust root, tenant placement, plans and billing are
  cross-tenant concerns and stay out of Tech Lab Operator's nav. "Operator"
  in this app's name is the human sense; the NATS trust-root sense belongs
  to [ARCHITECTURE-ACCOUNTS.md](ARCHITECTURE-ACCOUNTS.md).
- **Its scope is one tenant's cell.** Everything in the nav taxonomy below
  operates inside a single tenant's NATS account boundary, which is what
  keeps it distinct from `frontend/admin` (cross-tenant, system-level).
- **Features arrive as modules.** A product module is a vertical slice —
  backend service plus frontend — registered in the control plane's module
  catalogue and injected into the app harness, which populates navigation
  from what registered. The nav taxonomy in §3 is therefore a record of
  which modules a tenant is entitled to, not a hand-maintained menu.

> The diagram's source is
> `demos/01-dictionary/diagrams/v3-logistics-platform-overview.html`;
> re-export with `node export-html-png.mjs` from that directory.

## 2. What Tech Lab Operator is

The operator-facing counterpart to `frontend/admin` (which stays focused on
cross-tenant/system-level NATS observability and platform administration —
see [ARCHITECTURE-ADMIN.md](ARCHITECTURE-ADMIN.md)). Tech Lab Operator is
scoped to a single tenant's day-to-day operational tasks: setting up and
maintaining that tenant's own reference data, configuration, users,
companies, and organizations.

## 3. Nav taxonomy

As of Phase 36's design-gate approval:

```
Operations
  - Reference Data          (Phase 36.1 — rebrand + nav restructure)
```

Planned, not yet scheduled as their own phase:

```
Platform
  - Organizations          (Phase 36.2 — migrating in from admin's
      - Shippers                Platform group; see below)
      - Transporters
```

Additional operator tasks named in Phase 36's goal — registration of users,
companies, etc. — are not yet mapped to nav entries; they'll be added here
as their own design decisions land, each getting its own sub-phase the same
way 36.1/36.2 did.

## 4. Feature docs owned elsewhere

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
  account model Phase 36.2's Organizations migration has to reconcile
  `refdata`'s lighter `context` concept against (see that phase's "Open
  risk" design decision in `Main-POC-Plan.md`).

## 5. UI design system

Same shared system as every other app in this repo — no exception carved
out for Tech Lab Operator. See `shared/unifi-theme/LAYOUT.md` for the
slot API, nav (`NavList.vue`) conventions, and the "Panel top tabs" contract
(`Tabs`/`TabList`/`Tab`/`TabPanels`/`TabPanel`) that Phase 36.1's new
tabbed info panel follows, copying `admin`'s `RpcPanel.vue` as the concrete
reference implementation.
