---
name: demos_transparent_terminal_first
description: Demos 05/06/07 must be transparent and terminal-first, NATS-docs style; not merely "no Python". Thin wrappers; test machinery kept apart from the teaching path
metadata:
  type: feedback
---

The goal for the auth demos (05, 06, 07) is a **transparent, terminal-first
demo**, not just "replace Python with shell". Stated 2026-10-08, when the
user said they are moving away from Python in demos.

- Follow the NATS docs' style: explicit `nats auth` / `nats-server`
  commands, readable config, minimal helper logic. A person must be able to
  understand and reproduce the setup without reading a scripting framework.
- Three layers, kept apart:
  - **Walkthrough** (`EXERCISE-NN-TERMINAL-STEPS.md`): direct commands plus
    short explanations. This is the teaching path.
  - **Convenience scripts** (`lab/`): thin wrappers around the same steps.
    They save typing; they must not hide how trust, creds or the resolver
    are set up.
  - **Validation scripts** (`exNN-check.sh`): assertions and deliberate
    failures. Extra manipulation (e.g. a wrong-seed creds file) lives here,
    labelled as negative-test machinery, never shown as normal setup.
- Prefer a native NATS inspection command (`nats auth ... info`, `account
  query`, monitor endpoints) over custom JWT decoding. Where decoding is
  needed, keep it small and say that decoding a JWT does not verify it.
- Replacing Python with clever `awk`/`jq` pipelines is no win. If a tool
  such as `jq` is needed, check for it explicitly; don't assume it.
- A demo-local `.run/` (keys, creds, resolver, logs; gitignored) is fine.
  Explain its contents and cleanup.

**Why:** inline Python made the setup opaque; the user wants a reader to
see what each step establishes.

**How to apply:** when writing or reviewing any demo step, script or
check, ask "can a reader see the NATS command doing the work?" Related:
[[nats_auth_terminology_and_demo_boundaries]], [[demos_are_small_slices_not_demo01]].
