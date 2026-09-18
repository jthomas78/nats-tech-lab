# Proposed Linebooker V3 Architecture - L3-07 Requirements

This register maintains the requirements represented by
`LB-V3-L3-07 - Data Architecture`.

`LB-V3-L3-07` answers one question: **which store owns each kind of data, how
does one committed change reach the other stores, how long is each kind kept,
and where do reference, localisation, theme and configuration values come
from?** It zooms into `LB-V3-L2-01` requirement `L2-D06`, which assigns
per-domain data ownership, outbox, projections, retention, lineage and document
lifecycle to this document. It also receives `L3-08-033`, which delegates
per-domain data ownership, retention, lineage and document lifecycle here from
`LB-V3-L3-08 Multi-Region`.

`LB-V3-L3-07` is a **mixed-maturity document**. Unlike `LB-V3-L3-08`, several
elements on these sheets are built and running in the Dictionary POC today: the
service-owned PostgreSQL database, the JetStream fact log, the KV write-through
cache in front of a projection, the reference-data service and its
context-scoped lookup. Every element that is *not* built is a **directional V3
position as at 2026-09-04**, carries a per-tile status, and is not a settled
decision. Retention, lineage and lifecycle in particular have no agreed values
and no V3 decision of record.

The document is drawn as two A3 landscape sheets:

| Sheet | Question |
|---|---|
| 1 - Store Roles, Ownership and the Write Path | Which store owns what, who may write it, and how does one commit become a fact, a read model and a cache entry? |
| 2 - Reference Data, Distribution and Lifecycle | Where do shared values come from, how does a change reach every reader, and how long is each kind of data kept? |

## Status vocabulary

- **Included** - visibly represented in the current L3-07 document.
- **Derived** - intentionally delegated to another concern view or to an L4
  detailed design.
- **Open** - requires business, legal, regulatory or technical confirmation.

## Maturity vocabulary

Used by the drawing's tile treatment. This describes the element, not the
document.

- **Built** - running in the Dictionary POC today. Solid tile.
- **Proposed** - a directional V3 position with no implementation and no V3 ADR,
  or an ADR that has not been applied. Dashed grey tile.
- **Open** - a position that cannot be taken without a named owner's
  confirmation. Dashed amber tile, with the owner named on the tile.

## L3-07 requirements

| ID | Status | Requirement | L3-07 representation |
|---|---|---|---|
| L3-07-001 | Included | Name every store by its role, never by product alone. | Sheet 1, store-role group; each tile leads with the role and names the technology on its second line. |
| L3-07-002 | Included | Keep transactional truth, durable fact history, workflow state, derived read models and evidence documents as five separate stores. | Sheet 1, store-role group, five tiles. |
| L3-07-003 | Included | Show service-owned PostgreSQL as the default transactional source of truth. | Sheet 1, `Transactional truth` tile, drawn as built. |
| L3-07-004 | Included | Show the JetStream fact log as durable history with limits retention so it can be replayed. | Sheet 1, `Durable fact log` tile, drawn as built. |
| L3-07-005 | Included | Show Temporal persistence as workflow state and not as a business record store. | Sheet 1, `Workflow state` tile. |
| L3-07-006 | Included | Show the read model as a projection table with a cache in front of it, and state that it is rebuildable. | Sheet 1, `Derived read model` tile; Sheet 2, `Read model` retention tile. |
| L3-07-007 | Included | Show evidence documents in a vendor-neutral repository behind an adapter, not as a default NATS Object Store. | Sheet 1, `Evidence documents` tile, drawn as proposed. |
| L3-07-008 | Included | State that one service owns its database, with its own schema and its own role. | Sheet 1, `Database per service` tile. Governed by ADR-052. |
| L3-07-009 | Included | State that PostgreSQL infrastructure is shared by default and isolation is logical, not physical. | Sheet 1, `Shared instance by default` tile. Governed by ADR-053. |
| L3-07-010 | Included | State that an entity identifier is minted by the service, never by the database. | Sheet 1, `The service mints the ID` tile. Governed by ADR-051. |
| L3-07-011 | Included | State that one service is the only writer of its own records, and every other service is told rather than allowed to write. | Sheet 1, `One writer per record` tile. |
| L3-07-011a | Included | Refuse direct database access from outside the owning service, by another service, a report or a script. | Sheet 1, red panel. |
| L3-07-012 | Included | Show the write path as one ordered sequence from an accepted command to a warmed cache. | Sheet 1, write-path group, four tiles, read left to right. |
| L3-07-013 | Included | Show that a domain rule is checked before anything is committed. | Sheet 1, `Command accepted` tile. |
| L3-07-014 | Included | Show state and the fact it produced being committed together, so a published fact can never contradict the record. | Sheet 1, `Commit with an outbox` tile, drawn as proposed. |
| L3-07-015 | Included | Show the fact published on the owning service's own stream. | Sheet 1, `Fact published` tile. |
| L3-07-016 | Included | Show one projector writing the projection and overwriting the cache entry in the same handler. | Sheet 1, `Projector writes both` tile, drawn as built. |
| L3-07-017 | Included | Show the read path as cache first, projection on a miss, and the owning service as the only answerer. | Sheet 1, read-path group. |
| L3-07-018 | Included | Show governed extraction as the only route to data for anything outside the platform. | Sheet 1, `Governed extraction` tile. Carries L2-013. |
| L3-07-019 | Included | State that history is kept only where replay is a domain concern, and plain current-state storage is used otherwise. | Sheet 1, amber panel; Sheet 2, `No universal event sourcing` tile. Carries L2-014. |
| L3-07-020 | Included | Show reference data resolved through three layers: platform, tenant and organisation. | Sheet 2, reference-data group, three layer tiles. |
| L3-07-021 | Included | State that every reference-data lookup is context-scoped and that no unscoped global lookup exists. | Sheet 2, `Context-scoped key` tile. |
| L3-07-022 | Included | Show that reference data serves more than code lists: localisation, translation, theme values and configuration. | Sheet 2, `What it serves` group, five tiles. |
| L3-07-023 | Included | Show localisation values as reference data owned by this concern view. | Sheet 2, `Localisation values` tile, drawn as built. |
| L3-07-024 | Included | Show enumeration and string translations as one keyed set served in many languages. | Sheet 2, `Enum and string translations` tile, drawn as built. |
| L3-07-025 | Included | Show App Shell theme values as **stored and served** by this view, and name `LB-V3-L3-03` as the view that consumes and renders them. | Sheet 2, `App Shell theme values` tile and its `rendering is L3-03` line. |
| L3-07-026 | Included | Show a centralised configuration store for platform and tenant settings. | Sheet 2, `Configuration store` tile, drawn as proposed. |
| L3-07-027 | Included | Show how a changed reference value reaches every reader without polling. | Sheet 2, distribution group: change published, cache per cell, watched by readers. |
| L3-07-028 | Included | Show reference data as the one class that may be copied to every cell. | Sheet 2, `Copied to every cell` tile, cross-referencing `LB-V3-L3-08`. |
| L3-07-029 | Included | Show a version stamp so a reader can tell which reference-data version it is holding. | Sheet 2, `Version stamped` tile, drawn as open. |
| L3-07-030 | Included | State retention per store role, and show every unagreed retention value as unset. | Sheet 2, retention group, five tiles, four of them open. |
| L3-07-031 | Included | Show the durable fact log as the lineage record: what changed, when, and in what order. | Sheet 2, `Durable fact log` retention tile. |
| L3-07-032 | Included | Show the evidence document lifecycle as write-once, with the blob stored before the record that points at it. | Sheet 2, `Evidence documents` retention tile. Carries ADR-048's write-once ordering. |
| L3-07-033 | Included | Show tenant exit as an explicit data obligation: export, then delete. | Sheet 2, `Tenant exit` tile, drawn as open. |
| L3-07-034 | Included | Record what is deliberately not done, with the reason. | Sheet 2, `Deliberately not done` group, three tiles. |
| L3-07-035 | Included | State that reference data is looked up, never forked into a copy inside a consuming service. | Sheet 2, `No forked reference copy` tile. |
| L3-07-036 | Included | Mark every element with a maturity, and mark every unsettled V3 position as directional, dated, with a visible missing decision of record. | Both sheets: scope line, `as at` date, per-tile status line, `no V3 ADR` markers. |
| L3-07-037 | Included | Exclude all credential material, connection strings, host names and schema listings from the drawing. | No credential, DSN, host, table definition or column list appears on either sheet. |
| L3-07-038 | Derived | Subject families, stream configuration, consumer types, accounts and grants that carry a fact between services. | `LB-V3-L3-05 Messaging / NATS`. |
| L3-07-039 | Derived | The App Shell's consumption and rendering of theme values, locale selection in the browser, and plugin contribution points. | `LB-V3-L3-03 Application / MFE`. |
| L3-07-040 | Derived | Where a store physically sits, what may cross a border, backup, keys and recovery targets per jurisdiction. | `LB-V3-L3-08 Multi-Region`. |
| L3-07-041 | Derived | Encryption at rest, masking rules, access policy and data-protection audit. | `LB-V3-L3-09 Security + Identity`. |
| L3-07-042 | Derived | Workflow boundaries, signals, activities and compensation that produce the workflow state shown here. | `LB-V3-L3-06 Workflow / Temporal`. |
| L3-07-043 | Derived | The contracts and schemas of the governed extraction API, and export scheduling. | `LB-V3-L3-04 External Integration`. |
| L3-07-044 | Derived | Which capability owns which business record, and the domain dependency map. | `LB-V3-L3-02 Functional Domains`. |
| L3-07-045 | Derived | The whole reference-data service told as one story: its schema, seeding, subjects, endpoints and admin surface. | L4 detailed design under `LB-V3-L3-07`. Not yet numbered. |
| L3-07-046 | Derived | Table and column design, migration policy, index strategy, outbox table shape and projector rebuild procedure. | L4 detailed design. |

## Open confirmations

| ID | Open item | Owner |
|---|---|---|
| L3-07-O01 | Retention periods per store role and per data class. Nothing is agreed; the drawing shows every period as unset. | Business owner and legal |
| L3-07-O02 | How long the durable fact log is kept before its oldest facts are aged out, and whether any domain requires an unbounded log. | Architecture and legal |
| L3-07-O03 | The statutory retention rule for evidence documents per country, which decides the document lifecycle drawn here. | Legal |
| L3-07-O04 | The tenant-exit obligation: what must be exported, in what form, and how deletion is evidenced. | Legal and business owner |
| L3-07-O05 | Whether the transactional outbox is adopted platform-wide, or only for the services whose facts other services depend on. No V3 ADR exists. | Architecture |
| L3-07-O06 | Whether the centralised configuration store is a reference-data responsibility or a separate control-plane capability. | Architecture |
| L3-07-O07 | Whether reference data carries a version stamp that consumers assert against, and what a consumer does when it holds a stale version. | Architecture |
| L3-07-O08 | Whether the organisation layer of reference data is in scope for V3, or whether platform and tenant layers are sufficient at launch. | Business owner |
| L3-07-O09 | The document storage provider per country and whether one S3-compatible implementation serves every cell. Inherited from the L2 open confirmations. | Architecture and procurement |
| L3-07-O10 | A V3-scoped ADR for the write path - commit, outbox, fact, projection and cache - as one platform pattern. Only the lab shape exists today. | Architecture |

## Notes on evidence

- **Built and running in the lab today:** service-owned PostgreSQL as
  transactional truth; the `SHIPPING` and `REFDATA` JetStream streams under
  `LimitsPolicy` so they can be replayed; a projection table with an eager
  write-through NATS KV cache in front of it, written by the same handler; the
  reference-data service with context-scoped lookup, localisation and
  translations; Temporal persistence for the transporter vetting saga; and the
  `organizations-docs` NATS Object Store with blob-before-record ordering.
- **Not built:** the transactional outbox, the vendor-neutral document adapter,
  the organisation layer of reference data, the centralised configuration store,
  the version stamp, and every retention value.
- ADR-052 (one PostgreSQL instance, a database and a role per service) and
  ADR-051 (the service mints a ULID identifier) are applied in the lab. ADR-053
  (shared PostgreSQL instances by default) is V3-scoped and accepted but not yet
  applied to any V3 deployment.
- ADR-048 records the lab's choice of the NATS Object Store for documents, with
  write-once, blob-before-record ordering. `L2-012` deliberately does **not**
  carry that choice into V3 as the default; the drawing therefore shows the
  repository behind an adapter.
- The App Shell theme split follows the agreed placement recorded on
  2026-09-04: storing and serving theme values is a data concern and belongs
  here; consuming and rendering them is an application concern and belongs to
  `LB-V3-L3-03`.
- The reference-data layering shown on sheet 2 is the platform / tenant /
  organisation model already used in the lab, where `_platform` and
  `_default_bu` are reserved context roots.

## Change log

- 2026-09-04 - Register created alongside the drawn and print editions of
  `LB-V3-L3-07`, tracing to `LB-V3-L2-01` requirement `L2-D06` and to L2
  requirements L2-011 to L2-014, and receiving `L3-08-033` from
  `LB-V3-L3-08 Multi-Region`. Records the agreed split of App Shell theming
  between this view and `LB-V3-L3-03`.
