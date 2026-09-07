# Architecture Decision Records

All ADRs for this repo live under this folder, in a subfolder per scope. The
generated table is [ADR-INDEX.md](ADR-INDEX.md); the card deck PDF is
`output/pdf/Architecture Decision Records - Cards.pdf`.

## Folder

```
ADR/lab/   decisions implemented in this repo
ADR/v3/    Proposed Linebooker V3 platform principles
```

**The folder must match the front matter's `scope`.** `build-adr-cards.mjs`
fails the build when it does not, so a misfiled ADR cannot sit quietly in the
wrong group. Only these two folder names are read; a third would be ignored
entirely, which is worse than an error, so do not add one — add a `scope`
value to the script's `SCOPE` map first.

Scope, not context, is the grouping axis. Context is already in every
filename, so context is a glob (`ls */ADR-*-organizations-*`); scope changes
how the document is *read* — `lab` records what this repo does, `v3` records
a principle that governs future decisions. Two folders, and it stays two.

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

Renaming an existing ADR — or moving it between scope folders — is a
repo-wide link sweep (relative links from `Dictionary-POC/`, `demos/`,
`.claude/plans/`, memory, and the sibling ADRs). Do it in one commit and run
the link check below. Note the depth: an ADR is now one level deeper than the
folder it used to sit in, so a link out to `Dictionary-POC/` is
`../../Dictionary-POC/` and a link to an ADR of the *other* scope is
`../v3/ADR-nnn-...md`.

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
