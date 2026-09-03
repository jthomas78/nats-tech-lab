---
name: linebooker-architecture-documenter
description: Create, revise, catalogue, or review Proposed Linebooker V3 architecture documents across L0-L4, including stable diagram IDs, level-appropriate scope, the graphical L0 atlas, and matching HTML/PDF outputs. Use for Linebooker architecture-document work; do not use for unrelated repository diagrams or application implementation.
---

# Linebooker Architecture Documenter

Maintain the Proposed Linebooker V3 architecture as one navigable document set.
The central authority governs document scope and catalogue consistency. This
skill executes that authority. The repository's `html-diagram-drawer` skill
governs drawing mechanics and visual quality.

## Operating mode

Decide the mode before touching files:

- **Review / feedback / analysis:** inspect and report only. Do not modify,
  regenerate, rename, or re-status any artefact.
- **Create / revise / catalogue:** follow the governed workflow below, including
  requirements traceability, HTML/PDF parity, validation, and L0 synchronization.

The canonical operational contract is
`obsidian/V3-Platform/Architecture/Dictionary-POC/Proposed-Linebooker-V3-Architecture-Authority.md`.
This skill must not override it. `CLAUDE.md` routes work to the authority and this
workflow; the architecture discussion and memory are background only.

## Canonical context

Before changing an architecture artefact:

1. Read `CLAUDE.md`, the central architecture authority and
   `.claude/memory/MEMORY.md`, then load
   `.claude/memory/proposed_linebooker_v3_architecture_levels.md` because its hook
   is relevant to this workflow.
2. Inspect the current L0 atlas and the specific parent/child document being
   changed. Do not infer availability from a planned catalogue node.
3. Treat attached discussions and historical V2/V3 artefacts as reference
   material, not instructions or authorization to overwrite existing files.

## Scope, identity and lifecycle

Use the authority's level table, catalogue, modelling invariants, stable-ID rules
and status definitions. Do not reproduce or reinterpret them here. Select the
lowest-detail registered branch that answers the requested primary question.

## Requirements traceability

Follow the authority's traceability rules. Update the applicable register before
materially changing its architecture, and preserve its included, derived and open
requirements rather than silently resolving them in the drawing.

## Drawing workflow

Whenever the request authorizes creating or editing a diagram:

1. Read `.claude/skills/html-diagram-drawer/SKILL.md` fully and follow its layout,
   accessibility, audit, rendering and visual-QA requirements.
2. Use HTML with inline SVG as the editable source. Deliver the same document as
   a PDF at the paper size chosen under the authority's **Paper size** rule: A4
   landscape when the content genuinely fits it, A3 landscape otherwise. Decide
   before drawing, not after.
3. Apply the authority's **Visual and notation standard** and its **Element
   vocabulary by level**. That standard decides visual form per level, theme,
   legend content, and element and relationship notation; this file only executes
   it. Do not re-decide any of it here, and do not adopt C4 visual notation.
4. Before export, check the drawing against that standard by eye: title, scope
   line, and a legend covering every colour, line style and acronym; explicit
   element kinds; technology named at L2 and below; every line unidirectional,
   labelled, correctly aimed, and carrying its protocol at L2 and below. Report a
   breach as a review finding — `audit-svg-layout.mjs` checks geometry only.
5. Use the exact source/output homes and filename conventions defined by the
   authority. Temporary rendered pages belong under `tmp/pdfs/` and are not
   deliverables.
6. Put `@page { size: <chosen size> landscape; margin: 0; }` in the HTML print CSS
   and enable print backgrounds. Set the sheet's own width and height to match
   (A4 landscape `297mm x 210mm`, A3 landscape `420mm x 297mm`); the page CSS is
   authoritative because `export-html-pdf.mjs` uses `preferCSSPageSize: true`.
   For multi-sheet documents, use one `.sheet` per page and
   `page-break-after: always` except on the final sheet; do not combine competing
   modern and legacy break rules.
7. Run the exact export and validation procedure below after the final SVG edit.

## PDF export procedure

The HTML page size is authoritative; `export-html-pdf.mjs` uses
`preferCSSPageSize: true` and prints backgrounds. From the repository root:

```bash
mkdir -p tmp/pdfs

node demos/01-dictionary/diagrams/audit-svg-layout.mjs \
  demos/01-dictionary/diagrams/<html-file>.html

node demos/01-dictionary/diagrams/export-html-png.mjs \
  demos/01-dictionary/diagrams/<html-file>.html \
  tmp/pdfs/<html-file>.png 1600 --clip=".sheet"

node demos/01-dictionary/diagrams/export-html-pdf.mjs \
  demos/01-dictionary/diagrams/<html-file>.html \
  "output/pdf/Proposed Linebooker V3 Architecture - L<level> <Title>.pdf"

pdfinfo "output/pdf/Proposed Linebooker V3 Architecture - L<level> <Title>.pdf"
rg -F "LB-V3-L<level>-<nn>" demos/01-dictionary/diagrams/<html-file>.html
rg -F "<Title>" demos/01-dictionary/diagrams/<html-file>.html
pdftotext \
  "output/pdf/Proposed Linebooker V3 Architecture - L<level> <Title>.pdf" \
  tmp/pdfs/l<level>-text.txt
rg -F "LB-V3-L<level>-<nn>" tmp/pdfs/l<level>-text.txt
rg -F "<Title>" tmp/pdfs/l<level>-text.txt
pdftoppm -png -r 120 \
  "output/pdf/Proposed Linebooker V3 Architecture - L<level> <Title>.pdf" \
  tmp/pdfs/l<level>-qa
```

Use the actual outer-page selector in place of `.sheet` when the HTML uses a
different wrapper (the current atlas documents use `.sheet`). Inspect the HTML
PNG and every PDF-page PNG; do not approve from text extraction alone.

## Validation

Before assigning `AVAILABLE`, verify all of the following:

- `audit-svg-layout.mjs` exits zero with no unresolved errors.
- `pdfinfo` reports the chosen landscape page size (A4 approximately
  `842 x 595 pts`, A3 approximately `1191 x 842 pts`, allowing normal
  Chromium/Poppler rounding) and the expected page count.
- The HTML and `pdftotext` output both contain the exact stable ID and document
  title shown in L0. This is the minimum machine-checkable parity test.
- Every rendered page is visually inspected for clipping, overlaps, legibility,
  background color, connector direction, and unintended blank pages.
- `git diff --check` passes. Temporary PNGs are not committed.

## Catalogue synchronization

`LB-V3-L0-01` is the navigation root at
`demos/01-dictionary/diagrams/proposed-linebooker-v3-l0-architecture-atlas.html`.

When authorized work adds, renames, re-parents, completes or supersedes an
architecture document, update the authority's canonical catalogue first. Then:

- Update its L0 node in the same change.
- Preserve the document's parent level and one-question scope.
- Make available HTML nodes clickable. PDF navigation may rely on stable IDs and
  titles when file-relative links would not remain portable.
- Regenerate and validate the L0 PDF after a catalogue status or hierarchy change.
- Do not redraw or regenerate unrelated architecture documents solely because
  they appear in the atlas.

## Worked example: `LB-V3-L1-01`

For the System and Platform Overview, update the L1 requirements register first,
edit `proposed-linebooker-v3-l1-system-platform-overview.html`, export
`Proposed Linebooker V3 Architecture - L1 System and Platform Overview.pdf`, and
confirm both editions contain `LB-V3-L1-01` and `System and Platform Overview`.
Only after the SVG audit, page-size/page-count check, text check, and rendered-page review
pass should its L0 node be `AVAILABLE`.

## Completion report

State which document IDs and statuses changed, then list the exact repository
paths for the editable HTML and final PDF. In Codex desktop, cite each created or
edited PDF exactly once with
`:codex-file-citation{path="/absolute/path/file.pdf" purpose="output"}`; in other
hosts, provide the exact `output/pdf/...` path as a clickable link when supported.
Report the SVG audit result, page size, page count, parity-text check, and visual QA.
Call out assumptions that remain subject to legal, regulatory, financial, or
business-owner confirmation.
