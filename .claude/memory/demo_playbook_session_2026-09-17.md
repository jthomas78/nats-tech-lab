---
name: demo_playbook_session_2026-09-17
description: 2026-09-17 — demo-playbook.html/.pdf is the ONE demo lifecycle, four stages 01-04 (04 Learn, not 08); root CLAUDE.md points at it; demo 03 retro-fit finished except stage 04
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

## The retro-fit is done — only stage 04 is left

Done later the same day, commits `bf43d17` -> `e96c803`:

- `demos/03-multi-cluster-and-accounts/README.md` — stage 01, with requirement
  IDs `D03-R1`…`D03-R9`, and the role declared: **validation only**.
- that demo's own `CLAUDE.md` — stage 02, the rig. Says the **topology** is the
  variable, which is the mirror image of demo 02.
- `lab/` — stage 03 made re-runnable. Seven scripts, `./run-all.sh`, and both
  `REPORT.md` and `REPORT.html` generated from `run/results.tsv`. Last run:
  **73 passed, 0 failed, 19 notes**.
- `development-playbook.html` and `development-playbook.pdf` are now **tracked**,
  so `CLAUDE.md`'s link resolves. That loose end is closed.

**Only stage 04 — the pattern cards PDF — remains.** Read
[[demo03-state-and-handover]] before starting it; it carries the rules that bite.

Related: [[demo_context_isolation]], [[demo03-state-and-handover]]
