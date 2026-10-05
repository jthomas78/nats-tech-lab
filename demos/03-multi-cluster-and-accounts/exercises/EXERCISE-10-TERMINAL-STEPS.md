# Exercise 10 — can the meta leader live in the hub? Step by step

Part of [`HUB-META-LEADER-PLAN.md`](../HUB-META-LEADER-PLAN.md). The exercise
takes its number from its lab script, `lab/10-hub-meta-leader.sh`. Part
`stepN` of that script runs the checks for Step N here.

Each step starts from the state the step before it leaves. Each one was
written, run by hand and automated before the next one was written.

| Step | Plan test | Question | Requirement |
|---|---|---|---|
| 1 | — | Set up: is the rig whole, with the fixtures built? | `D03-R13` (baseline) |
| 2 | S1 | Can a hub step-down put the leader in the hub, every time? | `D03-R13` |
| 3 | S2 + S4 | One region dark, leader in the hub: do writes and metadata still work? Then recovery. | `D03-R14`, `D03-R16` |
| 4 | S3 + S4 | One region dark, leader in that region: who wins, and how fast? Then recovery, then a hub step-down. | `D03-R15`, `D03-R16` |
| 5 | S5 | Both regions dark: does the hub lose quorum although it is healthy? | `D03-R17` |
| 6 | S6 | One region dark: does only the stream placed there stop? | `D03-R18` |

One terminal, in `demos/03-multi-cluster-and-accounts`.

The rig is **T4**: three clusters of three servers — `za`, `au` and `arb` —
joined by gateways only. One meta group of nine voters. Quorum is 5. In this
experiment the `arb` cluster plays the **hub**. Every command uses the name
`arb`; "hub" is only its role.

Every `nats` command passes `--no-context`, `--server`, `--user` and
`--password`, so the CLI cannot pick up another demo's saved settings.
Passwords equal user names. This is lab only, on `127.0.0.1` only.

A "Predict" line is a prediction until you have run the step. Measured results
are in [`EXERCISE_OBSERVATIONS.md`](EXERCISE_OBSERVATIONS.md).

| Server | Client | Monitor |
|---|---|---|
| za-1..3 | 4231–4233 | 8231–8233 |
| au-1..3 | 4241–4243 | 8241–8243 |
| arb-1..3 (hub) | 4541–4543 | 8541–8543 |

---

## Step 1: Set up the rig and the fixtures

| | |
|---|---|
| Checks | `./exercises/ex10-check.sh step1` |
| Starting state | no `t-` server running; `run-all.sh` not running |
| Leaves behind | the rig up, four fixtures seeded, a meta leader somewhere (any cluster) |

### Step 1.1: Check that no rig is running

**Concept:** the rig shares ports with every script in `lab/`. Two at once
break each other.

```bash
pgrep -fl "nats-server -c t-"
```

**Purpose:** list any `t-` server already running.
**Predict:** no output. If there is output, run `./lab/rig-t4.sh down` and
check again. A server from another demo (for example demo 05 on 4522) is not
a `t-` server and is not listed.

### Step 1.2: Start the rig

**Concept:** the nine server configs are plain files in `exercises/config/`
(`t-za-1.conf` … `t-arb-3.conf`, plus `accounts.conf`). Read one first. Change
one if a step asks you to. `up` copies them into `lab/run/`, starts nine
servers from there, waits for a meta leader, and **leaves them running**.

```bash
cat exercises/config/t-arb-1.conf
```

```bash
./lab/rig-t4.sh up
```

For each server, `up` runs this from inside `lab/run/` (shown for `t-za-1`). It
starts the server in the background and saves its PID:

```text
cp exercises/config/t-za-1.conf exercises/config/accounts.conf lab/run/
cd lab/run
nats-server -c t-za-1.conf > log/za-1.log 2>&1 &
echo $! > pid/za-1.pid
```

Do not type this. It is shown so you know what `up` does. The command must
start with `nats-server -c t-`, because `down` finds the servers by that
pattern. The server runs from `lab/run/`, so its store (`./js/za-1`) lands in
`lab/run/js/`, which git ignores. Then `up` waits until all nine monitors
answer and there is a meta leader, and it checks that the meta group size is 9.

**Purpose:** a running nine-server rig.
**Predict:** `rig up: 9 servers`, then a table with all nine `up`, meta size
`9` everywhere, and one leader name that every server agrees on. Which cluster
will the leader be in? (Nothing pins it. Write your guess down.)

### Step 1.3: Read the meta group from the hub

**Concept:** the meta group is the answer. The logs are not.

```bash
curl -s "localhost:8541/jsz?meta=1" | jq '{leader:.meta_cluster.leader, size:.meta_cluster.cluster_size}'
```

**Purpose:** see who leads, from a hub server's point of view.
**Predict:** `size: 9`, and the same leader as Step 1.2. Quote the `?` — zsh
globs it.

### Step 1.4: Read every peer from the leader's point of view

**Concept:** the leader is not in its own replica list. Only the leader's
monitor shows **every other** peer, and whether each one is current.

```bash
./lab/rig-t4.sh status
```

**Purpose:** the full picture: who answers, who leads, who is current.
**Predict:** eight peers under the leader, all `current=true`. `active` is the
time since the leader last heard from that peer, in **nanoseconds**.

### Step 1.5: Create the hub stream

**Concept:** `--cluster arb` places all three copies in the hub. Its own Raft
group then needs only hub servers for quorum.

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  stream add ODOMETER_ARB --subjects evt.odo.arb.v1 --storage file \
  --replicas 3 --cluster arb --defaults
```

**Purpose:** the stream that should keep working whatever the regions do.
**Predict:** `Placement Cluster: arb`, a leader and two replicas, all
`t-arb-*`, and `Direct Get: true`.

### Step 1.6: Create one stream in each region

**Concept:** the same shape, placed in `za` and in `au`. Each one loses its
quorum when its region goes dark.

```bash
nats --no-context --server nats://127.0.0.1:4231 --user lb --password lb \
  stream add ODOMETER_ZA --subjects evt.odo.za.v1 --storage file \
  --replicas 3 --cluster za --defaults
```

```bash
nats --no-context --server nats://127.0.0.1:4241 --user lb --password lb \
  stream add ODOMETER_AU --subjects evt.odo.au.v1 --storage file \
  --replicas 3 --cluster au --defaults
```

**Purpose:** one stream per region, for Step 6.
**Predict:** `Placement Cluster: za` with only `t-za-*` servers, then the
same for `au`. A replica may show `outdated, not seen` for a moment straight
after creation.

### Step 1.7: Create the KV bucket in the hub

**Concept:** a KV bucket is really a stream (`KV_t7-vehicles`), so it is placed
the same way.

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  kv add t7-vehicles --storage file --replicas 3 --cluster arb
```

**Purpose:** a second kind of data in the hub.
**Predict:** `JetStream Stream: KV_t7-vehicles`, cluster `arb`, all three
servers `t-arb-*`.

### Step 1.8: Seed each stream with acknowledged messages

**Concept:** a plain `nats pub` that returns success proves nothing. `-J` waits
for the stream's acknowledgement and prints the stored sequence. The
`Nats-Msg-Id` header makes a retry safe and lets you check identity later.

```bash
for site in arb za au; do
  case $site in arb) port=4541;; za) port=4231;; au) port=4241;; esac
  for n in 1 2 3; do
    nats --no-context --server nats://127.0.0.1:$port --user lb --password lb \
      pub -J --no-templates -H "Nats-Msg-Id:seed.$site.$n" "evt.odo.$site.v1" \
      "{\"id\":\"seed.$site.$n\",\"vehicle\":\"v-$n\",\"km\":$((1000+n))}"
  done
done
```

**Purpose:** three known messages in each stream.
**Predict:** nine pairs of lines. Each pair ends `Stored in Stream: ODOMETER_<SITE>
Sequence: <n>`, with `n` = 1, 2, 3 for each stream. Write down every stream
and sequence. **That list is the seed.**

### Step 1.9: Read the seed back by Direct Get

**Concept:** Direct Get is answered by the stream's own replicas. It does not
need a meta leader, so it still works in Step 5. It returns a message, not a
count.

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  req --timeout 3s '$JS.API.DIRECT.GET.ODOMETER_ARB' '{"seq":1}'
```

Repeat for `seq` 2 and 3, and for `ODOMETER_ZA` (port 4231) and
`ODOMETER_AU` (port 4241).

**Purpose:** prove each seeded message is stored, with the right identity.
**Predict:** `Nats-Sequence: 1`, `Nats-Msg-Id: seed.arb.1`, and the payload
from Step 1.8. Then try `{"seq":4}`. What comes back for a sequence that does
not exist?

### Step 1.10: Seed and read the KV bucket

**Concept:** a KV put is a stream write; a KV get is a read.

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  kv put t7-vehicles vehicle-1 seed-k1-v1
```

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  kv get t7-vehicles vehicle-1
```

**Purpose:** one known key, for the recovery checks.
**Predict:** the put prints the value. The get shows `vehicle-1`, revision 1,
`seed-k1-v1`.

### Step 1.11: The metadata probe — create a stream, then delete it

**Concept:** writing to a stream that exists does not prove the meta group
works. **Creating** a stream does: only the meta leader can place it. This is
the probe every later step repeats.

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  --timeout 10s stream add PROBE_META --subjects probe.meta --storage memory \
  --replicas 1 --cluster arb --defaults
```

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  stream rm PROBE_META -f
```

**Purpose:** a baseline. With all nine servers up, metadata changes work.
**Predict:** `Stream PROBE_META was created`, then the delete prints nothing.

### Step 1.12: Record, then keep the rig or stop it

**Concept:** always leave a known state.

```bash
./lab/rig-t4.sh status
```

**Purpose:** record the starting state for Step 2: the leader's name and
cluster, and that all eight peers are current.
**Predict:** the same as Step 1.4.

- To go on to Step 2, **leave the rig running.**
- To stop:

```bash
./lab/rig-t4.sh down
```

**Predict:** `rig down`. Running it again prints `nothing running`.

---

## Step 2: Placement trials — can a step-down put the leader in the hub?

Plan test S1, requirement `D03-R13`.

| | |
|---|---|
| Checks | `./exercises/ex10-check.sh step2` — `ML24`–`ML41`. With no rig up it runs step1 first, then step2, then stops the rig. On a running rig it needs Step 1's fixtures and refuses without them |
| Starting state | Step 1 done: rig up, fixtures seeded, all nine monitors answer, nothing frozen. The leader can be in any cluster |
| Leaves behind | the rig up, the leader in the hub (`t-arb-*`), every server running |

**Prediction, written before the run:** every `step-down --cluster arb` puts
the leader on a `t-arb-*` server. A hub server that restarts does not move
the leader.

**Pass rule:** the leader is a `t-arb-*` server after every trial, and all
nine servers name the same one. A trial that lands outside the hub is **the
finding**. Record it. Do not call it a rig failure.

### Step 2.1: Check the starting state

**Concept:** a trial means nothing if a server is down or frozen.

```bash
./lab/rig-t4.sh status
```

**Purpose:** all nine `up`, meta size `9`, one leader, eight peers current.
**Predict:** that. Write down the leader. Which cluster is it in?

### Step 2.2: Move the leader into a region

**Concept:** a trial must start with the leader **outside** the hub. If it
starts in the hub, a hub step-down only moves it between hub servers, which
does not test "into the hub". `step-down` needs the system user (`admin`).

```bash
nats --no-context --server nats://127.0.0.1:4541 --user admin --password admin \
  server cluster step-down --cluster za
```

```bash
curl -s "localhost:8541/jsz?meta=1" | jq '{leader:.meta_cluster.leader, size:.meta_cluster.cluster_size}'
```

**Purpose:** a confirmed regional leader.
**Predict:** the CLI prints `Requesting leader step down of "<old>"`, then
`New leader elected "t-za-<n>"`. `/jsz` names the same `t-za-*` server.
Trust `/jsz`, not the CLI line. A success reply does not prove the leader
moved.

### Step 2.3: Ask for the leader in the hub

**Concept:** `--cluster arb` asks for a new leader in the hub. It is a
request, not a rule. The server may choose another healthy peer.

```bash
nats --no-context --server nats://127.0.0.1:4541 --user admin --password admin \
  server cluster step-down --cluster arb
```

Then ask all nine servers who leads:

```bash
for p in 8231 8232 8233 8241 8242 8243 8541 8542 8543; do
  curl -s --max-time 1 "localhost:$p/jsz?meta=1" | jq -r .meta_cluster.leader
done | sort | uniq -c
```

**Purpose:** trial 1.
**Predict:** one line: `9 t-arb-<n>`. Two lines means the servers do not
agree yet. Wait 5 s and ask again.

### Step 2.4: Repeat — five trials or more

**Concept:** one trial proves nothing. The plan asks for five or more. This
loop runs five more trials and swaps the starting region each time (`au`,
`za`, `au`, …).

```bash
for t in 2 3 4 5 6; do
  if [ $((t % 2)) -eq 0 ]; then r=au; else r=za; fi
  nats --no-context --server nats://127.0.0.1:4541 --user admin --password admin \
    server cluster step-down --cluster $r | tail -1
  sleep 2
  start=$(curl -s "localhost:8541/jsz?meta=1" | jq -r .meta_cluster.leader)
  nats --no-context --server nats://127.0.0.1:4541 --user admin --password admin \
    server cluster step-down --cluster arb | tail -1
  sleep 2
  echo "trial $t: started on $start; all nine say:"
  for p in 8231 8232 8233 8241 8242 8243 8541 8542 8543; do
    curl -s --max-time 1 "localhost:$p/jsz?meta=1" | jq -r .meta_cluster.leader
  done | sort | uniq -c
done
```

**Purpose:** six trials in all, each one from a regional leader.
**Predict:** for every trial, `started on t-za-*` or `t-au-*`, then one line
`9 t-arb-<n>`. Write a table: trial, start leader, leader after.

### Step 2.5: Step down while the leader is already in the hub

**Concept:** this is the case Step 2.2 avoids. It shows what a hub step-down
does when the leader is already in the hub.

```bash
nats --no-context --server nats://127.0.0.1:4541 --user admin --password admin \
  server cluster step-down --cluster arb
```

**Purpose:** see the move between hub servers.
**Predict:** the leader moves to a **different** `t-arb-*` server. This is
not a trial. Do not count it in Step 2.4's table.

### Step 2.6: Restart one hub server that is not the leader

**Concept:** NATS has no election priority. A hub server that comes back
does not take the lead. `restart` stops one server cleanly (TERM) and starts
it again from the same config and the same store.

```bash
curl -s "localhost:8541/jsz?meta=1" | jq -r .meta_cluster.leader
```

Pick a hub server that is **not** that leader. For example, if the leader is
`t-arb-3`, restart `arb-1`:

```bash
./lab/rig-t4.sh restart arb-1
```

```bash
./lab/rig-t4.sh status
```

**Purpose:** prove that a hub restart does not move the meta leader.
**Predict:** `t-arb-1 restarted: PID <old> -> <new>`. Then the same meta
leader as before, and eight peers current again.

Then check the hub stream:

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  stream info ODOMETER_ARB -j | jq '{msgs:.state.messages, leader:.cluster.leader, since:.cluster.leader_since, replicas:[.cluster.replicas[]|{name,current}]}'
```

**Predict:** `msgs: 3`, and every replica `current: true`. The **stream's**
leader may have changed if the restarted server led it. That is the stream's
own Raft group, not the meta group.

### Step 2.7: Record, then keep the rig or stop it

```bash
./lab/rig-t4.sh status
```

**Purpose:** the starting state for Step 3: the leader is in the hub, all
eight peers current.
**Predict:** a `t-arb-*` leader.

- To go on to Step 3, **leave the rig running.**
- To stop: `./lab/rig-t4.sh down`.

---

## Before every round of Steps 3–6: wait until the rig is whole

**Concept:** a server that is up can still be catching up. While a stream
replica on it is not current, its `/healthz` fails. Right after a restart or
a thaw, `/healthz` can pass, fail once, then pass again. This was measured
twice in full runs: 14:47 and 15:08 on 2026-10-05, both times on `t-arb-1`
with `KV_t7-vehicles is not current`. A round that starts during that flap
measures the last round's tail, not its own start.

So, before you read a round's starting state, wait until **all nine**
`/healthz` pass **three times in a row**, one second apart, within 60 s.
This is the same rule the script uses (`wait_whole`, `WHOLE_N=3`,
`RECOVERY_S=60`), so a hand run and a script run start from the same state.

```bash
ok=0; for i in $(seq 60); do n=0; for p in 8231 8232 8233 8241 8242 8243 8541 8542 8543; do curl -fs --max-time 1 "localhost:$p/healthz" >/dev/null && n=$((n+1)); done; if [ "$n" = 9 ]; then ok=$((ok+1)); else ok=0; fi; echo "$i: $n of 9, $ok in a row"; [ "$ok" -ge 3 ] && break; sleep 1; done
```

**Purpose:** a starting state that holds, not one lucky reading.
**Predict:** the last line ends `9 of 9, 3 in a row`. If 60 lines print and
it never gets there, **stop**: the rig is not whole, and the round would not
measure what it says. Find out which monitor fails before you go on. The
script records this case as a failed rig check (`ML42` and its
counterparts), and the verdict that depends on it as **inconclusive**.

One reading of 9 is not enough. In run `10-20261005-150839` the old rule
(one reading) saw 9, and the start check one second later read 8.

## Step 3: One region dark, leader in the hub — then recovery

Plan tests S2 and S4, requirements `D03-R14` and `D03-R16`.

| | |
|---|---|
| Checks | `./exercises/ex10-check.sh step3` — `ML42`–`ML75`. With no rig up it runs step1 first, then step3, then stops the rig. On a running rig it needs the state Step 1 leaves (3 messages in each stream) and refuses otherwise |
| Starting state | Step 1 done (Step 2 is optional). Nothing frozen. The leader in the hub — Step 3.1 puts it there if it is not |
| Leaves behind | the rig up, nothing frozen, more messages in each stream. The leader can be in any cluster |

**Prediction, written before the run:**

- With one region dark, 6 of 9 voters remain. Quorum is 5, so the hub leader
  stays. Writes to the two surviving streams go on. A new stream can still be
  created.
- When the region returns, the leader stays where it is. The returned peers
  are current again within 60 s. Every message reads back.

**Pass rule:** every probe while dark succeeds, and the leader does not
change. After the thaw: one live leader that all nine name, the **same** one
as before, the **meta term unchanged during recovery** (read just before the
thaw, and again at the end of the settle window; both must be numbers),
eight peers current, every message back by Direct Get, a write and a metadata
probe succeed. A leader that moves, or a new term with the same server back,
is **the finding**. Record it. The script reports three outcomes for each
return, and never merges them:

- **experiment** — valid, invalid or inconclusive: were the processes
  stopped, then resumed, did the start check pass, and was there one stable
  leader just before the thaw?
- **recovery** — recovered, not recovered or inconclusive: servers, peers,
  one agreed leader, every message, a write and a metadata probe, within
  60 s.
- **stability** — stable, disturbed or unknown: the same leader in the same
  term, from just before the thaw to the end of the settle window. This is
  the stability requirement. It is kept apart from recovery, and it is not
  weakened.

So "recovered; leadership disturbed" is a valid result, not a failed
recovery. Missing or broken readings give "inconclusive" or "unknown",
never a pass. (The term rule was added 2026-10-05: one return put the same
server back at term +2, so "same leader" alone does not show the term held.)

What the kept runs show: **During some regional recoveries, the metadata
group entered a new election term and sometimes elected a different leader.
In one diagnostic run, logs showed returning servers requesting votes with
higher terms, causing the incumbent leader to step down. Why those servers
initiated elections remains unproved, and this sequence has not been
confirmed in normal runs.**

Two rounds: Round 1 freezes `za` (Steps 3.1–3.7). Round 2 freezes `au`
(Step 3.8).

### Step 3.1: Put the leader in the hub

**Concept:** this step asks what happens when the leader is **already** in
the hub. So make sure it is.

```bash
nats --no-context --server nats://127.0.0.1:4541 --user admin --password admin \
  server cluster step-down --cluster arb
```

```bash
./lab/rig-t4.sh status
```

**Purpose:** a confirmed hub leader, all nine up, eight peers current.
**Predict:** `New leader elected "t-arb-<n>"`, and `status` shows that leader on
all nine. Write it down.

Then run the readiness wait ("Before every round", above). Go on only at
`9 of 9, 3 in a row`.

Then read the meta group's **term**. The term goes up on every election
**attempt**, won or not (Raft paper, sections 5.1–5.2). So a higher term shows
the group went through at least one new term, even when the same server is
leader again. It does **not** count successful elections: +2 is not "two
elections".

```bash
curl -s "localhost:8541/raftz?group=_meta_" | jq '.["$SYS"]._meta_ | {term, state}'
```

**Predict:** a small number. Write it down.

### Step 3.2: Freeze the za region

**Concept:** `freeze` sends `kill -STOP` (SIGSTOP) to all three `za` servers.
They stop dead, timers included. This is **not** a real network partition:
in a partition both sides keep running. It is not a restart either. Then
`freeze` reads each monitor until it stops answering. A freeze that did
nothing would read like a finding.

```bash
./lab/rig-t4.sh freeze za
```

```bash
./lab/rig-t4.sh status
```

**Purpose:** `za` dark, proved.
**Predict:** `frozen: za-1 za-2 za-3 -- monitors 8231 8232 8233 do not answer`.
`status` shows the three `za` rows `dark`. The other six name the **same**
leader as in Step 3.1. The `t-za-*` peers show `current=false` only after
about 10 s, but their `active` is large at once.

### Step 3.3: Write to the two surviving streams

**Concept:** an acked publish proves a stored write. Read it back by Direct
Get to prove its identity.

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  pub -J --no-templates -H "Nats-Msg-Id:s3.zadark.arb.1" evt.odo.arb.v1 \
  '{"id":"s3.zadark.arb.1","vehicle":"v-1","km":2001}'
```

```bash
nats --no-context --server nats://127.0.0.1:4241 --user lb --password lb \
  pub -J --no-templates -H "Nats-Msg-Id:s3.zadark.au.1" evt.odo.au.v1 \
  '{"id":"s3.zadark.au.1","vehicle":"v-1","km":2001}'
```

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  req --timeout 3s '$JS.API.DIRECT.GET.ODOMETER_ARB' '{"seq":4}'
```

**Purpose:** prove the hub stream and the other region's stream take writes.
**Predict:** `Stored in Stream: ODOMETER_ARB Sequence: 4` and
`ODOMETER_AU Sequence: 4`. The Direct Get shows `Nats-Msg-Id: s3.zadark.arb.1`
and the same payload. Do the Direct Get for `ODOMETER_AU` on port 4241 too.

### Step 3.4: The metadata probe

**Concept:** only the meta leader can place a new stream. This proves the
meta group still works, not only the streams.

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  --timeout 10s stream add PROBE_META --subjects probe.meta --storage memory \
  --replicas 1 --cluster arb --defaults
```

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  stream rm PROBE_META -f
```

**Purpose:** metadata can still change with one region dark.
**Predict:** `Stream PROBE_META was created`, `Placement Cluster: arb`. The
delete prints nothing.

### Step 3.5: Thaw za, then read the leader again

**Concept:** recovery. `thaw` sends `kill -CONT` (SIGCONT) and waits until
each monitor answers. The servers resume where they stopped; they do not
restart.

```bash
./lab/rig-t4.sh thaw za
```

Wait 10 s, then:

```bash
./lab/rig-t4.sh status
```

```bash
curl -s "localhost:8541/raftz?group=_meta_" | jq '.["$SYS"]._meta_ | {term, state}'
```

**Purpose:** is the region back, and did the leader stay?
**Predict:** all nine `up`, all name the **same** leader as in Step 3.1,
eight peers current. The term is the same as in Step 3.1.
If the leader or the term changed, leadership was disturbed on the return.
Look for it in the logs:

```bash
grep -n "metadata leader" lab/run/log/arb-1.log
```

The normal log shows only the result, not which event started the new
term. **Hypothesis, not a finding:** the returning servers' election timers
ran out while they were stopped, so they campaign the moment they resume.
To look closer, run the whole exercise in diagnostic mode. Every server then
runs with `-D` (debug log, Raft lines included), and the kept evidence gets
an `elections.txt` with every `_meta_` Raft line around each thaw:

```bash
LAB_DEBUG=1 ./exercises/ex10-check.sh
```

Debug logging slows each server, so a diagnostic run's timings are not a
normal run's timings. Its rows are labelled and never cited.

In `elections.txt`, look for three lines in order: a returning server's
`Switching to candidate`, the incumbent's `Stepping down from leader,
detected higher term`, and the winner's `Self is new JetStream cluster
metadata leader`. They show the sequence. To match the step-down to its
cause, open the incumbent's own log in `log/` and read the Raft line just
before the step-down: it must be `Received a voteRequest` at the same
term. No line shows why the returning server became a candidate, so the
hypothesis stays a hypothesis.

### Step 3.6: Read every message back

**Concept:** recovery is proved by the data, not by a count. Every seeded
and every acked message must read back, with the right id and payload, in
order.

```bash
for s in ARB:4541 ZA:4231 AU:4241; do
  st=${s%:*}; p=${s#*:}
  last=$(nats --no-context --server nats://127.0.0.1:$p --user lb --password lb \
    stream info ODOMETER_$st -j | jq .state.last_seq)
  for q in $(seq 1 $last); do
    nats --no-context --server nats://127.0.0.1:$p --user lb --password lb \
      req --timeout 3s "\$JS.API.DIRECT.GET.ODOMETER_$st" "{\"seq\":$q}" 2>&1 \
      | grep -E "Nats-Msg-Id|^\{" | tr '\n' ' '; echo
  done
done
```

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  kv get t7-vehicles vehicle-1 --raw
```

**Purpose:** nothing was lost or changed.
**Predict:** `ODOMETER_ARB` 4 messages, `ODOMETER_ZA` 3, `ODOMETER_AU` 4. Each
line shows the id you sent and its payload. The KV prints `seed-k1-v1`.

### Step 3.7: Write and change metadata again

**Concept:** the returned region must take writes too.

```bash
nats --no-context --server nats://127.0.0.1:4231 --user lb --password lb \
  pub -J --no-templates -H "Nats-Msg-Id:s3.zaback.za.1" evt.odo.za.v1 \
  '{"id":"s3.zaback.za.1","vehicle":"v-1","km":3001}'
```

Then repeat the metadata probe from Step 3.4.

**Purpose:** the system is whole again.
**Predict:** `ODOMETER_ZA Sequence: 4`. The probe is created and deleted.

### Step 3.8: Round 2 — the same with au dark

**Concept:** one region proves nothing about the other.

Repeat Steps 3.1 to 3.7 with these changes:

- Step 3.2: `./lab/rig-t4.sh freeze au`, later `./lab/rig-t4.sh thaw au`.
- Step 3.3: write to `ODOMETER_ARB` (4541) and `ODOMETER_ZA` (4231), with ids
  `s3.audark.arb.1` and `s3.audark.za.1`.
- Step 3.7: write to `ODOMETER_AU` (4241), id `s3.auback.au.1`.

**Purpose:** Round 2.
**Predict:** the same as Round 1.

When done, keep the rig running for Step 4, or stop it with
`./lab/rig-t4.sh down`.

---

## Step 4: One region dark, leader in that region — then recovery

Plan tests S3 and S4, requirements `D03-R15` and `D03-R16`.

| | |
|---|---|
| Checks | `./exercises/ex10-check.sh step4` — `ML76`–`ML111`. With no rig up it runs step1 first, then step4, then stops the rig. On a running rig it needs Step 1's fixtures. Steps 2 and 3 may have run before it |
| Starting state | Step 1 done. Nothing frozen. Step 4.1 moves the leader |
| Leaves behind | the rig up, nothing frozen, more messages in each stream, the leader in the hub |

**Prediction, written before the run:**

- With the leader's own region dark, 6 of 9 voters remain. Quorum is 5. So
  the six elect a new leader within 30 s. **Do not name the winner in
  advance** — the plan forbids it.
- Writes to the two surviving streams go on. A new stream can be created.
- When the region returns, the new leader keeps the job. The returned peers
  are current within 60 s. Every message reads back.
- After that, a hub step-down puts the leader in the hub.

**Pass rule:** a live leader outside the dark region within 30 s, and every
probe while dark succeeds. After the thaw: the same checks as Step 3,
including the **meta term unchanged during recovery**. The
"same leader" check means the leader elected **while dark**. The hub
step-down is measured on its own, after recovery.

Two rounds: Round 1 with `za` (Steps 4.1–4.8), Round 2 with `au` (Step 4.9).

Step 3's message numbers carry on here. The **Predict** lines give the
sequences for a rig fresh from Step 1. After Step 3, each number is higher.

### Step 4.1: Put the leader in za

**Concept:** this step asks what happens when the leader **itself** goes
dark. So put it in the region you will freeze.

```bash
nats --no-context --server nats://127.0.0.1:4541 --user admin --password admin \
  server cluster step-down --cluster za
```

Then run the readiness wait ("Before every round", above). Go on only at
`9 of 9, 3 in a row`.

```bash
curl -s "localhost:8541/raftz?group=_meta_" | jq '.["$SYS"]._meta_ | {term, state}'
```

**Purpose:** a confirmed za leader, a whole rig, and the term before the
freeze.
**Predict:** `New leader elected "t-za-<n>"`. Write down the leader and the
term.

### Step 4.2: Freeze za, and note the time

**Concept:** the clock starts just before the freeze. `freeze` sleeps 3 s
and then proves the monitors are dark, so the election may finish before it
returns.

```bash
date +%H:%M:%S.%N
```

```bash
./lab/rig-t4.sh freeze za
```

**Purpose:** the leader's region dark, proved.
**Predict:** `frozen: za-1 za-2 za-3 -- monitors 8231 8232 8233 do not answer`.

### Step 4.3: Who won, and how fast?

**Concept:** read the leader from a **live** server only. A frozen monitor
hangs. The `leader` field can still name the dark leader for a while, so
wait until it names a live server.

```bash
curl -s --max-time 2 "localhost:8541/jsz?meta=1" | jq '.meta_cluster | {leader, cluster_size}'
```

```bash
grep -n "metadata leader" lab/run/log/arb-1.log
```

```bash
curl -s --max-time 2 "localhost:8541/raftz?group=_meta_" | jq '.["$SYS"]._meta_ | {term, state}'
```

**Purpose:** the new leader, its cluster, and the time it took.
**Predict:** a leader in `au` or `arb`. The last log line `new metadata
leader` gives its time. Subtract the `date` from Step 4.2. The term is one
higher. Then `./lab/rig-t4.sh status`: six `up`, all name the new leader.

### Step 4.4: Write to the two surviving streams

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  pub -J --no-templates -H "Nats-Msg-Id:s4.zadark.arb.1" evt.odo.arb.v1 \
  '{"id":"s4.zadark.arb.1","vehicle":"v-1","km":2001}'
```

```bash
nats --no-context --server nats://127.0.0.1:4241 --user lb --password lb \
  pub -J --no-templates -H "Nats-Msg-Id:s4.zadark.au.1" evt.odo.au.v1 \
  '{"id":"s4.zadark.au.1","vehicle":"v-1","km":2001}'
```

Read each one back by Direct Get, as in Step 3.3.

**Purpose:** the streams outside the dark region still take writes.
**Predict:** `ODOMETER_ARB Sequence: 4` and `ODOMETER_AU Sequence: 4`. The
Direct Get shows the id and payload you sent.

### Step 4.5: The metadata probe

Run the two commands from Step 3.4.

**Purpose:** the **new** leader can change metadata.
**Predict:** `Stream PROBE_META was created`, placement `arb`. The delete
prints nothing.

### Step 4.6: Thaw za, then read the leader again

```bash
./lab/rig-t4.sh thaw za
```

Wait 10 s, then:

```bash
./lab/rig-t4.sh status
```

```bash
curl -s "localhost:8541/raftz?group=_meta_" | jq '.["$SYS"]._meta_ | {term, state}'
```

```bash
grep -n "metadata leader\|Self is" lab/run/log/za-1.log
```

**Purpose:** does the leader elected while dark keep the job?
**Predict:** all nine `up`, all name the leader from Step 4.3, eight peers
current. The term is the same as in Step 4.3. The old leader's log (use its
own file, `za-<n>.log`) shows it rejoin as a follower.

### Step 4.7: Read every message back, then write and probe

Run the read-back loop and the KV get from Step 3.6. Then:

```bash
nats --no-context --server nats://127.0.0.1:4231 --user lb --password lb \
  pub -J --no-templates -H "Nats-Msg-Id:s4.zaback.za.1" evt.odo.za.v1 \
  '{"id":"s4.zaback.za.1","vehicle":"v-1","km":3001}'
```

Then the metadata probe from Step 3.4.

**Purpose:** nothing lost, and the returned region takes writes.
**Predict:** 4 + 3 + 4 messages, each with the id you sent. KV
`seed-k1-v1`. `ODOMETER_ZA Sequence: 4`. The probe is created and deleted.

### Step 4.8: Now ask for the hub

**Concept:** the plan measures this **after** recovery, apart from what the
cluster did on its own.

```bash
nats --no-context --server nats://127.0.0.1:4541 --user admin --password admin \
  server cluster step-down --cluster arb
```

**Purpose:** a hub step-down still works after a region loss.
**Predict:** `New leader elected "t-arb-<n>"`.

### Step 4.9: Round 2 — the same with au

Repeat Steps 4.1 to 4.8 with these changes:

- Step 4.1: `step-down --cluster au`.
- Step 4.2: `./lab/rig-t4.sh freeze au`; Step 4.6: `./lab/rig-t4.sh thaw au`.
- Step 4.3: the new leader is in `za` or `arb`.
- Step 4.4: write to `ODOMETER_ARB` (4541) and `ODOMETER_ZA` (4231), ids
  `s4.audark.arb.1` and `s4.audark.za.1`.
- Step 4.7: write to `ODOMETER_AU` (4241), id `s4.auback.au.1`.

**Purpose:** Round 2.
**Predict:** the same as Round 1.

When done, keep the rig running for Step 5, or stop it with
`./lab/rig-t4.sh down`.

## Step 5: Both regions dark — then recovery, one region at a time

Plan test S5, requirement `D03-R17`. The last return is also an S4 return,
`D03-R16`.

| | |
|---|---|
| Checks | `./exercises/ex10-check.sh step5` — `ML112`–`ML136`. With no rig up it runs step1 first, then step5, then stops the rig. On a running rig it needs Step 1's fixtures. Steps 2–4 may have run before it |
| Starting state | Step 1 done. Nothing frozen. Step 5.1 puts the leader in the hub |
| Leaves behind | the rig up, nothing frozen, more messages in each stream. The leader can be in any cluster |

**Prediction, written before the run:**

- With both regions dark, 3 of 9 voters remain. Quorum is 5. So **no**
  leader, although the hub itself is healthy. The leader field goes empty
  within the quorum-loss deadline, 90 s. (It can name the old leader for a
  while first — the stale-leader window is about 60 s.)
- Acked writes to `ODOMETER_ARB` **go on**. All three of its replicas are in
  the hub, so the stream keeps its own quorum.
- Creating a stream **fails**. There is no meta leader to accept it.
- **The point of S5: the stream keeps working while metadata is frozen.**
- Thaw one region: 6 of 9. A leader again within 60 s.
- Thaw the last region: Step 3's return checks pass.

**Pass rule:** no leader on any hub monitor within 90 s, and none for 15 s
after that. Every acked write to `ODOMETER_ARB` stored and read back by Direct
Get. The create fails. After the first thaw (the **partial recovery check**,
from the plan): six live monitors answer and three still do not; one live
leader that the six name, within 60 s; the five live peers current on the
leader's view; every message in `ODOMETER_ARB` and the returned region's
stream back by Direct Get; a write to both and a metadata probe succeed.
After the last thaw: Step 3's pass rule after the thaw.

### Step 5.1: Put the leader in the hub

```bash
nats --no-context --server nats://127.0.0.1:4541 --user admin --password admin \
  server cluster step-down --cluster arb
```

Then run the readiness wait ("Before every round", above). Go on only at
`9 of 9, 3 in a row`.

```bash
curl -s "localhost:8541/raftz?group=_meta_" | jq '.["$SYS"]._meta_ | {term, state}'
```

**Purpose:** a hub leader, a whole rig, and the term before anything goes
dark.
**Predict:** `New leader elected "t-arb-<n>"`. Write down the leader and the
term.

### Step 5.2: Freeze za — quorum still held

```bash
./lab/rig-t4.sh freeze za
```

```bash
curl -s --max-time 2 "localhost:8541/jsz?meta=1" | jq '.meta_cluster | {leader, cluster_size}'
```

**Purpose:** the first region dark. 6 of 9 voters, so the group still has
quorum. This is Step 3's state.
**Predict:** `frozen: za-1 za-2 za-3 ...`, then the leader from Step 5.1.

### Step 5.3: Freeze au, then time the loss of the leader

**Concept:** now 3 of 9. Start the clock just before the freeze. The loop
reads the hub's leader field every 0.5 s and prints the time it goes empty.
Read only hub ports (`85xx`) from here: a frozen monitor hangs.

```bash
date +%H:%M:%S.%N
```

```bash
./lab/rig-t4.sh freeze au
```

```bash
for i in $(seq 1 180); do l=$(curl -s --max-time 1 "localhost:8541/jsz?meta=1" | jq -r '.meta_cluster.leader // "NONE"'); if [ "$l" = NONE ]; then date +%H:%M:%S.%N; break; fi; sleep 0.5; done
```

```bash
grep -n "metadata leader\|Self is" lab/run/log/arb-<n>.log
```

Use the old leader's own log for `<n>`. Then read the other two hub monitors
and the term:

```bash
curl -s --max-time 2 "localhost:8542/jsz?meta=1" | jq '.meta_cluster | {leader, cluster_size}'
```

```bash
curl -s --max-time 2 "localhost:8541/raftz?group=_meta_" | jq '.["$SYS"]._meta_ | {term, state, leader}'
```

**Purpose:** how long until the hub admits it has no leader.
**Predict:** a time within 90 s of the `date`. Every hub monitor shows
`"leader": null` — but not at the same moment. One field can still name the
old leader 30 s or more after another has cleared. If 8542 or 8543 still
names a leader, wait and read it again; do not read it once. The old leader's log has a `no metadata leader` line. The
raft state is `CANDIDATE`. The term keeps going up while no one wins: each
attempt starts a new term.

### Step 5.4: Write to the hub stream

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  pub -J --no-templates -H "Nats-Msg-Id:s5.bothdark.arb.1" evt.odo.arb.v1 \
  '{"id":"s5.bothdark.arb.1","vehicle":"v-1","km":4001}'
```

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  req '$JS.API.DIRECT.GET.ODOMETER_ARB' '{"seq":4}'
```

**Purpose:** the stream works with no meta leader.
**Predict:** `Stored in Stream: ODOMETER_ARB Sequence: 4`. The Direct Get
shows `Nats-Msg-Id: s5.bothdark.arb.1` and the payload you sent.

### Step 5.5: The metadata probe — expect it to fail

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb --timeout 10s \
  stream add PROBE_META --subjects probe.meta --storage memory --replicas 1 --cluster arb --defaults
```

**Purpose:** metadata is frozen.
**Predict:** `JetStream system temporarily unavailable (10008)`, exit 1.

### Step 5.6: Thaw za — the partial recovery check

**Concept:** 6 of 9 voters again. `au` is still frozen, so Step 3's full
return checks cannot pass yet. This is the plan's smaller check.

```bash
date +%H:%M:%S.%N
```

```bash
./lab/rig-t4.sh thaw za
```

```bash
for i in $(seq 1 120); do l=$(curl -s --max-time 1 "localhost:8541/jsz?meta=1" | jq -r '.meta_cluster.leader // "NONE"'); if [ "$l" != NONE ]; then echo "$l $(date +%H:%M:%S.%N)"; break; fi; sleep 0.5; done
```

```bash
./lab/rig-t4.sh status
```

```bash
curl -s --max-time 2 "localhost:8541/raftz?group=_meta_" | jq '.["$SYS"]._meta_ | {term, state}'
```

Then read `ODOMETER_ZA` back by Direct Get (seq 1 to 3, as in Step 3.6),
write once to each live stream, and run the metadata probe:

```bash
nats --no-context --server nats://127.0.0.1:4231 --user lb --password lb \
  pub -J --no-templates -H "Nats-Msg-Id:s5.zaback.za.1" evt.odo.za.v1 \
  '{"id":"s5.zaback.za.1","vehicle":"v-1","km":5001}'
```

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  pub -J --no-templates -H "Nats-Msg-Id:s5.zaback.arb.1" evt.odo.arb.v1 \
  '{"id":"s5.zaback.arb.1","vehicle":"v-1","km":5001}'
```

Then the two commands from Step 3.4 (create, then delete `PROBE_META`).

**Purpose:** does quorum come back with six, and where does the leader land?
**Predict:** a leader within 60 s of the `date`. `status`: six `up` naming
it, three `dark`; on the leader's view the five live peers `current=true`
and the three `t-au-*` `current=false`. `ODOMETER_ZA Sequence: 4`,
`ODOMETER_ARB Sequence: 5`. The probe is created and deleted.

### Step 5.7: Thaw au — the full return

**Concept:** this is a Step 3 return. Read the term just **before** the
thaw.

```bash
curl -s --max-time 2 "localhost:8541/raftz?group=_meta_" | jq '.["$SYS"]._meta_ | {term, state}'
```

```bash
./lab/rig-t4.sh thaw au
```

Wait 10 s, then:

```bash
./lab/rig-t4.sh status
```

```bash
curl -s "localhost:8541/raftz?group=_meta_" | jq '.["$SYS"]._meta_ | {term, state}'
```

**Purpose:** does the leader from Step 5.6 keep the job?
**Predict:** all nine `up`, all name the Step 5.6 leader, eight peers
current. The term is the same as just before the thaw.

### Step 5.8: Read every message back, then write and probe

Run the read-back loop and the KV get from Step 3.6. Then:

```bash
nats --no-context --server nats://127.0.0.1:4241 --user lb --password lb \
  pub -J --no-templates -H "Nats-Msg-Id:s5.auback.au.1" evt.odo.au.v1 \
  '{"id":"s5.auback.au.1","vehicle":"v-1","km":6001}'
```

Then the metadata probe from Step 3.4.

**Purpose:** nothing lost, and the last region takes writes.
**Predict:** `ODOMETER_ARB` 5 messages, `ODOMETER_ZA` 4, `ODOMETER_AU` 3, each
with the id you sent. KV `seed-k1-v1`. `ODOMETER_AU Sequence: 4`. The probe
is created and deleted.

When done, keep the rig running for Step 6, or stop it with
`./lab/rig-t4.sh down`.

## Step 6: Regional data — one region dark, only its stream stops

Plan test S6, requirement `D03-R18`.

| | |
|---|---|
| Checks | `./exercises/ex10-check.sh step6` — `ML137`–`ML158`. With no rig up it runs step1 first, then step6, then stops the rig. On a running rig it needs Step 1's fixtures. Steps 2–5 may have run before it |
| Starting state | Step 1 done. Nothing frozen. Step 6.1 puts the leader in the hub |
| Leaves behind | the rig up, nothing frozen, more messages in each stream. The leader can be in any cluster |

**Concept:** Steps 3–5 ask about the **meta** group — who may change
metadata. This step asks about **data**. Each `ODOMETER_*` stream is its own
Raft group of three, all in one cluster. So the meta leader can be alive and
in the hub while a regional stream has no quorum at all. **Meta leader
location is not data availability.**

**Prediction, written before the run:**

- With `za` dark, the meta group keeps quorum (6 of 9) and its hub leader.
- An acked write to `ODOMETER_ZA` is refused or times out — sent through a
  live server, because `za`'s own ports are frozen.
- Writes to `ODOMETER_ARB` and `ODOMETER_AU` are stored. The metadata probe
  passes.
- The same with `au` dark.

**Pass rule:** with the region dark, the meta leader is the hub leader from
before and the six live servers agree; the write to the dark region's stream
is **not** acknowledged; three acked writes each to the two other streams
are stored and read back by Direct Get; the metadata probe passes. After the
thaw, whether the unacknowledged write was stored after all is a **note**:
it has gone both ways (see the observations). Then the dark region's stream
must take a new acked write and read back whole — a rig check, so the next
round starts whole. Where the meta leader goes on the return is a note here:
Steps 3–5 measure that.

Two rounds: Round 1 with `za` (Steps 6.1–6.5), Round 2 with `au` (Step 6.6).

### Step 6.1: Put the leader in the hub

```bash
nats --no-context --server nats://127.0.0.1:4541 --user admin --password admin \
  server cluster step-down --cluster arb
```

If the leader is already a `t-arb-*` server, this moves it to another one.
That is fine.

**Purpose:** a meta leader that the freeze cannot touch.
**Predict:** `New leader elected "t-arb-<n>"`.

Then run the readiness wait ("Before every round", above). Go on only at
`9 of 9, 3 in a row`.

### Step 6.2: Freeze za, and check the meta group

```bash
./lab/rig-t4.sh freeze za
```

```bash
curl -s --max-time 2 "localhost:8541/jsz?meta=1" | jq '.meta_cluster | {leader, cluster_size}'
```

**Purpose:** the region is dark, but the meta group is fine.
**Predict:** `frozen: za-1 za-2 za-3 ...`, then the leader from Step 6.1.

### Step 6.3: Write to the dark region's stream

**Concept:** `za`'s client ports are frozen, so send through the hub. The
gateway carries the message to `za`. Nobody there can answer. `--timeout 5s`
caps the wait.

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb --timeout 5s \
  pub -J --no-templates -H "Nats-Msg-Id:s6.zadark.za.1" evt.odo.za.v1 \
  '{"id":"s6.zadark.za.1","vehicle":"v-1","km":7001}'
```

**Purpose:** the stream inside the dark region takes no writes.
**Predict:** `Published 49 bytes`, then `nats: error: nats: timeout`, exit 1.
"Published" only means the hub took the message. No ack came back.

### Step 6.4: Write to the other two streams, and change metadata

```bash
nats --no-context --server nats://127.0.0.1:4541 --user lb --password lb \
  pub -J --no-templates -H "Nats-Msg-Id:s6.zadark.arb.1" evt.odo.arb.v1 \
  '{"id":"s6.zadark.arb.1","vehicle":"v-1","km":7001}'
```

```bash
nats --no-context --server nats://127.0.0.1:4241 --user lb --password lb \
  pub -J --no-templates -H "Nats-Msg-Id:s6.zadark.au.1" evt.odo.au.v1 \
  '{"id":"s6.zadark.au.1","vehicle":"v-1","km":7001}'
```

Then the two commands from Step 3.4 (create, then delete `PROBE_META`).

**Purpose:** everything outside the dark region works.
**Predict:** `ODOMETER_ARB Sequence: 4`, `ODOMETER_AU Sequence: 4`. The probe
is created and deleted.

### Step 6.5: Thaw za — was the refused write stored after all?

**Concept:** the hub forwarded the message to a frozen process. A frozen
process can still have the bytes waiting in its socket. So look, after the
thaw, at what the stream really holds.

```bash
./lab/rig-t4.sh thaw za
```

Wait 10 s, then:

```bash
nats --no-context --server nats://127.0.0.1:4231 --user lb --password lb \
  stream info ODOMETER_ZA --json | jq '{messages: .state.messages, last: .state.last_seq, leader: .cluster.leader}'
```

```bash
nats --no-context --server nats://127.0.0.1:4231 --user lb --password lb \
  pub -J --no-templates -H "Nats-Msg-Id:s6.zaback.za.1" evt.odo.za.v1 \
  '{"id":"s6.zaback.za.1","vehicle":"v-1","km":7101}'
```

```bash
curl -s --max-time 2 "localhost:8541/jsz?meta=1" | jq '.meta_cluster.leader'
```

**Purpose:** what the stream really holds, and does it work again?
**Predict:** `messages: 3` (the seeds only) **or** `4` — the write that timed
out can land after the thaw. Both have been seen. If it is 4, read seq 4 by
Direct Get: it is `s6.zadark.za.1`. Then the new write gets the next
sequence. Write down the meta leader: it may have moved (Step 3 saw this).

**Lesson:** a timeout is **unknown**, not "not stored". A client must
retry with the same `Nats-Msg-Id`, so the stream's duplicate window keeps it
to one copy.

### Step 6.6: Round 2 — the same with au

Repeat Steps 6.1 to 6.5 with these changes:

- Step 6.2: `./lab/rig-t4.sh freeze au`; Step 6.5: `./lab/rig-t4.sh thaw au`.
- Step 6.3: write to `evt.odo.au.v1` through 4541, id `s6.audark.au.1`.
- Step 6.4: write to `ODOMETER_ARB` (4541) and `ODOMETER_ZA` (4231), ids
  `s6.audark.arb.1` and `s6.audark.za.1`.
- Step 6.5: read `ODOMETER_AU` on 4241, then write id `s6.auback.au.1`.

**Purpose:** Round 2.
**Predict:** the same as Round 1. After Round 1, `ODOMETER_AU` holds 4
acked messages, so expect `messages: 4` (or 5, if the timed-out write
landed), then the next sequence.

When done, stop the rig with `./lab/rig-t4.sh down`.
