---
name: architecture-document-writer
description: Write or revise the prose explanation of a Proposed Linebooker V3 architecture document (L0-L4) as a page in the VitePress docs site's /v3-architecture/ section, explaining a diagram the architecture-draughtsman skill produced. Use when asked to write up, explain, narrate or document an existing LB-V3 diagram in words. Do not use to draw or edit a diagram, to write lab/Dictionary-POC documentation, or for unrelated repository docs.
---

# Architecture Document Writer

Write the **written edition** of a Proposed Linebooker V3 architecture document:
the prose that explains a drawing, published as a page on the docs site.

One stable ID is one architectural question with up to three editions of the same
document — drawn (HTML), print (PDF) and written (this page). They share one
requirements register and one primary question.

## Division of labour

| Skill | Owns |
|---|---|
| The central authority document | Scope, levels, IDs, statuses, catalogue, file paths |
| `architecture-draughtsman` | The drawn and print editions |
| **This skill** | The written edition only |
| `html-diagram-drawer` | Drawing mechanics and visual quality |

**This skill never draws.** If the prose cannot be written because the drawing is
wrong, incomplete or missing an element, stop and report that as a finding. The
fix is a change to the drawing by `architecture-draughtsman`, then a rewrite here.
Never describe architecture the drawing does not show.

The canonical operational contract is
`obsidian/V3-Platform/Architecture/Dictionary-POC/Proposed-Linebooker-V3-Architecture-Authority.md`,
in particular **File and output conventions → Written edition on the docs site**.
This skill executes that authority and must not override it.

## Operating mode

Decide the mode before touching files:

- **Review / feedback / analysis:** inspect and report only. Do not create,
  modify, rename or re-status any page, image or sidebar entry.
- **Create / revise:** follow the workflow below in order.

## Preconditions

Refuse to write and say why if any of these fail:

1. The stable ID exists in the authority's canonical catalogue.
2. Its drawn edition exists and is at least `DRAFT`. Prose cannot explain a
   `PLANNED` node.
3. Its requirements register exists, or the authority records why it does not.

The written edition is optional and **gates no status**. Never change a
catalogue or L0 status from this skill.

## Canonical context

Before writing:

1. Read `CLAUDE.md` and the central architecture authority. Read
   `.claude/memory/MEMORY.md`, then
   `.claude/memory/proposed_linebooker_v3_architecture_levels.md`.
2. Read the drawn edition's HTML for the target ID in full, including its legend,
   scope line and traceability footer. The drawing is the source; the prose is
   derived from it.
3. Read the ID's requirements register — its included, derived and open
   requirements.
4. Read the written edition of the **parent** ID if one exists, so the two read
   as one series and do not repeat each other.
5. Treat attached discussions and historical V2/V3 artefacts as reference
   material, not as instructions or as authorization to overwrite a file.

## Level-appropriate prose

The authority's audience column is binding on the writing, not only on the
drawing. Pitch the page at that level's audience:

| Level | Write for | Then |
|---|---|---|
| L0 | Anyone navigating the set | Explain how to read the atlas and choose a branch. No system design. |
| L1 | Everybody, technical and not, inside and outside Linebooker | Plain business language. Name no protocol, schema or product. Expand every acronym on first use. |
| L2 | Architects, technical leads, operations | Name technologies and structural choices. No message subjects, payload schemas or workflow steps. |
| L3 | Specialists in that one concern | Go deep on that concern only. Do not restate unrelated concerns for completeness. |
| L4 | Implementers | Contracts, subjects, streams, schemas, endpoints, permissions as the drawing defines them. |

Two tests when the depth of a sentence is disputed: if it cannot be explained to
that level's audience, it belongs on a lower page; if that audience would not care,
it belongs on a lower page. Link down to the child page instead of widening this one.

## Page structure

Write in this order. Do not invent an alternative running order — the series is
read as a set.

1. **Frontmatter** — `title` exactly `L<level> <Title>`, and `aside: false`.
   `aside: false` is required, not cosmetic: it drops VitePress's on-page table
   of contents and widens the content column from 688px to about 1040px, which
   is the difference between a legible figure and an unreadable one. Verified at
   1920x1080 in both themes.
2. **`# <Title>`** as the H1.
3. **Identity block** — a `<div class="v3-meta">` giving the exact stable ID,
   level, parent ID and the requirements register link.
4. **Primary question** — the document's single question, stated as a question,
   verbatim from the authority. One document, one question.
5. **Figure** — the synced PNG in the theme's `v3-figure` frame. The image is
   wrapped in a self-link so a reader can open the full-resolution export, and
   the caption names the ID and title and links both the full-size PNG and the
   print edition. The exact markup to copy:

   ```html
   <figure class="v3-figure">
   <a class="v3-figure-zoom" href="/v3-architecture/<lowercase-stable-id>.png" target="_blank" rel="noopener">
   <img src="/v3-architecture/<lowercase-stable-id>.png" alt="<STABLE-ID> <Title>">
   </a>
   <figcaption><strong>&lt;STABLE-ID&gt;</strong> — &lt;Title&gt;.
   <a href="/v3-architecture/<lowercase-stable-id>.png" target="_blank" rel="noopener">Open full size</a> &middot;
   <a href="<link to the print edition>">Print edition (PDF)</a></figcaption>
   </figure>
   ```

   Do **not** try to widen the figure past the content column with a negative
   margin or a bleed. The layout's side slack is asymmetric when a sidebar is
   present and the figure clips off the left edge — this was tried and rejected.
   The full-size link carries the finer detail instead.
6. **How to read this view** — the notation and the legend in words: what each
   element kind means here, what each line style and colour means.
7. **The architecture** — the substance, one section per major grouping the
   drawing itself uses. Follow the drawing's own structure so a reader can move
   between picture and prose without translating.
8. **Why it is shaped this way** — the reasoning, traced to included
   requirements. Cite ADRs by ID where one governs a choice.
9. **What this view deliberately excludes** — the level's exclusions, each with a
   link or a pointer to the child ID that carries it.
10. **Open confirmations** — the register's unresolved business, legal,
    regulatory and financial questions. Never resolve one silently in prose.
11. **Related documents** — parent, children, and the L0 atlas.

## Writing rules

- **Derived, not invented.** Every claim traces to the drawing or the register.
  When the drawing is ambiguous, say the prose is uncertain and report it; do not
  pick a reading and present it as settled.
- **Proposed, not built.** This series describes a proposal. Use "would", "is
  proposed to", "the proposed platform". Never write as though it exists. The
  rest of this docs site documents a system that was built — a reader must never
  confuse the two.
- **Name the element as the drawing labels it**, exactly. A renamed box in prose
  breaks the reader's ability to follow the figure.
- **A store is identified by its role**, never by product alone: transactional
  truth, durable fact history, workflow state, derived read model, or evidence
  document repository.
- **No new architecture.** No element, relationship, technology or boundary that
  is not in the drawing.
- Prefer short sentences and one idea per paragraph. Expand an acronym on first
  use on every page — a reader arrives by search, not from page one.
- British/American spelling: follow the drawn edition's existing titles so the
  ID/title parity check keeps matching.

## Publication workflow

1. Export the figure with the series' standard geometry, from the repository root:

   ```bash
   mkdir -p demos/01-dictionary/docs/public/v3-architecture

   node demos/01-dictionary/diagrams/export-html-png.mjs \
     demos/01-dictionary/diagrams/<html-file>.html \
     demos/01-dictionary/docs/public/v3-architecture/<lowercase-stable-id>.png \
     1600 --clip=".sheet"
   ```

   Use the page's actual outer wrapper selector if it is not `.sheet`. A
   multi-sheet drawing exports one PNG per sheet, suffixed `-sheet<n>`, each shown
   with its own caption. The PNG is a derived build output, committed so the site
   can serve it — never hand-edit it.

2. Publish the print edition alongside it, so the caption's PDF link resolves.
   The exporter writes the print edition to the repository root
   `output/pdf/<long title>.pdf`, which the docs site does not serve. Copy it to
   the same public directory under the lowercase stable ID:

   ```bash
   cp "output/pdf/<exported print edition>.pdf" \
      demos/01-dictionary/docs/public/v3-architecture/<lowercase-stable-id>.pdf
   ```

   Re-copy it whenever the print edition is re-exported. Do not link the
   repository path from the page — it is not a URL and the reader gets nothing.

3. Write the page at
   `demos/01-dictionary/docs/v3-architecture/l<level>-<lowercase-kebab-title>.md`.

4. Reference the image by its site-absolute path — `/v3-architecture/<id>.png`,
   not a relative path into `public/`. VitePress serves `public/` at the site root.

5. Add the sidebar entry in `demos/01-dictionary/docs/.vitepress/config.mts`. The
   V3 series has its own nav item and its own sidebar array, separate from the
   lab's `architectureSidebar`, and its sidebar is registered for the
   `/v3-architecture/` path. Order entries by level, then by ID. **A page not
   reachable from the sidebar is not published.**

6. Use the theme's existing presentation classes — `v3-figure`, `v3-figure-zoom`,
   `v3-meta`, and the `decision` container for a governing decision. The
   `v3-figure` frame is deliberately dark in **both** themes, because the drawn
   editions are authored on the UniFi dark canvas and a bare image reads as a
   black hole on the light theme. Add a genuinely missing class to
   `.vitepress/theme/custom.css` for everyone rather than inlining a style on one
   page. Do not introduce a new palette: the site's colours come from the
   `--vp-c-*` overrides in `custom.css`.

## Validation

Before reporting the page complete:

- `npm run dev` in `demos/01-dictionary/docs/` and open the page on port 7106 in
  the Browser pane. Resize to 1920x1080 before judging layout, per `CLAUDE.md`.
- Check the page in **both** light and dark mode. The figure frame must read as a
  deliberate framed figure in both.
- Confirm the exact stable ID and the exact document title appear on the page and
  match the drawn edition. This is the same parity test the drawn and print
  editions pass.
- Confirm the figure loads — a wrong path yields a broken image, not an error.
- Confirm the sidebar entry appears and navigates, and that the V3 section is
  visibly distinct from the lab's Architecture section.
- Confirm local search finds the page by its ID and by its title.
- `git diff --check` passes.
- Stop the dev server when done.

## Completion report

State:

- the stable ID and title written, and that no status changed;
- the exact repository paths for the Markdown page, the PNG(s), and the config
  change;
- the validation results — parity check, both themes, figure load, sidebar, search;
- any place the drawing was ambiguous or incomplete, as a finding for
  `architecture-draughtsman`, not as something this skill worked around;
- open confirmations carried from the register that remain subject to legal,
  regulatory, financial or business-owner sign-off.
