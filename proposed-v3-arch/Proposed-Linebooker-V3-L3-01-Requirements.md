# Proposed Linebooker V3 Architecture - L3-01 Requirements

This register maintains the requirements represented by
`LB-V3-L3-01 - Participant + Tenancy`.

`LB-V3-L3-01` answers one question: **who is a tenant, what sits inside one,
and in which cells may that tenant exist?** It zooms into `LB-V3-L2-01`, whose
sheet 1 declares the NATS trust chain and the tenant account boundary and whose
sheet 2 declares the regional cell model and the rule that account and region
are separate axes.

The document exists because a team review treated tenancy and placement as one
nested hierarchy. They are not. The NATS trust hierarchy - operator, account,
user - carries no geography, and the placement hierarchy - region, cell,
runtime - carries no identity. The two axes cross. This document draws the
crossing so the mistake cannot be made from the picture.

`LB-V3-L3-01` is published as a **directional document**. It records a
first-pass position as at 2026-09-17 so the platform can move. It is not a
settled decision and it is not compliance evidence.

The document is drawn as three A3 landscape sheets:

| Sheet | Question |
|---|---|
| 1 - Two Axes That Cross | What is the trust hierarchy, what is the placement hierarchy, and how do a tenant and a cell meet? |
| 2 - Inside One Square | Which services run in one cell, whose credential do they hold, and what does a tenant own there? |
| 3 - Which Account Does Each Service Connect As | For every control-plane and domain service, which account does it connect as, which does it only import from, and which is refused? |

## Status vocabulary

- **Included** - visibly represented in the current L3-01 document.
- **Derived** - intentionally delegated to another concern view or to an L4
  detailed design.
- **Open** - requires business, legal, regulatory or technical confirmation.

## Maturity vocabulary

Used by the drawing's tile treatment. This describes the element, not the
document.

- **Measured** - the behaviour is proven by the `demos/03-multi-cluster-and-accounts`
  lab, which ran seven NATS topologies and recorded 73 passing checks on
  2026-09-17. Solid tile. The platform element around it is still proposed;
  only the NATS behaviour is evidence.
- **Proposed** - a directional V3 position with no implementation and no V3 ADR.
  Dashed grey tile.
- **Open** - a position that cannot be taken without a named owner's
  confirmation. Dashed amber tile, with the owner named on the tile.
- **Refused** - a combination the placement record does not permit. Red square.

## L3-01 requirements

| ID | Status | Requirement | L3-01 representation |
|---|---|---|---|
| L3-01-001 | Included | Show the NATS trust hierarchy as one operator, then accounts, then users. | Sheet 1, trust axis group, four tiles. |
| L3-01-002 | Included | Show the operator signing account claims and never signing a user directly. | Sheet 1, `Operator` tile. |
| L3-01-003 | Included | Show the tenant account as the hard messaging boundary, one per tenant. | Sheet 1, `Tenant account` tile. |
| L3-01-004 | Included | Show organisations and memberships living inside one tenant account, and state that an organisation never creates a new NATS account. | Sheet 1, `Organisation + membership` tile and the `ORG` note. |
| L3-01-005 | Included | Show a user credential as short-lived, issued by its own account, and held by either a person's session or a workload. | Sheet 1, `User credential` tile. |
| L3-01-006 | Included | Show the placement hierarchy as region, then cell, then cell-resident runtime. | Sheet 1, placement axis group, four tiles. |
| L3-01-007 | Included | Define a cell as one region's complete runtime and state that a cell is a place, not a tenant. | Sheet 1, `Cell` tile and the `CELL` note. |
| L3-01-008 | Included | Show the JetStream domain as a marker of one cell and explicitly not a tenancy boundary. | Sheet 1, `JetStream domain` tile. |
| L3-01-009 | Included | State that the trust axis carries no geography and the placement axis carries no identity. | Sheet 1, the two converging edge labels and the closing amber panel. |
| L3-01-010 | Included | Draw the two hierarchies as crossing rather than nesting. | Sheet 1, the grid: accounts as rows crossing cells as columns. |
| L3-01-011 | Included | Show a cell holding many accounts, and an account spanning many cells, in one figure. | Sheet 1, grid columns and grid rows. |
| L3-01-012 | Included | Show the standing accounts separately from tenant accounts. | Sheet 1, grid rows `$SYS` and `PLATFORM`. |
| L3-01-013 | Included | Show the system account as administrative only, carrying no business traffic. | Sheet 1, `$SYS` row intersections. |
| L3-01-014 | Included | Show a tenant permitted in more than one cell as one account keeping one identity in each. | Sheet 1, `TENANT A` row and the `ACCOUNT` note. |
| L3-01-015 | Included | Show a tenant permitted in exactly one cell. | Sheet 1, `TENANT B` row. |
| L3-01-016 | Included | Show an intersection the placement record refuses as an explicit refusal, not as a gap in the drawing. | Sheet 1, four red `Not permitted` squares. |
| L3-01-017 | Included | Show the sovereign cell refusing every regulated business path while still receiving signed claims. | Sheet 1, red barred sovereign edge and the red closing panel. |
| L3-01-018 | Included | State the governing rule: the account carries the tenant, the cell carries the region, and neither contains the other. | Sheet 1, amber closing panel. |
| L3-01-019 | Included | Show what one cell owns regardless of which tenant is being served. | Sheet 2, cell group, four tiles. |
| L3-01-020 | Included | Show three NATS servers and a local claim resolver as the cell's messaging runtime, and state that this is a logical minimum and not a sizing statement. | Sheet 2, `NATS, three nodes` tile and the `HA` note. |
| L3-01-021 | Included | Show the L2 service replicas as shared inside the cell rather than owned by a tenant. | Sheet 2, replica group, four tiles. |
| L3-01-022 | Included | Show every service replica holding a short-lived user credential rather than owning an account. | Sheet 2, replica group, credential line on each tile, and the amber closing panel. |
| L3-01-023 | Included | Show what a tenant owns inside one cell, separated by store role. | Sheet 2, tenant group: JetStream facts, KV projections, service Postgres, evidence documents. |
| L3-01-024 | Included | State that a replica may write only inside the account whose user credential it holds. | Sheet 2, the edge label between the replica group and the tenant group. |
| L3-01-025 | Included | Record as open whether a shared replica holds one credential per tenant account, or a replica is deployed per account. | Sheet 2, dashed amber closing panel. |
| L3-01-026 | Included | Show the measured NATS constraints that decide how a workload may cross an account or a cell. | Sheet 2, measured group, four solid tiles, each citing its demo 03 check IDs. |
| L3-01-027 | Included | State that an account is a wall for data and not a wall for availability. | Sheet 2, `An account is not a vote` tile. |
| L3-01-028 | Included | State that an explicit export and import is the only path between two accounts, that it is one-directional, and that it is read-only. | Sheet 2, `Export and import` tile. |
| L3-01-029 | Included | State that a consumer runs where its stream runs, so a replica in another cell pays the long link on every read. | Sheet 2, `A consumer follows its stream` tile. |
| L3-01-030 | Included | State that a new stream or key-value bucket lands where the client is unless a cluster is named. | Sheet 2, `Placement follows the client` tile. |
| L3-01-031 | Included | Distinguish measured NATS behaviour from a proposed platform position on the face of the drawing. | Solid, dashed grey and dashed amber tile treatments, with a legend entry on both sheets. |
| L3-01-032 | Included | Exclude all credential material from the drawing. | No key, seed, account identifier, claim body or credentials-file content appears on either sheet. |
| L3-01-033 | Included | Mark the document as directional, dated, with a per-item status and a visible missing decision of record. | Both sheets: scope line, `as at` date, per-tile status line, `no V3 ADR` markers. |
| L3-01-034 | Derived | Permission claim text, identity federation, role and attribute policy, and security audit. | `LB-V3-L3-09 Security + Identity`. |
| L3-01-035 | Derived | Subject families, stream and consumer settings, grant text and messaging failure behaviour. | `LB-V3-L3-05 Messaging / NATS`. |
| L3-01-036 | Derived | The placement record schema, country rule sets, residency, recovery targets and border allow-lists. | `LB-V3-L3-08 Multi-Region`. |
| L3-01-037 | Derived | Which business capability each domain service owns, and the boundaries between them. | `LB-V3-L3-02 Functional Domains`. |
| L3-01-038 | Derived | Where each replica runs, how it is packaged, scaled and rolled out per cell. | `LB-V3-L3-12 Deployment + Services`. |
| L3-01-039 | Derived | Per-domain data ownership, retention and lineage inside a tenant account. | `LB-V3-L3-07 Data Architecture`. |
| L3-01-040 | Derived | The exact account name scheme, credential lifetime, rotation and provisioning sequence. | L4 detailed design. |
| L3-01-041 | Included | Map every control-plane and domain service named in `LB-V3-L2-01` to the account it connects as. | Sheet 3, the whole matrix: 6 control-plane rows and 8 domain rows against 3 account columns. |
| L3-01-042 | Included | State that one NATS connection sees exactly one account, so a service needing two accounts needs two connections. | Sheet 3, scope line and the closing policy statement. |
| L3-01-043 | Included | Show that exactly one service connects as the system account for claim and placement work. | Sheet 3, `NATS provisioning` row, the only `Connects` mark in the `$SYS` column, and the closing policy panel. |
| L3-01-044 | Included | Show the control plane as platform-shaped: it owns its records in the platform account and publishes them outward. | Sheet 3, group A, `Connects` in the `PLATFORM` column on every row that owns records. |
| L3-01-045 | Included | Show the domain plane as tenant-shaped: every domain service connects as one tenant account at a time. | Sheet 3, group B, `Connects` in the `TENANT` column on all eight rows. |
| L3-01-046 | Included | Show that a domain service is refused the system account, so no business service can act as an administrator. | Sheet 3, group B, `Refused` in the `$SYS` column on all eight rows. |
| L3-01-047 | Included | Show shared reference data reaching a tenant by import only, read-only, and never by a tenant writing to the platform account. | Sheet 3, `Imports` marks in the `PLATFORM` column, carrying `L3-05-011`. |
| L3-01-048 | Included | Mark every cell with no source as open rather than guessing it. | Sheet 3, amber `Open` marks citing `L3-01-O07`, and the closing open panel citing `L3-01-O08`. |

## Open confirmations

| ID | Open item | Owner |
|---|---|---|
| L3-01-O01 | Whether one shared service replica holds a user credential per tenant account, or whether a replica is deployed per account. The drawing shows the question, not an answer. Demo 03 supplies only the constraint: an account is a hard wall, so a process reaching two accounts needs two connections. | Architecture |
| L3-01-O02 | Whether one tenant may be permitted in a shared cell and a sovereign cell at the same time, and what that does to the single-identity rule. | Architecture and legal |
| L3-01-O03 | Whether organisation-level isolation ever needs its own NATS account, for example where a transporter must not observe a shipper's traffic inside the same tenant. | Architecture |
| L3-01-O04 | Which standing accounts exist besides the system account and the platform account. | Architecture |
| L3-01-O05 | The authority that may add or remove a permitted cell for a live tenant, and what happens to data already held in a cell being removed. | Architecture and business |
| L3-01-O06 | A V3-scoped ADR for participant and tenancy. No decision of record exists today, so nothing here is settled for V3. | Architecture |
| L3-01-O07 | Whether identity, membership and tenant-facing audit records live in the platform account or in each tenant account. Sheet 3 marks both cells open rather than choosing. | Architecture |
| L3-01-O08 | Whether a domain service reaches shared reference data by an import into its tenant account, or by a second connection to the platform account. | Architecture |

## Notes on evidence

The four solid tiles on sheet 2 are the only elements in this document backed by
a measurement. They come from `demos/03-multi-cluster-and-accounts`, whose
`REPORT.md` is generated from a re-runnable rig and recorded 73 passed, 0
failed and 19 notes on 2026-09-17. The check IDs printed on each tile index into
that report's "Every result" tables.

Everything else on all three sheets is proposed or open. The Dictionary POC runs one
NATS server in one region and has no tenant account model in production.

Sheet 3 carries no solid tile at all. Every cell on it is a proposal, so the
sheet distinguishes a verdict by a small coloured square and a word - `Connects`,
`Imports`, `Open`, `Refused` or `not used` - and keeps every tile dashed. The
service names are taken verbatim from `LB-V3-L2-01`; the matrix invents no
service. The `$SYS` claim path comes from the L2 `NATS provisioning` tile and
from the run-time account creation recorded in `L3-05` notes on evidence. The
import-only reference path comes from `L3-05-011`. Where no source existed, the
cell is marked open instead of guessed.

## Change log

- 2026-09-17 - Register created with the document, tracing to `LB-V3-L2-01`.
- 2026-09-17 - Both editions published and validated. `audit-svg-layout.mjs`
  reports 0 errors and 0 warnings. The PDF was 2 pages at A3 landscape
  (1191 x 842 pts) and carries the stable ID and catalogue title verbatim.
  `LB-V3-L3-01` moved to `AVAILABLE` in the catalogue and in the L0 atlas.
- 2026-09-17 - Sheet 3 added, `Which Account Does Each Service Connect As`, after
  a review found the document named account types and service replicas but never
  said which account a replica connects as. Adds requirements `L3-01-041` to
  `L3-01-048` and open items `L3-01-O07` and `L3-01-O08`. The PDF is now 3 pages
  at A3 landscape (1191 x 842 pts).
