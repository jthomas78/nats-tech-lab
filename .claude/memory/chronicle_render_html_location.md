---
name: chronicle-render-html-location
description: Chronicle's Markdown-to-HTML renderer is documentation/architecture/L4_model/render-html.py; run it from that folder with explicit .md paths
metadata:
  type: reference
---

Chronicle (`/Users/jeremy/dev/github/linebooker/chronicle`) renders its L-model
Markdown docs (for example the ADRs) with
`documentation/architecture/L4_model/render-html.py`. The user moved it there on
2026-10-20. It was at the repo root before that, and in
`L3_L4/observability/` before that.

- Run it from `documentation/architecture/L4_model/`, with paths:
  `python3 render-html.py L3_L4/observability/ADR/ADR-OBS-TRACE.md`
  Paths are relative to where you run it, so an absolute path also works.
- The page is written next to its source. `SRC.md OUT.html --out` writes to another path.
- With no paths it prints help and writes nothing.
- The user removed the generated ADR-OBS-TRACE.html on 2026-10-20; links now
  point to `ADR/ADR-OBS-TRACE.md`. Do not regenerate the ADR HTML unasked.
- Diagram PNGs (figure checks) still use the lab's
  `demos/01-dictionary/diagrams/export-html-png.mjs <in.html> <out.png> 1920 "--clip=#fig-id"`.
- The only note for people is the script's own help text. The user reset
  Chronicle `documentation/README.md` on 2026-10-20, so do not add render notes there.

Related: [[chronicle-architecture-docs-layout]], [[chronicle-is-production-not-poc]]
