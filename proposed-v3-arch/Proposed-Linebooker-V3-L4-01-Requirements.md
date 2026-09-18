# Proposed Linebooker V3 Architecture - L4-01 Requirements

This register maintains the requirements represented by
`LB-V3-L4-01 - Physical Deployment on AWS, Two Regions`.

`LB-V3-L4-01` answers one question: **on AWS, in two regions, where does every
running thing sit, which cluster shape joins the two regions, what does each
shape survive, and what does each shape cost to move a byte?**

It is the first L4 document in the series. Its parent is `LB-V3-L3-08
Multi-Region`. `LB-V3-L3-08` requirement `L3-08-037` delegates cloud region
names, provider services, network paths and capacity per cell to L4 once a cloud
vendor is confirmed. AWS is now the stated vendor, so this document exists.

`LB-V3-L4-01` is published as a **directional document**. It records a
first-pass position as at 2026-09-18 so the platform can move. It is not a
settled decision and it is not a cost forecast. Nothing in it is built.

The document is drawn as three A3 landscape sheets:

| Sheet | Question |
|---|---|
| 1 - Physical Placement, Two Regions | Where does every running thing sit, from the system down to the availability zone? |
| 2 - Cluster Shapes T3, T4 and T5 | Which clusters exist, who votes, and what does each shape survive? |
| 3 - Failure Boundaries and Transfer Cost | Which failure is being bought, and what does each shape cost per byte moved? |

## Status vocabulary

- **Included** - visibly represented in the current L4-01 document.
- **Derived** - intentionally delegated to another concern view or to a later
  L4 detailed design.
- **Open** - requires business, legal, regulatory or technical confirmation.

## Maturity vocabulary

Used by the drawing's tile treatment. This describes the element, not the
document.

- **Built** - running in the Dictionary POC today. Solid tile. No element on any
  L4-01 sheet is built.
- **Proposed** - a directional V3 position with no implementation and no V3 ADR.
  Dashed grey tile.
- **Open** - not yet decided. Dashed amber tile with a named owner.

## The two chains this document draws

The document draws two chains side by side. They are not one tree.

- **Physical placement:** System -> AWS Region -> Cell -> Availability Zone ->
  compute and storage.
- **Runtime:** NATS cluster -> server instance, and service deployment ->
  replica.

The only join between them is the sentence *this instance runs on that compute
in that availability zone*. Five further axes - region, tenant, account, cell
and JetStream domain - are drawn as overlays on the placement, because they
cross and do not nest (ADR-056, `L3-01-010`).

## L4-01 requirements

| ID | Status | Requirement | L4-01 representation |
|---|---|---|---|
| L4-01-001 | Included | The physical chain is System -> AWS Region -> Cell -> Availability Zone -> compute and storage. Every element on the drawing has one stated home in that chain. | Sheet 1 nesting |
| L4-01-002 | Included | A cell is one region's complete runtime (`L3-01-007`). In V3 the cell boundary and the AWS Region boundary are the same boundary. | Sheet 1 cell frame |
| L4-01-003 | Included | The `za` cell runs in `af-south-1`, Africa (Cape Town), which has 3 availability zones. The region is opt-in: an account must enable it before anything can be placed there. | Sheet 1 left region |
| L4-01-004 | Included | The `au` cell runs in `ap-southeast-2`, Asia Pacific (Sydney), which has 3 availability zones and is enabled by default. | Sheet 1 right region |
| L4-01-005 | Included | An availability zone is one or more data centres with their own power, cooling and physical security, joined to sibling zones by private low-latency links. An availability zone is not one building. | Sheet 1 legend |
| L4-01-006 | Included | Zones are named by availability zone ID, for example `afs1-az1`, not by the account-local name `af-south-1a`. Account-local names are shuffled per account; IDs are not. | Sheet 1 zone labels |
| L4-01-007 | Included | The runtime chain is NATS cluster -> server instance, and service deployment -> replica. It is drawn beside the placement chain, not inside it. | Sheet 1 right band |
| L4-01-008 | Included | One NATS cluster per cell. Three server instances per cluster, one per availability zone. Three is the smallest number that keeps a RAFT majority when one zone is lost. | Sheet 1 and sheet 2 |
| L4-01-009 | Included | Cluster is the axis that separates the three shapes. T3 and T4 join clusters with gateways into one supercluster and one meta group. T5 joins clusters with leafnode links and keeps one meta group per cluster. | Sheet 2 |
| L4-01-010 | Included | Three NATS servers across three zones prove NATS availability only. Every other stateful part states its own replica count and zone spread: Postgres primary and standby, object store, Temporal, ingress, Kubernetes nodes, attached volumes, and each service's replicas. | Sheet 1 per-zone rows |
| L4-01-011 | Included | T3 is `za(3) + au(3) + one arbiter instance`: 7 meta voters, majority 4. Losing one region leaves 4 voters, a majority with zero slack. Measured as checks `C1` to `C12` in demo 03. | Sheet 2 column 1 |
| L4-01-012 | Included | T4 is `za(3) + au(3) + arbiter(3)`: 9 meta voters, majority 5. Losing one region leaves 6, one node of slack (`F11`). It survives a region plus one more node and no further (`F12`). | Sheet 2 column 2 |
| L4-01-013 | Included | T5 is `hub(3) + za(3) + au(3)` joined by leafnode links: three separate meta groups of 3, majority 2 each, and one JetStream domain each (`D1`, `D2`). Losing the whole hub leaves both leaves with their own meta leader and still able to create streams (`D10` to `D14`). Nothing replicates between domains by itself (`D15`). | Sheet 2 column 3 |
| L4-01-014 | Included | The three shapes are compared on one basis: voter count, majority, what survives one zone, what survives one region, what still works when the majority is lost, and what the shape costs to move a byte. | Sheet 2 comparison table |
| L4-01-015 | Included | Losing a meta majority blocks changes, not traffic. Creating or editing a stream or consumer returns `10008 JetStream system temporarily unavailable`, and only once the old leader ages out, about 40 seconds (`D03-R5`, `F7a`). Publishing into a stream that already exists keeps working (`F9`). | Sheet 2 footer |
| L4-01-016 | Included | If T5 is chosen, the hub cluster runs in `af-south-1` beside the `za` cell. This is a business decision taken on 2026-09-18. | Sheet 2 column 3 |
| L4-01-017 | Included | A hub placed beside the `za` cell means one region failure takes both the hub and the `za` cell. The `au` leaf keeps its own meta group and its own domain and keeps serving. What is lost is the cross-region path, not the `au` cell. | Sheet 2 and sheet 3 |
| L4-01-018 | Included | The document states three failure boundaries and treats them separately: one availability zone, one AWS Region, one country or jurisdiction. A shape that answers one does not answer the others. | Sheet 3 band 1 |
| L4-01-019 | Included | `ap-southeast-4`, Asia Pacific (Melbourne), 3 zones, opt-in, is recorded as a second AWS Region in Australia and a candidate arbiter home. It is not a cell. | Sheet 3 band 1 |
| L4-01-020 | Included | Sydney and Melbourne are separate AWS Regions in one country. An arbiter in Melbourne answers an AWS Region failure requirement. It does not answer a country failure requirement. | Sheet 3 band 1 |
| L4-01-021 | Included | An arbiter in Melbourne biases the vote toward Australia. In T3 the Australian side then holds 3 + 1 = 4 of 7, which is a majority; the South African side holds 3 and can never reach one. A jurisdiction failure requirement needs a third jurisdiction, not a third region. | Sheet 3 band 1 |
| L4-01-022 | Included | Tenant overlay: which tenants are served from which cell. Tenant is business ownership. It crosses region and does not nest inside it (`L3-01-010`). | Sheet 1 overlay A |
| L4-01-023 | Included | Account overlay: the NATS account is the only machine-enforced wall of the five axes. An account is a wall for data and not a wall for availability (`L3-01-027`). | Sheet 1 overlay B |
| L4-01-024 | Included | JetStream domain overlay: storage identity. T3 and T4 as drawn use one domain across the supercluster. T5 uses one domain per cluster (`D1`, `D2`). | Sheet 1 overlay C, sheet 2 |
| L4-01-025 | Included | Data transfer cost is a design input, not a footnote. Each shape carries the traffic it forces across a zone boundary and across a region boundary. | Sheet 3 band 2 |
| L4-01-026 | Included | Cross-zone rate is `$0.01` per GB, charged at both ends, so a GB that crosses a zone costs `$0.02`. The rate is the same in `af-south-1`, `ap-southeast-2` and `ap-southeast-4`. Traffic that stays inside one zone on private addresses is free. | Sheet 3 rate card |
| L4-01-027 | Included | Cross-region rates are charged on the sending side only; inbound is free. `af-south-1` to `ap-southeast-2` `$0.147`/GB. `ap-southeast-2` to `af-south-1` `$0.098`/GB. `ap-southeast-4` to `af-south-1` `$0.100`/GB. `ap-southeast-2` to and from `ap-southeast-4` `$0.080`/GB. | Sheet 3 rate card |
| L4-01-028 | Included | Cross-zone cost per cell per day is about `D x ((R - 1) + f) x $0.02`, where `D` is GB published per day, `R` is the JetStream replica count, and `f` is the share of reads served from a different zone. With `R = 3` and `f = 0.67` the factor is `2.67`. | Sheet 3 formula |
| L4-01-029 | Included | One cross-border GB costs about seven cross-zone GB (`$0.147` against `$0.02`). The zone spread is cheap. The region crossing is the part worth designing around. | Sheet 3 headline |
| L4-01-030 | Included | The arbiter in T3 and T4 holds no stream assets, so it carries meta-group control traffic only. Its cross-region bill is small and close to constant. Mirrors and gateway message flow are the variable part. | Sheet 3 band 2 |
| L4-01-031 | Included | Demo 03 ran seven topologies as bare `nats-server` processes on one Mac. It measured quorum and NATS behaviour. It did not measure AWS latency, AWS networking, real availability or cost. Every AWS number on these sheets is a published list price or a published region fact, not a measurement. | Sheet 3 evidence note |
| L4-01-032 | Included | No credential, key, endpoint or account identifier appears on any sheet (`L3-08-030`). | All sheets |
| L4-01-033 | Derived | Instance types, node counts, sizing and capacity per cell. | `LB-V3-L3-12` and `L3-08-037` |
| L4-01-034 | Derived | How each process is packaged, scaled and rolled out per cell. | `LB-V3-L3-12` (`L3-08-034`) |
| L4-01-035 | Derived | Placement record schema, gateway allow-list text, backup schedules and restore procedures. | A later L4 (`L3-08-036`) |
| L4-01-036 | Open | Which of the three cluster shapes is chosen. | See `L4-01-O01` |
| L4-01-037 | Open | Which AWS Region holds the arbiter for T3 and T4. | See `L4-01-O02` |
| L4-01-038 | Open | Which failure boundary the business is buying. | See `L4-01-O03` |
| L4-01-039 | Open | Actual GB per day per cell. Every cost figure here is a shape, not a forecast, until this input is given. | See `L4-01-O04` |

## The cost estimate

An initial estimate only. It uses list prices from the AWS price list published
`2026-09-16`, version `20260916132208`. It excludes committed-use discounts and
excludes NAT gateway, Transit Gateway, PrivateLink and VPC endpoint charges,
each of which is billed per hour as well as per GB.

Assumptions: JetStream replica count `R = 3`, one server per zone, and two in
three reads served from a zone other than the stream leader's (`f = 0.67`). The
cross-zone factor is therefore `2.67` GB charged per GB published. A month is 30
days.

| Published per cell per day | Cross-zone, one cell | Mirror all of it Cape Town to Sydney | Mirror all of it Sydney to Cape Town |
|---|---|---|---|
| 1 GB | `$1.60` / month | `$4.41` / month | `$2.94` / month |
| 10 GB | `$16.02` / month | `$44.10` / month | `$29.40` / month |
| 100 GB | `$160.20` / month | `$441.00` / month | `$294.00` / month |

What the estimate says: at these volumes the three-zone spread is cheap enough
that cost is not a reason to collapse it. The region crossing is seven times
dearer per GB, so the design lever that matters is *how much is mirrored*, not
*how many zones are used*.

## Open confirmations

| ID | Open item | Owner |
|---|---|---|
| L4-01-O01 | Which cluster shape is chosen: T3, T4 or T5. No V3 ADR exists (see `L3-08-O10`). | Architecture |
| L4-01-O02 | Which AWS Region holds the arbiter in T3 and T4, and whether a third jurisdiction is required rather than a third region. | Architecture and legal |
| L4-01-O03 | Which failure boundary the platform must survive: one zone, one AWS Region, or one country. This choice decides `L4-01-O01` and `L4-01-O02`. | Business owner |
| L4-01-O04 | Expected GB per day per cell, split into JetStream publishes, consumer reads and mirrored volume. Every cost figure depends on it. | Business and architecture |
| L4-01-O05 | Whether the T5 hub holds durable data of its own or is only a join point. | Architecture |
| L4-01-O06 | Whether the trade between a larger quorum and its transfer cost has an agreed limit, in money per month. | Business owner |
| L4-01-O07 | Zone spread, replica count and failover behaviour for every non-NATS stateful part: Postgres, object store, Temporal, ingress, Kubernetes nodes and attached volumes. | Architecture |
| L4-01-O08 | Whether `af-south-1` and `ap-southeast-4` opt-in enablement is approved for the target AWS accounts. | Architecture and operations |
| L3-08-O02 | Whether gateway connectivity between cells exists at launch. | Architecture |
| L3-08-O03 | Which subjects and payload classes may cross a border. | Legal and architecture |
| L3-08-O04 | Whether mirrors are used, and in which direction. | Architecture |
| L3-08-O05 | Recovery point and recovery time targets per data class. | Business owner |
| L3-08-O06 | Whether a second cell is required as a recovery site for a non-regulated tenant. | Business owner |
| L3-08-O07 | The chosen cloud or hosting provider per country. AWS is the stated vendor for South Africa and Australia; other countries are still open. | Architecture and procurement |
| L3-08-O10 | A V3-scoped ADR for multi-region placement, residency and recovery. | Architecture |
| L3-05-O05 | Replica count per stream per cell. | Architecture |

## Notes on evidence

- Nothing on any sheet is implemented. The lab runs one region.
- The quorum arithmetic for T3, T4 and T5 comes from
  `demos/03-multi-cluster-and-accounts/REPORT.md`, checks `C1` to `C12`, `F1` to
  `F12` and `D1` to `D15`, all passing on `nats-server v2.14.6`. Those checks
  ran as local processes on one machine. They are evidence about NATS, not about
  AWS.
- AWS region and availability zone facts come from the AWS global infrastructure
  region table. Transfer prices come from the AWS `AWSDataTransfer` price list,
  version `20260916132208`.
- `af-south-1` and `ap-southeast-4` are opt-in regions. `ap-southeast-2` is
  enabled by default.
- The five axes and the rule that they cross rather than nest come from ADR-056.
- Cell contents and the border contract are carried down from `LB-V3-L3-08`.
  This document adds placement, cluster shape and cost. It does not restate the
  residency rules.

## Change log

- 2026-09-18 - Register created as the first L4 document in the series, parented
  under `LB-V3-L3-08` on the delegation in `L3-08-037`. Records the AWS vendor
  decision, `af-south-1` and `ap-southeast-2` as the two cells, the Cape Town
  hub decision for T5, the three failure boundaries, the T3, T4 and T5
  comparison traced to demo 03 check IDs, and a first transfer-cost estimate
  from list prices published `2026-09-16`. Inherits open items `L3-08-O02` to
  `L3-08-O07`, `L3-08-O10` and `L3-05-O05`. Published as a directional document
  as at 2026-09-18.
