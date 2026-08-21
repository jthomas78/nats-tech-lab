---
name: archive-plan-phase
description: Keep the three POC plan files split correctly — move a completed phase's detail out of .claude/plans/Main-POC-Plan.md into Main-POC-Plan-ARCHIVE.md, and deferred/candidate/on-hold phases into Main-POC-Plan-Candidates.md, leaving self-describing stubs behind. Use whenever a plan phase is finished, deferred, or put on hold, when the live plan has grown with completed or not-in-flight detail still in it, or when asked to archive, trim, or tidy the POC plan.
---

# Maintaining the POC plan's three-file split

`Main-POC-Plan.md` is read whenever plan context is needed, so it must hold
**only what is actively being worked or is next up**. Everything else lives in
one of two sibling files under `.claude/plans/`:

| File | Holds | Editable? |
| --- | --- | --- |
| `Main-POC-Plan.md` | In-flight and next-up phases, plus a one-line stub per phase that has moved out | yes |
| `Main-POC-Plan-ARCHIVE.md` | Full verbatim detail of **completed** phases, plus the whole renumbering history | **append-only** |
| `Main-POC-Plan-Candidates.md` | Phases that were **never implemented** — candidate, proposed, deferred, placeholder, approved-but-on-hold | yes |

Neither sibling file is read into context by default.

## Which file does a phase belong in?

- **Completed** → archive, in full. Leave a `- [x]` stub in the live plan.
- **Never implemented** (candidate, proposed, deferred, placeholder, approved
  but implementation on hold) → candidates, in full. Leave a `- [ ]` entry in
  the live plan's "Candidate, deferred, and on-hold phases" section.
- **Actively worked or next up** → stays in the live plan, in full.

A phase can move candidates → live plan → archive, but **never candidates →
archive directly**: the archive is for things that were actually built.

## Doing it

Follow the shape the existing sections already use: a `### Phase N — Completed
(archived YYYY-MM-DD)` heading, the standing note that full detail is archived
and *not read into context by default*, then one checked bullet per phase or
sub-phase naming what it delivered. Append to the archive as
`## Phase N — Completed (archived YYYY-MM-DD)` followed by the phase's verbatim
`### ...` detail.

## Rules that matter when doing this

- **Archive by completion, not by number.** Completed phases are rarely a
  contiguous block — a later phase is often finished while an earlier one is
  still `PROPOSED`. Never archive an unfinished phase just because it sits
  between two finished ones.
- **Never edit the archive's existing content.** It is a set of frozen
  snapshots. Append new sections; don't rewrite old ones, and don't update
  their phase numbers during a renumbering (the archived renumbering history
  records why). The candidates file carries no such rule — its entries are
  expected to be edited and re-scoped in place.
- **Keep the stub bullet self-describing.** Someone should be able to tell what
  a phase did, or is proposed to do, from the live plan alone, and only need
  the other two files for original rationale or checklist detail.
- **Renumbering logs go straight to the archive**, under its "Renumbering
  history" section — never into the live plan. They are pure history and were
  the plan's second-largest cost before being moved out (2026-08-21).
- **A phase deferred out of the live plan keeps its original number.** The 100+
  block is where *newly proposed* candidates start, not a destination to
  renumber into — renumbering costs cross-reference trails in docs, commits,
  and business rules for no gain.
- **Never lose content in a move.** Cut and paste verbatim, and check the
  combined size of the three files afterwards: it should grow by roughly the
  size of the new headings and stubs, never shrink.
