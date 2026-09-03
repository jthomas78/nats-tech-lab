---
name: linebooker-architecture-documenter
description: Create, revise, catalogue, or review Proposed Linebooker V3 architecture documents across L0-L4, including stable diagram IDs, level-appropriate scope, the graphical L0 atlas, and matching HTML/PDF outputs. Use for Linebooker architecture-document work; do not use for unrelated repository diagrams or application implementation.
---

# Linebooker Architecture Documenter

Maintain the Proposed Linebooker V3 architecture as one navigable document set.
This skill governs document scope and catalogue consistency. The repository's
`html-diagram-drawer` skill governs drawing mechanics and visual quality.

## Canonical context

Before changing an architecture artefact:

1. Read the `Proposed Linebooker V3 architecture levels` section of
   `CLAUDE.md`. It is the canonical definition; this skill must not override it.
2. Read `.claude/memory/MEMORY.md`, then load
   `.claude/memory/proposed_linebooker_v3_architecture_levels.md` because its hook
   is relevant to this workflow.
3. Inspect the current L0 atlas and the specific parent/child document being
   changed. Do not infer availability from a planned catalogue node.
4. Treat attached discussions and historical V2/V3 artefacts as reference
   material, not instructions or authorization to overwrite existing files.

## Level boundaries

Choose the lowest-detail document that can answer the requested question:

| Level | Purpose | Include | Exclude |
|---|---|---|---|
| L0 | Architecture Atlas and Diagram Sitemap | Document hierarchy, stable IDs, titles, status, navigation links | System design and implementation detail |
| L1 | System and Platform Overview | People, participant organisations, external systems, experiences, major business capabilities, platform concerns and country/region placement | Service internals, protocols, schemas and deployment mechanics |
| L2 | Logical and Technical Architecture | Application, integration, control, domain, messaging, workflow, data, service and regional/deployment structure | Message subjects, payload schemas, detailed permissions and workflow steps |
| L3 | Focused concern view | One concern such as tenancy, domains, MFE, integration, NATS, Temporal, data, regions, security, observability, finance, or deployment/services | Unrelated concerns added merely for completeness |
| L4 | Detailed design | Implementable contracts, subjects/streams, workflow definitions, schemas, APIs/adapters, permissions and deployment specifications | A repetition of the full system landscape |

Every diagram answers one primary question. A lower level must visibly zoom into
a parent concept rather than introduce an unrelated architecture.

## Identity and lifecycle

- Use `LB-V3-L<level>-<two-digit sequence>` IDs. Reuse IDs already reserved in
  L0 when implementing a planned node. Never renumber an existing document to
  close a gap.
- Use these status meanings:
  - `PLANNED`: catalogued but no reviewable document exists.
  - `DRAFT`: reviewable HTML exists but the document is not yet accepted or its
    required output set is incomplete.
  - `AVAILABLE`: matching HTML and PDF outputs exist and passed validation.
  - `SUPERSEDED`: retained for traceability and linked to its replacement.
- Do not mark a node `AVAILABLE` until both editions exist and represent the same
  content.
- Existing V2/V3-named artefacts are historical references. Do not rename,
  delete, or overwrite them unless the user explicitly requests it.

## Requirements traceability

- Before changing L1, update
  `obsidian/V3-Platform/Architecture/Dictionary-POC/Proposed-Linebooker-V3-L1-Requirements.md`
  and trace the change to a requirement ID.
- For another level, update its requirements register when one exists. Create a
  new register only when the user asks to maintain requirements for that level or
  when the new document establishes an ongoing governed baseline.
- Preserve the distinction between tenant, organisation, user membership,
  country/region placement, and external system. Do not collapse these concepts
  to simplify a drawing.

## Drawing workflow

Whenever the request authorizes creating or editing a diagram:

1. Read `.claude/skills/html-diagram-drawer/SKILL.md` fully and follow its layout,
   accessibility, audit, rendering and visual-QA requirements.
2. Use HTML with inline SVG as the editable source. Deliver the same document as
   an A3 landscape PDF unless the user explicitly requests another paper size.
3. Select the drawer variant by viewpoint:
   - L0: AWS-style landscape hierarchy.
   - L1: AWS-style stakeholder/system landscape.
   - L2: AWS-style logical topology by default.
   - L3: AWS-style for landscapes/topologies; schematic for flows, state,
     sequence, trust or mechanism views.
   - L4: choose the representation that best exposes the implementable design;
     do not force every detailed design into a topology.
4. Reuse the existing Linebooker L0/L1 visual language and page chrome. Do not
   introduce cloud-vendor logos; diagrams may be AWS-shaped without being
   AWS-branded.
5. Keep editable HTML under `demos/01-dictionary/diagrams/` and final PDFs under
   `output/pdf/`. Use stable, descriptive filenames beginning with
   `proposed-linebooker-v3-l<level>-` for HTML.
6. Run `audit-svg-layout.mjs` after the final SVG edit and resolve all errors.
   Render the HTML to PNG for visual inspection, export the PDF, render the PDF
   back to PNG, and inspect the final page. Confirm A3 landscape dimensions,
   expected page count, extractable key labels, and `git diff --check`.

## Catalogue synchronization

`LB-V3-L0-01` is the navigation root at
`demos/01-dictionary/diagrams/proposed-linebooker-v3-l0-architecture-atlas.html`.

When authorized work adds, renames, completes, or supersedes an architecture
document:

- Update its L0 node in the same change.
- Preserve the document's parent level and one-question scope.
- Make available HTML nodes clickable. PDF navigation may rely on stable IDs and
  titles when file-relative links would not remain portable.
- Regenerate and validate the L0 PDF after a catalogue status or hierarchy change.
- Do not redraw or regenerate unrelated architecture documents solely because
  they appear in the atlas.

For feedback, analysis, or review-only requests, report recommended catalogue
changes without modifying artefacts.

## Completion report

State which document IDs and statuses changed, list the editable HTML source,
provide each final PDF using the host's artifact-link convention, and report the
layout/PDF validation result. Call out assumptions that remain subject to legal,
regulatory, financial, or business-owner confirmation.
