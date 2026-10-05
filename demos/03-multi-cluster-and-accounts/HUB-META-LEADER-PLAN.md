# Plan — can the hub hold the JetStream meta leader? (a placement experiment on T4)

**Status: plan only. Nothing here is measured.** Every expected result below is
`inferred`. Do not promote one to `measured` without a run (see `CLAUDE.md`,
"Where this demo's documents live"). Revised after a Codex review: six fixes.

**Role: validation only.** The topology is held still. The variable is **where
the meta leader sits** and **which region is dark**.

## The question

> In three gateway-joined clusters of three servers — a hub and two regions —
> can the JetStream meta leader be kept in the hub? What happens to metadata and
> to regional data when a region goes dark, and when it comes back?

## The shape: it is T4, with the arbiter cluster playing the hub

This is **not a new topology.** `lab/06-arbiter3.sh` (T4 / Figure F) already
builds it: `za` + `au` + a three-node third cluster, gateways only, no leaf
links, one nine-member meta group, quorum 5. Calling the third cluster "hub"
changes its **role** in the experiment, not the wiring.

- Reuse 06's wiring and ports. Its third cluster is `arb`: servers
  `arb-1`..`arb-3`, clients 4541–4543, monitors 8541–8543, routes 6541–6543,
  gateways 7541–7543.
- **The cluster keeps the name `arb`.** Every command, placement request, stream
  placement and leader regex uses `arb`. "Hub" is the **role** only, used in
  prose. Say "hub (T4's arbiter cluster, `arb`)" the first time in any report.
- Add a new structural difference only if one is wanted. None is planned.
- T6 (`lab/07-gateway-and-hub.sh`) is a different shape: it adds a hub leaf link.

Held still: `ODOMETER`, KV `t7-vehicles`, `nats-server 2.14.6`, host `nats` CLI,
plain password auth, `127.0.0.1` only. Every scratch config and `server_name`
starts `t-`. Kill with `pkill -f "nats-server -c t-"` and nothing else.

The hub is assumed always on and healthy. The regions are what fail.

## What we know before we start

- NATS has **no setting that pins the meta leader to a cluster.** Step-down asks
  for a move. It is not a rule.
- Step-down takes a placement filter. The CLI form is
  `nats server cluster step-down --cluster <name>`. Codex reports the host CLI
  `0.4.0` supports it. **Confirm with `--help` before scripting.**
- The filter picks a preferred target. It does not force one. The server may
  fall back to another healthy peer. A success reply does not prove the leader
  moved.
- NATS has no election priority. A hub server that restarts does not take the
  lead back.
- `/jsz` can name a **stale leader** for about a minute after a failure. A leader
  that exists is not a leader that was re-elected. Use `wait_live_leader`.
- A frozen server still counts as a member. Membership size proves nothing about
  who is alive.

## Rules for every check

1. **Read the leader from the meta group, not the logs.**
   `curl -s "localhost:8541/jsz?meta=1" | jq '{leader:.meta_cluster.leader, size:.meta_cluster.cluster_size}'`
   Quote the `?`. Ask a server that is **not** frozen.
2. **Check where the leader is separately from whether the API said yes.**
3. **A region goes dark with `freeze`. It returns with `thaw`.** Both are in
   `lab/_common.sh`. Never `docker network disconnect`. A dead process
   (`kill_server`) is a follow-up run, not the first run.
4. **Every freeze is proved by `wait_dark` on that region's monitor ports.** If
   it fails, the run stops. A freeze that did nothing reads like a finding.
5. **Use the existing helpers. Do not write new ones:** `meta_leader`,
   `meta_size`, `wait_live_leader`, `wait_meta_leader`,
   `seconds_until_no_leader`, `wait_dark`, `freeze`, `thaw`.
6. **Three separate deadlines**, because they are three separate waits. Fix the
   numbers before the first run. Proposed values:

   | Deadline | What it covers | Proposed |
   |---|---|---|
   | Election | a live leader is named after a loss that keeps quorum | 30 s |
   | Quorum-loss detection | the leader field goes `NONE` after quorum is lost | 90 s (the stale-leader window is about 60 s) |
   | Recovery | a returned region is current and a leader exists | 60 s |

   Past its deadline a check is `not met`. Wait about 5 s after any link change.
7. **Write the pass rule before you run the check.**
8. **Prove a write by reading the acknowledged messages back.** An ordinary
   `nats pub` that returns success proves nothing. Publish with an ack and a
   `Nats-Msg-Id`, and record each acked sequence. Then read every acked
   sequence back by **Direct Get** and check its `Nats-Msg-Id` and payload, in
   order. Copy `direct_get` and the read-back loop from `09-switch-t4-t5.sh`.
   - Direct Get returns **messages, not a count.** Use it for identity.
   - **With no meta leader, `STREAM.MSG.GET` answers 10008** (measured in the
     T4 → T5 runs). Direct Get is answered by the stream's own replicas.
   - A **count probe** is separate and optional. If one is kept, confirm during
     setup that it still answers with no meta leader before using it in S5.
   - Direct Get needs `allow_direct` on each fixture stream.
9. **Rig or procedure — every check has a KIND** (see `CLAUDE.md`, "Rules for
   anything added here"). Rig checks: the freeze worked, nine servers came up,
   the fixtures were seeded. Procedure checks: every placement and recovery
   outcome. A failed procedure check stays a FAIL. Never rewrite it to expect
   the failure. **Each scenario ends with one `verdict` row.**
10. **Tell streams apart by Placement Cluster and message count**, never by
    `created`.

## Fixtures — built once, before any failure

Build these with all nine servers up and the leader confirmed. Record each
stream's Placement Cluster and message count as the **seed** values.

| Fixture | Where | Purpose |
|---|---|---|
| `ODOMETER_ARB` — R3 stream, placement cluster `arb` | hub | a stream whose own quorum survives any regional loss, even both |
| `ODOMETER_ZA` — R3 stream, placement cluster `za` | za | a stream that loses quorum when `za` goes dark |
| `ODOMETER_AU` — R3 stream, placement cluster `au` | au | the same, for `au` |
| KV `t7-vehicles` | hub | seeded keys, read back by Direct Get |

Each stream gets a fixed seed of acknowledged messages. State the account and
subject for each in the script header. Put all fixtures in the shared
account `LB` (user `lb`). `_common.sh` defines three business accounts
(`LB`, `LB_ZA`, `LB_AU`) on every server. `LB` is the one meant to be shared
across regions, and accounts are not the variable here.

**Metadata probe.** A metadata operation is not the same as a write to an
existing stream. After each failure, create a **temporary stream** placed in a
surviving cluster, check it exists, then delete it. A write to an existing
stream cannot prove the meta group is available.

## Requirements

Next free IDs after `D03-R12`. Add them to `README.md` only when the user says so.

| ID | Question | Status |
|---|---|---|
| **D03-R13** | With all nine servers up, can a hub step-down put the meta leader in the hub, and does it land there every time? | unmeasured |
| **D03-R14** | One region dark, leader already in the hub: does the leader stay, do writes continue, and can metadata still change? | unmeasured |
| **D03-R15** | One region dark, leader was in that region: who wins, and how long does it take? | unmeasured |
| **D03-R16** | A dark region returns: does the leader stay where it is, with the meta Raft term unchanged during recovery, and is the region current again? The term part was added 2026-10-05 with the user's agreement, after a return put the same server back at term +2. A term rises on every election attempt, so it shows disturbance, not a count of elections. Recovery and leadership stability are reported apart | unmeasured |
| **D03-R17** | Both regions dark: does the hub lose quorum even though it is healthy? | unmeasured |
| **D03-R18** | One region dark: does a stream placed inside it lose writes while the other streams keep them? | unmeasured |

**Check IDs use the prefix `ML`** (`ML1`, `ML2`, …). The letters `A` to `H`,
`SA` and `T` already belong to other scripts in `lab/`, and `H1`–`H9` belong to
`08-hub-leaf-per-region.sh`. `ML` is unused (checked 2026-10-05).

## The tests

### S1 — all up (D03-R13)

All nine servers up. All nine monitors answer. Fixtures built.

1. **Baseline.** Record the leader and meta size.
2. **Trial loop, 5 or more trials.** Each trial starts from a **confirmed regional
   leader**. Make one with `step-down --cluster za` or `--cluster au`, and read
   the leader (`wait_live_leader`) before going on. Then run
   `step-down --cluster arb`. Read the leader again.
   - Record: API reply, leader after, seconds to settle.
   - Why: a step-down while the leader is already in the hub tests moves between
     hub servers, not into the hub.
3. **Hub server restart.** Restart one non-leader hub server. Record whether the
   leader moves. (Expect: no.)

Pass: the leader is in the hub after every trial. A trial that lands outside the
hub is recorded. It is the finding, not a rig failure.

### S2 — one region dark, leader in the hub (D03-R14)

Leader confirmed in the hub. `freeze` all three `za` servers. `wait_dark` on
their monitors.

- Reachable voters: 6. Quorum survives.
- Expect: leader unchanged (election deadline applies if it moves).
- **Write probe:** acknowledged writes to `ODOMETER_ARB` and `ODOMETER_AU`.
  Every acked message reads back by Direct Get (rule 8).
- **Metadata probe:** create and delete a temporary stream placed in `arb`.
- Then `thaw` and run S4's recovery checks. Repeat with `au` dark.

### S3 — one region dark, leader in that region (D03-R15)

Make the leader sit in `za`. `freeze` all three `za` servers. `wait_dark`.

- Reachable voters: 6. A new election must happen.
- Record: elected within the election deadline or not; the new leader's
  cluster; seconds. **Do not name the winner in advance.**
- **Write probe** and **metadata probe**, as in S2.
- **Do not move the leader into the hub yet.** Go straight to S4. A hub
  step-down comes after S4 has observed recovery.
- Repeat with `au`.

### S4 — recovery (D03-R16)

Runs after S2 and after S3. `thaw` the dark region.

Recovery is proved by all of these. A matching size or a leader that agrees is
not enough.

1. **All nine monitors answer** (`/healthz`).
2. **The meta group names a live leader**, and every returned server reports the
   same leader. (Recovery deadline.)
3. **Returned peers are current.** Ask the **elected leader's** monitor for
   `/jsz?meta=1`. Read `.meta_cluster.replicas[]`: each returned peer's `.name`
   must show `.current == true`. The leader is not in its own replica list, so
   only the leader's view covers every other peer. (Field names come from saved
   `/jsz` responses in this lab. Confirm them on `2.14.6` during setup.)
4. **Seeded data matches.** Every seeded and every acknowledged message in each
   fixture stream reads back by Direct Get, with the right `Nats-Msg-Id` and
   payload, in order (rule 8). KV keys match by Direct Get.
5. **A write probe and a metadata probe pass** again.

Then record, as separate results:

- **After S3 only:** does regional leadership persist after the region returns?
  (Expect: it stays. This is a result to measure, not a given.)
- **Then** ask for `step-down --cluster arb`. Record the reply and where the
  leader lands. Measure this apart from the automatic behaviour above.

A note, not a pass rule: the T4 → T5 runs once showed a server rolling its meta
log back. There is no probe for that here. If one is wanted, name its
observable first.

### S5 — both regions dark (D03-R17)

Leader in the hub. `freeze` `za`, then `au`. `wait_dark` on all six monitors.

- Reachable voters: 3 of 9. No quorum.
- Expect: no site elects a leader. Time it against the **quorum-loss detection**
  deadline, not the election deadline. Use `seconds_until_no_leader`.
- **Write probe** to `ODOMETER_ARB`: expect acknowledged, stored writes to
  **continue**. Its three replicas are all in the healthy hub, so the stream's
  own quorum survives. Read the acked messages back by Direct Get (rule 8).
- **Metadata probe:** expect creating the temporary stream to **fail**. There is
  no meta leader.
- This is the point of S5: **the stream keeps working while metadata is frozen.**
- Then `thaw` **one** region. Reachable voters: 6. Run the **partial recovery
  check** below. Record whether quorum returns, the time against the recovery
  deadline, and where the leader lands.
- Then `thaw` the last region and run S4's full checks.

**Partial recovery check (six servers up, three still frozen).** S4's full
checks cannot pass yet, because three monitors are still frozen and one
regional fixture has no quorum. Instead:

1. The six live monitors answer. The three frozen ones still **pass**
   `wait_dark` (it succeeds when a monitor stops answering).
2. A live leader is named, and the six live servers agree on it.
3. On the leader's `/jsz?meta=1`, every **live** peer in `.meta_cluster.replicas[]`
   is `.current`. The three frozen peers are expected not to be.
4. Every seeded and acked message in `ODOMETER_ARB` and the returned region's
   stream reads back by Direct Get (rule 8). The frozen region's stream is not
   checked.
5. A write probe and a metadata probe pass.

### S6 — regional data (D03-R18)

Uses the fixtures built before any failure.

- `freeze` `za`. `ODOMETER_ZA` has lost its quorum, but the meta group has not.
- Expect: writes to `ODOMETER_ZA` are refused or time out. Writes to
  `ODOMETER_ARB` and `ODOMETER_AU` keep succeeding. The metadata probe passes.
- Show that **meta leader location is not data availability.**
- Repeat with `au` dark.

## What a finding needs

- A check ID (`ML1`, `ML2`, …) for every claim.
- A statement of what the check proves and what it does not.
- A tag — `measured`, `inferred` or `unmeasured` — on every row.

## Out of scope for this plan

- Giving the hub more votes. The math: with six regional voters, the hub needs
  **7 voters** (7 of 13) to keep a majority when both regions go dark. That is a
  heavy price and it still does not pin the leader. Cite it in the findings. Do
  not build it now.
- The hub itself going dark.
- Dead-process (`kill_server`) failures. Run them after the `freeze` run is done.
- Domains, leaf links and accounts. The shape has none of them.

## Exercises — terminal first

Each scenario is done **by hand in a terminal first**, then automated in `10`.
This follows `demo-playbook.html` stage 03: the terminal shows the exact error
text a check must look for. The existing `lab/` scripts and reports keep their
shape. `exercises/` sits beside them (decided 2026-10-05: "keep their shape"
means do not restructure, not "add nothing").

### Shared rig lifecycle — one script, used by both

`lab/rig-t4.sh` (new, not numbered, not in `run-all.sh`'s `LABS` list). It
sources `lab/_common.sh`. It writes no configs: the nine are static files in
`exercises/config/`.

| Command | Does |
|---|---|
| `./lab/rig-t4.sh up` | refuses if a rig is already running; copies the nine `t-*.conf` files and `accounts.conf` from `exercises/config/` into `lab/run/`, starts the nine servers, waits for all nine monitors and a meta leader, and **leaves the rig running** |
| `./lab/rig-t4.sh status` | **read-only.** Prints, per server: answering or dark, meta leader, meta size; and the leader's `.meta_cluster.replicas[]` `.current` list |
| `./lab/rig-t4.sh restart <server>` | thaws, then stops one running server with TERM, waits for it to exit, starts it again from the same config and store, and waits for its monitor. Refuses if that server is not running. Used by Step 2.6 (S1's hub restart) |
| `./lab/rig-t4.sh down` | thaws every frozen server first, then stops all `t-` servers. Safe to run when nothing is running |

**Why `up` cannot call `lab_init` as it is.** `lab_init` in `lab/_common.sh`
deletes `lab/run/{log,pid,js}` and the `t-*.conf` files, kills every `t-`
server, and sets `trap lab_down EXIT INT TERM`. That trap would stop the rig as
soon as `up` exits. So:

- **`up`** checks first, deletes second:
  1. If any `t-` server is running (`pgrep -f "nats-server -c t-"`), or any of
     the rig's client, monitor, route or gateway ports is in use, **stop with
     a message and delete nothing.** It never overwrites an active run.
  2. Only then clear and recreate `lab/run/{log,pid,js}` and the `t-*.conf`
     files.
  3. During startup, set a trap that tears the rig down **if startup fails or
     is interrupted.**
  4. On success, **remove that trap** (`trap - EXIT INT TERM`) and exit 0 with
     the rig running.
- **`status`** never calls `lab_init`, never deletes, never traps, never
  freezes or thaws.
- **`down`** calls `lab_down` (it already sends `CONT` before `TERM`). It exits
  0 when nothing is running.
- **`10` full run** keeps automatic teardown. It tears down on completion, on
  failure and on interrupt. Like `up`, it **refuses to start if a rig is
  already running**, because `lab_init`'s `lab_down_quiet` would silently kill
  a manual rig and lose its state.
- **`10 stepN` (one step)** may attach to a running rig. See "Automating each
  step" below for its rules.
- Split `lab_init`'s parts so `up` can use the clear-and-create step without the
  trap. Add the split to `_common.sh` **without changing what `lab_init`
  does**, because the nine measured scripts call it.

- The exercises and `10` both use this script. There is one way to start and
  stop the rig.
- **Never run the manual rig and `run-all.sh` at the same time.** They use the
  same ports. `run-all.sh`'s scripts call `lab_init`, which kills a manual rig
  without warning. Run `down` first.
- Freeze and thaw by hand with the PIDs in `lab/run/pid/<name>.pid`:
  `kill -STOP "$(cat lab/run/pid/za-1.pid)"`. Then prove it with the monitor
  port: `curl -fs --max-time 1 localhost:8231/healthz` must fail.

### Layout

**One exercise, numbered after its lab script: exercise 10.** It has six
steps, and each step has numbered sub-steps (`Step 1.1`, `Step 1.2`, …), the
same shape as demo 05's exercises. The files sit flat in `exercises/`, as in
demo 05. A later experiment (`lab/11-….sh`) is exercise 11 in the same folder.

```text
lab/10-hub-meta-leader.sh
exercises/
├── config/
│   ├── accounts.conf          included by every server config
│   └── t-{za,au,arb}-{1,2,3}.conf
├── EXERCISE-10-TERMINAL-STEPS.md
├── ex10-check.sh
└── EXERCISE_OBSERVATIONS.md
```

| Step in exercise 10 | Plan test | Holds |
|---|---|---|
| Step 1 | — | set up: `rig-t4.sh up`, check readiness, build the fixtures (rule 8, fixtures table) |
| Step 2 | S1 | all up — placement trials |
| Step 3 | S2 + S4 | one region dark, leader in the hub, then recovery |
| Step 4 | S3 + S4 | one region dark, leader in that region, then recovery, then hub step-down |
| Step 5 | S5 | both regions dark, then partial and full recovery |
| Step 6 | S6 | regional data |

| Path | Holds |
|---|---|
| `exercises/config/` | the nine static server configs and `accounts.conf` |
| `exercises/EXERCISE-10-TERMINAL-STEPS.md` | all six steps, with sub-steps |
| `exercises/EXERCISE_OBSERVATIONS.md` | what each hand run actually printed |
| `exercises/ex10-check.sh` | a thin wrapper: `exec bash "$(dirname "$0")/../lab/10-hub-meta-leader.sh" "$@"`. No checks of its own |

**The nine server configs are static files in `exercises/config/`**, like demo
05 (user, 2026-10-05). A person can read and change them; no script writes
them. `rig-t4.sh up` copies them into `lab/run/` and starts each server from
there, so the command stays `nats-server -c t-<name>.conf` (the kill pattern
still matches) and the JetStream stores land in `lab/run/js/` (gitignored).
The other lab scripts (`01`–`09`) still generate their own configs; they are
measured and stay as they are.

**S4 is not its own step.** Recovery only makes sense after a failure, so it
is part of Steps 3, 4 and 5.

`00` is not a valid exercise number.

### Every step has the same parts

1. **Prerequisites** — which step must run first, and whether the rig is up.
2. **Starting state** — the expected leader's cluster, which servers are
   frozen, and how to check it (`rig-t4.sh status`).
3. **Prediction** — written before the run. It matches the plan's expected result.
4. **Sub-steps** — one concept per sub-step. Exact commands. Every `nats` command
   names its server and user. No context is assumed.
5. **Verification** — what to read, from where, and the pass rule.
6. **Cleanup** — thaw everything, then confirm all nine monitors answer and
   a leader exists. Or, at the end, `rig-t4.sh down`.

### Hand-run results are evidence

- A hand run is a real run. Record in `EXERCISE_OBSERVATIONS.md`: the commands,
  the actual output, the date and time, and the versions (`nats-server`, `nats`
  CLI).
- Label them **measured by hand**.
- They carry no `ML` check IDs. The decks cite only `REPORT.md`, and
  `tools/check-deck-numbers.py` reads only that file. **Do not cite a hand
  result in the pattern cards until `10` has repeated it.**
- The playbook says to run each measurement more than once. One hand run gives
  the error text, not a finding.

### Automating each step — one implementation, two ways in

Every check lives in `lab/10-hub-meta-leader.sh`, split into one part per
step of exercise 10 (`step1` … `step6`). Nothing else holds a check, so the
hand steps and the automation cannot drift into two versions.

| Command | Runs |
|---|---|
| `exercises/ex10-check.sh step1` | Step 1's checks (a wrapper; the same as `lab/10-hub-meta-leader.sh step1`) |
| `exercises/ex10-check.sh` | every step, in order — the full suite (the same as `lab/10-hub-meta-leader.sh`) |

**Prerequisites are explicit.** A part never depends on an earlier run without
checking it.

- **No rig running:** the part starts its own rig, builds what it needs
  (`step3` runs `step1`'s set-up first), and **tears the rig down at the end**,
  on failure and on interrupt too.
- **A rig already running** (started by hand with `rig-t4.sh up`): the part
  **attaches**. It first checks its starting state — all nine monitors answer,
  no server frozen, the fixtures exist — and **refuses with a message** if any
  of that is wrong. It never calls `lab_init`. It **leaves the rig running**
  when it ends, with every server it froze thawed.
- **The full suite** refuses if a rig is running (see the lifecycle above).

**A partial run never reaches `REPORT.md`.** Every check row is appended to
`lab/run/results.tsv` (`_common.sh`, `record`), and only `run-all.sh` clears
that file. So a part run alone would add its rows to the last full run's file,
and `./run-all.sh report` would publish them mixed in. Therefore:

- A part run, or `10` run on its own, writes its rows to its own file,
  `lab/run/10-<part>.tsv` (for example `10-step1.tsv`), and prints its own
  PASS/FAIL summary.
- Only `10` called from `run-all.sh` writes to `results.tsv`.
- The file name marks a run as partial. No partial row can enter the report.

**Check or note.** A check needs one answer known before the run.

- **Check:** "a live leader exists, and all nine servers name the same one"
  (`wait_live_leader`). Meta size is 9. All eight peers are current.
- **Note:** *which* server leads after start-up. Nothing pins it, so it has no
  expected answer. (The first hand run elected `t-au-2`.)
- **Check, after a hub step-down (S1):** the leader is a `t-arb-*` server —
  that is the requirement. A trial that lands outside the hub is a procedure
  FAIL, recorded as the finding (rule 9).
- `.current` lags a freeze by several seconds (measured by hand, about 11.6 s).
  A liveness check reads `active` as well, never `current` alone.

### Order of work

One step at a time, all the way through, before the next one starts:

1. `lab/rig-t4.sh`. (Done.)
2. For each step of exercise 10, 1 to 6, in order:
   1. Write the step and its sub-steps in `EXERCISE-10-TERMINAL-STEPS.md`.
   2. Run it by hand. Record the output in `EXERCISE_OBSERVATIONS.md`.
   3. Add part `stepN` to `10`, using the exact error text seen by hand.
   4. Run `ex10-check.sh stepN`, on its own rig and attached to a
      hand-started rig, until its checks are stable.
3. Run the full `10` on its own until its checks and verdicts are stable.
4. Add `10` to `run-all.sh`'s `LABS` list.

## Where the work goes

| Piece | Path |
|---|---|
| Rig lifecycle | `lab/rig-t4.sh` (new; builder copied from `06-arbiter3.sh`; uses `lab/_common.sh`) |
| Hand-run procedure | `exercises/EXERCISE-10-TERMINAL-STEPS.md` — Steps 1–6 |
| Hand-run results | `exercises/EXERCISE_OBSERVATIONS.md` |
| Automated check | `lab/10-hub-meta-leader.sh` (uses `rig-t4.sh`), one part per step |
| Exercise check | `exercises/ex10-check.sh [stepN]` — wrapper only, calls `10 [stepN]` |
| Partial-run results | `lab/run/10-<part>.tsv` — never `results.tsv` |
| Scratch configs | `lab/run/`, gitignored, every file `t-` prefixed |
| Findings | `REPORT.md` and `REPORT.html`, generated by `lab/run-all.sh` — do not hand-edit |
| Demo rules | add `exercises/` and `rig-t4.sh` to demo 03's `CLAUDE.md` documents table |
| Requirements | add `D03-R13`…`D03-R18` to `README.md` only after the user agrees |

**Decided:**
- Copy 06's small topology builder (`gw3`, `build`) into `lab/rig-t4.sh`, and
  reuse `lab/_common.sh`. `06-arbiter3.sh` is measured, so it stays untouched.
- Run `10` by hand first. Add it to `run-all.sh` only once its checks and
  verdicts are stable.
- Exercises sit beside `lab/`. No playbook change.
- Checks live only in `10`. `ex10-check.sh` is a wrapper (Codex review,
  2026-10-05).
- One exercise, numbered 10 after its lab script, with Steps 1–6 and
  sub-steps, flat in `exercises/` like demo 05 (user, 2026-10-05). This
  replaced six exercises in `exercises/10-hub-meta-leader/`. Each step is
  automated before the next one is written.
- A partial run writes to `lab/run/10-<part>.tsv`, so it cannot change
  `REPORT.md`.
