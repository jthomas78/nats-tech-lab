---
name: verify-before-resuming-offloaded-work
description: When resuming a compacted conversation whose last action was "I'm offloading this to another agent," verify current git state before acting on the summary's account of what's still open
metadata:
  type: feedback
---

Before acting on a resumed/compacted summary that describes a gap or pending task, check `git log`/`git diff` and confirm the relevant files still match the summary's account — especially when the summary's last user message was "I'm going to offload this to another agent."

**Why:** During Phase 11.10 (frontend-port localization), a summary described "no frontend test harness exists yet" as the open gap and the next step. Between that session and this one, the user's offloaded agent had already implemented and committed the harness (`f9b36e9 feat(frontend-port): split fleet and port views with localization tests` — Vitest + `@vue/test-utils`, `App.spec.js`, CI wiring, all checked off in `.claude/plans/Dictionary-Service-Plan.md`'s Track 3). Acting on the stale summary, I re-added Track 3 to the plan as new/unchecked work, producing a duplicate section sitting right above the real, already-completed one. Caught only by running `git diff` on the plan file and comparing against `git log -- <path>` for the actual commit history.

**How to apply:** A conversation summary is a snapshot frozen at compaction time, not a live view — this holds generally ([[br-classification-heuristic]] and the "before recommending from memory" rule apply the same logic to `~/.claude` memory, but plan/doc files inside the repo are just as susceptible). When a resumed task's premise is "X is missing" or "Y is still open," grep/read for X or Y in the current tree first, especially if real time may have passed or another agent/session could have touched the same files. Treat a "some agent is handling this" handoff as an explicit signal that the state may have moved — verify before writing, and after any edit to a shared doc (plan files, BUSINESS_RULES.md), diff against HEAD to make sure the edit is additive, not a duplicate of something already there.
