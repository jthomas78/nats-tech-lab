---
name: html-diagram-drawer
description: Create hand-authored HTML+inline-SVG architecture diagrams, sequence/flow diagrams, and UI mockups, rendered to a high-DPI PNG via headless Chrome for embedding in this repo's ARCHITECTURE-*.md docs. Use this whenever a diagram benefits from real CSS layout and prose captions around it (multiple related diagrams building one narrative on a page, a design write-up, or a UI mockup for review before implementation) rather than a pure Draw.io node/edge graph — see drawio-architecture-drawer for that case instead. Always reach for this skill, not raw ad hoc SVG or a screenshot mockup, whenever the user asks to "diagram," "sketch," "mock up," or "illustrate" something for this repo's architecture docs.
---

# HTML Diagram Drawer

Use this skill when a diagram or mockup should be a real, precisely-drawn
technical illustration checked into this repo — not a slide, not a
loosely-drawn SVG, and not a live app screenshot. The technique: a
self-contained HTML document (real CSS layout, typography, and prose)
that embeds one or more tightly-drawn inline SVG diagrams, screenshotted
to PNG by headless Chrome.

This is not a new invention — it's already how 12+ diagrams and mockups
in `demos/01-dictionary/diagrams/` were built (`otlp-bridge-ingest.html`,
`admin-traces-panel.html`, the `accounts-overview-*-mockup.html` set,
`natstrace-browserrpc-extraction.html`, `tenants-manager-extraction.html`,
`trace-detail-*.html`, and others). This skill exists to make that
established, unwritten convention explicit and repeatable, using
`otlp-bridge-ingest.html` as the canonical reference — read it directly
(`demos/01-dictionary/diagrams/otlp-bridge-ingest.html`) alongside this
skill whenever the exact numbers below need double-checking.

## When to reach for this vs. the alternatives

- **`drawio-architecture-drawer`** — use instead when the deliverable
  needs to stay editable as a true node/edge graph in a GUI diagramming
  tool (e.g. a non-technical stakeholder will edit it later), or when it
  belongs as a new page in the existing `architecture-dictionary.drawio`
  workbook. Reach for *this* skill instead when the diagram benefits from
  surrounding prose/captions in the same artifact, when several related
  diagrams build one narrative on a single page, or for a UI mockup —
  cases where a real CSS layout engine and typographic hierarchy earn
  their keep over a pure shape canvas.
- **`artifact-diagramming`** — for diagrams embedded live inside a
  published Claude Artifact page. This skill is for generating a static
  PNG asset checked into this repo's Markdown docs, not a live-rendered
  page — different output, different constraints (no runtime, no
  interactivity to design for).
- **`dataviz`** — for charts, stat tiles, and dashboards. Not this
  skill's job; architecture/flow diagrams and UI mockups are a different
  visual grammar entirely (boxes, edges, lifelines — not axes and marks).

Both this skill and `drawio-architecture-drawer` export into the same
`obsidian/V3-Platform/Architecture/Dictionary-POC/images/` folder and
follow the same UniFi visual language at the color level — a PNG's
provenance (Draw.io workbook page vs. hand-authored HTML) is recorded in
the embedding doc's caption/re-export blockquote, not by folder location
or filename convention.

## Execution: Codex when available, Claude Code otherwise

Once the content is decided — which boxes, which edges, what each one
says, where it gets embedded — actually building it (writing the HTML)
is mechanical, well-specified work worth offloading rather than spending
Claude Code's own context on it. Check availability, then route:

1. Invoke `Skill("codex:setup")` and read its JSON output's `ready`
   field (its `codex.available` and `auth.loggedIn` sub-fields explain
   *why* if `ready` comes back false and the user needs to know).
2. **If `ready: true`**, hand the HTML-authoring step off instead of
   doing it yourself: call `Agent` with `subagent_type:
   "codex:codex-rescue"` and a fully self-contained prompt. The
   codex-rescue subagent is a thin forwarder — it does not read this
   skill file or inspect the repo on your behalf, so the prompt itself
   must:
   - Tell Codex to read `.claude/skills/html-diagram-drawer/SKILL.md`
     and follow it (Codex gets its own full filesystem/shell access once
     running, so it doesn't need every convention pasted inline — just
     pointed at the file).
   - State exactly which diagram(s) to build: file name(s), what each
     box/edge/figure should say, and which markdown/doc page(s) to embed
     the result into, at what point in the existing prose.
   - Repeat any deviation from the skill's own worked example that
     applies to this task — e.g. if the PNGs are landing somewhere other
     than the obsidian vault's `images/` folder (see the VitePress
     `docs/public/` case noted later in this skill), say so explicitly,
     since Codex reading the skill file alone would otherwise default to
     the vault path shown in its examples.
   - **Do not ask Codex to run the render step.** Tested 2026-08-20:
     Codex's own shell sandbox on macOS cannot launch headless Chromium
     (`node export-html-png.mjs` fails with a sandbox I/O error at
     Chromium launch — this is a sandbox restriction, not a bug in the
     script, and held even with `--write`/full write access granted).
     Ask Codex to write and validate the HTML only (`xmllint`-equivalent
     sanity, or just re-reading its own output), then stop. You (Claude
     Code) run the render command yourself afterward, per step 4 in the
     Workflow section below — Claude Code's own Bash tool isn't sandboxed
     the same way and renders these pages fine. If a future Codex/sandbox
     update lifts this restriction, this note can be revisited, but don't
     assume it's fixed without re-testing — the failure is silent-ish (a
     stack trace, not a clear "sandboxed" message), easy to mistake for a
     script bug and waste time re-debugging `export-html-png.mjs` itself.
3. **If `ready: false`**, don't stop and ask — proceed with the rest of
   this skill's workflow directly in Claude Code (build the HTML
   yourself with Write/Edit and run `export-html-png.mjs` yourself via
   Bash, or delegate to a `general-purpose` Claude Code subagent the same
   way you would for any other mechanical multi-file task). Mention to
   the user, once, that Codex wasn't available and this ran in Claude
   Code instead — don't silently swap execution paths without saying so,
   since the two can drift on style edge cases even when both are
   genuinely following this skill correctly.

Either way, still run the validation checklist below yourself (or have
whichever agent built it report against the same checks) before calling
a diagram done — routing to Codex doesn't relax what "done" means here.

## Dark only, going forward

Every new page built with this skill is **dark-only** — no light-mode
CSS block, no `data-theme` toggle, no theme-switch button. Earlier pages
in this repo's `diagrams/` folder (including the canonical
`otlp-bridge-ingest.html` reference) shipped a light/dark toggle; that
was a reasonable choice at the time, but it added CSS and a script for a
mode nothing in this repo's docs actually uses — every exported PNG in
`images/` has always been dark. Don't carry the toggle forward into new
pages. Use the fixed token block in "Page chrome" below as-is; if you're
extending an existing toggle-carrying page, leave its toggle alone rather
than stripping it out mid-edit, but don't add the pattern to anything
new.

## Naming convention

Match the existing set in `demos/01-dictionary/diagrams/`:

- `<topic>.html` — an architecture/flow diagram (e.g. `otlp-bridge-ingest.html`).
- `<topic>-mockup.html` — a UI mockup for design review before
  implementation (e.g. `admin-rpc-overview-mockup.html`).
- `<topic>-extraction.html` — a design-decision write-up, typically
  documenting a refactor or extraction (e.g. `tenants-manager-extraction.html`).

## Page chrome

Every page starts from this shell — a centered column, an eyebrow +
title + subtitle header, and one or more `<figure>` diagram panels
followed by optional prose sections. Tokens are lifted from
`shared/unifi-theme/unifi.css`, dark values only:

```css
:root {
  --text: #dee0e3; --muted: #b7bcc2; --dim: #737c87;
  --bg: #14171b; --panel: #1a1e23; --border: #2c3138;
  --accent: #006fff; --nested: #171c29;
  --sync: #4d94ff; --store: #2dd4bf; --evtl: #f0b429; --bad: #f87171; --hop: #8b93a1;
  --mono: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  --sans: 'Inter', -apple-system, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
}
* { box-sizing: border-box; }
body {
  margin: 0; background: var(--bg); color: var(--text);
  font-family: var(--sans); font-size: 13px; line-height: 20px;
  -webkit-font-smoothing: antialiased;
}
.wrap { max-width: 1080px; margin: 0 auto; padding: 24px 18px 64px; display: flex; flex-direction: column; gap: 26px; }
.head { display: flex; align-items: flex-end; gap: 16px; flex-wrap: wrap; }
.head-txt { flex: 1; min-width: 260px; display: flex; flex-direction: column; gap: 3px; }
.eyebrow { font-family: var(--mono); font-size: 10px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--dim); }
h1 { margin: 0; font-size: 20px; line-height: 26px; font-weight: 600; letter-spacing: -0.01em; text-wrap: balance; }
h2 { margin: 0; font-size: 15px; line-height: 21px; font-weight: 600; text-wrap: balance; }
.sub { margin: 0; color: var(--muted); max-width: 68ch; }
```

The eyebrow label carries a phase/section cross-reference, e.g.
`Phase 28 · ARCHITECTURE-COMMUNICATIONS §6` — always cite the phase
and/or the doc section a diagram serves, so the diagram stays traceable
back to the business rules or design decision it encodes (this repo's
Quality Rules already require traceable business rules; a diagram
illustrating one should name it, e.g. a `<code>BR-AC30</code>` reference
inline in a caption).

## Diagram panels

Each diagram sits in a `<figure>`, not a bare `<svg>` floating on the
page — the panel chrome and caption are what make this read as a
document rather than a loose sketch:

```css
figure { margin: 0; background: var(--panel); border: 1px solid var(--border); border-radius: 4px; overflow: hidden; }
.fig-body { padding: 14px 14px 4px; overflow-x: auto; }
figure svg { display: block; width: 100%; min-width: 0; max-width: 100%; height: auto; color: var(--muted); }
figcaption {
  padding: 9px 14px 11px; border-top: 1px solid var(--border);
  color: var(--muted); font-size: 12px; line-height: 18px;
}
figcaption b { color: var(--text); font-weight: 600; }
figcaption code, .sub code, li code, p code { font-family: var(--mono); font-size: 11px; background: color-mix(in srgb, var(--text) 8%, transparent); border-radius: 3px; padding: 0 4px; }
```

`overflow-x: auto` on `.fig-body` matters: a wide diagram scrolls inside
its own panel instead of breaking the page's layout or forcing text down
to an unreadable size. Every `<svg>` carries `role="img"` and a full,
descriptive `aria-label` summarizing what the diagram shows in prose —
not optional, these are technical diagrams meant to be genuinely
accessible, not decorative background art. The `<figcaption>` explains
the diagram in prose: a bolded lead-in claim (`<b>Ingest topology.</b>`)
followed by the explanation, with `<code>` spans for identifiers,
subjects, and rule IDs.

A single page commonly carries several `<figure>` blocks building up one
idea (e.g. topology → per-message handling → a not-chosen-vs-chosen
contrast), closed out by a `<section class="sec">` with an `<h2>` and a
`<ul>` of prose bullets, each `<li>` leading with a bolded claim:

```css
.sec { display: flex; flex-direction: column; gap: 11px; }
ul { margin: 0; padding-left: 18px; color: var(--muted); display: flex; flex-direction: column; gap: 5px; max-width: 74ch; }
li b { color: var(--text); font-weight: 600; }
```

## The SVG diagram style — this is what makes it look professional

The restraint here is the entire difference between this and a naive
generated SVG. Get these specifics right:

- **Outlined boxes, never solid fills.** `stroke-width: 1`, `fill: none`.
  A node is a thin rectangle with `currentColor` stroke, not a colored
  block:
  ```css
  .n  { fill: none; stroke: currentColor; stroke-width: 1; }
  .nb { fill: none; stroke: currentColor; stroke-width: 1; stroke-dasharray: 4 3; stroke-opacity: 0.5; }
  ```
  `.n` is an ordinary node; `.nb` is a dashed boundary/grouping rect (an
  account boundary, a process boundary, a "not chosen vs. chosen"
  divider). Reserve solid fills entirely for this drawing style — if a
  design genuinely needs a filled accent box, that's a sign to reach for
  `drawio-architecture-drawer`'s node-tile convention instead of forcing
  it into this one.
- **Small corner radius, never a pill.** `rx="3"` on ordinary node
  rects, `rx="4"` on boundary/group rects. A large relative radius reads
  as a rounded card, not a technical box.
- **Monospace for every piece of diagram text**, at small, disciplined
  sizes:
  ```css
  .lbl  { font-family: var(--mono); font-size: 11px; fill: var(--text); }
  .lbl2 { font-family: var(--mono); font-size: 9.5px; fill: var(--dim); }
  .grp  { font-family: var(--mono); font-size: 9.5px; fill: var(--dim); letter-spacing: 0.07em; }
  .edge { font-family: var(--mono); font-size: 9.5px; fill: var(--muted); }
  ```
  `.lbl` is a node's primary label; `.lbl2` is a secondary/detail line
  underneath it (a type, a config detail — e.g. `ONE durable consumer`
  under a `trace projector` node's `.lbl`); `.grp` is an
  uppercase, letter-spaced boundary/section header (`TENANT ACCOUNTS —
  ONE PER TENANT`); `.edge` labels a connector line. Node box heights
  stay small and consistent — 30–52px depending on how many label lines
  a box carries.
- **A small, closed set of semantic edge colors**, each with its own CSS
  class and its own arrowhead `<marker>` — never one generic arrowhead
  reused for every edge, since the color is what tells the reader what
  *kind* of thing is flowing:
  | Token | Hex | Meaning |
  | --- | --- | --- |
  | `--sync` | `#4d94ff` | synchronous / live flow |
  | `--store` | `#2dd4bf` | a write to a projection/store |
  | `--evtl` | `#f0b429` | eventual / replay / async path (usually also dashed) |
  | `--bad` | `#f87171` | an error/failure path |
  | `--hop` | `#8b93a1` | a generic/incidental hop — use sparingly |

  ```css
  .flow { fill: none; stroke: var(--sync); stroke-width: 1.4; }
  .proj { fill: none; stroke: var(--store); stroke-width: 1.4; }
  .rep  { fill: none; stroke: var(--evtl); stroke-width: 1.4; stroke-dasharray: 5 4; }
  .err  { fill: none; stroke: var(--bad); stroke-width: 1.4; }
  ```
  Pair each with its own marker, sized ~7–8px:
  ```html
  <marker id="a-sync" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
    <polygon points="0,1 8,4 0,7" fill="var(--sync)"/>
  </marker>
  ```
  Give each `<svg>` its own marker IDs (e.g. prefix by figure —
  `a-sync`, `b-sync`, `c-sync` — as `otlp-bridge-ingest.html` does) so
  multiple diagrams on one page don't collide.
- **Streams/queues are cylinders, not rectangles.** An open-top cylinder
  — an `<ellipse>` cap plus a `<path>` body arc — visually distinguishes
  an append-only log from a plain service/component box at a glance:
  ```html
  <path class="n" d="M334 158 L334 208 A112 9 0 0 0 558 208 L558 158"/>
  <ellipse class="n" cx="446" cy="158" rx="112" ry="9"/>
  ```
- **Coordinate discipline.** Pick a small set of shared x/y constants
  per diagram (column positions, row heights) before placing boxes, the
  same grid discipline `drawio-architecture-drawer` asks for — this is
  what produces crisp alignment instead of a hand-placed look, and it's
  what makes editing the diagram later tractable.

## Workflow

1. Decide the file name (`<topic>.html` / `<topic>-mockup.html` /
   `<topic>-extraction.html`) and create it in
   `demos/01-dictionary/diagrams/`, starting from the page-chrome CSS
   above.
2. Build each diagram inline as SVG inside its own `<figure>`, following
   the style rules above. Write the `aria-label` describing the diagram
   in full prose as you go — it forces you to check the diagram actually
   tells one coherent story.
3. Write the `<figcaption>` for each figure and any closing `<section>`
   prose, citing the phase/business-rule/doc-section the diagram
   encodes.
4. Render it — **always pass `--clip`, never rely on the plain
   `fullPage` capture:**
   ```
   node demos/01-dictionary/diagrams/export-html-png.mjs \
     demos/01-dictionary/diagrams/<file>.html \
     obsidian/V3-Platform/Architecture/Dictionary-POC/images/<file>.png \
     1024 --clip=".wrap"
   ```
   `--clip` accepts one or more comma-separated CSS selectors and
   captures the union of their bounding boxes plus a small margin,
   instead of the whole page. `.wrap` — the outermost content column
   every page chrome template above already defines — is the right
   default target for an ordinary diagram page: it bounds the export to
   the actual laid-out content.

   Without `--clip`, the script screenshots
   `document.documentElement.scrollHeight` at whatever viewport height
   Puppeteer started at — and Chrome floors `scrollHeight` at the
   initial viewport height, it never reports shorter than that even if
   the real content is smaller. `export-html-png.mjs` now sets that
   initial height to a deliberately tiny `100` (fixed in this script
   after this exact bug produced a large dead-space band at the bottom
   of several diagrams built under this skill — short pages were
   silently padded out to the *old* initial height of `1200`, while
   pages whose real content already exceeded 1200px, like
   `otlp-bridge-ingest.html`, never showed the symptom), so `fullPage`
   capture is no longer floored in practice. Still always pass `--clip`
   on top of that fix, not instead of it — clipping to `.wrap` bounds
   the export to actual laid-out content regardless of viewport-height
   floors, oversized SVG `viewBox`es, stray `min-height` rules, or any
   other future way a document's measured height could drift from its
   visible content. Treat the two as complementary: the script fix
   removed the specific bug that was found; `--clip` removes the whole
   class of bug it belongs to.

   **A `display: flex` body needs `align-items: flex-start`.** The script
   starts Puppeteer at a 100px-tall viewport (see the note above). A
   `body { display: flex }` page — the shape every `*-mockup.html` here
   uses — makes the mockup panel a flex item whose cross-axis size is
   *stretched to the container*, i.e. to that 100px viewport, and
   `.mock`'s `overflow: hidden` then swallows everything below. The export
   silently comes out ~76px tall showing only the top banner, which looks
   like a clip-selector problem and isn't. Add `align-items: flex-start`
   to `body` so the panel is sized by its content. Retrofitted to the five
   existing account mockups 2026-08-21; their committed PNGs predate the
   viewport change and were never regenerated, which is why the bug went
   unnoticed.

   For a UI mockup where only one component's chrome belongs in the doc
   (not the surrounding explanatory prose/tables also on the page), clip
   a narrower selector instead of `.wrap`:
   ```
   node demos/01-dictionary/diagrams/export-html-png.mjs \
     demos/01-dictionary/diagrams/<file>.html \
     obsidian/V3-Platform/Architecture/Dictionary-POC/images/<file>.png \
     1024 --clip=".mock"
   ```

   Width defaults to 1024 — that's the geometry every existing diagram
   in this repo was designed and reviewed at; only override it
   deliberately, since changing it changes the layout, not just the
   export resolution. `deviceScaleFactor` is fixed at 2 inside the
   script for crisp @2x output.

   Note the script borrows its Puppeteer install from the local
   `mermaid-cli` Homebrew package (`MERMAID_CLI` constant near the top
   of `export-html-png.mjs`) rather than a dependency of its own — a
   real, slightly awkward quirk of this tooling, not a bug to route
   around.
5. Check the script's own printed output —
   `<file>.png  <w>x<h> css px @2x  body-bg <color>` — and confirm
   `body-bg` matches the dark canvas hex (`rgb(20, 23, 27)` for
   `#14171b`). Since new pages under this skill carry no light-mode CSS
   at all, there's no toggle-pinning failure mode to worry about here —
   but the printed background is still worth a glance as a sanity check
   that the page rendered at all (an all-white or blank background means
   something failed to load, e.g. a bad file path).
6. Open the PNG and inspect it: no clipped text, no overlapping boxes or
   edges, labels legible at the exported size, arrowheads pointing the
   right direction, edge colors matching their semantic meaning.
7. Embed it in the target `ARCHITECTURE-*.md` doc, immediately followed
   by a blockquote naming the editable source and the exact re-export
   command — this is the established convention every existing embed
   follows, and it's what lets someone regenerate the PNG without
   archaeology:
   ```markdown
   ![<Alt text matching the SVG's aria-label>](images/<file>.png)

   Editable source: [<file>.html](../../../../demos/01-dictionary/diagrams/<file>.html)
   — hand-authored inline SVG rather than a Draw.io workbook page, so
   `./diagrams/export-png.sh` does **not** regenerate it. Re-export with
   `node diagrams/export-html-png.mjs diagrams/<file>.html \`
   `  ../../obsidian/V3-Platform/Architecture/Dictionary-POC/images/<file>.png 1024 --clip=".wrap"`
   from `demos/01-dictionary/`. The 1024px width is the geometry the page
   was reviewed at; changing it changes the layout. The `--clip=".wrap"`
   is load-bearing, not optional — see the Workflow section's step 4 for
   why (dropping it can silently reintroduce a dead-space band at the
   bottom of the export).
   ```
   Adjust the relative path depth (`../../../../`) to match where in the
   vault the embedding doc actually lives — `ARCHITECTURE-COMMUNICATIONS.md`
   and `ARCHITECTURE-ADMIN.md` sit at the same depth as this example, but
   check before copying it blindly into a doc at a different level.

## Validation checklist

- `xmllint --noout <file>.html` doesn't apply (it's HTML, not XML) — instead
  open the rendered PNG and eyeball it per step 6 above.
- The export was clipped (`--clip=".wrap"` or a narrower mockup
  selector) — never accept a plain `fullPage` capture for a finished
  diagram. If you inherited a PNG that wasn't clipped, don't just eyeball
  it and move on: re-render it with `--clip` and diff the two.
- No large dead-space band at the bottom (or any edge) of the image —
  content should fill close to the full exported height. If one shows
  up anyway even with `--clip=".wrap"` in use, the selector itself is
  probably too generous (e.g. it's matching an ancestor with its own
  inflated height) — narrow the clip selector rather than accepting the
  gap.
- No cell/box exceeds its `<figure>`'s content width at the 1024px
  render width — check `.fig-body`'s `overflow-x: auto` isn't silently
  hiding a diagram that's actually too wide to read without scrolling.
- Every `<svg>` has `role="img"` and a real `aria-label`, not a filename
  or a placeholder.
- Every edge uses one of the five semantic color classes deliberately —
  not `--hop` (grey) as a default for edges that actually have a more
  specific meaning.
- The doc embed's blockquote is present and its re-export command is
  copy-pasteable exactly as written (right file names, right relative
  path depth).
- `git diff --check` on the new/changed `.html` and `.md` files.

## Agent portability

This skill uses plain HTML/CSS/SVG, a Node.js script, and
repository-relative paths — no Codex-specific or Claude-specific
tooling. In Codex or another agent host, use that host's normal shell
and image-inspection capability for the validation steps.
