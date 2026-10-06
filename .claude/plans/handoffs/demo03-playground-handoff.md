# Handoff — demo 03 T4 playground (target: Claude)

## Goal

Build the live T4 playground for demo 03: a small Go control service
(`playground/`, port 20302) and one Vue lab-shell remote (`frontend/`, port
20301). Follow `demos/03-multi-cluster-and-accounts/PLAYGROUND-PLAN.md`
(revision 2) and the mockup. The page is a live playground, not a replay.

## Done so far (branch `poc/demo05`, nothing pushed)

- `94cb569` — `PLAYGROUND-PLAN.md` revision 2 and
  `docs/mockups/playground-mockup.html` (triangle layout, gateway arrows).
- `3fe07d2` — the rule "a demo frontend is a live playground first", in
  root `CLAUDE.md`, `demo-playbook.html` / `.pdf` and `.claude/memory/`.
- **No implementation exists yet.** No `playground/`, no `frontend/`.
- `EVIDENCE-REPLAY-PLAN.md` and `docs/mockups/evidence-replay-mockup*.html`
  are superseded and may still be untracked. Do not build from them.

## Next steps — the plan's "Order of work". Each step is a gate.

1. **Scope documents** (plan Part 4). Show them to the user. Nothing else
   starts first. *Check:* user approves. README requirement IDs
   `D03-R19`…`D03-R32` go in only when the user says so.
2. **L1 reproduce** (plan Part 3), with demo 04. *Check:* reproduced or not,
   written down. Fix only if reproduced.
3. **`proc.go`, `summary.go`, `transitions.go`, `gateways.go`** — specs only,
   fake `ps`/`lsof` output and fixture readings. No live server.
   *Check:* `ginkgo ./demos/03-multi-cluster-and-accounts/playground/...` green,
   spec tree printed.
4. **`monitor.go`, `commands.go`, `serve.go`** with fakes. *Check:* specs for
   the route table; a pending 5 s publish does not delay resume; Resume
   cancels a freeze; Stop cancels every pending command; the watchdog closes
   a command whose fake never returns (rule 9); a body value outside its set
   is 400.
5. **`rig.go`, `natsops.go`**, then a by-hand `curl` check on the live rig
   (sequence in plan step 5). *Check:* results in
   `exercises/EXERCISE_OBSERVATIONS.md` as playground observations, not
   exercise 10 evidence.
6. **Frontend** against the live service. *Check:* matches the mockup at
   1920x1080; Vitest specs green; no console errors.
7. **L1 fix (if reproduced), L1b, L2, L3.** *Check:* build mode works end to
   end in dev and in preview. Packaged preview runs on **7110**
   (`lab-shell-preview-7110` in `.claude/launch.json`), never 4173 — the
   service refuses commands from any other Origin.
   **Follow-up, out of step 7:** `/demo-readiness/<unknown>` answers 200 HTML
   in preview, not 404 (`demoReadiness.js` guards dev only). See plan L1b.
8. **Stage 04** only if the playground shows something the lab has not
   measured — then measure it in `lab/` first.

## Decisions already made — do not re-open

- Live playground, not a replay. Go service + one Vue remote, demo 04's
  `cqrs serve` shape. Ports 20301 (UI), 20302 (service), `127.0.0.1` only.
- Lab shell with `VITE_PLUGIN_SOURCE=build`; three `ClusterPanel`s; one
  shared controller `usePlayground.js`; UniFi theme; no AppShell in the plugin.
- Triangle layout: arb hub top middle, za and au below left and right.
- Gateway arrows per direction, read only from the origin cluster's fresh
  `/gatewayz` `outbound_gateways` (D03-R32). Listed is not proof the far side
  answers.
- The step-down action is named only **"Request leadership here"**. One
  leadership request at a time for the whole rig.
- **Publish new** / **Retry same ID** (same `Nats-Msg-Id`). A timeout means
  the outcome is unknown.
- Client connection: Auto = destination cluster, then arb, then the other
  region. No general configuration editor.
- Rules 1–10 of the plan (stale = unknown, not zero; dark = SIGSTOP, not a
  partition; unreachable monitor is not proof of a stopped process; control
  only verified `t-` PIDs; no pattern kill; no arbitrary shell).
- Owned rig vs attached rig, per the plan's ownership table.
- Out of scope: partition, single-server restarts, T5, Docker, saving
  sessions as evidence, any change to `lab/`, the reports or the deck.

## Traps

- `go test` can print `ok` without proving much. Use Ginkgo and read the tree.
- Never run the playground beside `lab/run-all.sh` or
  `lab/10-hub-meta-leader.sh` (`lab_init` kills every `t-` server).
- Never call `rig-t4.sh down` from the service (pattern kill). Never call
  `rig-t4.sh freeze|thaw` (they sleep and block). Send SIGSTOP/SIGCONT to
  verified PIDs.
- Do not disturb the demo 05 server (`127.0.0.1:4522`).
- Never hand-edit `REPORT*`.
- How long a live server keeps listing a SIGSTOPped peer in `/gatewayz` is
  **unmeasured**. The mockup's timing for it is made up.
- Browser pane: the tab id changes on navigate — call `tabs_context`, then
  `resize_window {width:1920,height:1080}` again. Reset to `desktop` after.
- The mockup's SVG arrows come from `getBoundingClientRect`. A CSS transform
  breaks the geometry; measure with no transform.
- L1, L1b and L2 need rule rows in
  `demos/01-dictionary/BUSINESS_RULES-APP-SHELL.md` (sealed demo 01). Ask the
  user first.
- One command per Bash call. No `&&`. Never push.

## Read first

- `demos/03-multi-cluster-and-accounts/CLAUDE.md`
- `demos/03-multi-cluster-and-accounts/PLAYGROUND-PLAN.md` (targeted reads)
- `demos/03-multi-cluster-and-accounts/docs/mockups/playground-mockup.html`
- `demo-playbook.html`, stage 03 (Activities)
- demo 04's `cqrs serve` and `frontend/` (the pattern)
- `demos/03-multi-cluster-and-accounts/lab/rig-t4.sh`
- `demos/03-multi-cluster-and-accounts/exercises/EXERCISE-10-TERMINAL-STEPS.md`
