# Proposed Linebooker V3 Architecture - L3-08 Requirements

This register maintains the requirements represented by
`LB-V3-L3-08 - Multi-Region`.

`LB-V3-L3-08` answers one question: **where is a tenant's data allowed to live,
how does a request reach the permitted cell, what may cross a border, and what
happens when a region fails?** It zooms into `LB-V3-L2-01` requirement `L2-D07`,
which assigns routing, gateway allow-lists, data classifications, high
availability, disaster recovery, backup and recovery per jurisdiction to this
document.

`LB-V3-L3-08` is published as a **directional document**. It records a
first-pass position as at 2026-09-03 so the platform can move. It is not a
settled decision and it is not compliance evidence. Nothing in it is built: the
lab runs one NATS server, one database instance and one region.

The document is drawn as two A3 landscape sheets:

| Sheet | Question |
|---|---|
| 1 - Placement, Routing and the Border Contract | What decides where a tenant lives, how does a request get there, and what may cross a border? |
| 2 - Availability, Recovery and Failure by Jurisdiction | What keeps a cell up, how is it recovered inside its jurisdiction, and what happens when a region or a link is lost? |

## Status vocabulary

- **Included** - visibly represented in the current L3-08 document.
- **Derived** - intentionally delegated to another concern view or to an L4
  detailed design.
- **Open** - requires business, legal, regulatory or technical confirmation.

## Maturity vocabulary

Used by the drawing's tile treatment. This describes the element, not the
document.

- **Built** - running in the Dictionary POC today. Solid tile. No element on
  either L3-08 sheet is built.
- **Proposed** - a directional V3 position with no implementation and no V3 ADR.
  Dashed grey tile.
- **Open** - a position that cannot be taken without a named owner's
  confirmation. Dashed amber tile, with the owner named on the tile.

## L3-08 requirements

| ID | Status | Requirement | L3-08 representation |
|---|---|---|---|
| L3-08-001 | Included | Show that placement is decided by policy for a tenant account, not by where a user signs in. | Sheet 1, placement group, `Tenant placement record` tile and the closing policy statement. |
| L3-08-002 | Included | Show the placement record as the single artefact that names a tenant's permitted cells. | Sheet 1, `Tenant placement record` tile. |
| L3-08-003 | Included | Show country policy as a separate input from tenant policy. | Sheet 1, `Country rule set` tile. |
| L3-08-004 | Included | Show that every data item carries a class, and that the class decides border behaviour. | Sheet 1, `Data class on every item` tile and the data-class group. |
| L3-08-005 | Included | State that a tenant account is one identity in every permitted cell, and that region is not a tenancy axis. | Sheet 1, `One account, several cells` tile. |
| L3-08-006 | Included | Show the routing path from a country endpoint to a cell ingress. | Sheet 1, routing group, four tiles. |
| L3-08-007 | Included | Show that the tenant and its policy are resolved before an ingress is selected. | Sheet 1, `Resolve before routing` tile. |
| L3-08-008 | Included | Show that web assets, the app shell and the micro-frontend registry are served from the same cell as the data. | Sheet 1, `Local web delivery` tile. |
| L3-08-009 | Included | State that a regulated request is never re-routed to another country to recover from an outage. | Sheet 1, `No silent re-routing` tile; Sheet 2, `Cell lost` tile. |
| L3-08-010 | Included | Name the four data classes and give each one an explicit border rule. | Sheet 1, data-class group: platform reference data, tenant business records, regulated country data, operational telemetry. |
| L3-08-011 | Included | Show shared platform reference data as the one class that may be distributed to every cell. | Sheet 1, `Platform reference data` tile. |
| L3-08-012 | Included | Show regulated country data as a class with no border path at all. | Sheet 1, `Regulated country data` tile and the blocked border line. |
| L3-08-013 | Included | Show telemetry as maskable and only crossing under an explicit policy. | Sheet 1, `Operational telemetry` tile. |
| L3-08-014 | Included | Name every path that may cross a border, and show that there is no other one. | Sheet 1, border-contract group, three tiles. |
| L3-08-015 | Included | Show signed policy and account claims as the control-plane path into a cell, carrying no business payload. | Sheet 1, `Signed policy and claims, in` tile. |
| L3-08-016 | Included | Show the gateway as an allow-listed path between shared cells only. | Sheet 1, `Approved subjects, across` tile. |
| L3-08-017 | Included | Show durable history crossing only as a named mirror or source, never by default. | Sheet 1, `Named stream copy, across` tile. |
| L3-08-018 | Included | Show the sovereign border refusing every regulated cross-border path. | Sheet 1, blocked border line to the sovereign cell. |
| L3-08-019 | Included | Show what keeps one cell available before any cross-region mechanism is considered. | Sheet 2, in-cell availability group, four tiles. |
| L3-08-020 | Included | State that three NATS servers and replicated stores are logical minimums, not a sizing statement. | Sheet 2, `NATS, three servers` tile and the `HA` note. |
| L3-08-021 | Included | Show that backups, keys and restores stay inside the jurisdiction that owns the data. | Sheet 2, backup group, four tiles. |
| L3-08-022 | Included | Show a restore rehearsal as a required, dated operational obligation rather than an assumption. | Sheet 2, `Restore rehearsal` tile. |
| L3-08-023 | Included | Show recovery targets per data class, and show them as unset where no owner has agreed one. | Sheet 2, recovery-target group, four tiles, each marked open. |
| L3-08-024 | Included | Show what the platform does for each failure it can suffer at region scale. | Sheet 2, failure group: cell degraded, cell lost, border link lost, control plane unreachable. |
| L3-08-025 | Included | State that the control plane being unreachable stops new claims and policy changes but does not stop a running cell. | Sheet 2, `Control plane unreachable` tile. |
| L3-08-026 | Included | Record what is deliberately not done, with the reason. | Sheet 2, `Deliberately not done` group, three tiles. |
| L3-08-027 | Included | State that no cluster is stretched across a long link. | Sheet 2, `No stretched cluster` tile. |
| L3-08-028 | Included | State that an outage never overrides a residency rule. | Sheet 2, `Residency outranks recovery` tile and the sheet 1 policy statement. |
| L3-08-029 | Included | Mark the document as directional, dated, with a per-item status and a visible missing decision of record. | Both sheets: scope line, `as at` date, per-tile status line, `no V3 ADR` markers. |
| L3-08-030 | Included | Exclude all credential material, keys, endpoints and account identifiers from the drawing. | No key, seed, host name, address or claim body appears on either sheet. |
| L3-08-031 | Derived | Operator, accounts, imports and exports, subject families, streams and messaging failure behaviour. | `LB-V3-L3-05 Messaging / NATS`. |
| L3-08-032 | Derived | Identity federation, authorisation policy, key management, masking rules and security audit. | `LB-V3-L3-09 Security + Identity`. |
| L3-08-033 | Derived | Per-domain data ownership, retention, lineage and document lifecycle. | `LB-V3-L3-07 Data Architecture`. |
| L3-08-034 | Derived | Where each process runs, how it is packaged, scaled and rolled out per cell. | `LB-V3-L3-12 Deployment + Services`. |
| L3-08-035 | Derived | Telemetry pipeline, masking implementation and operational ownership per region. | `LB-V3-L3-10 Observability`. |
| L3-08-036 | Derived | The placement record schema, the gateway allow-list text, backup schedules, retention values and restore procedures. | L4 detailed design. |
| L3-08-037 | Derived | Cloud region names, provider services, network paths and capacity per cell. | L4 detailed design, after a cloud vendor is confirmed. |

## Open confirmations

| ID | Open item | Owner |
|---|---|---|
| L3-08-O01 | The authoritative legal and regulatory controls for Botswana. The sovereign-cell boundary drawn here is a proposal and is not compliance evidence. | Legal |
| L3-08-O02 | Whether South Africa and Australia require gateway connectivity at launch, or whether both cells begin fully independent. Inherited from `L3-05-O01`. | Architecture and business |
| L3-08-O03 | Which subject classes may traverse a gateway, and which tenant accounts may span shared cells. Inherited from `L3-05-O02`. | Architecture and legal |
| L3-08-O04 | Which streams, if any, are mirrored or sourced across a region, and in which direction. Inherited from `L3-05-O03`. | Architecture |
| L3-08-O05 | Recovery point and recovery time targets per data class, per jurisdiction. Nothing is agreed; the drawing shows every target as unset. | Business owner |
| L3-08-O06 | Whether a second cell is required as a recovery site for a non-regulated tenant, or whether in-cell redundancy plus backups is the accepted posture. | Business owner |
| L3-08-O07 | The chosen cloud or hosting provider per country, and whether a compliant in-country region exists for Botswana. | Architecture and procurement |
| L3-08-O08 | Whether operational telemetry may leave a jurisdiction once masked, and what masking is sufficient for each country. | Legal and security |
| L3-08-O09 | Where backups and keys are held for a jurisdiction with no compliant second site, and what that means for the recovery targets in `L3-08-O05`. | Legal and architecture |
| L3-08-O10 | A V3-scoped ADR for multi-region placement, residency and recovery. No decision of record exists today, so nothing here is `Chosen` for V3. | Architecture |

## Notes on evidence

- Nothing on either sheet is implemented. `nats/nats.conf` is a single server in
  a single region, and the compose stack runs one Postgres instance. Every tile
  is therefore drawn as proposed or open, and both sheets say so.
- The cell contents, the sovereign-cell example and the gateway posture are
  carried down from `LB-V3-L2-01` sheet 2 and its register entries L2-015 to
  L2-020, L2-022, L2-023, L2-026 and L2-028.
- `demos/02-multi-region/docs/Multi-Region-Plan.md` remains DRAFT and is the working source
  for the mirror-over-gateway preference recorded on sheet 1.
- Region is a deployment axis. It never appears in a subject token or a
  `{context}` value, so a subject in one cell is byte-identical to its
  counterpart in another. That is why copying data between cells is stream
  replication rather than translation.

## Change log

- 2026-09-03 - Register created alongside the drawn and print editions of
  `LB-V3-L3-08`, tracing to `LB-V3-L2-01` requirement `L2-D07` and to L2
  requirements L2-015 to L2-020, L2-022, L2-023, L2-026 and L2-028, and
  inheriting open items `L3-05-O01` to `L3-05-O03`. Published as a directional
  document as at 2026-09-03.
