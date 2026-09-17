---
name: demo03-three-node-arbiter-buys-one-node-of-slack
description: "[demo 03] T4 (gateway + 3-node arbiter, 9 peers, majority 5) survives a whole region PLUS one more node. T3's one-node arbiter (7 peers, majority 4) has no slack at all."
metadata:
  type: project
---

Measured 2026-09-17 by `demos/03-multi-cluster-and-accounts/lab/06-arbiter3.sh`.
Before this, the README marked T4 `inferred` — it had never been built.

**The shape.** 9 servers: `za` 1–3, `au` 1–3, `arb` 1–3. The arbiter cluster
uses new ports — client `454x`, http `854x`, route `654x`, gateway `754x`. Each
server names all three gateway peers (local `gw3()` helper in that script).

**What was measured, walking down one node at a time:**

| Peers up | Result | Checks |
|---|---|---|
| 9 of 9 | one meta group, size 9, majority is 5 | `F1`, `F2` |
| 6 of 9 (AU frozen) | leader elected, ZA still creates streams | `F3`, `F4` |
| 5 of 9 (+1 arbiter node) | **still works** — exactly the majority | `F5`, `F6` |
| 4 of 9 (+1 more) | leader gone, creates refused, publishes into EXISTING streams still work | `F7`, `F8`, `F9` |
| back to 9 | size 9 again | `F10` |

**The point (`F11`).** Losing a whole region leaves **one node of slack** at 6
of 9. T3's one-node arbiter in the same state sits at 4 of 7 — exactly the
majority, no slack, one more node freezes it (`C12`). That is the whole reason
to pay for a third site with three servers instead of one.

**Cost.** A shared fate stays shared. At 4 of 9 T4 freezes exactly the way a
plain gateway does.

A region is cut with `kill -STOP` (`freeze`/`thaw` in `_common.sh`), never with
Docker. `F6` needs the retry loop, not a sleep —
see [[demo03-both-edges-of-a-failover-lie]].
