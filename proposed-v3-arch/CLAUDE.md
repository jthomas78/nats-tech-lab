# CLAUDE.md — `proposed-v3-arch/`

Rules for this folder. An agent working inside it reads this file **instead of**
the repository root `CLAUDE.md`, except where this file points back.

## What this folder is

The **Proposed Linebooker V3 architecture** document series: its governing
rules, its requirements registers, and its drawings. It is a **proposal**, not a
record of anything this lab built. Nothing here is implemented.

It was moved here from `demos/01-dictionary/diagrams/proposed-v3-arch/` and
`obsidian/V3-Platform/Architecture/Dictionary-POC/` on 2026-09-18, so that the
rules sit beside the drawings they govern.

## Layout

```
proposed-v3-arch/
  CLAUDE.md                          this file
  Proposed-Linebooker-V3-*.md        hand-written sources of truth
  drawings/                          every hand-authored HTML drawing
```

Sources sit at the top so a person opening the folder sees them first. The
drawings sit one level down because they are the thing the sources govern, not
the other way round. Both kinds are hand-authored; neither is generated. The
split is by role, not by whether a file can be rebuilt. Drawings moved down on
2026-09-18.

The Markdown files must stay at the top level: the authority links each register
by plain sibling filename, so moving one breaks those links.

## Do not delete the Markdown files

`Proposed-Linebooker-V3-*.md` are the **source of truth** for this series. They
are not generated, they are not notes, and nothing can regenerate them.

- Never delete one. Never move one out of this folder.
- Never "tidy" this folder by removing Markdown that looks like documentation
  sitting among HTML deliverables. That mix is deliberate.
- Renaming one is a repo-wide link sweep. See the authority's change control.

The HTML drawings are also hand-authored and also not generated. Only the PDFs
under `output/pdf/` are regenerable, and only from these HTML files.

## Read the authority first

For **any** creation, revision, catalogue or review work in this series, read
`Proposed-Linebooker-V3-Architecture-Authority.md` before anything else. It is
the central operational authority: the L0-L4 hierarchy, the canonical catalogue,
stable IDs, statuses, per-level scope, the visual and notation standard, paper
size, traceability, and change control.

It also states its own precedence. When sources disagree:

1. The user's explicit instruction for the current change.
2. This authority.
3. The applicable level requirements register.
4. The current HTML and PDF artefacts.
5. Discussions, research notes, historical documents and session memory.

The skills implement the authority's workflow and **must not redefine it**.

## Three kinds of file live here, and only one is governed

| Files | Governed by | Has an `LB-V3` ID |
|---|---|---|
| `Proposed-Linebooker-V3-*.md` | Themselves — the authority and its registers | n/a |
| `drawings/proposed-linebooker-v3-l*.html` | The authority's catalogue | Yes |
| `drawings/c4-*.html` | `.claude/skills/architecture-C4-draughtsman/SKILL.md` | **No** |
| `drawings/*-landscape.html`, `drawings/adr-*.html` | Nothing. Standalone figures | **No** |

A C4 view or a standalone figure never claims an `LB-V3-*` ID, never appears in
the L0 atlas, and never changes a catalogue status. If you are about to give one
an ID, you are drawing the wrong kind of document.

## Which skill to use

| Task | Skill |
|---|---|
| Draw or revise a numbered `LB-V3` document, or its catalogue | `.claude/skills/architecture-draughtsman/SKILL.md` |
| Write the prose edition of an existing `LB-V3` drawing | `.claude/skills/architecture-document-writer/SKILL.md` |
| Draw a C4 Context / Container / Component / Deployment / Dynamic view | `.claude/skills/architecture-C4-draughtsman/SKILL.md` |
| Any other figure here, and all drawing mechanics | `.claude/skills/html-diagram-drawer/SKILL.md` |

## Rules from the root `CLAUDE.md` that still apply

- **One command per `Bash` call.** Do not chain with `&&`.
- **Do not read large docs whole by default.** The authority and the bigger
  registers are long — `grep` or a targeted `Read` first.
- **The dark UniFi palette**, and the 1920x1080 design viewport for any layout
  judgement.
- **ADRs** live in `obsidian/V3-Platform/Architecture/ADR/`, not here. A V3
  principle is an ADR with `scope: v3`. This folder's figures may illustrate
  one — `drawings/adr-056-five-axes-vocabulary.html` does — but the decision
  itself is always the ADR.

## Outputs

- Print editions: `output/pdf/Proposed Linebooker V3 Architecture - L<level> <Title>.pdf`
- Written editions: `demos/01-dictionary/docs/v3-architecture/`

Both are regenerated from the HTML in `drawings/`. `output/pdf/` stays at the
repository root: it also holds the ADR card deck and older reference decks that
this folder does not own. See the authority's "File and output conventions" for
the exact commands and naming.
