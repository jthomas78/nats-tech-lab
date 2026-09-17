---
name: demo_playbook_session_2026-09-17
description: 2026-09-17 session — demo-playbook.html is the one demo lifecycle (04 Learn, not 08); demo 03 retro-fit still open; development-playbook.* still untracked
metadata:
  type: project
---

# Session 2026-09-17 — the demo playbook landed

Commit `7dfa142` "docs: a demo playbook, and one lifecycle instead of two".

## What shipped

- `demo-playbook.html` + `demo-playbook.pdf` — nine A4 pages, built on
  `development-playbook.html`'s stylesheet and its eight-field stage-card anatomy.
- Four stages, `01`–`04`: Define the question / Design the rig / Validate
  (build + measure are ONE stage) / Learn. The stage the development playbook
  numbers `08 Learn` is **renumbered to `04`** here, not left as a gap. The PDF
  text was checked — no stray `08` except real page numbers.
- Root `CLAUDE.md`'s own four-step demo list was deleted. The table there now
  points at the playbook and uses `04`, so the two files agree.
- Two rules the playbook adds and applies to itself: every demo declares its
  **role** (showcase / validation / both), and every requirement gets an **ID**
  (`D03-R1`, …), because stage `04` has nothing to point back at otherwise.
- Page 09 scores demo 03 against all four stages, and says **do not re-run
  stage 03** — the evidence is good and re-measuring would be theatre.

## Why demo 03 is the worked example

Demo 03's goal had to be recovered from a git commit message. Nothing in
`demos/03-multi-cluster-and-accounts/` stated it. That is the proof that
declaring the role matters.

## Open — next session starts here

1. **Retro-fit demo 03 to the playbook** (approved, not started):
   - `demos/03-multi-cluster-and-accounts/README.md` — stage 01: the question,
     the role, requirement IDs `D03-R1`…
   - that demo's `CLAUDE.md` — stage 02: ports, naming, the `t-` prefix rule
   - the pattern cards deck — stage 04 (see the `pattern-cards` skill)
   - Do **not** re-run stage 03.
2. **Loose end:** `development-playbook.html` and `development-playbook.pdf` are
   still **untracked**. `CLAUDE.md` links `development-playbook.pdf`, so the link
   is broken for anyone else cloning the repo. Waiting on the user's word to add
   them.

Related: [[demo_context_isolation]]
