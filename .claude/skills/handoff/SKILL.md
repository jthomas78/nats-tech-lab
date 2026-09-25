---
name: handoff
description: Close out the current work and write a self-contained prompt so a fresh Claude session or Codex can continue without this conversation's context. Use when asked to hand off, write a prompt for a new session, delegate to Codex, or when a long session should be restarted to save tokens.
---

# Handoff

A fresh session costs far fewer tokens than a long or compacted one. This
skill makes the break clean.

1. **Check.** Run the tests that cover what changed. If a test fails, stop and
   tell the user. Do not hand off red work.
2. **Archive.** If a plan phase finished, use the `archive-plan-phase` skill.
3. **Fix stale state.** Update any line in `CLAUDE.md`, `.claude/memory/` or
   docs that this session made wrong. Nothing else.
4. **Commit** only files changed in this session. Never push.
5. **Write the prompt** to `.claude/plans/handoffs/<phase-or-topic>-handoff.md`:
   - Goal, in one or two sentences.
   - Done so far, with commit hashes.
   - Next steps, in order, each with its acceptance check.
   - Decisions already made. Say "do not re-open".
   - Traps found this session (wrong causes ruled out, env gotchas).
   - Exact file paths to read first. Keep the list short, and do not paste file contents.
   - Target: "Claude" or "Codex". For Codex, state every assumption explicitly.
6. **Verify the prompt.** Re-read each claim against the code or git log. A
   backwards claim in a handoff has happened before.
7. **Report** the file path and one line the user can paste into the new session.
