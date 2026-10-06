# Plan — the T4 playground (a live lab-shell plugin for demo 03)

**Status: plan only. Nothing here is built or run.** Written 2026-10-06.
It supersedes [`EVIDENCE-REPLAY-PLAN.md`](EVIDENCE-REPLAY-PLAN.md). The user
chose a live playground on the real T4 rig, with a small Go control service.
A replay of recorded results is not the primary UI.

Mockup: [`docs/mockups/playground-mockup.html`](docs/mockups/playground-mockup.html).
Every state in it is mock data. It controls no process. Its scene buttons
exist only in the mockup; the real page has none.

**Revision 2, 2026-10-06**, after a Codex review of revision 1. Added: a
Dark on / off state with pending transitions and timers per cluster; a
leadership card that stays visible; a transitions table timing each change
to the next agreement; client settings (connection cluster with Auto, and
publish timeout), kept apart from the destination stream; Retry same ID; an
optional exercise 10 guide. Fixed: a command could stay pending for ever.
Rule 9 below closes that for the service too. Later the same day, on the
user's request: gateway arrows between the cluster panels (`D03-R32`).

This plan follows the playbook rule added on the same day (stage 03,
Activities): **a demo frontend is a live playground on the running feature,
not a summary of report data.**

## Role: validation + showcase

Today demo 03 is **validation only**. This plan adds a **showcase**: a person
changes cluster state and sees what NATS does. `README.md` must say so
**before** any code (playbook trap: "letting the role drift silently").

The two jobs stay apart:

- **Validation** stays `lab/` → `REPORT.md`, `REPORT-10.md`, the pattern
  cards. Unchanged.
- **Showcase** is the playground. It is a way to look at the rig. **What it
  shows is not evidence.** A session is never cited, and never feeds a report.

## The question

> When I freeze a region, resume it, or request the meta leader in another
> cluster, what do the nine servers report, and can I still create a stream
> and write to each region's stream?

The playground answers "what is the machine doing now?" It never answers
"what will it do?" It predicts nothing. It shows what was observed, and when.

## Rules for everything in this plan

1. **Observed, not inferred.** A server's leader, term and role come only from
   its own monitor reading. The page shows each reading's age.
2. **Process state and monitor state are two things.** A stopped process is
   read from the OS (`ps` state `T`). A monitor that does not answer is a
   monitor that did not answer. Neither proves the other, and neither proves
   lost quorum.
3. **Missing or stale means unknown.** It never counts as zero voters, and it
   never counts as a vote.
4. **"Dark" means SIGSTOP of that cluster's three processes.** The page never
   calls it a WAN partition or a network cut.
5. **"Request leadership here"** is the only name for the step-down action. It
   never says "pin", "move" or "set". The page reports the leader it then
   observed, which may be elsewhere.
6. **A publish timeout is "outcome unknown".** Only a later readback can say
   "present at seq N" or "absent", and only as of that readback's time.
7. **Control only what this rig started.** Signals go only to processes whose
   identity is verified (see "Process identity"). No pattern kill. No shell.
   No command text from the browser.
8. **Playground output never lands in evidence.** The service never writes to
   `lab/run/evidence/`, `lab/run/results.tsv`, `lab/run/obs/` or a report.
9. **Every command ends.** Each command ends in exactly one of: result,
   error, timeout, or cancelled (with the reason). A watchdog in the command
   runner closes any command that outlives its limit by 1 s, as "no completion
   within N s". Nothing stays pending for ever.
10. **Guidance never acts.** The exercise 10 guide is text. It ticks a step
   only when it sees the user do it. No guide control sends a command or
   jumps to a result.

## Requirements

New IDs, from the next free number. **Add them to `README.md` only when the
user says so** — until then they live here.

| ID | Requirement | Kind |
|---|---|---|
| `D03-R19` | Start the managed T4 rig, or attach to one, and show its lifecycle and who owns it. | showcase |
| `D03-R20` | Freeze and resume each cluster, in any combination, both regions included. Show "Dark: on / off", the pending transition ("Going dark", "Coming back", N of 3 confirmed), the confirmed process state, and the time since the change. Show process state and monitor state apart. | showcase |
| `D03-R21` | Request meta leadership in a chosen cluster. Keep the last request visible near the controls: the cluster, leader and term before, the reply, and the leader and term then observed, with the time it took. | showcase |
| `D03-R22` | Publish to each cluster's stream. Show each message ID. Keep acked, acked as duplicate, refused (not sent), timed out (outcome unknown) and verified storage apart. **Publish new** makes a new ID; **Retry same ID** resends one. | showcase |
| `D03-R23` | Try a metadata operation, to compare metadata availability with stream writes. | showcase |
| `D03-R24` | Restore every frozen cluster in one action. Resume and restore answer while other commands are pending. | showcase |
| `D03-R25` | A meta summary that tells agreement, disagreement, stale readings and too little evidence apart. | showcase |
| `D03-R26` | A timestamped history of actions, results, observations, pending commands and errors. | showcase |
| `D03-R27` | Signals reach only verified rig processes; no arbitrary execution; local callers only. | safety |
| `D03-R28` | A playground session never writes to, or becomes, validation evidence. | safety |
| `D03-R29` | Time every change (freeze, resume, accepted leadership request) to the next agreed reading, and show the leader and term after it, so repeated tries can be compared. | showcase |
| `D03-R30` | Let the user pick the client connection cluster (Auto, za, arb, au) and the publish timeout. The destination stream stays the panel's. No general configuration editor. | showcase |
| `D03-R31` | An optional exercise 10 guide beside the controls: steps the user performs; it never acts (rule 10). | showcase |
| `D03-R32` | Draw T4's gateway links between the three cluster panels: one arrow per direction (six), each read from the `/gatewayz` of the cluster that dials out. A direction with no fresh reading from its own end is unknown, never down. | showcase |

`D03-R27` and `D03-R28` are the two a stage 04 review must check first.

## Part 1 — the Go control service (`playground/`)

### Shape, copied from demo 04's `cqrs serve`

| Demo 04 | Demo 03 playground |
|---|---|
| `demos/04-jetstream-cqrs/cqrs/`, own `go.mod`, in root `go.work` | `demos/03-multi-cluster-and-accounts/playground/`, own `go.mod`, added to `go.work` |
| module `…/demos/04-jetstream-cqrs/cqrs`, Go 1.27, nats.go v1.53.1 | module `…/demos/03-multi-cluster-and-accounts/playground`, Go 1.27, nats.go v1.53.1 |
| flat `package main`; `main.go` dispatches with `flag.NewFlagSet` | the same; one command, `serve` |
| `127.0.0.1:20402` (`names.go`) | `127.0.0.1:20302` |
| plain `http.NewServeMux`, `r.Method` checked per handler, 405 JSON | the same |
| `apiDeps` struct of function types, so specs pass fakes | the same: `rig`, `monitor`, `nats` behind interfaces |
| `writeJSON`, `refusal{rule,error,message}` | the same shapes |
| `/readyz` never CORS'd | the same |
| `signal.NotifyContext`, 5 s graceful shutdown, `ReadHeaderTimeout` 5 s | the same, plus the ownership cleanup below |
| Ginkgo suite, `httptest`, `docs_test.go` checks the route table | the same |
| browser reads live data from NATS over WebSocket | **different:** the browser polls `GET /state` every 500 ms |

**Why poll, not WebSocket or SSE.** The browser cannot read nine monitor
ports: they are cross-origin and carry no CORS. NATS WebSocket would need a
listener on all nine T4 configs, which changes the rig under test. SSE would
need streaming through the shell's per-route proxy and nginx buffering
turned off. A 500 ms `GET /state` uses the existing proxy as it is. Demo 04
already polls this way (`PoolFixture.vue`, 2 s).

### Files

| File | Holds |
|---|---|
| `main.go` | flags (`-addr`, `-lab`, `-origin`), context, `serve` |
| `names.go` | ports, the nine servers, clusters, accounts, stream names, timeouts. One place, like demo 04 |
| `serve.go` | mux, handlers, `writeJSON`, refusals, origin check |
| `rig.go` | lifecycle: start (calls `rig-t4.sh up`), attach, stop; ownership |
| `proc.go` | process identity, signals, process-state reads |
| `monitor.go` | the nine concurrent pollers and the reading store |
| `summary.go` | the meta summary rules (pure functions) |
| `transitions.go` | the transition timer (`D03-R29`), pure: fed each summary, returns the open and closed transitions |
| `gateways.go` | the six arrow states (`D03-R32`), pure: fed the nine last `/gatewayz` answers and the time |
| `commands.go` | the command runner: pending set, lanes, timeouts, cancellation, the watchdog (rule 9) |
| `natsops.go` | publish, readback, metadata probe, leadership request (nats.go) |
| `history.go` | the event log, in memory, plus the session file |
| `*_test.go` | specs beside each file |

### Reuse of the rig helpers — what is called, what is not

| Need | Use | Why |
|---|---|---|
| Start the rig | `lab/rig-t4.sh up`, run with a fixed argv (`exec.CommandContext`, no shell), 120 s limit | It already copies the static configs, starts nine servers, writes PID files, waits for a nine-member meta group, and kills everything it started if it fails. Not duplicated. |
| Status for a person | `lab/rig-t4.sh status` is **not** called | It blocks on curl; the service has its own monitor |
| Freeze / resume | **not** `rig-t4.sh freeze|thaw` | They `sleep 3` and then wait on monitors (`wait_ready` has no `--max-time`). Resume would block behind them, which breaks `D03-R24`. The service sends `SIGSTOP`/`SIGCONT` to verified PIDs and lets the monitor show the effect. The signal itself is one syscall, so nothing of value is duplicated. |
| Stop the rig | **not** `rig-t4.sh down` | It uses `pkill -f 'nats-server -c t-'`, a pattern kill that also reaches t- servers of other lab scripts (rule 7). The service sends `SIGCONT` then `SIGTERM` to its nine verified PIDs and waits up to 30 s, as `restart` does. |
| PID files | read `lab/run/pid/<server>.pid`, written by `start_server` | One source of PIDs for both the lab and the service |
| Stream setup | the same commands as exercise 10 step 1, through nats.go: `ODOMETER_<SITE>`, subject `evt.odo.<site>.v1`, file, R3, `--cluster <site>`, account `LB` | Same shape as the measured rig, so the playground shows the same streams |

The service never changes `lab/` scripts. If a future change to `rig-t4.sh`
would help (for example a PID-based `down`), it is proposed separately.

### Process identity (`D03-R27`)

A PID from a PID file is a claim, not a fact (a stale file after a reboot can
name another process). Before **every** signal, and when attaching, the
service checks all four, and refuses on any mismatch:

1. The PID file exists under `<lab>/run/pid/` and holds one integer.
2. The process command line is exactly `nats-server -c t-<server>.conf`,
   optionally followed by `-D` (read with `ps -o args= -p <pid>`, fixed argv).
3. Its working directory is `<lab>/run` (`lsof -a -p <pid> -d cwd -Fn`).
4. It is the process listening on that server's monitor port
   (`lsof -nP -iTCP:<port> -sTCP:LISTEN -Fp`).

It also records the process start time (`ps -o lstart=`) at verification. A
later signal re-checks that the start time is unchanged, so a reused PID is
caught. Verification result per server is part of `/state`, so the page can
say why control is refused.

Process state is read every 1 s with `ps -o stat= -p <pid>`: `T` → stopped,
present → running, absent → gone. This is shown beside, never instead of,
the monitor state.

### Lifecycle and ownership

States: `absent` → `starting` → `ready` → `stopping` → `absent`, plus
`partial` (some t- servers or ports found, not all nine verified) and
`refused` (an identity check failed). `starting` reports each server as it
appears.

| | Rig the service **started** (`owned`) | Rig the service **attached** to (`attached`) |
|---|---|---|
| How | **Start rig**: `rig-t4.sh up` (it refuses if anything is running) | **Attach**: all nine verified, meta size 9 read from a monitor |
| Stop button | yes: CONT, then TERM, nine verified PIDs | **no** — the service did not start it |
| Service shuts down (Ctrl-C, SIGTERM) | CONT every verified PID, then TERM all nine, wait up to 30 s | CONT **only the processes this service stopped**, then leave the rig running |
| Service crashes | the rig keeps running; a later service can attach to it | the same |
| Frozen by someone else (a terminal) | shown as stopped; **Restore all** resumes it | the same |

Attach refuses while a lab script is running, because `lab_init` kills every
t- server (`rig-t4.sh:24-26`). The check reads the process list for an exact
`lab/*.sh` script path; it never signals one.

### Monitoring (`D03-R25`, never blocks commands)

- Nine goroutines, one per server. Each reads `/raftz?group=_meta_` every
  500 ms with a 1 s client timeout (the same endpoint and the same four
  kinds as `classify-10.py`: `ok`, `no_leader`, `unreachable`, `invalid`).
  `/jsz?meta=1` is read once a second for meta size and peer currency.
  `/gatewayz` is read once a second for the gateway arrows (below).
- Each poll writes its result into a store guarded by a mutex: the last
  answer (kind, leader, term, role, time) and the last attempt (time, error).
  A failed poll never erases the last answer; it ages.
- Commands never wait for the pollers, and the pollers never wait for
  commands. They share only the store.
- An observation event goes into the history only when something changes:
  a server's leader, term or role; a monitor starting or stopping to answer;
  a process state change. Not every poll.

### The meta summary rules (pure function, `summary.go`)

Input: the nine last answers and the current time. Fresh means an answer
under 1.5 s old (three polls).

| State | When | Shown |
|---|---|---|
| **Agreed** | Every fresh `ok` answer names the same leader and term, the named leader's own fresh answer says LEADER, and there are at least 5 fresh answers | leader, term, "named by N fresh readings" |
| **Agreed: no leader** | At least 5 fresh answers, all `no_leader` | "no server reports a leader" |
| **Disagreement** | Fresh answers name more than one (leader, term), or a leader and no leader | each view and who holds it |
| **Stale** | The last agreement exists, but its support is now older than 1.5 s | "last agreed t-arb-1, term 15, 4.2 s ago" |
| **Insufficient evidence** | Anything else: fewer than 5 fresh answers, or the named leader has no fresh answer of its own | what is missing |

Always shown beside it: fresh / stale / no answer / never answered counts,
and process counts (running / stopped / gone). The number 5 is **derived**
(quorum of 9) and labelled so. The summary never says "N voters available".

These rules get fixture specs built from exercise 10's kept `obs/*.jsonl`
readings (copied as test data, originals untouched), including the ML51
disagreement window.

### The gateway arrows (`D03-R32`, pure function, `gateways.go`)

T4 has three gateway links, a full mesh: za and arb, arb and au, **za and
au direct**. The panels are laid out as that triangle: the hub, arb, at
the top centre; za below it on the left; au below it on the right. The
za–au arrows cross the gap between the two regions and never touch arb,
because traffic between the regions does not pass through the hub.

Each link is two arrows, one per direction. The arrow from A to B is read
only from A's servers: how many of A's three servers have a fresh
`/gatewayz` answer whose `outbound_gateways` lists B. B's readings never
decide A's arrow.

| Arrow | When | Drawn |
|---|---|---|
| listed | 3 of 3 of A's servers answer fresh and list B | solid, `3/3` |
| partial | some answer fresh, but fewer than 3 list B | solid, warning colour, `k/3` |
| not listed | 3 fresh answers, none lists B | solid, error colour, `0/3` |
| unknown | no fresh answer from A | dashed, "no reading" |

- Hovering an arrow shows each of A's servers: listed, not listed, or no
  fresh reading with its age.
- **Listed is not proof that the far side answers.** A server keeps a
  gateway connection to a SIGSTOPped peer while the socket stays open. How
  long the live side keeps listing a stopped peer is **unmeasured**; the page
  shows what `/gatewayz` says and claims nothing more. The mockup keeps a
  stopped peer listed for the whole freeze; that timing is made up.
- The arrows are drawn over the panel grid as one SVG, placed from the
  panels' measured positions (a `ResizeObserver`), so they follow the
  layout. An 84 px gap under the hub holds the two arb–region pairs, as
  vertical arrows where the panels overlap. A 132 px gap between za and au
  holds their pair, centred on the height the two panels share. Each arrow's tag names its direction (`za → au 3/3`).
  The page may scroll at 1920x1080; the arrows get the room first (user,
  2026-10-06).

### Commands and their lanes (`D03-R24`)

A command gets an ID, enters the pending set, and returns 202 at once with
that ID. Its result arrives in `/state` and the history.

| Lane | Commands | Rule |
|---|---|---|
| **signal** | freeze, resume, restore all | synchronous syscalls after identity checks; never queued behind another lane; answer in well under 1 s |
| **nats** | publish (new or retry), verify storage, metadata operation, request leadership | goroutine each, own timeout; **at most one per kind per cluster** (else 409 `Busy`); request leadership is **one at a time for the whole rig**, because a step-down acts on the one meta group |
| **lifecycle** | start, attach, stop | exclusive; while it runs, only `restore all` and `GET /state` are accepted |

Timeouts, from `names.go`, shown on the page while pending:

| Command | Timeout | Matches |
|---|---|---|
| publish | the user's choice: 1, 2, 5, 10 or 30 s ack wait; default 5 s | `DARK_PUB_S=5`, step 6 |
| verify storage | 3 s per request | `direct_get --timeout 3s` |
| metadata operation | 10 s per call | `--timeout 10s` probe |
| request leadership | 3 s request, then 10 s of observation | — |

**Which server a NATS command goes through (`D03-R30`).** A connection to a
stopped server hangs, so the service only connects to a server whose process
is running. The request names a client connection cluster:

- **Auto** (default): the destination's own cluster, then `arb`, then the
  other region.
- **za / arb / au**: a running server in that cluster only. If none is
  running, the command is **refused before sending**: "No running server in
  za". A refused publish has a known outcome: not sent.

The destination stream is always the panel's, so "publish through au to
`ODOMETER_ZA`" is one choice of each. The chosen server is part of every
result ("via t-au-1").

**Dark, per cluster (`D03-R20`).** Each cluster has a wanted state (`on` /
`off`) and a confirmed state read from `ps`.

| Shown | When |
|---|---|
| **Going dark**, N of 3 confirmed stopped | SIGSTOP sent; `ps` has not yet shown `T` for all three |
| **Dark: on**, dark for X s | all three confirmed `T` |
| **Coming back**, N of 3 confirmed running | SIGCONT sent; not all three confirmed |
| **Dark: off**, resumed X s ago | all three confirmed running |

Clusters are independent: any combination is allowed, both regions
included. **Resume always wins.** A Resume sent while a freeze is still
being confirmed cancels that freeze ("superseded by Resume after 0.2 s") and
sends SIGCONT to all three. Restore all is a Resume for every cluster whose
wanted state is `on` or that has a stopped process. Neither is ever blocked
by a pending NATS or lifecycle command. Stop rig cancels every pending
command first, with the reason "the rig is stopping".

**Transitions (`D03-R29`, `transitions.go`).** A transition opens when a
freeze or resume is fully confirmed, or when a leadership request is
accepted. It records the last agreed leader and term before it. It closes:

- at the first **Agreed** summary after the summary has left Agreed, or
  that names a different leader or term: "agreed after 1.9 s", plus the
  leader, term and change ("term +2, leader moved" / "unchanged");
- after 15 s with no change: "no change in 15 s";
- after 15 s with no agreement: "no agreement in 15 s";
- when the next transition opens: "next change came first".

Times are measured on the service clock from the confirmed change to the
poll that saw agreement. The page states that polling is every 0.5 s, so
each time is good to about half a second. These are playground numbers, not
measurements (rule 8).

### What each NATS command does

- **Request leadership here** — `$JS.API.META.LEADER.STEPDOWN` with
  `{"placement":{"cluster":"<c>"}}` as `$SYS` admin (the request type is
  `JSApiLeaderStepdownRequest`, `jetstream_api.go:190,622` in v2.14.6). The
  CLI exercise 10 uses, `nats server cluster step-down --cluster`, sends this
  subject; confirm that by test, not by name. Result: the server's reply
  (accepted / error / no reply in 3 s), then the leader observed when the
  summary next agrees **with a higher term than before**, or "no new agreed
  leader within 10 s". Text: "Requested arb. Observed t-arb-2 in arb, term
  19." or "Requested za. Observed t-au-1 in au." The last request is kept
  in `/state` as one record — cluster, via, time, leader and term before
  (or "no agreement"), reply, observed leader and term, seconds from reply
  to observation — until the next request replaces it (`D03-R21`). The
  requested cluster's panel repeats it in one line.
- **Publish** — JetStream publish to `evt.odo.<site>.v1` as `LB`, header
  `Nats-Msg-Id`, body as exercise 10. **Publish new** makes the ID
  `pg-<session>-<site>-<n>`. **Retry same ID** resends a ledger entry with
  its own ID and body; the request names the ID, and the service accepts
  only an ID from this session's ledger for that stream (else 404).
  Outcomes per attempt: `acked` (seq), `acked as duplicate` (the PubAck's
  `duplicate` flag: JetStream already held that ID and stored nothing new),
  `refused, not sent` (no running server for the chosen connection),
  `timed out — outcome unknown` (at the chosen timeout), or `error` (code
  and text). A ledger entry lists every attempt. Duplicate detection holds
  only inside the stream's duplicate window (2 min by default), and the
  page says so beside the retry button.
- **Verify storage** — for each ledger entry not yet verified: read the
  stream's last sequence, then Direct Get every sequence above the last one
  verified, and match `Nats-Msg-Id`. Result per entry: `present at seq N`
  or `absent`, each stamped "as of 14:03:20". An absent entry can turn
  present later, so the button can be pressed again.
- **Metadata operation** — the exercise 10 probe, as `LB`: create
  `PG_PROBE_<n>` (memory, R1, cluster of the entry server), delete it, check
  it is gone. Three results, each with its error code if it failed (for
  example `10008`).

### Routes

All under `/demo-api/03-multi-cluster-and-accounts` through the shell; each
is listed in `demo.json`, so the shell forwards nothing else.

| Method | Route | Lane |
|---|---|---|
| GET | `/state` | — (readings, summary, rig, pending, history after `?after=<seq>`) |
| POST | `/rig/start` | lifecycle |
| POST | `/rig/attach` | lifecycle |
| POST | `/rig/stop` | lifecycle (owned only, else 409) |
| POST | `/clusters/{za,arb,au}/freeze` | signal |
| POST | `/clusters/{za,arb,au}/resume` | signal |
| POST | `/clusters/{za,arb,au}/leadership` | nats |
| POST | `/clusters/{za,arb,au}/publish` | nats |
| POST | `/clusters/{za,arb,au}/verify` | nats |
| POST | `/meta/probe` | nats |
| POST | `/restore` | signal |
| GET | `/readyz` | not forwarded, not CORS'd (as demo 04) |

`/clusters/` is one prefix route in `demo.json`; the handler accepts only
the three cluster names and five verbs, else 404.

**Request bodies.** Every POST body is JSON with **enumerated values
only**, checked against fixed sets:

| Field | Routes | Allowed |
|---|---|---|
| `via` | every `/clusters/…` and `/meta/probe` route | `auto`, `za`, `arb`, `au` |
| `timeoutS` | `publish` | `1`, `2`, `5`, `10`, `30` |
| `retryOf` | `publish` | a message ID already in this session's ledger for that stream |

Anything else is 400. The settings live in the browser (the shared
controller) and travel with each command, so the service holds no settings
state.

**Local callers only.** The listener is `127.0.0.1`. Every POST must carry
`Content-Type: application/json` and an `Origin` in the allow-list
(`http://localhost:7110`, `http://127.0.0.1:7110`, `http://localhost:20301`,
`http://127.0.0.1:20301`), else 403. That stops another web page in the same
browser from sending signals. No request body carries a command, a path or a
PID: only fixed verbs on fixed names.

### Session file (`D03-R28`)

The history is also appended to
`demos/03-multi-cluster-and-accounts/playground/.run/sessions/<stamp>.jsonl`
(gitignored). Its first line says `"kind":"playground","evidence":false`, the
rig owner, server version and service commit. It lives outside `lab/run/`,
so no report renderer ever reads it.

## Part 2 — the frontend (`frontend/`)

### Shape, copied from demo 04

| | Value |
|---|---|
| dev port | `20301`, `strictPort` |
| base | `/plugins/demo-03/` |
| federation | name `demo_03`, exposes `./plugin`, `remoteEntry.js`; shared `vue`, `@primeuix/styled` singletons |
| `src/plugin.js` | `components = { playground: PlaygroundRoute, overview: OverviewRoute }`; `activate()` sets the API base to `/demo-api/03-multi-cluster-and-accounts` (demo 04's `setCommandApi`) |
| `src/App.vue` | standalone only, uses `AppShell` |
| plugin entry | **no AppShell, no vue-router** (spec, as demo 04's `plugin.spec.js`) |
| routes (manifest) | `/demo-03/playground` (first, default), `/demo-03/overview`; both get `routeId` |
| nav | group `multi-cluster`, two items |
| `public/demo.json` | `devServer.port 20301`; `api {devPort: 20302, routes: [/state, /rig/start, /rig/attach, /rig/stop, /clusters/, /meta/probe, /restore]}`; **no** `hostedUpstream` (host processes only; with none, `demoApi.js:112` writes no nginx entry); **no** readiness (see L2); `runCommand` |
| theme | `@unifi-theme`, `@ui-shell` aliases; judged at 1920x1080 |

### One shared controller — `usePlayground.js`

A composable, not a store, as demo 04's `useDemoState`: one call in
`PlaygroundRoute`, one `reactive` state passed down. It:

- polls `GET /state?after=<seq>` every 500 ms and merges the history;
- shows the service as unreachable (not the rig) when the poll fails;
- sends commands and keeps the pending entries the server returns;
- never computes the meta summary: it renders the service's;
- holds the two client settings, `via` and `timeoutS`, and sends them in
  each command body; the service keeps no settings of its own;
- keeps the ID of each message it published, so **Retry same ID** can send
  `retryOf`.

### Components

Screen order, top to bottom: rig bar, summary, client settings, then the
topology (the centre of the page). Its top row: the leadership card and the
metadata card on the left, the arb panel in the middle, the transitions card
on the right. Its bottom row: za and au, half the width each. Each panel
shows its three servers side by side. The gateway arrows join the three
panels. The right
column holds the guide (collapsible) above the history. Overview holds the
reports.

| Component | Shows |
|---|---|
| `RigBar` | lifecycle state, owner, verified count, Start / Attach / Stop, **Restore all frozen clusters** (enabled whenever a cluster is dark or going dark, also while other commands are pending), service reachability |
| `MetaSummary` | the summary state, leader and term only when Agreed, fresh / stale / no-answer counts, process counts, the derived quorum line, labelled |
| `ClientSettings` | client connection (Auto, za, arb, au) and publish timeout; one line on what Auto means; held in the shared controller and sent with each command |
| `ClusterPanel` ×3 (`za`, `arb` hub, `au`) | **Request leadership here**; a `DarkControl`; a one-line leadership note when the last request named this cluster; three `ServerRow`s; its `StreamLane`. The whole panel takes the warning border while dark |
| `GatewayLinks` | six arrows over the panel grid, one per direction, with an `n/3` tag each; dashed when unknown; per-server detail on hover. Renders the service's arrow states; computes none |
| `DarkControl` | "Dark: on / off", "Going dark" / "Coming back" with N of 3 confirmed, the time since the change; **Freeze** and **Resume** beside it |
| `ServerRow` | process (running / stopped / gone / unverified) and monitor (answering, age / no answer since / never) in two separate cells; last leader, term, role; greyed when stale |
| `StreamLane` | **Publish new**, **Verify storage**; one entry per message ID, listing each attempt with its pill (waiting for ack N s / limit, acked seq N, acked as duplicate, refused not sent, timed out outcome unknown) and the server it went through; its verification (present at seq N / absent, with the time); **Retry same ID** on an entry whose last attempt timed out or was refused |
| `LeadershipCard` | the last request: cluster, via, before, reply, observed leader and term, seconds to observation; "Nothing keeps it there" when it landed where asked |
| `TransitionsCard` | the last five transitions: change, time, agreement after N s (or watching / no change / no agreement / next change came first), leader and term after, change pill |
| `MetaProbeCard` | **Try metadata operation** and its three results, with the server used and the time |
| `Exercise10Guide` | eight steps, ticked only when the matching action is seen in `/state`; hide / show; never acts (rule 10) |
| `HistoryLog` | every event with a time; pending entries with a running timer; cancelled entries with the reason; errors in red; filter by cluster |
| `OverviewRoute` | the question, the role, how to start, what each control does, what the page cannot show, links to `REPORT-10.html` and the deck |

### Tests

Vitest, as demo 04: `plugin.spec.js` (no AppShell, no router, components
map), `usePlayground.spec.js` (merge, pending, unreachable service, the
settings travel in each body), and one
spec per component on fixture `/state` payloads: an unknown gateway arrow is
dashed and never drawn as not listed, a stale reading is greyed,
an unreachable monitor never shows as "down", Leader text appears only in
Agreed, the leadership result never says "pinned", Resume and Restore all
stay enabled while a publish is pending, Retry same ID sends `retryOf` and
Publish new does not, the guide has no control that sends a command.

## Part 3 — lab-shell integration

These are the only changes outside demo 03. Carried over from the replay
plan, with the backend added.

- **L1 — preview serves built assets (prove first).** Reproduce with demo 04:
  build it, build the shell with `VITE_PLUGIN_SOURCE=build`, stop 20401, run
  `vite preview`, request `/plugins/demo-04/remoteEntry.js`. Fix only if
  reproduced: return the asset proxy only when `isPreview` is false. Keep the
  API and readiness proxies in preview. Check: preview serves
  `/plugins/demo-03/` and `/plugins/demo-04/` from `dist/` with the demo dev
  servers stopped; dev still hot-reloads through 7110. Fix the comment at
  `pluginAssets.js:149-151`.
- **L1b — the demo API in preview.** Read from code, not seen: the demo API
  proxy is in `server.proxy`, so preview copies it, but the 404 guard for
  undeclared `/demo-api/` paths is dev-only (`demoApi.js:168-189`). Check
  that preview forwards the seven declared routes to 20302 and answers
  nothing else. Fix only if wrong.
- **L2 — one Home entrance.** `labDemos.js`: demo 03 keeps its question,
  summary, run instructions and findings, gains `plugin: 'demo-03'`, the deck
  link moves v0.3 → v0.6, and `REPORT-10.html` is added. `DemoCards.vue`: a
  lab demo whose plugin is active links to the plugin's default route by
  route name, with no status element; with no plugin, it links to
  `/lab-demos/03-multi-cluster-and-accounts` as today. Specs: one demo 03
  card in build mode and in registry mode. Demo 03 has no readiness block,
  because the playground page shows the service and rig state itself; a
  readiness block would add a second card.
- **L3 — launch.** `.claude/launch.json`: `demo03-playground` (frontend,
  20301). The Go service is started by hand (`go run ./playground serve`),
  like demo 04's `cqrs serve`, which is not in launch.json either.
- **Needs the user's go-ahead:** the shell's rules live in
  `demos/01-dictionary/BUSINESS_RULES-APP-SHELL.md`, inside demo 01's sealed
  folder. L1, L1b and L2 change shell behaviour, so they need rule rows there.

Not changed: the manifest schema, `pluginLoader.js` and the shell API (no
`fetch` helper is added), readiness, `registry.json`, `lab-shell/Dockerfile`.

## Part 4 — scope documents

Made **first**, before any code, and shown to the user:

| File | Change |
|---|---|
| `demos/03-…/README.md` | role line → validation + showcase, with the split above; port table (20301 UI, 20302 control service); D03-R19…R32 when the user says so |
| `demos/03-…/CLAUDE.md` "What this demo is" | role line; the playground is not evidence; the playbook rule that a demo frontend is a live playground first, so no later change turns this page into a report viewer |
| `CLAUDE.md` intro ("no application code at all") | except `playground/` (Go) and `frontend/` (Vue) |
| `CLAUDE.md` "Only nats-server, nats, jq, curl and python3" | `lab/` keeps that list; `playground/` adds Go |
| `CLAUDE.md` Ports | a second table: 20301, 20302 (demo 04's `20<demo><n>` pattern) |
| `CLAUDE.md` "Root rules that do NOT apply" | the frontend design system and 1920x1080 apply to `frontend/`; Ginkgo applies to `playground/` |
| `CLAUDE.md` new section "The playground" | rules 1–10 above, the ownership table, "never call `rig-t4.sh down` from the service", "never run the playground beside `run-all.sh` or `10-hub-meta-leader.sh`" |
| `CLAUDE.md` documents table | `playground/`, `frontend/`, `PLAYGROUND-PLAN.md`, `EVIDENCE-REPLAY-PLAN.md` (superseded) |
| root `CLAUDE.md`, "Running demo 03" | "no Go code and no UI" → a playground UI and a Go control service exist; the rig is still host processes |
| root `go.work` | add `./demos/03-multi-cluster-and-accounts/playground` |
| `.gitignore` | `demos/03-…/playground/.run/` |

## Order of work — each step is a gate

1. **Scope documents** (Part 4), shown to the user. Nothing else starts first.
2. **L1 reproduce.** No demo 03 code needed.
3. **`proc.go`, `summary.go`, `transitions.go` and `gateways.go`** with specs only: fake
   `ps`/`lsof` output, and fixture readings from kept `obs/` files. No live
   server.
4. **`monitor.go`, `commands.go`, `serve.go`** with fakes; the route table
   spec; specs that a pending 5 s publish does not delay `resume`, that a
   Resume during a freeze cancels the freeze, that Stop cancels every
   pending command, that the watchdog closes a command whose fake never
   returns (rule 9), and that a body value outside its set is 400.
5. **`rig.go` and `natsops.go`**, then a by-hand check against the live rig in
   a terminal, with `curl`: start, freeze, resume, request leadership,
   publish while frozen, retry the same ID after resume (expect acked or
   acked as duplicate), publish through au to `ODOMETER_ZA`, verify, probe,
   restore, stop. Recorded in
   `exercises/EXERCISE_OBSERVATIONS.md` as playground observations, not as
   exercise 10 evidence.
6. **Frontend**, against the live service, judged at 1920x1080.
7. **L1 fix (if reproduced), L1b, L2, L3**, then build mode checked end to end
   in dev and in preview.
8. **Stage 04:** a pattern card only if the playground shows something the
   lab has not measured; then it is measured in `lab/` first.

Terminal first is already met for the actions themselves:
`EXERCISE-10-TERMINAL-STEPS.md` does each one by hand. Step 5 does the same
for the service's routes.

## Out of scope

- A packet-filter partition (rule 4). Restarts of single servers. A T5 rig.
- Hosted / Docker deployment of the playground. It runs on the host only.
- Saving sessions as evidence, or comparing a session against a report.
- Any change to `lab/` scripts, `classify-10.py`, the reports or the deck.
- A replay of recorded runs. `EVIDENCE-REPLAY-PLAN.md` is kept for the
  record only.
