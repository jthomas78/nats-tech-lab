---
name: chronicle-architecture-docs-layout
description: Chronicle's L1–L4 architecture docs live under documentation/architecture/L4_model/ (moved by the user 2026-10-20)
metadata:
  type: project
---

In Chronicle (`/Users/jeremy/dev/github/linebooker/chronicle`), every L1 to L4
architecture doc lives under `documentation/architecture/L4_model/`. The user
moved them there on 2026-10-20. `documentation/architecture/overview.html` (from
`develop`) stays outside it.

```
documentation/architecture/L4_model/
  L0-architecture-atlas-v0.1.html        L0 atlas; sits above the L1–L4 folders (no L0/ folder)
  architecture-model-guide-v0.1.html     the L0–L4 model guide
  L1/system-platform-overview-v0.1.html
  L2/logical-technical-architecture-v0.1.html
  L3_L4/<concern>/                       L3 overview + L4 designs per concern
    ADR/                                 that concern's ADRs
```

Observability today: `L3_L4/observability/` holds
`L3-observability-overview-v0.1.html`, `L4-observability-tracing-design-v0.1.html`
(the former v0.2) and `ADR/ADR-OBS-TRACE.md`. The user removed `_archive/`
(old v0.1-simple design, tracing update, lab comparison) in commit 81f015a;
git history keeps them.

**Why:** the user wants the L-model docs grouped apart from the other docs.
**How to apply:** create new L1–L4 docs and ADRs under `L4_model/`. Use the
render paths in [[chronicle-render-html-location]].
