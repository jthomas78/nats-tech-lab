# Proposed Linebooker V3 Architecture - L2-02 Requirements

This register maintains the requirements represented by
`LB-V3-L2-02 - Technology Selection and Rationale`.

`LB-V3-L2-02` is a **directional document**. It records the first-pass technology
position for V3 so the platform can move. It is not a record of settled
decisions. The decision of record for any selection is its ADR; where no ADR
exists, the card says so and that gap is a to-do.

`LB-V3-L2-01` answers how the platform is constructed. `LB-V3-L2-02` answers
which technologies were selected and why. They are separate documents because
they answer different questions, not because they cover different concerns.

## Status vocabulary

- **Included** - visibly represented in the current L2-02 document.
- **Derived** - intentionally delegated to an L3/L4 concern view or to an ADR.
- **Open** - requires business, legal, regulatory or technical confirmation.

## Selection status vocabulary

Used by the **Status** field on each card. This describes the selection, not the
document.

- **Chosen** - an accepted V3 decision with an ADR of record.
- **Proposed** - a directional first-pass position with no V3 ADR yet.
- **Proven in lab** - exercised in the Dictionary POC, not yet accepted for V3.

## L2-02 requirements

| ID | Status | Requirement | L2-02 representation |
|---|---|---|---|
| L2-02-001 | Included | State that the document is directional and give the date its position is stated as at. | Scope line and header date stamp. |
| L2-02-002 | Included | Answer "which technologies are we using" as an inventory, without relational context. | Card catalogue; no connectors are drawn. |
| L2-02-003 | Included | Answer "why" for every technology listed. | Solves and Relies on fields on each card. |
| L2-02-004 | Included | Name the problem each technology solves, not only its features. | Solves field, expressed as a Linebooker problem. |
| L2-02-005 | Included | Name the features actually relied on, so a substitute can be assessed. | Relies on field. |
| L2-02-006 | Included | Record what was rejected, so a reader knows the choice was a choice. | Instead of field. |
| L2-02-007 | Included | Give every selection an explicit selection status. | Status chip using the vocabulary above. |
| L2-02-008 | Included | Point at the decision of record, and show it as missing where there is none. | Decision field carrying an ADR number or `no V3 ADR yet`. |
| L2-02-009 | Included | Trace every card back to the L2-01 elements that depend on it. | Used by field naming L2-01 element titles. |
| L2-02-010 | Included | Group cards by the plane they serve, using the same colours as L2-01. | Four plane groups; colour meaning is unchanged from L2-01. |
| L2-02-011 | Included | Distinguish a lab decision from a V3 decision. | `Proven in lab` status and lab-scoped ADR references marked as such. |
| L2-02-012 | Included | Do not brand or select a cloud vendor. | No cloud vendor card; object storage is listed as an S3-compatible API contract. |
| L2-02-013 | Included | Identify a store by its role, never by product alone. | Each store card leads with its role in the Realises field. |
| L2-02-014 | Open | Confirm the V3 messaging, workflow and data selections through V3-scoped ADRs. | Thirteen of the sixteen sheet 1 cards show `no V3 ADR yet`. |
| L2-02-015 | Open | Confirm the identity provider commercially before treating WorkOS as chosen. | WorkOS card is `Proposed`. |
| L2-02-016 | Derived | Version pinning, sizing, high-availability topology and operational runbooks. | L3/L4 concern views. |
| L2-02-017 | Derived | Per-technology configuration, subjects, schemas and deployment specifications. | L4 detailed designs. |
| L2-02-018 | Derived | The full argument for each selection. | The ADR named on the card. |
| L2-02-019 | Included | Separate technologies the platform is built on from services it buys or integrates with. | Sheet 1 platform selections; sheet 2 external services. |
| L2-02-020 | Included | State every third-party dependency observed in the V2 codebase, using the same card shape as sheet 1. | Sixteen sheet 2 cards drawn from a read of the V2 repository. |
| L2-02-021 | Included | Mark an inherited dependency as inherited rather than selected. | `carried from V2` marker; every sheet 2 card is `Proposed`, none is `Chosen`. |
| L2-02-022 | Included | Record what V2 technology is deliberately not carried into V3. | The `Not carried into V3` card closing sheet 2. |
| L2-02-023 | Included | Name a relationship-based authorisation selection distinct from authentication. | OpenFGA card, separate from the WorkOS identity federation card. |
| L2-02-024 | Included | Show an unmade infrastructure decision as an open card rather than omitting it. | `Open` status on the container platform, cloud provider and build pipeline cards. |
| L2-02-025 | Included | Exclude endpoints, credentials, keys and account identifiers from the sheet. | No vendor endpoint, key, project or account identifier appears on sheet 2. |
| L2-02-026 | Open | Re-tender or confirm each inherited external service for V3, and resolve the two duplicate second-factor services. | Every sheet 2 card is `Proposed` with `no V3 ADR yet`. |

## Open confirmations

- Only `ADR-053` is V3-scoped. `ADR-046` to `ADR-052` are lab-scoped and do not
  by themselves accept a technology for V3.
- The V3 ADRs for NATS, JetStream, NATS KV, Temporal, object storage, identity
  federation, relationship authorisation, Module Federation, OpenTelemetry and
  the service language have not been written. Until they are, those cards remain
  `Proposed`.
- The container platform, cloud provider and build pipeline are genuinely
  undecided and carry an `Open` status rather than a proposal.
- No external service on sheet 2 has been re-tendered for V3. Card payments,
  bank transfer, the voucher retailer and most telematics vendors are South
  Africa only and need a regional equivalent for an Australian cell.
- Two services offer a second factor. Retaining both the identity provider and
  the verification service is a cost and support decision that has not been made.

## Change log

- 2026-09-03 - Register created with `LB-V3-L2-02` as a directional card
  catalogue of technology selections and their rationale.
- 2026-09-03 - Added `L2-02-019` to `L2-02-026`. `LB-V3-L2-02` became a
  two-sheet document: sheet 1 platform selections (now sixteen cards, adding
  relationship authorisation and three open infrastructure decisions), sheet 2
  external services and integrations observed in the V2 codebase.
