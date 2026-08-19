---
name: mockup-fidelity-functional-capability
description: design-gate mockups must show real create/edit affordances (verified against the running app), not just read-only layout
metadata:
  type: feedback
---

A first-pass Phase 36.1 mockup (Tech Lab Operator's Reference Data
tabbed panel) showed the new nav + tab layout but rendered every list as
read-only — no create/edit buttons, no per-item action menus. The user
rejected this: "the current functional, editing capability and views
should be available in the information panel, e.g. enums have `new enum`
and `add value` buttons," and pointed at the live app
(`demos/01-dictionary/frontend/refdata`, running at `localhost:7102`) as
the source of truth.

**Why:** a mockup's job is to preview the *outcome* of a restructuring, not
just its shell. Dropping existing functionality from the visual (even
unintentionally, by defaulting to a plain data table) reads to the user as
a proposal to remove that functionality, and design-gate approval is
worthless if it's approving a picture that doesn't match what will actually
ship. See [[phase36-tech-lab-operator-rebrand]] for the phase this applied to.

**How to apply:** before building any UI mockup for an app that already
exists and runs, open it in the browser (`preview_start`/`navigate` +
`read_page`/screenshot) and inventory its actual buttons, per-row action
menus, nested tabs (e.g. refdata's Details/Translations/Usage on every
item), and secondary views (e.g. Versioning's Contexts/Corpus
Versions/Diff) before drafting layout. Carry every affordance found forward
into the mockup in its new location — a redesign changes arrangement, not
capability, unless the user explicitly says otherwise. Cross-check written
architecture docs too, but trust the running app over a doc's "known gaps"
note if they conflict (docs can go stale) — flag the discrepancy to the
user rather than silently picking one.
