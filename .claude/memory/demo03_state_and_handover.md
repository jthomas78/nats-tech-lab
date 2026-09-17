---
name: demo03-state-and-handover
description: "[demo 03] Where demo 03 stands as of 2026-09-17: stage 03 complete, seven topologies measured, 73 checks green. Stage 04 (pattern cards) not started. How to pick the work up."
metadata:
  type: project
---

State of `demos/03-multi-cluster-and-accounts/` on 2026-09-17, branch
`poc/dictionary3.1`, commit `4fa0e01`.

## Done

Stages 01, 02 and 03. Seven runnable shapes, one script each, in `lab/`:

| Script | Shape | Servers |
|---|---|---|
| `00-islands.sh` | T1 — no link | 6 |
| `01-gateway.sh` | T2 / A — gateway | 6 |
| `02-domain-over-gateway.sh` | B — gateway + per-cluster domain (**does not work**) | 6 |
| `03-arbiter.sh` | T3 / C — gateway + 1-node arbiter | 7 |
| `04-hub-and-leaf.sh` | T5 / D — hub + leaf clusters | 9 |
| `05-export-import.sh` | E — export / import between accounts | 6 |
| `06-arbiter3.sh` | T4 / F — gateway + 3-node arbiter | 9 |

Last full run: **73 passed, 0 failed, 19 notes**. Takes about 12 minutes.

## Not done

- **Stage 04 — the pattern cards PDF.** This is the closing deliverable for
  every demo. Read the `pattern-cards` skill first.
- Four measurements that one laptop cannot give: `D03-R4` cross-region latency;
  mirror lag and bandwidth; hub-as-a-real-store; leaf reconnect with many
  regions. These are listed honestly in the report's "Still open" table. Leave
  them there unless the rig grows.

## Rules that bite

- **Both reports are generated. Never hand-edit `REPORT.md` or `REPORT.html`.**
  Everything flows `run/results.tsv` -> `lab/render-report.py` -> both editions.
- `lab/figures.html` is the ONE hand-drawn file: seven SVG fragments keyed
  `t1`/`a`/`b`/`c`/`d`/`e`/`f` by `<!--FIG key-->` markers, spliced in by key.
  Follow the `html-diagram-drawer` skill.
- **Prose carries no numbers.** `render-report.py`'s hand-written prose cites
  check IDs only. Every figure comes from the TSV.
- A **`check`** has an expected answer and fails on any difference. A **`note`**
  records a measurement with no expected answer and **can never fail**, so it
  guards nothing. Do not turn a failing check into a note to make a run green.
- After any figure change, this must print `0 errors, 0 warnings` before you
  commit:
  `node demos/01-dictionary/diagrams/audit-svg-layout.mjs demos/03-multi-cluster-and-accounts/REPORT.html`
- **Codex cannot run that audit, or `export-html-png.mjs`.** Its sandbox cannot
  launch headless Chromium. Claude Code runs both.

## Do not tidy this away

Every scratch config and `server_name` starts `t-`, and servers start from
inside `$RUN_DIR` with a RELATIVE `-c t-xx.conf` path. That is what stops
`pkill -f "nats-server -c t-"` reaching the six committed configs. Rewriting
those to absolute paths has already killed the live lab twice.

Related: [[demo03-both-edges-of-a-failover-lie]],
[[demo03-mirror-over-gateway-vs-leaf]],
[[demo03-export-import-is-the-safe-sharing]],
[[demo03-three-node-arbiter-buys-one-node-of-slack]],
[[demo_playbook_session_2026-09-17]].
