# Exercise observations

What each hand run actually printed. Exercise 10 (hub meta-leader) plan:
[`HUB-META-LEADER-PLAN.md`](../HUB-META-LEADER-PLAN.md).

Every result here is **measured by hand**. These results carry no `ML` check
IDs. **Do not cite them in the pattern cards until
`lab/10-hub-meta-leader.sh` has repeated them** — the decks cite only
`REPORT.md`. One hand run gives the error text and a first reading, not a
finding.

---

## 2026-10-05 — Exercise 10: rig smoke test and Step 1 trial (agent run)

**Versions:** `nats-server v2.14.6`, `nats` CLI `0.4.0`, macOS (Darwin 25.4.0).
**Who ran it:** Claude, while building `lab/rig-t4.sh`. This is a check that the
steps work. It is not the user's own run of Step 1.

### `rig-t4.sh` lifecycle

| Action | Printed |
|---|---|
| `up` (clean) | `rig up: 9 servers`; all nine `up`, meta size `9`, leader `t-au-2` on all nine |
| `up` again, rig running | `refusing: t- servers are already running` — exit 1, nothing deleted |
| `status`, `za-1` frozen | `za-1` shown `dark`; no hang; the other eight read normally |
| `down` | `rig down` |
| `down` again | `nothing running` |

A demo 05 server (`nats-server -c exercises/config/ex03-nats-defaults.conf`,
port 4522) was running throughout. `up`, `status` and `down` left it alone.

### First leader

`t-au-2` — a **regional** server, not the hub. Nothing pins the leader. One
reading; it says nothing about how often each cluster wins.

### `.current` lags a freeze

`za-1` frozen with `kill -STOP`, read from the leader's monitor (`8242`):

| Time after freeze | `t-za-1` |
|---|---|
| ~5 s | `current=true`, `active=4524261583` (≈4.5 s since last contact) |
| ~12 s | `current=false`, `active=11608238833` (≈11.6 s) |

So `.current == true` alone does not prove a peer is alive in the first few
seconds after a failure. `active` (nanoseconds since last contact) shows the
gap immediately. **Consequence for the plan:** a recovery check must read
`current` **and** a small `active`, and must not read `current` too soon after
a freeze. Field `offline` was absent from these replica entries.

### Field names confirmed on 2.14.6

`.meta_cluster.leader`, `.meta_cluster.cluster_size`,
`.meta_cluster.replicas[].name`, `.current`, `.active`, `.peer`.

### Fixtures (Steps 1.5–1.7)

| Fixture | Placement Cluster | Stream leader | Replicas |
|---|---|---|---|
| `ODOMETER_ARB` | `arb` | `t-arb-3` | `t-arb-1`, `t-arb-2` — current |
| `ODOMETER_ZA` | `za` | `t-za-3` | `t-za-1`, `t-za-2` — current |
| `ODOMETER_AU` | `au` | `t-au-2` | `t-au-3` current; `t-au-1` **"outdated, not seen, 1 operation behind"** straight after creation |
| KV `t7-vehicles` | `arb` | `t-arb-2` | `t-arb-1`, `t-arb-3` — current |

All four show `Direct Get: true`.

### Acked publish and Direct Get (Steps 1.8–1.9, one message only)

```
pub -J -H "Nats-Msg-Id:seed.arb.1" evt.odo.arb.v1 …
  Stored in Stream: ODOMETER_ARB Sequence: 1

req '$JS.API.DIRECT.GET.ODOMETER_ARB' '{"seq":1}'
  Nats-Sequence: 1
  Nats-Msg-Id: seed.arb.1
  {"id":"seed.arb.1","vehicle":"v-1","km":1001}
```

### Metadata probe (Step 1.11)

`stream add PROBE_META … --cluster arb` → `Stream PROBE_META was created`,
leader `t-arb-2`. `stream rm PROBE_META -f` → no output.

### Not run in this trial

Step 1.8's full loop (only `seed.arb.1` was sent), Step 1.9 for `seq` 2–4 and
for the regional streams, and Step 1.10 (KV put and get). Those are for the
user's run of Step 1.

---

## 2026-10-05 12:04–12:07 SAST — Exercise 10: Step 2 placement trials (agent run)

**Versions:** `nats-server v2.14.6`, `nats` CLI `0.4.0`, macOS (Darwin 25.4.0).
**Who ran it:** Claude, by hand, one command at a time, while writing Step 2.
**Starting state:** a fresh `rig-t4.sh up` (configs from `exercises/config/`),
then `ex10-check.sh step1` attached: 23 passed, 0 failed. Leader after
start-up: `t-au-2` (regional).

### The step-down command on CLI 0.4.0

- `nats server cluster step-down --help` lists `--cluster`, `--tags`, `--host`,
  `-j` and `-f`. `--cluster` works. It needs the system user (`admin`).
- The CLI prints two lines and waits for the new leader:
  `Requesting leader step down of "t-au-2" in a 9 peer RAFT group`, then
  `New leader elected "t-arb-3"`.
- `-j` printed the **same two text lines**, not JSON. A script must read the
  leader from `/jsz`, not from the CLI.

### Trials — each from a confirmed regional leader

| Trial | Leader before (`/jsz`) | `step-down --cluster arb` → leader after | All nine agree | CLI wall time |
|---|---|---|---|---|
| 1 | `t-au-2` | `t-arb-3` | yes (read from `8541`) | not timed |
| 2 | `t-au-3` | `t-arb-2` | yes | 0.53 s |
| 3 | `t-za-1` | `t-arb-2` | yes | 0.53 s |
| 4 | `t-au-1` | `t-arb-1` | yes | 0.53 s |
| 5 | `t-za-2` | `t-arb-2` | yes | 0.53 s |
| 6 | `t-au-3` | `t-arb-2` | yes | 0.53 s |

**6 of 6 trials put the leader in the hub.** Every regional step-down
(`--cluster za` / `--cluster au`) also landed in the cluster asked for. The
hub server chosen varies: `t-arb-2` four times, `t-arb-1` and `t-arb-3` once
each. The wall time is the whole CLI call, including its own wait. It is not
an election time.

### Step-down with the leader already in the hub (Step 2.5)

`t-arb-2` → `t-arb-3`. The leader moved to another hub server. Not a trial.

### Hub server restart (Step 2.6)

Leader `t-arb-3`. `./lab/rig-t4.sh restart arb-1` →
`t-arb-1 restarted: PID 16101 -> 18259`.

- Meta leader after: still `t-arb-3` on all nine. Eight peers `current=true`.
  **The leader did not move.**
- `ODOMETER_ARB` after: `msgs: 3`, both replicas current. **The stream's own
  leader changed** (`leader_since` 10:06:19, the time of the restart): it is
  now `t-arb-2`. That is the stream's Raft group, not the meta group.

### What this does not prove

One run of six trials. The rig was idle: no client load, no frozen server.
A hub step-down chose a hub server six times out of six, but the plan's own
background says the filter is a preference, not a rule. Steps 3–5 test it
with servers dark.

---

## 2026-10-05 12:26–12:29 SAST — Exercise 10: Step 3, one region dark with the leader in the hub (agent run)

**Versions:** `nats-server v2.14.6`, `nats` CLI `0.4.0`, macOS (Darwin 25.4.0).
**Who ran it:** Claude, by hand, one command at a time, while writing Step 3.
**Starting state:** a fresh `rig-t4.sh up`, then `ex10-check.sh step1`
attached (23 passed). Leader after start-up `t-za-2`. Then
`step-down --cluster arb` → `t-arb-1`.

New rig commands, used here for the first time:
`./lab/rig-t4.sh freeze za` printed
`frozen: za-1 za-2 za-3 -- monitors 8231 8232 8233 do not answer`.
`./lab/rig-t4.sh thaw za` printed `thawed: za-1 za-2 za-3 -- monitors answer`.

### Round 1 — `za` dark, leader `t-arb-1`

| What | Printed |
|---|---|
| `status`, about 13 s after the freeze | six `up`, all name `t-arb-1`; the three `t-za-*` peers `current=false`, `active≈13.09 s` |
| acked publish, `ODOMETER_ARB` (port 4541) | `Stored in Stream: ODOMETER_ARB Sequence: 4` |
| acked publish, `ODOMETER_AU` (port 4241) | `Stored in Stream: ODOMETER_AU Sequence: 4` |
| Direct Get of both seq 4 | the right `Nats-Msg-Id` and payload |
| metadata probe | `Stream PROBE_META was created` (placement `arb`), delete prints nothing |
| `stream info ODOMETER_ZA` from 4541, 5 s timeout | `could not request Stream info: context deadline exceeded` (Step 6 territory — noted only) |

**With `za` dark, the leader stayed, writes went on, and metadata changed.**

### Round 1 recovery — `thaw za`

- **The meta leader moved.** Before: `t-arb-1`. After: **`t-au-3`** — out of
  the hub. `arb-1.log`: `12:27:41.770 JetStream cluster no metadata leader`,
  then `12:27:42.018 new metadata leader: t-au-3/au`. About 0.25 s with no
  leader.
- Meta Raft term (`/raftz?group=_meta_`, field `term`) read after: `5`.
  Not read before this round.
- All nine name `t-au-3`; the eight other peers `current=true`.
- `ODOMETER_ZA`: `messages: 3`, its own leader re-elected at 12:27:41.80
  (`t-za-1`), both replicas current.
- Direct Get of every stored message in all three streams (11) gave the right
  id and payload, in order. KV `vehicle-1` = `seed-k1-v1`.
- Write probe after: `ODOMETER_ZA Sequence: 4`. Metadata probe: created and
  deleted.

### Round 2 — `au` dark, leader `t-arb-3`

`step-down --cluster arb` from `t-au-3` → `t-arb-3`. Term before the freeze:
`6`.

| What | Printed |
|---|---|
| acked publish, `ODOMETER_ARB` | `Sequence: 5` |
| acked publish, `ODOMETER_ZA` (port 4231) | `Sequence: 5` |
| metadata probe | created and deleted |
| term and leader while dark | term `6`, leader `t-arb-3` — unchanged |

### Round 2 recovery — `thaw au`

- **The leader moved again**, this time inside the hub: `t-arb-3` →
  `t-arb-2`. Term `6` → **`8`** (+2; see the term correction at the end of
  this file — not "two elections"). `arb-3.log`:
  `12:28:48.558 no metadata leader`, `12:28:48.847 new metadata leader:
  t-arb-2/arb`. About 0.29 s with no leader.
- All nine name `t-arb-2`; eight peers current.
- Direct Get: every stored message (14) right, in order. Write probe
  `ODOMETER_AU Sequence: 5`. Metadata probe created and deleted.

### What this shows, and what it does not

- **While a region is dark,** the hub leader held, both surviving streams took
  acked writes, and metadata changed. Two rounds, both regions.
- **When the region came back,** the meta group held a new election both
  times, and the leader changed both times. Once it left the hub. Data was
  intact both times.
- **The finding, as worded everywhere:** During some regional recoveries, the
  metadata group entered a new election term and sometimes elected a different
  leader. In one diagnostic run, logs showed returning servers requesting
  votes with higher terms, causing the incumbent leader to step down. Why
  those servers initiated elections remains unproved, and this sequence has
  not been confirmed in normal runs.
- **The cause is not proved.** The logs show only the result. The term rose
  by 2 on the second thaw. **Hypothesis, not a finding:** the thawed
  servers' election timers ran out during the freeze, and they campaign on
  wake. Prove it before writing it down (debug logs or the server source).
- These tests use `kill -STOP` (SIGSTOP) and `kill -CONT` (SIGCONT). That
  freezes a process whole, timers included. It is not a real network
  partition, where the far side keeps running, and it is not a restart. A
  real partition's return may behave differently.

### Repeated by the script, 12:34 and 12:35 SAST

`lab/10-hub-meta-leader.sh step3` (`ML42`–`ML75`), twice: once attached to a
hand-started rig after `step1`, once on its own rig. Rows in
`lab/run/10-step3.tsv` (not in `REPORT.md` — script 10 is not in `run-all.sh`).

| Run | Round | S2 verdict (dark) | Leader before → after thaw | Term | S4 verdict |
|---|---|---|---|---|---|
| attached | za | passed | `t-arb-3` → `t-arb-1` | 2 → 4 | failed (`ML54`) |
| attached | au | passed | `t-arb-1` → `t-za-3` | 4 → 6 | failed (`ML71`) |
| own rig | za | passed | `t-arb-2` → `t-au-1` | 2 → 4 | failed (`ML54`) |
| own rig | au | passed | `t-arb-2` → `t-za-2` | 5 → 7 | failed (`ML71`) |

The only unmet check in each S4 is "same leader". Everything else after the
thaw passed: eight peers current about 3.2 s after the thaw, every message
back by Direct Get, writes and metadata work. Counting the hand run: **6 of 6
returns caused an election, and the leader left the hub in 4 of 6.** The term
rose by 2 on every scripted return. The cause is still not proved.

---

## 2026-10-05 12:39–12:43 SAST — Exercise 10: Step 4, one region dark with the leader in that region (agent run)

**Versions:** `nats-server v2.14.6`, `nats` CLI `0.4.0`, macOS (Darwin 25.4.0).
**Who ran it:** Claude, by hand, one command at a time, while writing Step 4.
**Starting state:** a fresh `rig-t4.sh up`, then `ex10-check.sh step1`
attached (23 passed). Leader after start-up `t-au-3`.

Times come from the `date` call just before `rig-t4.sh freeze` and from the
`arb-1.log` line `new metadata leader`. `freeze` sends `kill -STOP`, then
sleeps 3 s, so the gap is measured from the moment just before the STOP.

### Round 1 — leader in `za`, then `za` dark

| What | Printed |
|---|---|
| `step-down --cluster za` | `t-au-3` → `t-za-1`. Term after: `3` |
| `freeze za`, at 12:40:03.29 | `frozen: za-1 za-2 za-3 -- monitors 8231 8232 8233 do not answer` |
| `arb-1.log` | `12:40:08.284 new metadata leader: t-au-3/au` — **about 5.0 s** after the freeze. No `no metadata leader` line in this log |
| term while dark | `4` — one election |
| `status` | six `up`, all name `t-au-3`; the three `t-za-*` peers `current=false`, `active=0` |
| acked publish, `ODOMETER_ARB` / `ODOMETER_AU` | `Sequence: 4` each; Direct Get gives the right id and payload |
| metadata probe | created (placement `arb`) and deleted |

### Round 1 recovery — `thaw za`

- **The leader stayed.** `t-au-3` before and after. Term `4` before and after.
  No election in the group.
- `za-1.log` (the old leader): `12:40:54.317 no metadata leader`, then
  `12:40:54.349 new metadata leader: t-au-3/au` — it rejoined as a follower
  in about 32 ms.
- All nine name `t-au-3`; eight peers current.
- Direct Get: every stored message (11) right, in order. KV `vehicle-1` =
  `seed-k1-v1`. Write probe `ODOMETER_ZA Sequence: 4`. Metadata probe OK.
- **Hub step-down after recovery:** `step-down --cluster arb` → `t-arb-3`.

### Round 2 — leader in `au`, then `au` dark

| What | Printed |
|---|---|
| `step-down --cluster au` | `t-arb-3` → `t-au-3`. Term after: `6` |
| `freeze au`, at 12:41:49.54 | frozen, monitors 8241–8243 dark |
| `arb-1.log` | `12:41:53.770 new metadata leader: t-za-3/za` — **about 4.2 s** after the freeze |
| term while dark | `7` — one election |
| acked publish, `ODOMETER_ARB` / `ODOMETER_ZA` | `Sequence: 5` each |
| metadata probe | created and deleted |

### Round 2 recovery — `thaw au`

- **The leader stayed.** `t-za-3` before and after. Term `7` before and after.
- `au-3.log` (the old leader): `12:42:07.408 no metadata leader`, then
  `12:42:07.438 new metadata leader: t-za-3/za` — about 30 ms.
- Direct Get: every stored message (14) right, in order. Write probe
  `ODOMETER_AU Sequence: 5`. Metadata probe OK.
- **Hub step-down after recovery:** `t-za-3` → `t-arb-2`.

### What this shows, and what it does not

- **The new leader went to the other region both times** (`t-au-3`, then
  `t-za-3`), not to the hub. Two readings. The plan says not to name a winner
  in advance, and two readings do not make a rule.
- **A new leader in about 4–5 s**, inside the 30 s deadline, both times.
- **Opposite of Step 3 on the return:** here the term did not move and the
  leader stayed. In Step 3 every return caused an election. Both are recorded.
  **Why they differ is not proved** — do not write a cause down.
- A hub step-down after recovery put the leader in the hub both times.

### Repeated by the script, 12:45–12:53 SAST

`lab/10-hub-meta-leader.sh step4` (`ML76`–`ML111`), three runs. Run 1 was
attached to the rig above, after the hand run. Run 2 was on its own rig. Run 3
was attached to a fresh rig after `step1` and `step3`. Rows in
`lab/run/10-step4.tsv`.

Run 1 timed the election from **after** `rig-t4.sh freeze` returned. That
adds its 3 s sleep and the monitor probes: it read 9.2–9.3 s. The script now
sends the STOP itself and starts the clock with it. Runs 2 and 3 use that.

| Run | Round | New leader while dark | Seconds from STOP | S3 | Term (before freeze → after return) | Leader on return | S4 |
|---|---|---|---|---|---|---|---|
| 1 | za | `t-arb-1` | (9.24, old timing) | passed | 9 → 10 | stayed | passed |
| 1 | au | `t-arb-2` | (9.30, old timing) | passed | 12 → 13 | stayed | passed |
| 2 | za | `t-arb-3` | 3.75 | passed | 3 → 4 | stayed | passed |
| 2 | au | `t-arb-2` | 3.52 | passed | 6 → 7 | stayed | passed |
| 3 | za | `t-arb-3` | 3.75 | passed | 7 → 8 | stayed | passed |
| 3 | au | `t-arb-3` | 4.33 | passed | 10 → 12 | **moved to `t-au-2`** | **failed** (`ML106`) |

- **Every S3 passed:** a new leader outside the dark region in 3.5–4.3 s,
  writes stored, every message read back, metadata changed.
- **The new leader was in the hub 6 of 6 times in the script**, and in the
  other region 2 of 2 times by hand. Do not read a rule into either.
- **On return, 7 of 8 Step 4 rounds had no election** (term +1 = the dark
  election only). Run 3's `au` round had one more (term +2), and the leader
  moved out of the hub to `t-au-2`.
- Hub step-down after recovery (`ML93`, `ML111`): in the hub 6 of 6.

### Step 3 again, run 3 (12:49 SAST) — a re-elected leader can be the same server

Before run 3 of step4, step3 ran attached on the same rig. In the `za` round
the leader stayed `t-arb-2`, so `ML54` ("same leader") **passed**. But the term
went `2 → 2` while dark (`ML49a`) and `2 → 4` on the return (`ML54a`): the
term advanced by 2, and the same server was leader again. The `au` round moved the leader
(`t-arb-2` → `t-arb-3`, term 4 → 6, `ML71` failed).

So **"same leader" does not show the term held.** Step 3 returns so far: the
term advanced in 8 of 8; the leader moved in 7 of 8.

### Term correction, and the term check (2026-10-05, after a Codex review)

**Correction.** Earlier notes above call the term an election counter and read
+2 as "two elections". That is wrong. A Raft term rises on every election
**attempt**, won or not (Raft paper, sections 5.1–5.2). A rise shows that the
term advanced. It does not count successful elections. Read "one election" in
the Step 4 tables as "term +1".

**The check.** On the user's choice (option 1), every return now has a term
check, suffix `b`: `ML54b`, `ML71b`, `ML88b`, `ML106b`. It is named "meta term
unchanged during recovery". Its window: read just before the thaw, read again
at the end of the settle window (peers current, then 10 s, then an agreed
leader). The term only rises, so the end value covers the window. Both
readings must be numbers; a missing reading is a rig failure, so the verdict
becomes inconclusive. The verdict now reports **recovery** and **leadership
stability** apart.

**Re-test, own rig each:**

| Part | Round | Leader before → after thaw | Term just before thaw → end of window | Recovery | Leadership stability |
|---|---|---|---|---|---|
| step3 | za | `t-arb-2` → `t-arb-3` | 2 → 4 | met | not met (`ML54`, `ML54b`) |
| step3 | au | `t-arb-3` → `t-za-1` | 4 → 6 | met | not met (`ML71`, `ML71b`) |
| step4 | za | `t-au-1` → `t-au-1` | unchanged | met | met |
| step4 | au | `t-za-2` → `t-au-1` | 7 → 8 | met | not met (`ML106`, `ML106b`) |

step3: 51 passed, 4 failed (all procedure). step4: 55 passed, 2 failed (all
procedure). No rig check failed.

What this supports: hub placement can be restored on request (`ML93`,
`ML111`), and a region's return can disturb leadership even when the hub stays
healthy. Data recovered every time. **Why leadership is disturbed is not
proved.**

---

## 2026-10-05 13:04–13:07 SAST — Exercise 10: Step 5, both regions dark (agent run)

**Versions:** `nats-server v2.14.6`, `nats` CLI `0.4.0`, macOS (Darwin 25.4.0).
**Who ran it:** Claude, by hand, one command at a time, while writing Step 5.
**Starting state:** a fresh `rig-t4.sh up`, then `ex10-check.sh step1`
attached (23 passed). Leader after start-up `t-au-3`.

### Into the dark

| What | Printed |
|---|---|
| `step-down --cluster arb` | `t-au-3` → `t-arb-3`. Term `3` |
| `freeze za` | frozen, monitors 8231–8233 dark. Leader still `t-arb-3` |
| `date`, then `freeze au` | `13:04:59.54` |
| `arb-3.log` (the old leader) | `13:05:16.259 no metadata leader` — about **16.7 s** after the `date` |
| arb-1 `/jsz` leader field | still `t-arb-3` until `13:05:36.65` — about **37.1 s** after the `date`. `arb-1.log` has no `no metadata leader` line at all |
| 8542, 8543 `/jsz` | `"leader": null` |
| arb-1 `/raftz` | `CANDIDATE`, leader `null`, term `7`; a minute later term `12` |
| acked publish, `ODOMETER_ARB` | `Sequence: 4`; Direct Get gives `s5.bothdark.arb.1` and the payload |
| `stream add PROBE_META` | `JetStream system temporarily unavailable (10008)`, exit 1 |

### Partial recovery — `thaw za`

| What | Printed |
|---|---|
| `date`, then `thaw za` | `13:06:10.27` |
| `arb-2.log` | `13:06:11.728 Self is new JetStream cluster metadata leader` — about **1.5 s** after the `date` |
| arb-1 `/jsz` names it | `t-arb-2` at `13:06:13.46` |
| `status` | six `up` name `t-arb-2`; three `t-au-*` `dark`. On `t-arb-2`'s view the five live peers `current=true`; the three `t-au-*` `current=false`, `active=0` |
| term | `14`, `FOLLOWER` on arb-1 |
| Direct Get `ODOMETER_ZA` seq 3 | `seed.za.3`, right payload |
| acked publishes | `ODOMETER_ZA Sequence: 4`, `ODOMETER_ARB Sequence: 5` |
| metadata probe | created (placement `arb`) and deleted |

### Full return — `thaw au`

- Term just before the thaw: `14`. After 12 s: `14`. **Unchanged.**
- All nine name `t-arb-2`; eight peers current. `au-1.log`:
  `13:06:42.735 new metadata leader: t-arb-2/arb`.
- Direct Get: every stored message (5 + 4 + 3 = 12) right, in order. KV
  `vehicle-1` = `seed-k1-v1`. Write probe `ODOMETER_AU Sequence: 4`.
  Metadata probe created and deleted.

### What this shows, and what it does not

- **The point of S5 holds, once:** with no meta leader, the hub stream took an
  acked write and read it back, and a create failed with `10008`.
- **The leader field lags the log.** The old leader logged "no metadata
  leader" at about 16.7 s. arb-1's `/jsz` still named it until about 37 s.
  Both are inside the 90 s deadline. A check must wait for the field, not read
  it once.
- **The term rose from 3 to 14 with no leader for most of it.** This is the
  term-correction note above in action: failed attempts raise the term.
- **The first thaw elected a hub server in about 1.5 s.** One reading. Do not
  name a winner in advance.
- **The last return did not disturb leadership** (same leader, same term).
  One reading. Step 3's returns mostly did. **Why they differ is not
  proved** — do not write a cause down.

### Repeated by the script, 13:09–13:20 SAST

`lab/10-hub-meta-leader.sh step5` (`ML112`–`ML136`). Rows in
`lab/run/10-step5.tsv`.

**Run 1 (attached, after the hand run) found a bug in the check, not in the
rig.** `ML116` took its snapshot when arb-1's field went `NONE` (20.6 s). At
that moment arb-3 still named the old leader `t-arb-2`. So `ML116` and `ML117`
failed. The create still failed with `10008` in the same run, so nothing had
been elected; the name was the stale-leader field. The plan's rule is the 90 s
deadline, so the check now waits, within 90 s, until **all three** hub fields
are `NONE`. The rule did not change; the reading moment did. `ML117` now says
"names a leader", not "elected".

| Run | All three hub fields `NONE` (first / all) | S5 | Leader after `thaw za` (seconds until the six agree) | Partial | Last return: leader, term | S4 |
|---|---|---|---|---|---|---|
| 1 attached | (20.6 s, arb-1 only) | failed (`ML116`, `ML117`) | `t-arb-2` (9.4) | passed | same, unchanged | passed |
| 2 attached | 18.4 / 54.8 s | passed | `t-arb-1` (11.6) | passed | same, unchanged | passed |
| 3 own rig | 10.1 / 40.2 s | passed | `t-arb-1` (11.0) | passed | same, unchanged | passed |

Run 2: 23 passed, 0 failed. Run 3: 46 passed, 0 failed (with step1).

- **S5 holds, three times:** no meta leader with 3 of 9, hub stream writes
  stored and read back, create refused with `10008`.
- **The leader fields clear one by one.** First to last took 30–36 s. The
  slowest field cleared at 54.8 s — inside 90 s, but not by much. A
  check that reads one monitor once would be wrong.
- **The term rose by 9–10 while no leader existed** (`ML120a`: 3 → 12,
  19 → 29).
- **The first thaw elected a hub server every time** (hand run and 3 script
  runs: 4 of 4). Do not read a rule into it.
- **Partial recovery: the script's 9–12 s is not the hand run's 1.5 s.** The
  script waits until arb-1 names a leader **and** all six live monitors
  agree; the hand run read the log line. The two clocks measure different
  things. Which part takes the time is not measured.
- **The last return did not disturb leadership in 4 of 5** (same leader, same
  term). Corrected from "4 of 4" after the 14:47 full run below: its step-5
  return moved the leader `t-za-3` → `t-au-3`, term 34 → 35 (`ML132`,
  `ML132b`). That round is valid: every rig precondition passed (`ML112`
  `arb 9 8`, `ML113`–`ML115`, `ML122` `6 3`, `ML129` `9`). Step 3's returns
  disturbed leadership in most rounds. **Why is not proved.**

---

## 2026-10-05 14:36–14:38 SAST — Exercise 10: Step 6, regional data (agent run)

**Versions:** `nats-server v2.14.6`, `nats` CLI `0.4.0`, macOS (Darwin 25.4.0).
**Who ran it:** Claude, by hand, one command at a time, while writing Step 6.
**Starting state:** a fresh `rig-t4.sh up`, then `ex10-check.sh step1`
attached (23 passed). Leader after start-up `t-arb-3` — already in the hub,
so Round 1 had no step-down.

### Round 1 — `za` dark

| What | Printed |
|---|---|
| `freeze za` | frozen, monitors 8231–8233 dark |
| meta leader, term | `t-arb-3`, term `2` — unchanged |
| acked publish to `evt.odo.za.v1` through **4541** (`--timeout 5s`) | `Published 49 bytes`, then `nats: error: nats: timeout` after about 5.2 s, exit 1 |
| the same through **4241** (au) | the same: `nats: timeout`, exit 1 |
| acked publish, `ODOMETER_ARB` / `ODOMETER_AU` | `Sequence: 4` each |
| metadata probe | created (placement `arb`) and deleted |
| `thaw za`, then `stream info ODOMETER_ZA` after 8 s | `messages: 3`, `last_seq: 3` — **neither timed-out write was stored**. Stream leader `t-za-1`, `leader_since` 12:37:04Z (a new stream election after the thaw) |
| acked publish, `ODOMETER_ZA` | `Sequence: 4` |
| meta leader after the thaw | `t-au-1`, term `4` — **moved out of the hub** |

### Round 2 — `au` dark

| What | Printed |
|---|---|
| `step-down --cluster arb` | `t-au-1` → `t-arb-1` |
| `freeze au` | frozen, monitors 8241–8243 dark |
| acked publish to `evt.odo.au.v1` through 4541 | `nats: timeout`, exit 1 |
| acked publish, `ODOMETER_ARB` / `ODOMETER_ZA` | `Sequence: 5` each |
| metadata probe | created and deleted |
| `thaw au`, then `stream info ODOMETER_AU` after 8 s | `messages: 4` (3 seeds + `s6.zadark.au.1`) — **the timed-out write was not stored** |
| acked publish, `ODOMETER_AU` | `Sequence: 5` |
| meta leader after the thaw | `t-za-2`, term `7` — **moved out of the hub** |

### What this shows, and what it does not

- **S6 holds, both rounds:** with the meta group healthy and its leader in the
  hub, the dark region's stream took no acked write. The other two streams
  and metadata worked. **Meta leader location is not data availability.**
- **The failure is a timeout, not a refusal.** The CLI prints "Published"
  first: that means only that the connected server took the message.
- **In this hand run, no timed-out write was stored later** — three writes,
  after a freeze of about 30 s. **The script's own-rig run below stored one.**
  So a timeout is **unknown**, not "not stored".
- **Both returns moved the meta leader out of the hub** (term +2 each). Same
  pattern as Step 3. Not S6's question; recorded only. Cause not proved.

### Repeated by the script, 14:39–14:44 SAST

`lab/10-hub-meta-leader.sh step6` (`ML137`–`ML158`), twice: attached to the
hand-run rig, then on its own rig. Rows in `lab/run/10-step6.tsv`.

| Run | Round | Meta leader while dark | Write to dark stream | S6 | Unacked write stored after the thaw? | Meta leader on return |
|---|---|---|---|---|---|---|
| attached | za | `t-arb-3`, held | timeout, 5.03 s | passed | no | → `t-au-3` (term +2) |
| attached | au | `t-arb-1`, held | timeout, 5.03 s | passed | no | → `t-arb-2` (term +2) |
| own rig | za | `t-arb-3`, held | timeout, 5.02 s | passed | **yes** | → `t-arb-1` (term +2) |
| own rig | au | `t-arb-1`, held | timeout, 5.09 s | passed | no | `t-arb-1` (term +2) |

Attached: 20 passed, 0 failed. Own rig: 43 passed, 0 failed (with step1).

- **S6 holds, 6 of 6 rounds** (hand and script): only the dark region's
  stream stopped. The meta leader stayed in the hub every time.
- **A write that timed out WAS stored after the thaw, once** (own rig, `za`
  round, `ML146a`: 4 held, 1 beyond the acked). `lab/run/ledger-za` line 4,
  read back by Direct Get: seq 4 = `s6.zadark.za.1`, payload `km 7001` — the
  exact write the CLI reported as `nats: timeout`. In the other 7 tries it was
  not stored. **Why it landed in that one try is not proved.** One idea is
  that the bytes waited in the frozen server's socket, but nothing here shows
  that. Do not write it down as the cause.
- **What it means for a client:** a JetStream publish timeout is an
  **unknown** outcome, not a failure. Retry with the same `Nats-Msg-Id`
  inside the duplicate window (2 min here), or read the stream to find out.
- **Every return raised the term by 2**, and in 3 of 4 the meta leader moved.
  In the fourth the same server came back. Same pattern as Step 3. Recorded
  only; not S6's question.

---

## 2026-10-05 14:47–15:20 SAST — Exercise 10: full runs, steps 1–6 (script)

**Versions:** `nats-server v2.14.6`, `nats` CLI `0.4.0`, macOS (Darwin 25.4.0).
**Who ran it:** Claude, with `exercises/ex10-check.sh` (all steps, own rig).
**Report:** `REPORT-10.md` and `REPORT-10.html`, generated by
`lab/render-report-10.py` from the four kept runs below. The cited run is
`10-20261005-150304`. Every failed run is kept; none was re-run to get green.

| Run | Rig checks | Procedure checks | Kept as |
|---|---|---|---|
| `10-20261005-144730` | 58 passed, 1 failed (`ML42`) | 85 met, 5 not met | kept by hand after the run; `log/arb-1.log` gap |
| `10-20261005-150304` | 59 passed, 0 failed | 84 met, 6 not met | **cited** |
| `10-20261005-150839` | 58 passed, 1 failed (`ML42`) | 87 met, 3 not met | kept; `ML50`, `ML58` inconclusive |
| `10-20261005-151454` | 59 passed, 0 failed | 84 met, 6 not met | kept; first run with `WHOLE_N` |

### 14:47 — `ML42` read `arb 8 8`, wanted `arb 9 8`

- `ML42` is a rig precondition of the za round in step 3. One hub monitor
  did not count as answering.
- **Cause, as far as the log shows:** at 14:47:53, about 1 s after the step-2
  restart, t-arb-1's `/healthz` failed with `stream 'LB > KV_t7-vehicles' is
  not current`. Step 3 read its start check then.
- **Gap:** `log/arb-1.log` starts 14:47:52.55. The step-2 restart truncated
  it, so t-arb-1's first 22 s are lost. `rig-t4.sh` now keeps the old log as
  `.before-restart.log`.
- **Fix 1:** `wait_whole` — a round's start check now waits until all nine
  `/healthz` pass.
- **Consequence:** `ML58` in that run is **inconclusive**, not failed. The
  script of that time printed "failed". The renderer applies the
  precondition rule to old runs too (`PRE` in `render-report-10.py`).
- Its step-5 return is valid and moved the leader (`ML132`, `ML132b`). That
  is why the step-5 line above now says 4 of 5.

### 15:03 — `10-20261005-150304`, clean

- 0 rig failures. Verdicts `ML58`, `ML75`, `ML110` **failed**: the leader
  moved on return (`ML54`, `ML71`, `ML106`).
- S6 au: the same leader `t-arb-2` came back in a higher term (`ML157a`).
- This is the cited run: figures 2 and 3 and card 14 draw from it.

### 15:08 — `ML42` again, `arb 8 8`

- **Proved from `log/arb-1.log`:** t-arb-1's `/healthz` passed after its
  restart (15:08:50.7), failed once at 15:08:51.79 (`KV_t7-vehicles is not
  current`), then passed again. `wait_whole` saw the first pass; `ML42` read
  the failure.
- **Fix 2:** `WHOLE_N=3` — `wait_whole` now asks for three all-nine readings
  in a row. The check itself is unchanged.
- The script marked `ML50` and `ML58` **inconclusive** itself. Correct.

### 15:14 — `10-20261005-151454`, clean, after fix 2

- 0 rig failures. `ML42` passed. One run is not proof the flap is gone, but
  the rig did not stop a measurement here.
- Verdicts `ML58`, `ML75`, `ML92` **failed**: the leader moved on return
  (`ML54`, `ML71`, `ML88`). These are genuine procedure failures.

### What the four runs show, and what they do not

- **Recovery was met in every valid return round.** Stability was not:
  the leader moved, or the term rose. `REPORT-10.md` shows it round by round
  and **by scenario**, never pooled into one number. Do not quote a rate.
- **The runs span three script revisions** (r1 `144730`; r2 `150304`,
  `150839`; r3 `151454`). They differ only in the readiness wait. The
  revisions come from the session record. From 15:20, `env.txt` records
  the script's sha256 and readiness rule itself.
- **Where the leader went is mixed.** Sometimes it went to the region that
  stayed up (150304 `ML54`, `ML71`). Sometimes it went to the returning one
  (150304 `ML106`). Do not write "the returning region takes it".
- **Which rounds disturb leadership changes from run to run.** No round
  held in every run.
- **Why a return disturbs leadership is not proved.**
- **Why one timed-out write was stored is not proved.**

## 2026-10-05 16:25–17:06 SAST — Exercise 10: three outcomes per return, and a diagnostic run (script)

From here every return round has three verdicts: experiment
(`ML<n>a`), recovery (`ML<n>b`) and stability (`ML<n>c`). Step 6 records
the same three words in a note (`ML146d`, `ML157d`). `lab/classify-10.py`
makes them; `lab/test-classify-10.py` is its fixture suite.

| Run | Classifier | Diagnostic | Kept because |
|---|---|---|---|
| `10-20261005-162522` | c1 | off | first run with three outcomes; no `.thaw` files, so no interval times |
| `10-20261005-163907` | c2 for steps 3–5, c3 for step 6 | off | the classifier was edited during the run; each round names its own revision |
| `10-20261005-164912` | c4 | on (`LAB_DEBUG=1`) | the Raft debug log around each thaw, in `elections.txt` |
| `10-20261005-170022` | c4 | off | **the cited run** in `REPORT-10.md`: one classifier revision, no debug logging, no rig check failed |

Older runs keep their recorded verdicts. `REPORT-10.md` shows each beside
the reassessment of the same readings with the current classifier (c5),
labelled with the original run, script and classifier. No reading was
added to an old run. c5 changed no kept verdict.

All seven recovery windows of each run are in `REPORT-10.md`: the five S4
returns, and the two step 6 returns (`ML146`, `ML157`). Step 6's recovery
word is meta recovery only; the stream's own recovery is `ML147` and
`ML158`. The four runs before `162522` did not classify step 6. Their two
step 6 returns show as "not assessed", with the reason, and are not
counted.

- **Classifier changes, each from a misread found in these runs.** c2: a
  group no-leader poll means no server says LEADER. c3: a stale leader
  field on one server is that server's view, not a conflict. c4: a poll
  that straddles the thaw is partial — single-server views only, never a
  group interval or a gap. c5 (after these runs): a straddling poll is
  still checked for two LEADERs in one term and for another leader in the
  baseline term; a live server's no leader after the thaw, with no LEADER
  in that poll, keeps the round from stable; a window with no complete
  poll before the final reading is unknown. Each has a fixture (`04h`–`04o`
  for c5).
- **Do not edit `classify-10.py` during a run.** It is run fresh for each
  round, so an edit changes the rules part way through. That is what
  happened in `163907`.
- **Recovery was met in every valid return round of these runs.** Stability
  was disturbed in some rounds and held in others. `REPORT-10.md` has them
  round by round.

### The diagnostic run, `10-20261005-164912`

All nine servers ran with `-D`. `elections.txt` keeps every `_meta_` Raft
line from just before each thaw to the watcher's last reading. Not cited:
debug logging changes the timing.

What the log shows, in this run:

- **In every return,** the returning servers logged `Switching to
  candidate` soon after the first SIGCONT.
- **In the five disturbed returns** (`ML51`, `ML68`, `ML85`, `ML146`,
  `ML157`), a returning server sent a vote request with a term above the
  incumbent's. The incumbent's own log has that request on the Raft line
  just before `Stepping down from leader, detected higher term`, at the
  same term. The next three Raft lines are `Stepping down`, `Switching to
  follower` and `Canceling catchup subscription`. `REPORT-10.md` quotes
  both microsecond times per return. This matched sequence is the
  evidence. It is the order `processVoteRequest` writes in nats-server
  v2.14.6, the version the rig runs:
  [raft.go L5291-L5323](https://github.com/nats-io/nats-server/blob/1aa10f9fe4e7a27b7d877af004a9c0022fdc4910/server/raft.go#L5291-L5323)
  (tag `v2.14.6`, commit `1aa10f9`). The request is logged at L5297, the
  step-down at L5315-L5317, then `stepdownLocked` (L1856-L1859),
  `switchToFollowerLocked` (L5497) and `cancelCatchup` (L3989-L3990,
  called at L5319). No other line in that file writes `Stepping down from
  leader`. The other higher-term step-down, on an AppendEntry response,
  writes `Detected another leader with higher term` (L4745-L4754). No log
  of this run holds it. That fits, but it proves nothing by itself. A
  later election picked the leader. Sometimes it was a live server,
  sometimes a returning one.
- **In the two stable returns** (`ML103`, `ML129`), returning servers also
  became candidates, but no candidate's term was above the incumbent's.
  No incumbent stepped down.
- **In `ML92c`,** `t-za-2` had already accepted the leader, then campaigned
  later and won. The log does not say why.

What the log does not show:

- **Why the returning servers became candidates.** No line names an
  election timer. The overdue-timer idea stays a **hypothesis**.
- **Why a vote was refused.** The line says `granted:false` only.
- **That a normal run does the same.** Debug logging slows every server.

So: in this run, a returning server's higher-term vote request made the
incumbent step down, then an election followed. The log does not show why
the returning server asked, and no normal run confirms the sequence. The
finding says exactly this.

---

## 2026-10-06 09:55–10:08 SAST — T4 playground, step 5 live check (agent run, playground session)

**This is a playground session, not exercise 10 evidence** (`D03-R28`,
`CLAUDE.md` "The playground"). It checks that the control service
(`playground/`, `PLAYGROUND-PLAN.md` order of work step 5) does what it
says on the live rig. It carries no `ML` IDs, nothing here is cited, and
nothing here may be promoted to a finding. Session file:
`playground/.run/sessions/20261006-095546.jsonl` (gitignored). Service
built from `74f39e7` plus uncommitted step 5 work. `nats-server 2.14.6`.

Every action was one `curl` POST to `127.0.0.1:20302`, with
`Origin: http://127.0.0.1:20301`. The demo 05 server (`4522`) was running
and was not touched.

| # | Action | Result the service reported |
|---|---|---|
| 1 | Start rig | `rig-t4.sh up`, 12 s, nine verified. Leader `t-za-2`, term 2, 9 fresh readings. `ODOMETER_ZA/ARB/AU` created: file, R3, placed per site, `allow_direct: true` (checked with `nats stream info`) |
| 2 | Freeze `au` | 3 of 3 confirmed stopped after 0.2 s; `ps` read `T` for all three. Summary stayed `t-za-2`, term 2, 6 fresh readings |
| 3 | Resume `au` | 3 of 3 running after 0.2 s. Transition: term +1, leader moved to `t-au-1`, agreed after 0.9 s |
| — | CLI check (by hand, not the service) | `nats server raft step-down --cluster arb --trace` sent `$JS.API.META.LEADER.STEPDOWN` with `{"placement":{"cluster":"arb"}}` — the subject and body the service sends. Leader `t-arb-1`, term 4 |
| 4 | Request leadership here, `za` | "Requested za. Observed t-za-2 in za, term 5." |
| 5 | Request leadership here, `arb` | "Requested arb. Observed t-arb-3 in arb, term 6." |
| 6 | Publish `za` via `za`, 2 s | `…-za-1` acked at seq 1 via `t-za-1` |
| 7 | Freeze `za` | 3 of 3 stopped after 0.2 s |
| 8 | Publish `za` via `arb`, 2 s | `…-za-2` **timed out after 2 s — outcome unknown** |
| 9 | Resume `za` | 3 of 3 running after 0.1 s. Transition: term +1, leader moved `t-arb-3` → `t-za-1`, agreed after 0.7 s |
| 10 | Retry same ID `…-za-2` via `arb`, 5 s | **acked as duplicate (seq 2)**: JetStream already held this ID |
| 11 | Publish `za` via `au`, 5 s | `…-za-3` acked at seq 3 via `t-au-1` |
| 12 | Verify `za` via `arb` | 3 present, 0 absent, last seq 3. The ledger keeps both attempts of `…-za-2` |
| 13 | Probe via `arb` | `PG_PROBE_1`: create ok, delete ok, gone ok |
| 14–15 | Freeze `au`, then probe via `za` | `PG_PROBE_2`: create ok, delete ok, gone ok, with `au` dark |
| 16 | Restore all | resumed `au`, 3 of 3 running after 0.2 s. Transition: term +2, leader moved to `t-arb-1`, agreed after 1.0 s |
| 17 | Stop | "rig stopped: nine processes ended" in 0.8 s. No `t-` server left |

After the session: nothing under `lab/run/evidence/`, `lab/run/obs/` or
`lab/run/results.tsv` was newer than the session start. Ctrl-C equivalent
(SIGTERM to the service) with no rig logged "no ready rig: nothing to
release".

What this shows about the **service**, and nothing more:

- Each command ended in a result or a timeout. None stayed pending.
- A publish timeout was reported as "outcome unknown", and only a later
  retry and readback said what happened.
- "Request leadership here" reported the leader it then observed.

Seen in passing, **not a finding** (one session, no checks): all three
returns (rows 3, 9, 16) moved the leader and raised the term. The
timed-out publish in row 8 was stored after `za` came back. Both match
what exercise 10 already reports; neither adds to it.

Not checked in this session: **Attach**, and the service's shutdown
release with a ready rig (owned: CONT then TERM; attached: CONT only to
what it stopped). The specs cover both with fakes.
