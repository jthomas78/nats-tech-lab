# Codex is the default worker, and it must run GPT-5.6 Terra at High effort

Set by the user 2026-09-14, while asking for topology diagrams in
`demos/03-multi-cluster-and-accounts/diagrams/combination-matrix.html`.

## Who does the work

1. **Codex by default.** Hand the task to Codex unless told otherwise.
2. **Claude is the fallback** — use Claude when Codex is unavailable, refuses,
   or the pinned model below is gone.
3. **The user naming Claude is the override.** If the user says "you do it" /
   "Claude does it", Claude does it. That beats the default.

This replaces the earlier one-off instruction *"don't use codex to create it.
Do it yourself"*, which applied to a single artifact, not to everything.

## The model, every time

**`gpt-5.6-terra`**, reasoning effort **`high`**. Not the machine default.

`~/.codex/config.toml` on this Mac defaults to `model = "gpt-5.6-sol"` and
`model_reasoning_effort = "medium"`, so the model **must be passed explicitly on
every call**. It is never picked up from config.

Checked 2026-09-14 on codex-cli 0.148.0:

- `gpt-5.6-terra` is a real model and has run on this machine before.
- The plugin runtime
  `~/.claude/plugins/cache/openai-codex/codex/1.0.6/scripts/codex-companion.mjs`
  takes `--model <model>` and `--effort <none|minimal|low|medium|high|xhigh>`.
- Bare CLI equivalent: `codex -m gpt-5.6-terra -c model_reasoning_effort="high"`.

## If the model is gone

**Do not silently fall back to another model.** Tell the user, and let them
choose: go ahead with whatever Codex offers, or hand the job to Claude.

See also [[admin_ui_design_viewport]].
