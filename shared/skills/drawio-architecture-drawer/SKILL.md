---
name: drawio-architecture-drawer
description: Create and maintain editable multi-page Draw.io architecture diagrams, apply the UniFi visual theme, export PNGs for Markdown, and validate the rendered outputs. Use for architecture maps, lifecycle diagrams, and sequence diagrams.
---

# Draw.io Architecture Drawer

Use this skill when an architecture diagram must remain editable in Draw.io and render cleanly in Markdown.

## Source model

- Prefer one native `.drawio` workbook with multiple named `<diagram>` pages.
- Keep stable page names for the architecture map, localized rendering lifecycle, and runtime sequence.
- Use native Draw.io shapes, connectors, swimlanes, and text. Do not replace the editable source with Mermaid or a flattened image.
- Keep generated PNGs beside the Markdown document that embeds them.
- Use relative Markdown links so the document remains portable.

For the dictionary demo, the source workbook is:

`demos/01-dictionary/diagrams/architecture-dictionary.drawio`

The generated images are:

`demos/01-dictionary/backend/refdata-service/shipping-ui-dictionary-map.png`
`demos/01-dictionary/backend/refdata-service/localized-rendering-lifecycle.png`
`demos/01-dictionary/backend/refdata-service/shipping-ui-dictionary-sequence.png`

## UniFi visual language

Use the existing UniFi theme:

- Canvas: `#14171B`
- Primary UI and service nodes: `#006FFF`
- Authoritative data nodes: `#27C07F`
- Panels: `#1A1E23`
- Lane and lifeline strokes: `#4A515B`
- Primary text: `#DEE0E3`
- Secondary text: `#B7BCC2`
- Warning or fallback: `#9A7B1E`

Prefer restrained rounded rectangles, dashed system boundaries, clear directional arrows, and short labels. Keep titles and labels readable at the exported PNG size. For sequence diagrams, use explicit lifelines and visually separate cache-hit, fallback, and mutation-refresh phases.

## Workflow

1. Inspect the existing architecture document, theme assets, and current rendered diagrams.
2. Update the appropriate page inside the single workbook. Preserve page IDs and names unless a deliberate rename is required.
3. Use the canonical glyphs from `shared/unifi-theme/icons.svg`: `ico-browser`, `ico-service`, `ico-nats`, `ico-db`, `ico-kv`, `ico-stream`, `ico-cache`, and `ico-sse`. Do not redraw substitute icons.
4. Synchronize the embedded Draw.io image cells and font stack before exporting:

   `node demos/01-dictionary/diagrams/sync-unifi-assets.mjs`

   The workbook embeds each glyph because standalone Draw.io exports cannot reliably resolve external SVG `<use>` references. The exact font stack is `Inter, -apple-system, 'Segoe UI', sans-serif`, matching `shared/unifi-theme/icons.svg`.
5. Keep page geometry and text within the page bounds. Use `background="#14171B"` and explicit dark fills on swimlanes.
6. Export the pages with the repository script:

   `cd demos/01-dictionary && ./diagrams/export-png.sh`

   The installed Draw.io Desktop build uses one-based `--page-index` values for this workbook. Keep its page-to-output mapping synchronized with the workbook.
7. Validate the result:
   - `xmllint --noout demos/01-dictionary/diagrams/architecture-dictionary.drawio`
   - `git diff --check`
   - `file demos/01-dictionary/backend/refdata-service/*.png`
   - visually inspect each PNG for clipping, overlap, unreadable labels, missing lifelines, and incorrect canvas colors.
8. Update Markdown image links and the editable workbook link together.

## Agent portability

This skill intentionally uses plain Markdown, Draw.io XML, shell commands, and repository-relative paths. It does not depend on Codex-specific tools, Claude-specific commands, or provider-specific APIs. In Codex or Claude, use the host's normal shell and image inspection capability for the validation steps.
