---
name: pattern-cards
description: Use when a demo in this lab reaches step 4 of "the life of a demo" (root CLAUDE.md) — building the closing pattern cards deliverable, a PDF deck of lessons, recommendations, and pros/cons for architects and developers.
---

# Pattern cards — the closing deliverable

**When a demo completes, it gets a pattern cards PDF.** It summarises the lessons
learnt, the recommendations, and the pros and cons — in the same house form as
the other pattern card decks in this repo (`demos/02-multi-region/diagrams/`,
`demos/04-jetstream-cqrs/docs/`).

- **Each demo localises its own docs.** The deck lives at
  `demos/<demo>/docs/<demo>-pattern-cards.html`, exported beside it as `.pdf`.
  Nothing goes in a shared folder.
- **Both editions ship.** The HTML is the editable source; the PDF is what gets
  handed to somebody. Export with
  `node demos/01-dictionary/diagrams/export-html-pdf.mjs <in.html> <out.pdf>`.
  That script is the one agreed exception to a demo's folder seal.
- **One card, one pattern.** Each card states the decision it answers, the
  mechanism, a `pro` panel, a `con` panel, and a one-line verdict.
- **A card may carry numbers, but every number names its date and machine.** The
  deck ends with a "Where every number came from" page listing each figure, the
  day it was measured and what it ran on. A figure with no provenance becomes a
  stale constant.
- **Keep a retraction in the deck.** If a measurement was wrong and was
  corrected, the correction is a card. A lab whose numbers only ever improve is
  not measuring.
- **Guard the deck's shape with a spec** in the demo's own test suite — that the
  file and its PDF exist, that every card title is present, that each card has a
  pro and a con. A guard checks shape; it can never check truth.
- Dark UniFi palette, A4 via `@page`, same as the existing decks (see root
  `CLAUDE.md` § "Frontend Design System" for the palette values).
