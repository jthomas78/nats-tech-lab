# Proposed Linebooker V3 Architecture - Operational Authority

## Purpose

This document is the central operational authority for the Proposed Linebooker
V3 architecture document set. It defines how architecture documents are
identified, scoped, related, governed, validated and published.

All L0-L4 architecture documents branch from this authority. The L0 Architecture
Atlas is the graphical navigation view of the catalogue maintained here; it is
not a competing source of truth.

## Authority and precedence

When sources disagree, use this order:

1. The user's explicit instruction for the current change.
2. This operational authority.
3. The applicable level requirements register.
4. The current HTML and PDF architecture artefacts.
5. Architecture discussions, research notes, historical V2/V3 documents and
   session memory.

`CLAUDE.md` routes agents to this document. The
`linebooker-architecture-documenter` skill implements the workflow and must not
redefine this authority. The `html-diagram-drawer` skill governs drawing
mechanics and visual quality.

## Architecture hierarchy

Each document answers one primary architectural question. A lower level must be
a recognizable zoom into a parent concept rather than an unrelated view.

| Level | Audience | Purpose | Include | Exclude |
|---|---|---|---|---|
| L0 | Anyone navigating the document set | Architecture Atlas and Diagram Sitemap | Document hierarchy, stable IDs, titles, status and navigation | System design and implementation detail |
| L1 | Everybody, technical and non-technical, inside and outside Linebooker | System and Platform Overview | People, participant organisations, external systems, experiences, major business capabilities, platform concerns and country/region placement | Service internals, protocols, schemas and deployment mechanics |
| L2 | Architects, technical leads and operations | Logical and Technical Architecture | Application, integration, control, domain, messaging, workflow, data, service and regional/deployment structure | Message subjects, payload schemas, detailed permissions and workflow steps |
| L3 | Specialists in the single concern being viewed | Focused concern view | One concern such as tenancy, domains, MFE, integration, NATS, Temporal, data, regions, security, observability, finance or deployment/services | Unrelated concerns added for completeness |
| L4 | Implementers of that design | Detailed design | Implementable contracts, subjects/streams, workflows, schemas, APIs/adapters, permissions and deployment specifications | A repetition of the full system landscape |

Audience is the deciding test when a document's detail is disputed. If an
element cannot be explained to that level's audience, it belongs lower. If the
audience would not care, it belongs lower. Do not create a level that adds no
value for its audience merely to complete the set.

The hierarchy currently branches as follows:

```text
Architecture Operational Authority
|
+-- LB-V3-L0-01  Architecture Atlas
|
`-- LB-V3-L1-01  System and Platform Overview
    |
    +-- LB-V3-L2-01  Logical and Technical Architecture
    |   |
    |   +-- LB-V3-L3-01  Participant + Tenancy
    |   +-- LB-V3-L3-02  Functional Domains
    |   +-- LB-V3-L3-03  Application / MFE
    |   +-- LB-V3-L3-04  External Integration
    |   +-- LB-V3-L3-05  Messaging / NATS
    |   +-- LB-V3-L3-06  Workflow / Temporal
    |   +-- LB-V3-L3-07  Data Architecture
    |   +-- LB-V3-L3-08  Multi-Region
    |   +-- LB-V3-L3-09  Security + Identity
    |   +-- LB-V3-L3-10  Observability
    |   +-- LB-V3-L3-11  Financial Architecture
    |   `-- LB-V3-L3-12  Deployment + Services
    |
    `-- LB-V3-L2-02  Technology Selection and Rationale
```

Two documents sit at L2 because they answer different questions, not because
they cover different concerns. `LB-V3-L2-01` answers how the platform is
constructed; `LB-V3-L2-02` answers which technologies were selected and why.
Splitting L2 by concern is not permitted - that is what L3 is for.

L4 detailed designs are registered beneath the most relevant L3 view when their
scope is defined. Their parent must be recorded in the catalogue.

## Element vocabulary by level

Each level draws from a fixed set of element kinds. Needing a kind that is not
listed is the signal that the content belongs at another level, not a reason to
widen the level.

| Level | Permitted element kinds |
|---|---|
| L0 | Catalogue node, level group, navigation edge |
| L1 | Human actor, business participant organisation, tenant authority, application experience, business capability, external system, country/region placement, platform concern |
| L2 | Experience channel, integration/ingress boundary element, control-plane capability, domain service, messaging or workflow platform element, store identified by role, deployment cell, policy catalogue, technology selection card |
| L3 | The concern's own vocabulary, plus any L2 element it zooms into. Every element must be traceable to a parent L2 element |
| L4 | Implementable artefact: subject, stream, workflow definition, schema, endpoint, adapter, permission claim, deployment specification |

A store is always identified by its role, never by product alone: transactional
truth, durable fact history, workflow state, derived read model, or evidence
document repository.

## Stable identity and lifecycle

- Use `LB-V3-L<level>-<two-digit sequence>` identifiers.
- Reuse IDs already reserved in this catalogue. Never renumber an existing
  document to close a gap.
- Document status describes artefact readiness, not whether every architectural
  decision inside the document has received business, legal or regulatory
  approval.
- Use only these catalogue statuses:
  - `PLANNED`: catalogued, but no reviewable document exists.
  - `DRAFT`: reviewable HTML exists, but the document is not accepted or its
    required output set is incomplete.
  - `AVAILABLE`: matching HTML and PDF exist, contain the same exact document ID
    and title, and have passed the required validation.
  - `SUPERSEDED`: retained for traceability and linked to its replacement.
- Do not use `CURRENT` as a status.

## Canonical catalogue

| ID | Level | Title | Parent | Status | Requirements |
|---|---|---|---|---|---|
| LB-V3-L0-01 | L0 | Architecture Atlas | Authority | AVAILABLE | This authority |
| LB-V3-L1-01 | L1 | System and Platform Overview | Authority | AVAILABLE | [L1 requirements](Proposed-Linebooker-V3-L1-Requirements.md) |
| LB-V3-L2-01 | L2 | Logical and Technical Architecture | LB-V3-L1-01 | AVAILABLE | [L2 requirements](Proposed-Linebooker-V3-L2-Requirements.md) |
| LB-V3-L2-02 | L2 | Technology Selection and Rationale | LB-V3-L1-01 | DRAFT | [L2-02 requirements](Proposed-Linebooker-V3-L2-02-Requirements.md) |
| LB-V3-L3-01 | L3 | Participant + Tenancy | LB-V3-L2-01 | PLANNED | Create when scoped |
| LB-V3-L3-02 | L3 | Functional Domains | LB-V3-L2-01 | PLANNED | Create when scoped |
| LB-V3-L3-03 | L3 | Application / MFE | LB-V3-L2-01 | PLANNED | Create when scoped |
| LB-V3-L3-04 | L3 | External Integration | LB-V3-L2-01 | PLANNED | Create when scoped |
| LB-V3-L3-05 | L3 | Messaging / NATS | LB-V3-L2-01 | PLANNED | Create when scoped |
| LB-V3-L3-06 | L3 | Workflow / Temporal | LB-V3-L2-01 | PLANNED | Create when scoped |
| LB-V3-L3-07 | L3 | Data Architecture | LB-V3-L2-01 | PLANNED | Create when scoped |
| LB-V3-L3-08 | L3 | Multi-Region | LB-V3-L2-01 | PLANNED | Create when scoped |
| LB-V3-L3-09 | L3 | Security + Identity | LB-V3-L2-01 | PLANNED | Create when scoped |
| LB-V3-L3-10 | L3 | Observability | LB-V3-L2-01 | PLANNED | Create when scoped |
| LB-V3-L3-11 | L3 | Financial Architecture | LB-V3-L2-01 | PLANNED | Create when scoped |
| LB-V3-L3-12 | L3 | Deployment + Services | LB-V3-L2-01 | PLANNED | Create when scoped |

### Published artefacts

- `LB-V3-L0-01`
  - [Editable HTML](../../../../demos/01-dictionary/diagrams/proposed-linebooker-v3-l0-architecture-atlas.html)
  - [Final PDF](<../../../../output/pdf/Proposed Linebooker V3 Architecture - L0 Architecture Atlas.pdf>)
- `LB-V3-L1-01`
  - [Editable HTML](../../../../demos/01-dictionary/diagrams/proposed-linebooker-v3-l1-system-platform-overview.html)
  - [Final PDF](<../../../../output/pdf/Proposed Linebooker V3 Architecture - L1 System and Platform Overview.pdf>)
- `LB-V3-L2-01`
  - [Editable HTML](../../../../demos/01-dictionary/diagrams/proposed-linebooker-v3-l2-logical-technical-architecture.html)
  - [Final PDF](<../../../../output/pdf/Proposed Linebooker V3 Architecture - L2 Logical and Technical Architecture.pdf>)
- `LB-V3-L2-02`
  - [Editable HTML](../../../../demos/01-dictionary/diagrams/proposed-linebooker-v3-l2-technology-selection-rationale.html)
  - [Final PDF](<../../../../output/pdf/Proposed Linebooker V3 Architecture - L2 Technology Selection and Rationale.pdf>)

## Modelling invariants

- Keep tenant, organisation, user membership, country/region placement and
  external system as separate concepts.
- Organize functional views around business capabilities before deployable
  technology names.
- Show authentication by an external identity provider separately from
  Linebooker authorization, membership, RBAC and ABAC.
- Treat NATS accounts as messaging isolation boundaries, not regions.
- Distinguish PostgreSQL transactional truth, JetStream durable fact/event
  history, Temporal workflow state, projections/NATS KV and evidence-document
  object storage.
- Immutable audit does not imply universal event-sourced reconstruction.
- Architecture may be AWS-shaped without selecting or branding a cloud vendor
  until that decision is explicitly confirmed.
- Regulatory, residency, financial and legal assertions remain proposed until
  confirmed by the accountable owner.

## Visual and notation standard

This standard is binding on every L0-L4 document. The
`linebooker-architecture-documenter` and `html-diagram-drawer` skills execute it
and must not re-decide it. Several rules below are adapted from the C4 model's
notation guidance; the concepts are adopted, the C4 visual notation is not.

### Theme

- Use the repository's dark UniFi palette and Inter typography defined in
  `CLAUDE.md`. Do not adopt C4 shapes, C4 colours or stick-figure people.
- Reuse the established Linebooker page chrome: header band with document ID,
  level and sheet number; body; note rail; traceability footer.
- Colour is global. A colour carries the same meaning in every document at every
  level. A colour identifies responsibility, never a vendor.
- Do not use cloud-provider logos or icons. This is a deliberate departure from
  C4's deployment-diagram advice, and it holds until a cloud vendor is formally
  selected.

### Visual form by level

| Level | Default form |
|---|---|
| L0 | Landscape hierarchy |
| L1 | Stakeholder and system landscape |
| L2 | Logical topology; card catalogue for a selection, inventory or rationale view that has no relationships to draw |
| L3 | Topology for landscapes; schematic for flow, state, sequence, trust or mechanism views |
| L4 | Whichever form best exposes the implementable design; do not force a topology |

### Every sheet must carry

- A title naming the view and its scope.
- A one-line scope statement saying what the sheet answers and what it defers.
- A legend explaining every colour, every line style, and every acronym used.
  A view with no relationships to draw has no line styles to explain; it still
  explains every colour and every acronym, and it still carries a legend.
- The exact stable document ID and catalogue title.

### Elements

- State the element kind explicitly. Two boxes of different kinds must not be
  indistinguishable.
- Give every element a short description of its responsibility.
- Name the technology for every element at L2 and below.

### Relationships

- Every line is unidirectional.
- Every line is labelled, and the label reads correctly in the direction drawn.
- Do not use bare labels such as "Uses", "Calls" or "Talks to".
- At L2 and below, name the transport or protocol on the line, for example NATS
  request/reply, JetStream, HTTPS or WebSocket.
- A line must connect the elements it actually describes. A label that applies to
  one element must not terminate on its neighbour.

### Self-containment

- A sheet is read without a narrator. Anything needed to understand it is on the
  sheet or in its legend.
- Expand or remove acronyms. If one must stay, define it in the legend.

### Deployment and runtime views

- A deployment view shows instances placed on nesting nodes, not element types
  floating beside them. Every element must have a stated home.
- A runtime view uses numbered interactions to show ordering. Draw one only for a
  sequence that the static views genuinely fail to explain.

### Directional documents

A document may be published as **directional**: a first-pass position recorded so
the platform can move, not a settled decision. A directional document must:

- say so in its scope line, in those words;
- carry the date it states its position as at, on the sheet;
- give every provisional item its own status, so a reader never has to infer it;
  and
- point at the decision of record where one exists, and show the pointer as
  missing where one does not.

A missing pointer is a visible to-do, not a defect. Do not remove reasoning to
make a directional document look more certain than it is; mark it instead.

### Enforcement

These rules are advisory and checked by review, not by script.
`audit-svg-layout.mjs` continues to check geometry only. A reviewer reports a
breach as a finding against this section. If the checks are automated later, the
rules above are the specification.

## Requirements and traceability

- Update a document's requirements register before materially changing its
  architecture.
- Every visible material concern must trace to at least one included requirement.
- Record intentionally deferred detail as a derived requirement with its target
  L3/L4 document.
- Record unresolved business, technical, legal and regulatory choices as open
  confirmations rather than silently choosing them in a diagram.
- Create a requirements register for a new branch when it becomes an ongoing
  governed baseline.

## Registering and publishing a branch

1. State the new document's single primary question.
2. Select the lowest level capable of answering it and identify its parent.
3. Allocate the next unused stable ID at that level and add a `PLANNED` row to
   this catalogue.
4. Add the planned node to L0 without implying that a reviewable artefact exists.
5. Establish or update the requirements register.
6. Create the editable HTML and distributable PDF using the architecture
   documenter skill.
7. Validate scope, SVG layout, A3 page geometry, page count, visual rendering and
   exact ID/title parity.
8. Change the catalogue and L0 status to `AVAILABLE` only after all checks pass.
9. When replacing a document, retain the old ID as `SUPERSEDED` and record the
   replacement ID; do not overwrite its historical identity.

## File and output conventions

- HTML source:
  `demos/01-dictionary/diagrams/proposed-linebooker-v3-l<level>-<lowercase-kebab-title>.html`
- Final PDF:
  `output/pdf/Proposed Linebooker V3 Architecture - L<level> <Title>.pdf`
- `output/pdf/` is the committed deliverable location for this series.
- PDFs in the Obsidian architecture directory are historical or separately
  governed references unless this authority explicitly registers them.
- The HTML and PDF must display the exact stable ID and document title.

### Paper size

- **A4 landscape when the content genuinely fits it. A3 landscape otherwise.**
  A3 is the working default for this series because most L1 and L2 topologies
  need it, but A3 is not a target. Do not spread a small diagram across A3 to
  fill the page.
- Content "fits A4" when, at A4 landscape, no element is clipped, no label is
  shrunk below the theme's minimum readable size, no connector is rerouted
  awkwardly to save room, and the legend and traceability footer still sit on
  the sheet. If any of those fail, use A3.
- Decide the size before drawing, not after. Set the chosen size once in the
  HTML `@page` rule; `export-html-pdf.mjs` follows the page CSS.
- Splitting one sheet into two A4 sheets to avoid A3 is not a fit. Prefer one
  A3 sheet over two A4 sheets for a single view.
- A size other than A4 or A3 landscape requires explicit user approval.

## Change control

For review-only requests, report findings without editing artefacts. For an
authorized creation or revision:

- update this catalogue when a document is added, renamed, re-parented,
  completed or superseded;
- update the requirements register and document in the same change;
- synchronize L0 whenever catalogue hierarchy, title or status changes;
- regenerate L0 only when its visible catalogue changes;
- preserve historical V2/V3 artefacts unless the user explicitly requests their
  migration or removal; and
- record remaining legal, regulatory, financial or business-owner assumptions in
  the requirements register and completion report.

## Supporting sources

- [Background and rationale](../../../../proposed-linebooker-v3-architecture-discussion.md)
- [Execution workflow](../../../../.claude/skills/linebooker-architecture-documenter/SKILL.md)
- [Drawing workflow](../../../../.claude/skills/html-diagram-drawer/SKILL.md)
- [Graphical catalogue - LB-V3-L0-01](../../../../demos/01-dictionary/diagrams/proposed-linebooker-v3-l0-architecture-atlas.html)

## Change history

- 2026-09-03 - Extended `LB-V3-L2-02` to two sheets: sheet 1 platform
  selections, sheet 2 external services and integrations read out of the V2
  codebase. Established that an inherited dependency is marked as inherited
  rather than selected, that a genuinely unmade decision is shown as an open
  card rather than omitted, and that a technology inventory records what is
  deliberately not carried forward.
- 2026-09-03 - Registered `LB-V3-L2-02 Technology Selection and Rationale` as a
  second L2 document. Recorded that L2 may branch by question but never by
  concern, added the technology selection card and the card catalogue form,
  relaxed the legend rule for views with no relationships, and defined
  directional documents.
- 2026-09-03 - Added a paper size rule: A4 landscape when the content fits,
  A3 landscape otherwise. A3 remains the working default, not a target.
- 2026-09-03 - Added an audience column to the hierarchy, an element vocabulary
  per level, and a binding visual and notation standard adapting C4 notation
  concepts to the existing Linebooker theme. Enforcement is by review.
- 2026-09-03 - Established this file as the central operational authority for
  the Proposed Linebooker V3 L0-L4 architecture document set.
