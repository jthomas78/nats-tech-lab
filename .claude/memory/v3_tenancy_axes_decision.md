---
name: v3-tenancy-axes-decision
description: V3 Linebooker tenancy model review (2026-08-11) — tenant is the marketplace-operating business not the region; five axes region/tenant/organisation/context/user; rejects the ChatGPT "account = ZA/NA/AU" model
metadata:
  type: project
---

**Design review 2026-08-11** comparing a ChatGPT-authored model (saved at repo root as `logistics-nats-linebooker-chat (1).md`), the Linebooker V2 repo (`/Users/jeremy/dev/github/linebooker/linebooker`), and this POC. Full transcript exported to `V3-Tenancy-NATS-Security-Review.pdf` at repo root.

**Recommended five axes:**

1. **Region** — deployment / data residency (za, au). Separate NATS server + Postgres. Never in a subject or JWT.
2. **Tenant** — the *marketplace-operating business*. **NATS account** = hard, server-enforced wall.
3. **Organisation** — **Shipper** | Transporter | Operator-self | Integrator (+ future Warehouse/Broker). Lives *inside* a tenant account, separated by subject scope — see [[nats_scoped_signing_keys]]. **"Shipper" replaced "Customer" on 2026-08-13**, closing the open recommendation in [[linebooker_shipper_vs_customer_naming]]: "Customer" named the demand side by its commercial relationship to a seller while "Transporter" names the supply side by market function — an inconsistent axis across one marketplace. Committed in the Phase 26 plan section of `.claude/plans/Main-POC-Plan.md`, whose `TradingPartner` aggregate carries `PartnerType` = `SHIPPER` | `TRANSPORTER`, so this is the settled V3 term rather than a proposal. V2's own enum remains literally `BusinessType {CUSTOMER, …}` — expect the rename to happen at the port boundary, and read any `CUSTOMER` in V2 source as this axis's `Shipper`.
4. **Context / BU** — `{context}` token; naming scope, not a boundary. Unchanged from [[phase16_tenancy_taxonomy]].
5. **User** — member of one or more organisations; credential minted **per membership**, not per user.

**The key correction: tenant ≠ region.** The ChatGPT model put one NATS account per regional marketplace (PLATFORM/ZA/NA/AU). That collapses two independent axes: it leaves no hard isolation *inside* a market (all customers + transporters share one account, one JetStream limit pool, one blast radius), and a NATS account is a within-server construct that cannot express "different continent" anyway.

**Why:** V2's infrastructure already proves tenant is a business, not a geography — there is a dedicated white-label build pipeline (`infra/cloud-build/new-stack-no-db-whitelabel.yaml`) and a named non-Linebooker stack (`thornlands`). Two marketplace operators can share one region. Making the distinction is free now and expensive after the first white-label customer.

**What the ChatGPT model got right and should be adopted:** generic Organisation + type discriminator (V2 already has this as `BusinessEntity` + `BusinessType {CUSTOMER, TRANSPORTER, OPERATOR, INTEGRATOR}`); many-to-many `OrganisationTenantMembership` so one org joins several markets without duplicate rows (V2 lacks this — `UserEntity` belongs to exactly one `BusinessEntity`); no direct customer→transporter messaging; a small PLATFORM account that exports (already built, [[phase21_account_exports_imports]]).

**Naming hazards to settle first:** "Operator" means three different things (NATS operator / `BusinessType.OPERATOR` = Linebooker staff / the chat's regional tenant) — reserve it for the NATS operator. "Region" means a province in V2 (`RegionEntity`) and a deployment here — free the word by migrating V2 geography onto `GeoAreaEntity`. Tenant names are opaque ids that land in `.creds` filenames — pick one convention (ISO codes) before seeding.

**Open decisions (none made yet):**
- Is a tenant a marketplace-operating business or a country? Commercial roadmap decides.
- Do organisations get subject-scoped isolation from day one, or deferred?
- Participant-plane subject grammar: arity-7 `api.*` vs a separate `org.*` family (separate family preferred, so `domain.SubjectDetails`' fixed-6 check stays untouched).
- Is refdata global or regional? — see [[linebooker_v2_refdata_candidates]].

**How to apply:** this is a design agreement from discussion only — nothing implemented, and no plan phase covers memberships, users, or region (per [[design_discussion_vs_implementation_signal]], do not start building those). **Partial exception as of 2026-08-13:** the **terminology** for this axis ("Shipper" replacing "Customer") is settled — that was confirmed directly with the user via [[linebooker_shipper_vs_customer_naming]], independent of any plan's approval status. The **discriminator shape** (`PartnerType` = `SHIPPER` | `TRANSPORTER` on a `TradingPartner` aggregate) is only *proposed*, in the Phase 26 plan section of `.claude/plans/Main-POC-Plan.md`, which is still PROPOSED and awaiting sign-off — don't treat that shape as settled until the phase itself is signed off (per [[linebooker_trading_partner_phase_v1_scope]]). Neither this nor Phase 26 touches subject-scoped organisation isolation (still open below); Phase 26 deliberately builds no tenant-membership layer. Record the whole model in `ARCHITECTURE-ACCOUNTS.md` when the user is ready.
