# Plan — exercise 10 evidence replay (a lab-shell plugin for demo 03)

> **SUPERSEDED 2026-10-06 by [`PLAYGROUND-PLAN.md`](PLAYGROUND-PLAN.md).**
> The user chose a live playground on the real T4 rig, with a Go control
> service. A replay of recorded results is no longer the primary UI. Nothing
> in this file was built. Its parts 1 and 2 (record more evidence, build a
> replay dataset) are dropped. Its lab-shell findings (L1 preview proxy, L2
> Home card) carry over into the new plan. The text below is kept unchanged
> as the record of the earlier decision.

**Status: plan only. Nothing here is built, recorded or run.** Written
2026-10-05 after a feedback round. The user chose: record the missing
evidence (decision 1A), and keep the existing Home card but point it at the
plugin (decision 2A).

**Role: unchanged — validation.** The replay is a reading aid for evidence
that the lab already measured. It runs no NATS, measures nothing and decides
no outcome. It is not a showcase, because nothing in it works live. If that
changes, the role changes, and `README.md` must say so first (playbook trap:
"letting the role drift silently").

## The question

> Can a reader step through what exercise 10 actually recorded — leader,
> term, voters, writes — one recorded moment at a time, and see where the
> record has nothing to show?

The replay answers "what did the rig record?" It never answers "what would
NATS do?"

## Hard rules for everything in this plan

1. **Animate only captured observations.** A value changes on screen only at
   the time of a recorded reading or event. Between readings, the last
   reading stays, with its age. Past its freshness limit it shows as
   **stale**. Where no reading exists, the screen shows **no reading**.
   Nothing is interpolated: no election, no leader, no storage outcome.
2. **Three layers, never mixed, always told apart on screen:**
   - **Observed** — a reading or event, with run ID, source file and time.
   - **Recorded result** — a check, note or verdict row, with its check ID.
   - **Derived** — quorum arithmetic only: available voters against the
     fixed quorum of 5 out of 9. Computed in the UI by one function, from
     observed availability, and labelled "derived".
   - Explanatory text (findings, notes) is a fourth thing. It is prose,
     with its source path, and it never drives the timeline.
3. **No authoritative leader when the record disagrees.** The summary names a
   leader only when every answering server in one complete poll names the
   same leader in the same term, and that server says `LEADER`. Otherwise it
   says "no agreed leader" and lists what each server reported.
4. **No action that predicts.** "Make ZA dark" moves the playhead to a
   **recorded** freeze of za in the chosen scenario. It is disabled when the
   scenario has no such freeze ahead of the playhead. There is no free
   "toggle a region" that computes what happens next.
5. **Preserve the evidence.** Kept runs under `lab/run/evidence/` are never
   edited, moved or rewritten. Historical verdicts stay as recorded. The
   replay ships a **normalized copy** with provenance back to each source.
6. **The prose-only delayed-storage observation is not replay evidence.**
   `exercises/EXERCISE_OBSERVATIONS.md:570-576` records one write that was
   stored after a timeout. It came from a part run that was not kept. The UI
   shows it only on the Overview, marked "prose observation — not in the
   kept evidence", with its source line. It never appears on a timeline.
7. **Demo 04's MFE conventions, `VITE_PLUGIN_SOURCE=build`.** One remote. The
   plugin entry draws no `AppShell` and imports no router. UniFi theme. Judge
   layout at 1920x1080.

## Requirements

Next free IDs after `D03-R18`. Add them to `README.md` only when the user
says so.

| ID | Requirement | Status |
|---|---|---|
| **D03-R19** | Exercise 10 saves the observations its loops already make, with one clock, and records what it saved and how in `env.txt` | not built |
| **D03-R20** | One new full run fills the observation gaps, judged on recording completeness and rig checks only — never on a NATS outcome | not run |
| **D03-R21** | A committed, normalized replay dataset, generated from kept runs, with source provenance for every value | not built |
| **D03-R22** | One remote with three `ClusterPanel` instances and one shared playback controller, following demo 04's conventions | not built |
| **D03-R23** | The replay animates only observed values, shows gaps and staleness, and keeps observed, recorded, derived and prose apart | not built |
| **D03-R24** | One deliberate Home entrance, with the introduction, findings and run instructions kept | not built |
| **D03-R25** | Packaged preview serves built plugin assets with every demo dev server stopped; development keeps hot reload | not built |

## Part 1 — record the missing evidence (D03-R19, D03-R20)

### What is missing today

Only the seven recovery windows have readings over time (`obs/ML*-watch.jsonl`,
all nine servers every 0.5 s, from about 0.45 s before each thaw to about
+15 s). Everything else is a check row with no time:

| Finding | Kept today | Gap |
|---|---|---|
| Hub leadership can be requested | rows; ML36a durations | no readings during the step-2 trials |
| A dark region does not move a hub leader | rows | the 15 s hold loop polls and discards |
| Recovery can disturb it | obs readings | none inside the window |
| Quorum and stream are separate | rows; final ledgers | no times on writes or readbacks |
| A timed-out publish is not a failed publish | one duration row | no start time; the later readback is one count |
| A leader field can be stale | two numbers (ML116a) | the 0.2 s `hub_kinds` loop polls and discards |

The results file `10-all.tsv` has no timestamps. The ledgers have no times
and show only the end state.

### Save what is already polled — no new polling

Each loop below already makes its request. The change is to **write the
answer down** with the epoch time from `now()` (the same clock as `obs/` and
`.thaw`). File names carry the check ID the loop serves.

| Loop (today, `lab/10-hub-meta-leader.sh`) | What it already reads | Saved as |
|---|---|---|
| `wait_agreed_leader` (step 2 trials, every return, step 6) | `meta_leader 8541` and `meta_group_count` of all nine, every 0.2 s | `obs/<id>-agree.jsonl` |
| step 3 hold loop (`HOLD_S`, every 0.5 s) | `meta_leader 8541` | `obs/<id>-hold.jsonl` |
| step 4 election loop (every 0.2 s) | `meta_leader 8541` | `obs/<id>-elect.jsonl` |
| step 5 quorum-loss loop and hold loop | `hub_kinds`: one token per hub server, from `/raftz` | `obs/<id>-hubkinds.jsonl` |
| step 5 za-return loop (every 0.2 s) | `meta_leader 8541`, `meta_group_count` of live ports | `obs/<id>-zaback.jsonl` |
| `current_peers` / `live_current_peers` loops | the count, every 1 s | `obs/<id>-peers.jsonl` |
| `pub_ack_via` | the CLI reply | `obs/pub.jsonl`: start, end, port, site, msg ID, seq or failure |
| step 6 timed-out publish | the CLI error | `obs/pub.jsonl`, the same record, `result: timeout` |
| step 6 "stored later?" read | `last_seq`, once | `obs/<id>-held.jsonl` |
| step 6 retry loop (same msg ID) | each attempt | `obs/pub.jsonl`, one record per attempt |
| `readback` | each Direct Get compare | `obs/readback.jsonl`: time, site, seq, matched |

Two sidecar files, so `10-all.tsv` keeps its format and every tool that reads
it keeps working:

- `rows-t.tsv` — check ID and the epoch time it was recorded. Written by the
  exercise-10 script only. `_common.sh`'s `record()` is shared by every lab
  script, so it does not change.
- `steps.tsv` — step name, start time, end time.

`classify-10.py kinds` gets an optional `--out <file>`. The request stays the
same, and the tokens on stdout stay the same.

**Single-server readings stay single-server.** `meta_leader 8541` is one hub
server's `/jsz` leader field, with no term. The replay labels it exactly so,
and shows the other eight servers as "no reading" for that span. Adding an
all-nine watcher during dark periods would be **new polling**, so this plan
does not add it.

### Provenance and timing effect

- `env.txt` gets a `recording` line. It lists each saved family and its
  cadence. The script and classifier hashes are already recorded.
- **Do not assume recording has no timing effect.** Each saved reading adds
  one file append, and `hub_kinds --out` adds one write per poll. The
  renderer shows the new run's timing notes beside the kept runs as
  **context**: ML36a settle times, ML116a first/all, the no-leader intervals
  in ML<R+3>c, and the total run time from `when` to `ended`. This is not a
  check, and it does not claim "no effect".

### Completeness checks

New rig checks, numbered from the next free ID (`ML159` onward, assigned when
written, never reused). One per saved family: the file exists, is valid
JSONL, and covers its loop's time span. They sit in no scenario verdict's
start-check list, so a recording fault cannot change a NATS verdict. A failed
one still makes the run exit non-zero, as every rig check does.

### The run

- **One new full run**, with the user's go-ahead, after part 1 passes review.
  Not diagnostic: no `-D`.
- **Acceptance: every rig check met, including the completeness checks.**
  Procedure outcomes are whatever they are.
- **Not acceptance criteria:** a disturbed recovery, a moved leader, or a
  publish stored after a timeout. One run cannot guarantee any of them. Do
  not repeat the run to get one.
- Rerun only if a rig check fails, and only with the user's go-ahead.
- Kept runs stay as they are. `REPORT-10` is re-rendered with the new run
  added to the list. The cited run changes only if the user says so.

## Part 2 — the replay dataset (D03-R21)

### Generator

`lab/build-replay-10.py <kept runs...>` writes
`frontend/public/replay/`. It is the only writer. The files are generated:
never hand-edit them. They are committed, because `lab/run/` is gitignored.
Tests: `lab/test-build-replay-10.py`.

`lab/render-report-10.py` also writes `frontend/public/replay/findings.json`
from its `FINDINGS`, `FINDING` and limits. The finding text then has one
source. Today it is hand-copied into the deck as well; the UI must not add a
fourth copy.

### Shape

- `index.json` — the runs, the scenarios, and for each run: run ID, script
  sha256, classifier revision, nats-server and CLI versions, diagnostic on or
  off, `recording` line, and whether the run has readings, thaw times and row
  times.
- `run-<stamp>.json`, one per run:
  - `sources` — every source file used, with its path relative to the demo
    and its sha256.
  - `observed` — readings and events. Each keeps its `source` and line
    number, its epoch `t` and the poll interval of its loop.
  - `rows` — check, note and verdict rows exactly as recorded: ID, kind,
    status, expected, actual, and the time from `rows-t.tsv` when it
    exists.
  - `scenarios` — the recorded windows. Each has a start, an end, its
    freeze and thaw events, and the IDs of the checks that judged it.
- No derived values are stored. The UI derives the quorum count.
- Only what the UI needs. Raw server logs do not ship. For diagnostic run
  `164912`, its `elections.txt` lines for the matched vote-request sequence
  ship in a separate `diagnostic` block, labelled so.

### Which runs ship

| Run | Ships as |
|---|---|
| the new run | full: readings, events, row times |
| `170022` (cited) | the seven recovery windows with readings; all other findings as recorded rows (before/after) |
| `164912` (diagnostic) | separate group, banner "debug logging — timing differs from a normal run — not cited" |
| `163907` | the seven recovery windows with readings |
| `162522` | readings without thaw times: shown, but with no time after the thaw quoted, as the report does |
| `151454`, `150839`, `150304`, `144730` | rows only, for the cross-run table; no timeline |

## Part 3 — the frontend (D03-R22, D03-R23)

### Layout, copied from demo 04

`demos/03-multi-cluster-and-accounts/frontend/`:

| File | Rule (demo 04 source) |
|---|---|
| `vite.config.js` | one build, two entries: `index.html` (standalone) and federation `exposes: { './plugin': './src/plugin.js' }`, `filename: 'remoteEntry.js'`, `name: 'demo_03'`; `base: '/plugins/demo-03/'`; `vue` and `@primeuix/styled` shared as singletons; `@unifi-theme` and `@ui-shell` aliases; port **20301**, `strictPort` |
| `public/manifest.json` | `id: "demo-03"`, `routePrefix: "demo-03"`, `remote: { kind: "federated", url: "/plugins/demo-03/remoteEntry.js", name: "demo_03", module: "plugin" }`; routes `overview` (`default: true`) and `replay`, one component; two nav entries in one group. The dev port never goes in the manifest |
| `public/demo.json` | `devServer.port: 20301` only. **No `readiness`**, no `api`. There is no backend to ask |
| `src/main.js`, `App.vue` | standalone only: theme, PrimeVue preset, `AppShell` |
| `src/plugin.js` | `components: { view: Demo03Route }`; no theme import |
| `src/plugin.spec.js` | demo 04's assertions, adapted. Add one demo 04 lacks: the plugin entry imports no theme |

The nav has two levels only (band, flat items). "Overview" and "Replay" are
two entries in one group, as demo 04's two lessons are. `Demo03Route.vue`
takes the shell's `routeId` prop and shows one view or the other.

### Components

- **`useReplay.js`** — the one controller. It owns the scenario, the
  playhead, play/pause, and the selectors every panel reads. Nothing else
  holds replay state.
- **`MetaGroupSummary.vue`** — the agreed leader or "no agreed leader", the
  term, and available voters against quorum 5 (derived, labelled so).
- **`ClusterPanel.vue`** ×3 — `za`, `arb` (hub), `au`. Each shows:
  - its three servers: answering, "no answer", or "no reading". A server
    that does not answer is never called "down"; a stopped process is shown
    only where a `ps`-state check recorded it.
  - the leader each server reported, with the age of that reading, and
    "stale" past its loop's freshness limit.
  - its own stream's events, in three separate states:
    **acknowledged** (with seq), **timed out — outcome unknown**, and
    **confirmed stored** (readback matched). A post-thaw read that finds
    nothing extra shows as "not stored (readback)", never as "failed".
- **`PlaybackBar.vue`** — pause, scrub, reset, and advance to the next
  recorded event. Scrubbing stays inside the recorded span.
- **`ScenarioList.vue`** — scenarios grouped by run. Stable and disturbed
  recoveries both listed, with their recorded stability word. The
  diagnostic run is in its own group.
- **`EvidenceGap.vue`** — where a finding has no readings in a run, a
  before/after card with its check rows, not a timeline.
- **`OverviewView.vue`** — the question, the role, the findings
  (`findings.json`), run instructions, links to `REPORT-10.html` and the
  v0.6 deck, and the prose-only delayed-storage note.

### Page shape at 1920x1080

```
┌ shell topbar / rail (the shell's, not ours) ───────────────────────────────┐
│ scenario list │ meta group: no agreed leader · term 17 · 6 of 9 (derived ≥5) │
│  run 170022   ├──────────────────┬──────────────────┬──────────────────────┤
│   ML58 dist.  │ za               │ arb (hub)        │ au                   │
│   ML136 stab. │ t-za-1 no answer │ t-arb-1 LEADER   │ t-au-1 → t-arb-1 0.3s│
│  new run      │ ...              │ ...              │ ...                  │
│  diagnostic ▸ │ stream: ...      │ stream: ...      │ stream: ...          │
│               ├──────────────────┴──────────────────┴──────────────────────┤
│               │ ▶ ‖  reset  ⇥ next event   [Make ZA dark: recorded freeze]│
│               │ ──●────────────── timeline: readings ticks, row markers ─── │
└───────────────┴────────────────────────────────────────────────────────────┘
```

UniFi tokens only. Observed values solid; derived values outlined and
labelled; gaps hatched with the words "no reading". One memorable element:
the nine "who I think the leader is" cells, so disagreement is visible at a
glance.

### Tests

- Controller: no value changes between readings; a server with no reading
  shows a gap; staleness past the limit; "no agreed leader" on any
  disagreement; "Make ZA dark" only to a recorded freeze; diagnostic runs
  never mixed into normal scenario lists.
- Panels: the three publish states render apart; "not stored" never reads
  as "failed".
- Generator: every value carries a source; a missing source fails; IDs and
  times round-trip; old runs without readings produce rows only.

## Part 4 — lab-shell integration (D03-R24, D03-R25)

These are the only changes outside demo 03.

### L1 — preview serves built assets (prove first)

Today `lab-shell/tools/buildCatalogue/pluginAssets.js:126` puts the
`/plugins/<id>` proxy in `server.proxy`. Vite 7 copies `server.proxy` into
preview (`node_modules/vite/dist/node/chunks/config.js:35142`), and preview
mounts the proxy before static files. So with a dev port declared, packaged
preview should proxy to the demo's dev server, not serve
`dist/plugins/<id>/`. That is read from code, not yet seen.

1. **Reproduce** with demo 04: build it; build the shell with
   `VITE_PLUGIN_SOURCE=build`; stop port 20401; run `vite preview`; request
   `/plugins/demo-04/remoteEntry.js`. Record the status.
2. **Fix only if reproduced:** return the plugin-asset proxy only when the
   config hook's `isPreview` is false (the field exists in Vite 7's
   `ConfigEnv`, `index.d.ts:3082`). Leave the readiness and API proxies in
   preview; demo 04's backend still needs them.
3. **Check:** with every demo dev server stopped, preview serves
   `/plugins/demo-04/` and `/plugins/demo-03/` from `dist/` (200). Spec in
   `pluginAssets` tests.
4. **Check dev:** with 20301 and 20401 running, an edit to a component
   hot-reloads through the shell on 7110.
5. Correct the comment at `pluginAssets.js:149-151`. The `configureServer`
   middleware runs before the proxy, not after it.

### L2 — one Home entrance, no readiness invented

A plugin with no readiness gets **no** card of its own (`DemoCards.vue:42`
reads the demo catalogue only). So there is no second card to remove. The
existing shell-owned card stays the one entrance.

- `lab-shell/src/shell/demos/labDemos.js` — demo 03's entry keeps its
  question, summary, run instructions and findings, and gains
  `plugin: 'demo-03'`. Fix the stale deck link (v0.3 → v0.6) and add
  `REPORT-10.html`. Update the file comment: demo 03 now has a frontend, and
  it still carries no readiness and no health mark.
- `lab-shell/src/shell/ui/DemoCards.vue` — a lab demo whose plugin is in the
  active catalogue links to that plugin's default route **by route name**
  (the nav's rule). It moves out of "Demos without a frontend" and carries
  no status element, because nothing probes it. If the plugin is absent —
  registry mode, or a failed load — the card links to the static intro page
  as it does today.
- `/lab-demos/03-multi-cluster-and-accounts` stays. It is the fallback, and
  old links keep working.
- Specs: `labDemos.spec.js` ("is demos 02 and 03, and nothing else" stays
  true) and a `DemoCards` spec: exactly one demo 03 card in build mode and in
  registry mode.

### L3 — launch and docs

- `.claude/launch.json` — add `demo03-replay` (port 20301).
- Root `CLAUDE.md`, "Running demo 03" — one line: there is an
  evidence-replay UI; there is still no Go code and no backend.
- **Needs the user's go-ahead:** the lab-shell rules live in
  `demos/01-dictionary/BUSINESS_RULES-APP-SHELL.md`, inside demo 01's sealed
  folder. L1 and L2 change shell behaviour, so they need a rule row there.

### Not changed

The manifest schema, the catalogue client, the pre-mount gate, readiness,
`demos/01-dictionary/registry.json` (demo 03 is not added to registry mode),
and `lab-shell/Dockerfile` (registry mode only, out of scope).

## Part 5 — demo 03 scope documents

The narrow change, made before any frontend code:

- `CLAUDE.md:23` — keep "Role: validation only". Add one sentence: an
  evidence-replay UI under `frontend/` shows kept readings and recorded rows
  only. It measures nothing and computes nothing but quorum arithmetic.
- `CLAUDE.md:352-353` — the frontend design system and the 1920x1080 rule
  apply to `frontend/`. The rest of the demo still has no UI.
- `CLAUDE.md` layout table — add `frontend/` and `frontend/public/replay/`
  (generated).
- `README.md:12` — the same limit, one sentence. Add a port table with
  20301. Add D03-R19 to D03-R25 when the user says so.

## Order of work — each step is a gate

1. **Docs scope** (part 5). Review.
2. **L1 reproduce.** It needs no demo 03 code. Fix only if it is reproduced.
3. **Recording** (part 1), with a classifier `--out` test. No live run.
   Review.
4. **The run**, with the user's go-ahead. Re-render `REPORT-10`.
5. **Generator and `findings.json`** (part 2), with tests.
6. **Frontend skeleton** from a fixture dataset: manifest, both entries,
   `plugin.spec.js`.
7. **Replay UI** (part 3), with tests; layout judged at 1920x1080.
8. **L2 and L3**, then build mode checked end to end, in dev and in
   packaged preview.
9. **Close:** README requirement statuses; `CLAUDE.md` facts. The pattern
   cards do not change: the replay adds no finding.

## Out of scope

- A live backend, or any control that freezes or thaws servers.
- An all-nine watcher during dark periods (new polling).
- Repeating runs to get a disturbed recovery or a stored-after-timeout
  publish.
- Registry mode, the Docker image, and demo 01's app shell.
