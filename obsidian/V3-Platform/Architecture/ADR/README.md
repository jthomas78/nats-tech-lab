# Architecture Decision Records

All ADRs for this repo live in this folder, whatever their scope. The
generated table is [ADR-INDEX.md](ADR-INDEX.md); the card deck PDF is
`output/pdf/Architecture Decision Records - Cards.pdf`.

## Filename

```
ADR-<nnn>-<scope>-<context>-<slug>.md
```

- **`nnn`** — one global sequence, three digits, never reused, never
  renumbered. Code, business rules and plans cite ADRs by bare number
  (`ADR-047`), so the number is the stable identity. Next free number: see
  the last row of `ADR-INDEX.md`.
- **`scope`** — where the decision applies:
  - `lab` — Tech Lab / Dictionary POC. Implemented in this repo.
  - `v3` — Proposed Linebooker V3 platform principle. Governs lab
    decisions; not itself implemented here.
- **`context`** — the bounded context or platform layer, one hyphen-free
  token where possible: `organizations`, `accounts`, `shipping`, `refdata`,
  `pricing`, `app-shell`, `data`, `nats`, `platform`. Add a new value when a
  real new context appears; do not invent synonyms.
- **`slug`** — short kebab-case title.

Renaming an existing ADR is a repo-wide link sweep (relative links from
`Dictionary-POC/`, `demos/`, `.claude/plans/`, memory). Do it in one commit
and run the link check below.

## Front matter

Every ADR starts with a YAML block. `build-adr-cards.mjs` reads only this
block, so the card and the index are exactly as good as these fields.

```yaml
---
adr: 52
title: One Postgres Instance, One Database and One Role per Service
status: Accepted            # Proposed | Accepted | Deprecated | Superseded
date: 2026-09-03
scope: lab                  # lab | v3
context: data
decision: <one or two sentences — the decision itself, no rationale>
why: <one or two sentences — the deciding force>
related: [53]               # numbers only
applies: [53]               # optional: v3 ADR this lab ADR implements
applied_by: [52]            # optional: lab ADRs implementing this v3 ADR
---
```

The filename tokens (`nnn`, `scope`, `context`) must agree with the front
matter; the build script fails if they do not.

## Body

Below the front matter, the existing shape: `# ADR-nnn: Title`, then the
`**Status:** / **Date:** / **Deciders:** / **Related:**` lines, then
Context, Decision, Options Considered, Trade-off Analysis, Consequences,
Action Items. A `lab` ADR that applies a `v3` ADR adds a `**Governed by:**`
line pointing at it (see ADR-052).

## Regenerate index and cards

```bash
node demos/01-dictionary/diagrams/build-adr-cards.mjs
```

```bash
node demos/01-dictionary/diagrams/export-html-pdf.mjs demos/01-dictionary/diagrams/adr-cards.html "output/pdf/Architecture Decision Records - Cards.pdf"
```

Run both after adding or changing any ADR's front matter.
