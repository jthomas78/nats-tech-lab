---
name: architecture-C4-draughtsman
description: Draw C4-model architecture views - Context, Container, Component, Deployment and Dynamic - as hand-authored HTML+inline-SVG diagrams in this repository's dark UniFi house style. Use when the request names C4, or asks for a context/container/component/deployment diagram, or asks to document a system's structure in C4 terms. These are standalone illustrations, not catalogue documents. Do not use when the deliverable is a numbered Proposed Linebooker V3 document (an LB-V3-Lx-nn ID, a catalogue status, a requirements register, or the L0 atlas) - that is architecture-draughtsman. Do not use for non-C4 mechanism, sequence or mockup figures - that is html-diagram-drawer directly.
---

# Architecture C4 Draughtsman

Draw C4-model views of a system as checked-in HTML+inline-SVG diagrams.

This skill merges two sources:

- **The C4 model's thinking** — what belongs at each level, what every
  element must carry, how relationships are labelled, and the anti-patterns
  that make a diagram lie. Distilled in
  `references/c4-modelling.md`, adapted from the MIT-licensed
  `c4-architecture` skill in `davila7/claude-code-templates`.
- **This repository's drawing mechanics** — `.claude/skills/html-diagram-drawer/SKILL.md`
  owns the page chrome, SVG style, layout geometry, audit, render and
  validation steps. Read it in full before drawing. This skill does not
  restate it.

**Adopt C4 concepts. Do not adopt C4 notation.** No Mermaid `C4Context`
blocks, no C4 blue-and-grey palette, no stick figures, no cloud-vendor
logos. Every view is drawn in the dark UniFi token set defined in
`html-diagram-drawer`'s "Page chrome" section. This matches the standing
rule in
`proposed-v3-arch/Proposed-Linebooker-V3-Architecture-Authority.md`
§ "Visual and notation standard", which already adopts C4's concepts and
rejects its visuals.

## Boundary against architecture-draughtsman

| The deliverable is… | Use |
|---|---|
| A numbered LB-V3 document (`LB-V3-L2-01`), a catalogue status change, a requirements register, the L0 atlas, or a governed A3/A4 PDF | `architecture-draughtsman` |
| A C4 view of any system — including Linebooker V3 subject matter — that is **not** entering the LB-V3 catalogue | this skill |

A C4 view drawn here never claims an `LB-V3-*` ID, never appears in the L0
atlas, and never changes a catalogue status. If the user wants one promoted
into the catalogue, stop and say so — that is a hand-off to
`architecture-draughtsman`, not something to do quietly.

## Operating mode

Decide before touching files:

- **Review / critique:** inspect and report only. Score the diagram against
  the checklist in `references/c4-modelling.md` § "Review checklist". Do not
  redraw.
- **Create / revise:** follow the workflow below.

## Workflow

1. **Set the scope.** Name the one system in scope, its boundary, and the
   audience. Write both into the sheet's subtitle before drawing anything.
2. **Choose the level(s).** Read `references/c4-modelling.md` § "Levels".
   Default to Context + Container. Draw a Component, Deployment or Dynamic
   view only when it answers a question the first two cannot.
3. **List the elements and relationships in text first.** Every element gets
   a name, a kind, a technology (Container level and below), and a
   one-line responsibility. Every relationship gets a direction, an action
   verb, and a protocol (Container level and below). If the list exceeds 20
   elements, split the view — do not shrink the labels.
4. **Check the list against the anti-patterns** in
   `references/c4-modelling.md` § "Anti-patterns" before drawing. Catching a
   container/component confusion in a list costs a minute; catching it in
   placed SVG coordinates costs an hour.
5. **Draw it.** Follow `html-diagram-drawer` end to end — page chrome, SVG
   style, layout geometry, the lane grid, and its naming convention. Apply
   the element and relationship mapping in this file's next two sections.
6. **Every sheet carries a legend.** Explain every colour, every line style
   and every acronym used on that sheet. A C4 view without a legend is
   incomplete here, even though plain C4 tolerates one.
7. **Audit, render, inspect, embed** exactly as `html-diagram-drawer`'s
   Workflow steps 4-8 specify. Do not invent a variant of those commands.

## Element kind to visual mapping

C4 names the kind; the UniFi tokens carry it. Tokens are the ones in
`html-diagram-drawer` § "Page chrome" — do not introduce new hexes.

| C4 element kind | Visual |
|---|---|
| Person, internal | Rounded rect, `--accent` stroke, small rounded avatar mark. Never a stick figure |
| Person, external | Same shape, dashed `--dim` stroke |
| Software system, in scope | Solid panel, `--accent` stroke, heavier weight |
| Software system, external | `--panel` fill, dashed `--hop` stroke |
| Container, application or service | Rect, `--panel` fill, `--sync` stroke |
| Container, datastore | Rect with a store mark, `--store` stroke |
| Container, queue / stream / topic | Rect with a queue mark, `--evtl` stroke |
| Component | Smaller rect, `--nested` fill, `--sync` stroke |
| Boundary (enterprise, system, container) | Dashed `--border` rect, `--nested` tint, label set top-left in the mono eyebrow style |
| Deployment node | Nesting rect, `--border` stroke, node type in a mono eyebrow above the name |

Rules that override C4's own defaults:

- **State the kind on the element.** A mono eyebrow line reading
  `CONTAINER · DATASTORE` beats relying on colour alone. Two boxes of
  different kinds must never be indistinguishable in greyscale.
- **A datastore is named by its role, not only its product** — transactional
  truth, durable fact history, workflow state, derived read model, or
  evidence document repository. Product name goes in the technology line.
- **No cloud-vendor logos or icons**, in any view, including Deployment.
  A Deployment view may be cloud-shaped; it may not be cloud-branded.

## Relationship mapping

Every line is unidirectional. Every line is labelled. Every line at
Container level and below names its protocol.

| Meaning | Style |
|---|---|
| Synchronous request / response | Solid `--sync` arrow |
| Asynchronous event, message or publish/subscribe | Dashed `--evtl` arrow |
| Read or write against a store | Solid `--store` arrow |
| Failure, fallback or degraded path | `--bad` arrow |
| A hop with no more specific meaning | `--hop` arrow — a last resort, not a default |

Label form: action verb first, then the protocol as a second line or a
parenthetical. `Publishes ship.arrived to · JetStream`, not `Uses`.
Bare `Uses`, `Calls` and `Talks to` are rejected at review.

For a Dynamic view, number every interaction `1.`, `2.`, `3.` in the label,
and draw one only when the static views genuinely fail to explain the
sequence.

## Naming and output

Follow `html-diagram-drawer`'s naming, with a `c4-` prefix so a C4 set is
recognisable as one:

- `c4-<system>-context.html`
- `c4-<system>-containers.html`
- `c4-<system>-components-<feature>.html`
- `c4-<system>-deployment-<environment>.html`
- `c4-<system>-dynamic-<flow>.html`

Sources live in `proposed-v3-arch/drawings/`. PNGs export to
`obsidian/V3-Platform/Architecture/Dictionary-POC/images/` and embed into the
relevant `ARCHITECTURE-*.md` with the re-export blockquote
`html-diagram-drawer` § Workflow step 8 defines. Do not create a
`docs/architecture/` folder — the upstream C4 skill's output convention does
not apply in this repository.

A PDF is optional here and is not a governed edition. When one is asked for,
use `export-html-pdf.mjs`, set `@page { size: A4 landscape; margin: 0; }` (A3
only if the content genuinely will not fit), and match the sheet's own width
and height to it — `preferCSSPageSize: true` makes the page CSS authoritative.

## Consistency across a set

A C4 set is read as one document. Within a set:

- One element keeps one name, one colour and one kind in every view.
- A colour carries the same meaning in every view.
- Every sheet repeats the legend rather than pointing at a sibling sheet.
- The subtitle of each sheet states its level and its scope in one line.

## Completion report

State which views were drawn or changed and at which levels. Give the exact
repository path of each HTML source and each exported PNG. Report the
`audit-svg-layout.mjs` result, the printed `body-bg`, and what the visual
inspection of each PNG found. Name any element whose technology,
responsibility or protocol you inferred rather than confirmed — those are the
lines a reviewer must check.
