---
name: demo03-both-edges-of-a-failover-lie
description: "[demo 03] `/jsz?meta=1` names a dead leader for tens of seconds AFTER a majority goes dark, and names a new leader seconds BEFORE it will accept a change. Never trust one reading."
metadata:
  type: project
---

Measured 2026-09-17 in `demos/03-multi-cluster-and-accounts/lab/`.

Two separate traps, same cause: `/jsz?meta=1` reports what a server *believes*,
not what the cluster will *do*.

**Trap 1 — the stale leader (already known, documented in `_common.sh`).**
After the majority goes dark, `/jsz` keeps naming a leader. Measured waits:
`A10a` 22s and 54s on two runs of `01-gateway.sh`; `F7a` 40s on
`06-arbiter3.sh`. The named server is already gone.

**Trap 2 — the unready leader (found 2026-09-17, NEW).**
After an election, `/jsz` names the new leader *before* it will take a change.
Ask the instant it is named and `stream add` is refused. Measured as `F6a` in
`06-arbiter3.sh`: 4s on one run, 0s on another — so it is a race, not a fixed
delay.

**How the rig handles it.** Do NOT paper over it with a magic `sleep`. Retry the
real operation in a loop and record how long it took as a `note`. That turned a
flaky check into a finding. The pattern lives in `06-arbiter3.sh` around check
`F6` — 20 tries, 2s apart, then `note F6a` with the elapsed seconds.

Helpers in `_common.sh` that exist for this: `wait_live_leader`,
`seconds_until_no_leader`, `wait_meta_leader`.

Related: [[demo02-wan-cut-freezes-jetstream-management-only]].
