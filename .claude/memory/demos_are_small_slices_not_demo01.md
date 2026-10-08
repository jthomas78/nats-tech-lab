---
name: demos_are_small_slices_not_demo01
description: A demo is one small NATS feature slice; demo 01 is an oversized POC — never the model for, or a dependency of, a new demo
metadata:
  type: feedback
---

A demo showcases and tests ONE NATS feature or scenario as a small slice.
Demo 01 (dictionary) is a huge multi-container POC — too big to count as a
demo shape. Do not model a new demo on it, do not lean on it ("demo 01
already does X, so treat X as setup"), and do not build on its stack.

**Why:** user said so on 2026-10-08 while scoping a demo for operator mode,
decentralized auth and auth callout with WorkOS. I had argued operator mode
was "only setup" because demo 01 runs it — wrong framing.

**How to apply:** each new demo builds what it needs from nothing, in its own
sealed folder (demo 05 is the model: one bare server, host CLI, scripts).
If a demo grows past a handful of exercises, propose a split. Demo 01 may be
cited as background, never as a prerequisite. See [[demo_context_isolation]].
Demo 05 / 06 / 07 auth scope lines: [[nats_auth_terminology_and_demo_boundaries]].
