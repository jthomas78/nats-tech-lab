# Proposed Linebooker V3 Architecture - L3-05 Requirements

This register maintains the requirements represented by
`LB-V3-L3-05 - Messaging / NATS`.

`LB-V3-L3-05` answers one question: **how does the messaging layer keep tenants
apart, where do its servers and durable state sit, and what may cross a
boundary?** It zooms into `LB-V3-L2-01` requirement `L2-D04`, which assigns
operator and accounts, signing keys, imports and exports, subjects, streams,
gateways and failure behaviour to this document.

The document is drawn as two A3 landscape sheets:

| Sheet | Question |
|---|---|
| 1 - Trust, Accounts and the Cross-Account Contract | How is messaging divided, and what is the only path between two accounts? |
| 2 - Server Topology, Durable State and Failure Behaviour | Where do the servers run, what durable state do they hold, and what happens when a link breaks? |

## Status vocabulary

- **Included** - visibly represented in the current L3-05 document.
- **Derived** - intentionally delegated to another concern view or to an L4
  detailed design.
- **Open** - requires business, legal, regulatory or technical confirmation.

## Maturity vocabulary

Used by the drawing's solid and dashed tile treatment. This describes the
element, not the document.

- **Built** - running in the Dictionary POC today. Solid tile.
- **Proposed** - a directional V3 position with no implementation and no V3 ADR.
  Dashed tile.

## L3-05 requirements

| ID | Status | Requirement | L3-05 representation |
|---|---|---|---|
| L3-05-001 | Included | Show the operator as the single trust root, and show that it signs account claims rather than user claims directly. | Sheet 1, trust chain group, `Operator` tile. |
| L3-05-002 | Included | Show that each account signs its own users, so a compromised account cannot mint another account's users. | Sheet 1, `Account claim` and `User credential` tiles. |
| L3-05-003 | Included | Show that a NATS server holds no list of accounts, and that account claims are served from a resolver. | Sheet 1, `Account resolver` tile and its `no account list in the server config file` line. |
| L3-05-004 | Included | Show that an account can be added or revoked while the platform runs, with no server restart. | Sheet 1, `Claim push` tile and `Tenant account, next` tile. |
| L3-05-005 | Included | State that the account, not the subject, is the tenant isolation boundary. | Sheet 1, account plane group title, `ACCOUNT` note and the closing policy statement. |
| L3-05-006 | Included | Distinguish the three standing account roles from the per-tenant account. | Sheet 1, four account tiles: system, platform, tenant, tenant-next. |
| L3-05-007 | Included | Show the system account as administrative only, carrying no business traffic. | Sheet 1, `System account` tile. |
| L3-05-008 | Included | Name every subject family the platform speaks in, and say for each whether it may cross an account boundary. | Sheet 1, subject families group, five tiles. |
| L3-05-009 | Included | State that `{context}` is a company or business unit, and that it is neither the tenant nor the region. | Sheet 1, `CONTEXT` note and the closing policy statement. |
| L3-05-010 | Included | Show export and import as the only path between two accounts. | Sheet 1, cross-account contract group. |
| L3-05-011 | Included | Show the shared reference data path as one corpus imported by each tenant, not a copy per tenant. | Sheet 1, `Reference data, down` tile. |
| L3-05-012 | Included | Show that the server, not the client, writes the tenant name onto an imported or exported subject. | Sheet 1, `Telemetry and stream views, up` tile. |
| L3-05-013 | Included | Show that stream write operations are never exported, so a cross-account view is read-only. | Sheet 1, `Everything else is refused` tile. |
| L3-05-014 | Included | State that isolation is enforced by the server rather than by application code. | Sheet 1, closing policy statement. |
| L3-05-015 | Included | Show the regional cell as the repeatable unit of messaging runtime. | Sheet 2, `Inside one regional cell` group. |
| L3-05-016 | Included | Show three servers as the logical availability minimum, and state that this is not a sizing statement. | Sheet 2, `Three NATS servers` tile and `HA` note. |
| L3-05-017 | Included | Show the JetStream domain as a marker of a cell, explicitly not a tenancy boundary. | Sheet 2, `JetStream store` tile and `DOMAIN` note. |
| L3-05-018 | Included | Show the browser entry path as a web socket carrying a short-lived token, never a credentials file. | Sheet 2, `Web socket entry` tile; Sheet 1, `BROWSER` note. |
| L3-05-019 | Included | Show the monitoring endpoint as an operator-facing surface, not a public one. | Sheet 2, `Monitoring endpoint` tile. |
| L3-05-020 | Included | Show leaf nodes at a site, and say why a leaf is used instead of a plain client. | Sheet 2, edge group, four tiles. |
| L3-05-021 | Included | Show the durable state each account holds, separated by role. | Sheet 2, durable state group: log, change feed, observability, key-value, object store. |
| L3-05-022 | Included | State that streams keep a limits retention policy so history can be replayed. | Sheet 2, `Shipping log` tile and `REPLAY` note. |
| L3-05-023 | Included | State that observability state is capped and may be dropped under load, so it is never on a business path. | Sheet 2, `Observability` tile; Sheet 1, `obs.` tile. |
| L3-05-024 | Included | Show a gateway as a join between two clusters, not a single cluster stretched across a long link. | Sheet 2, `Gateway` tile and `GATEWAY` note. |
| L3-05-025 | Included | State that durable history crosses a region only as a named mirror or source, and never by default. | Sheet 2, `Mirror or source` tile and the cyan copy relationship. |
| L3-05-026 | Included | State that every cell must trust one operator and one resolver, so an account keeps one identity everywhere. | Sheet 2, `One operator, one resolver` tile. |
| L3-05-027 | Included | Show what the platform does when a link breaks, for each kind of link drawn. | Sheet 2, failure behaviour group, four tiles. |
| L3-05-028 | Included | Distinguish what is built today from what is proposed, on the face of the drawing. | Solid and dashed tile treatment, with a legend entry on both sheets. |
| L3-05-029 | Included | Exclude all credential material from the drawing. | No key, seed, account identifier, claim body or credentials-file content appears on either sheet. |
| L3-05-030 | Derived | Permission claim text, identity federation, relationship authorisation, policy and audit. | `LB-V3-L3-09 Security and Identity`. |
| L3-05-031 | Derived | Data residency, country rules, recovery point and recovery time targets. | `LB-V3-L3-08 Regions and Residency`. |
| L3-05-032 | Derived | Where each service process runs and how it is packaged. | `LB-V3-L3-12 Deployment and Services`. |
| L3-05-033 | Derived | The exact subject list, stream and consumer settings, bucket settings and per-account grant text. | L4 detailed design. |
| L3-05-034 | Derived | Server sizing, storage sizing and version pinning. | L4 detailed design. |

## Open confirmations

| ID | Open item |
|---|---|
| L3-05-O01 | Whether the South African and Australian cells require gateway connectivity at all, or whether each cell stays independent. |
| L3-05-O02 | Which subject classes may traverse a gateway. Only core subjects are drawn as candidates; the allow-list itself is not decided. |
| L3-05-O03 | Which streams, if any, are mirrored or sourced across a region, and in which direction. |
| L3-05-O04 | Whether leaf nodes are adopted at depots, warehouses, ports and fleet gateways, and what subject scope a site credential carries. |
| L3-05-O05 | The JetStream replica count per cell. Three is drawn as the proposal; one is what the lab runs. |
| L3-05-O06 | A V3-scoped ADR for NATS, JetStream and NATS KV. Only lab-scoped ADRs exist today, so no selection here is `Chosen` for V3. |

## Notes on evidence

- Sheet 1 is drawn from the Dictionary POC as built: an operator with a signing
  key, four accounts each with its own signing key, run-time tenant account
  creation over the system claims API, and the exact export and import shape
  shown, including subject remapping in both directions.
- Sheet 2 is mixed. The cell contents, durable state and consumer restart
  behaviour are built. Clustering, gateways, mirrors, sources and leaf nodes are
  not built anywhere in the repository; they are drawn as proposed and marked
  dashed.
- `demos/02-multi-region/docs/Multi-Region-Plan.md` remains DRAFT and is the working source
  for `L3-05-O01` to `L3-05-O03`.

## Change log

- 2026-09-03 - Register created alongside the drawn and print editions of
  `LB-V3-L3-05`, tracing to `LB-V3-L2-01` requirement `L2-D04` and to L2
  requirements L2-006, L2-016, L2-017, L2-018, L2-023 and L2-026.
