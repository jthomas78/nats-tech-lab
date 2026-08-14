---
name: admin-stat-card-one-ratio-rule
description: Admin UI summary cards — one ratio (value / limit) plus a bar, and ONE value type size across the whole row (20px); long counters shorten, never shrink
metadata:
  type: feedback
---

**Type size:** every value in a summary row is `20px/600` (mono 20px in
`OverviewPanel`), pairs included — no per-card `.small` modifier. Jeremy flagged
`20` at 20px next to `/ 65,536` at 13px and the msgs pair at 15px: "I need the
size of the numbers to be consistent." A counter too long for its card is fixed
by shortening the number (`src/format.js` `compactCount` → `1.2M`, exact figure
in the `title`) and by letting `.summary-row` wrap
(`repeat(auto-fit, minmax(min(165px, 100%), 1fr))`), never by dropping a type
size — the old fixed 4-column grid left only ~94px of text room at a 760px
viewport, which is what forced the smaller fonts in the first place.

In the Admin UI's summary cards (`ConnectionsPanel`, and by extension the other
`*Panel.vue` stat rows), a card shows **one number, optionally as `value / max`,
with a bar under it** — never two stacked gauges, and never a caption whose only
job is to say the healthy case is healthy.

Jeremy rejected a Total card that read `18`, then `18 / 65,536 allowed`, then
`all 18 shown · page holds 1,024`: "I find this view confusing. It should only
show total/maximum. For example if the maximum is 65536 and the total is 18 then
we should show `18 / 65536`. I'm happy to have the bar below."

**Why:** the count appeared three times in one 110px card, and only one of the
two ratios was a real ratio — the other (`/connz`'s page size) is a property of
the API reading, not of the server, so pairing it with the count read as a
capacity gauge that didn't exist. Thousands separators stay (`65,536`), matching
the panel's other numbers.

**How to apply:** put the meaningful ceiling in the value itself (`20` at 20px +
`/ 65,536` at 13px muted, same baseline), the bar directly below, and nothing
else. Facts that only matter in an exceptional state get rendered **only in that
state** — the `/connz` paging line now appears solely when the response actually
paged, amber, with the page size in its tooltip. Reuse the existing amber
(`--p-amber-400`) and the 80% threshold that `AccountsPanel`'s usage meters use
rather than inventing a second warning vocabulary. See
[[connz_limit_is_page_size_not_capacity]].
