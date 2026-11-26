---
name: uml-diagram-html-drawer
description: Draw UML use case, class and sequence diagrams as one self-contained dark HTML page with hand-placed inline SVG, a numbered "Source · file:line" column beside each figure, and a layout audit. Use when asked for a use case diagram, a class diagram, a sequence diagram, a "how does X call Y" walkthrough, a startup/shutdown sequence, or a code-traced explainer page that mixes these. Portable — carries its own template, tokens and scripts, no project paths.
---

# uml-diagram-html-drawer

One HTML page. One or more figures. Each figure is an inline SVG with a
reference column on its right. Every arrow carries a number; every number has
a `file:line` in the column. The reader can check each claim in the code.

Reference page: `examples/otel-tracing-sequence.html` (seven figures: an
accounts-and-hops context view, use case, class, four sequences, gaps). Start every new page from `template.html`.

## When to use this skill

- Use case, class or sequence diagrams, alone or together on one page.
- A walkthrough of how code calls code, traced to `file:line`.
- Another skill (for example a general HTML diagram skill) routes these three
  diagram kinds here. This skill owns their notation and palette.

Not for: architecture boxes-and-arrows, deployment, flowcharts, before/after
comparisons. Use the project's general diagram skill for those.

## Rules

1. **Read the code first.** Every arrow, class member and use case comes from
   code you have read in this session. Record the `file:line` as you read.
   No line number from memory.
2. **One page, self-contained.** Inline CSS and SVG. No scripts, no external
   assets. Fonts fall back to the system stack.
3. **Scope is a choice; say it.** The lede says what the page leaves out.
   Show only the classes and calls that answer the page's question.
4. **Numbers tie figure to source.** Sequence arrows are numbered `1 · …` in
   order. The refs column lists the same numbers. A use case or class figure
   uses names (`<b>Trust</b>`) instead of numbers.
5. **Cross-reference figures by number** ("Detail in figure 4, steps 1–5").
   When you insert or reorder a figure, renumber every `h2 .no`, every
   "figure N" in prose, and the section comments. Grep for `[Ff]igure [0-9]`.
6. **Gaps last.** A closing `.gaps` box lists what the diagrams show is
   missing or never set in the code, each with `file:line`.
7. **HTML is the deliverable.** Export a PNG only when asked.

## Page chrome (from the template — do not redesign)

- `main` max-width 1840px. Design viewport **1920x1080**; judge layout there.
- `.eyebrow` (scope · component · as-of), `h1`, `.lede`, `.legend` row.
- Each `figure`: `h2` with `<span class="no">N</span>`, then `.fig` grid
  `minmax(0,1fr) 400px`: the SVG (`svg.d`, `width:100%`) and `.refs`.
- `.refs`: `.hd` "Source · file:line", optional `.keys` (path aliases such
  as `org/ = long/path/`), then `ol > li > b + code`. `li.acct` / `.warn` /
  `.badtext` color a line by concern.
- `figcaption` with a bold lead sentence, then one or two plain sentences.
- Optional `table.t` under a figure for a matrix (`.yes` / `.no-c` cells).
- Below 1200px the refs column drops under the SVG. That is the only
  breakpoint.

## Tokens (palette)

All colors are CSS custom properties on `:root` in `template.html`. Marker
fills in the shared `<defs>` repeat the hex values. To adapt to another
project, change the token values and the six marker fills. Change nothing
else.

| Token | Default | Role |
|---|---|---|
| `--bg` | `#131416` | page and SVG canvas; label halo color |
| `--panel` | `#1A1E23` | figure panels, class boxes, use case fill |
| `--stroke` | `#4A515B` | borders, lifelines, dividers |
| `--text` / `--muted` / `--dim` | `#DEE0E3` / `#B7BCC2` / `#8B929A` | text, sub-labels, stereotypes |
| `--accent` / `--accent-t` | `#006FFF` / `#6FA8FF` | calls, SERVER bars, numbers |
| `--truth` / `--truth-t` | `#27C07F` / `#5FD9A3` | the one highlighted concern |
| `--warn` / `--warn-t` | `#9A7B1E` / `#D6B656` | async, SDK, secondary path |
| `--bad` | `#F87171` | not captured, failure, gap |

Use the highlight color (`truth`) for **one** concern per page (for example
"the account as metadata"). Name it in the legend.

## Class vocabulary

Shapes: `.n` box · `.n.acct` highlighted · `.n.ext` dashed (outside the
system) · `.n.sdk` warn border · `.uc` / `.uc.acct` use case ellipse ·
`.actor` stick figure · `.bnd` system boundary · `.nb` combined fragment or
group (dashed) · `.life` lifeline · `.div` compartment line · `.divider`
dotted section break · `.bar-s` / `.bar-c` activation bars.

Lines: `.flow` call · `.ret` reply (dashed, open head) · `.rep` async ·
`.acl` highlighted flow · `.err` not captured · `.self` self call ·
`.assoc` class association · `.embed` embedding / generalization (hollow
triangle) · `.assoc-uc` actor–use case (no head) · `.inc` «include» /
«extend» (dashed, open head).

Text: `.lbl` 13px name · `.lbl2` 11px sub-name · `.stereo` 10.5px
`«stereotype»` · `.mem` / `.mem.m` mono members · `.t` arrow label ·
`.s` mono sub-label · modifiers `.c` centre, `.e` end, `.g` / `.w` / `.r`
color · `.cl` relation label on a panel · `.nbl` frame / column label.

**Every `.t`, `.s` and `.cl` has a halo** (`paint-order: stroke`, stroke in
the background color, 4px). A label may cross a line or lifeline. That is
allowed; the halo keeps it readable. Labels must not overlap other labels.

## UML subset

### Use case diagram

- Primary actors (people, calling programs) as stick figures, **left**.
  Secondary systems as `.n.ext` boxes with `«system»`, **right**.
- One `.bnd` rectangle is the system, with its name top-left.
- Use cases are verb phrases in `.uc` ellipses ("Start a SERVER span").
  `rx` = text width / 2 + ~20; `ry` 26, or 30 with a `.s` sub-line.
- `.assoc-uc` from the actor's arm (x+18) to the ellipse edge.
- `«include»`: base → included. `«extend»`: extension → base, with the
  condition as a `.s` line in the extending ellipse (`[no endpoint]`).
- Refs column: one line per use case, pointing to the entry code and to the
  sequence figure that details it.

### Class diagram

- Columns by package, each headed by an `.nbl` label (`CALLERS`,
  `CORE PACKAGE`, `VALUE TYPES`, `SDK`).
- Box: `.stereo` line (`package · «struct»`), `.lbl` name, `.div`, fields,
  `.div`, methods. Member row step 17px, mono.
- Visibility marks are optional (Go uses case). Multiplicities only when the
  code fixes them.
- Relation label `.cl` above the line, at its midpoint.

### Sequence diagram

- Lane heads: 140–160 wide, 44 tall, at `y=16`; centres 160–190 apart.
  Lifelines `.life` from the head bottom to the figure bottom.
- Row step **44px**. Arrow label at `y − 7`, sub-label at `y + 15`.
- Arrows stop 2px short of the target lifeline (6px for an activation bar).
- Self call: `M x y L x+34 y L x+34 y+14 L x+6 y+14`; label at `x+42, y+6`.
- Activation bars: 10 wide, centred on the lifeline. Use them when "open"
  means something (a span, a transaction). Name them in the legend.
- Combined fragments: `.nb` rect with an `.nbl` label `alt [cond]`,
  `opt [cond]`, `loop [each X]`, `par`. An `alt` else-branch is a `.divider`
  line with its own `[cond]` label.
- A later or async phase (export, shutdown) starts after a full-width
  `.divider` with an uppercase `.nbl` heading.

## Workflow

1. Read the code. Make a list: step, from, to, label, `file:line`.
2. Copy `template.html`. Set `<title>` (2–4 words), eyebrow, h1, lede,
   legend. Delete template figures you do not need.
3. Fix the viewBox width from the lane or column count. Place shapes on a
   grid; compute coordinates, do not nudge.
4. Write the refs column from the step list.
5. Audit:
   ```bash
   node scripts/audit.mjs page.html
   ```
   It needs puppeteer: `npm i puppeteer`, or set `PUPPETEER_PATH` to a
   package folder whose `node_modules` holds it (a mermaid-cli install
   works). It checks every `svg[role="img"][aria-label]` for text overlap,
   text outside its box, connectors through nodes, overlapping shapes and
   dangling arrows. Fix every ERROR. Add `data-audit="skip"` only for a
   deliberate exception.
6. Look at it at 1920x1080. Check there is no horizontal scroll.
7. PNG on request only:
   ```bash
   node scripts/export-png.mjs page.html out.png 1920 --clip='figure:nth-of-type(3)'
   ```

## Checklist

- [ ] Every arrow, member and use case was read in code this session.
- [ ] Every sequence step number appears in the refs column.
- [ ] Every "figure N" reference matches the `h2` numbers.
- [ ] One highlight concern, named in the legend.
- [ ] Every label has a halo; no label overlaps another label.
- [ ] Audit: 0 errors.
- [ ] 1920x1080: no horizontal scroll, refs column readable.
- [ ] Gaps box lists what is missing, with `file:line`.
