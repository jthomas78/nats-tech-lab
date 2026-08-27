---
name: admin-ui-design-viewport
description: Every frontend in this repo is designed for a 1920x1080 viewport — verify layout at that size, not at the browser pane's default
metadata:
  type: feedback
---

**The target design dimensions for the UIs in this repo are 1920x1080.** Verify
and reason about layout at that viewport — column widths, table density, dead
space, wrapping — not at whatever size the preview pane happens to open at
(the Browser pane defaults to roughly 800px wide, and the `resize_window`
`desktop` preset just returns it to that pane size rather than to a design
width).

**Why:** stated by the user on 2026-08-27, after I reported a Connections-panel
layout limit ("15 of 16 rows fit on one line at 1440px") as if 1440 were the
bar. It isn't — at 1920 the constraint I was describing largely doesn't exist,
so a trade-off I'd presented as settled was an artifact of measuring at the
wrong width. Sizing decisions made at a narrower viewport tend to over-tighten
columns and then read as cramped on the real target.

**How to apply:** before judging or reporting on any layout in
`lab-shell/` or `demos/01-dictionary/frontend/*`, call
`mcp__Claude_Browser__resize_window` with `{width: 1920, height: 1080}` and
evaluate there. Reset with the `desktop` preset when finished. Narrower widths
are still worth a look for graceful degradation, but they are not what the
design is *for* — don't spend column budget or introduce horizontal scroll to
satisfy them. Relevant to [[admin_stat_card_one_ratio_rule]] and any
table-density work in the Admin UI.
