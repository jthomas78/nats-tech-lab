# Demo 03 — Multi-Cluster Topologies and Accounts

**One question:**

> In a two-region logistics platform, when one region goes dark,
> who is still alive to take a JetStream write?

Everything below exists to answer that, for **ZA** and **AU**.

## The role of this demo

**Validation only.** Not a showcase.

This demo produces measured evidence so a topology decision can be made. It
does **not** have a one-command setup that a stranger can run. The rig is six
hand-written config files and a host `nats` CLI, driven by hand.

If you want to see a feature work, read demo 02. If you want to pick a
topology, read this one.

> Written retroactively on **2026-09-17**. The rig was built and measured in
> September 2026, before this page existed. The evidence is not being re-run —
> it is good, and re-measuring it would be theatre.

## The slice we speak in

A truck drives kilometres. The `ODOMETER` stream holds the drives. A
`t7-vehicles` KV bucket holds the vehicle list. Two regions, ZA and AU, each
with its own customers and its own trucks.

So every answer below can be read as: *can Australia still book a load when
South Africa is off the air, and where does that number end up?*

## What we varied

Four things, one at a time. Everything else held still.

| Axis | The options tried |
|---|---|
| **Link between clusters** | none · gateway · leaf node |
| **JetStream domain** | one shared (`lb`) · one per cluster |
| **Accounts** | one shared · one per region |
| **Stream / subject shape** | same name · region token · mirror · export/import |

A "cluster" here means **3 NATS instances**, which is what RAFT needs to elect
a leader and survive one loss.

## The six topologies

| ID | Shape | Meta group | Majority |
|---|---|---|---|
| **T1** | one cluster per region, **no link** | 3 + 3, separate | 2 and 2 |
| **T2** | one cluster per region, **gateway** | 6, shared | 4 |
| **T3** | T2 + a **1-instance** arbiter site | 7, shared | 4 |
| **T4** | T2 + a **3-instance** arbiter cluster | 9, shared | 5 |
| **T5** | 3-instance **hub** + a **leaf** cluster per region | 3 + 3 + 3, separate | 2, 2 and 2 |
| **T6** | T2 **and** T5 at once — a gateway **and** a hub leaf link | 3 + 6 | 2 for the hub, 4 for the regions |

T6 was added on **2026-09-21**, after the other five. It asks the one question
the first five leave open: if a system has **both** links, which one decides?
Its figure is **G**, and it appears only in [`REPORT.html`](REPORT.html).

T2 is the real rig in this folder. T1 is the same six files with the
`gateway {}` block removed.

The two evidence pages use **different names for the same shapes**. The map:

| Matrix page | Quorum page | |
|---|---|---|
| T2 | **A** | the baseline |
| matrix row 5 | **B** | gateway + per-cluster domain — **does not work** |
| T3 | **C** | the arbiter |
| T5 | **D** | hub and leaf |
| T1, T4 | — | matrix only |
| T6 | — | neither page — [`REPORT.html`](REPORT.html) only, as figure **G** |

T4 also has a figure **F**, but only in [`REPORT.html`](REPORT.html). It was
built and measured on 2026-09-17, after both evidence pages were written.

**B and E are not topologies.** Both are T2 — the same six servers and the same
gateway — with one thing changed: a domain per cluster for B, an export/import
pair for E. The lab labels them `T2 / B` and `T2 / E` so the rig and the
variable can be read apart. Every label starts with its topology number.

## Requirements

Ranked by *uncertain and expensive to change*. The cheap ones are not here.

| ID | What we must know | State |
|---|---|---|
| **D03-R1** | Which topology lets **both** regions accept a JetStream write while the other is dark? | answered |
| **D03-R2** | Is it the **account**, the **cluster**, or the **domain** that lets a region own its own stream? | answered |
| **D03-R3** | What does `jetstream.domain` actually change, in each of the six topologies? | answered |
| **D03-R4** | Where do a stream, its **consumer** and its **KV bucket** physically land, and what does a read from the other region cost? | answered |
| **D03-R5** | What does losing a **region**, a **cluster** or a **single instance** do to quorum, and what error code does the client see? | answered |
| **D03-R6** | **Gateway or leaf node** for two regions — what does each one buy, and what does each one cost? | answered |
| **D03-R7** | How does data get a **second copy** in the other region, and what does that cost? | partly answered |
| **D03-R8** | Can two accounts share a subject **on purpose**, via export / import? | answered |
| **D03-R9** | If we add an arbiter site, can real data land on it **by accident**? | answered |
| **D03-R10** | If a system has a **gateway and a leaf link at the same time**, which one decides the JetStream shape? | answered |
| **D03-R11** | Can a running **T4** be converted **in place** to **T5**, and keep its data? | **failed** for the one procedure tested — 2026-09-28, 2026-09-29 |
| **D03-R12** | Can a running **T5** be converted **in place** to **T4**, and keep its data? | **failed** for the one procedure tested, not repeatable — 2026-09-28, 2026-09-29 |
| **D03-R13** | On **T4**, does a hub step-down put the meta leader in the hub, every time? | answered — yes, every trial; requested, not pinned — 2026-10-05 |
| **D03-R14** | One region dark, leader in the hub: does the leader stay, and do writes and metadata go on? | answered — yes — 2026-10-05 |
| **D03-R15** | One region dark, leader in that region: who wins, and how long does it take? | answered — a new leader outside that region; who and how long are notes — 2026-10-05 |
| **D03-R16** | When the region returns: does the leader stay, does the term stay, is the region current? Recovery and stability reported apart | **recovery met** in every valid round; **stability not met** in some — the leader moved, or the term rose — 2026-10-05 |
| **D03-R17** | Both regions dark: does the hub lose the meta quorum even though it is healthy? | answered — yes; the hub's own stream still writes — 2026-10-05 |
| **D03-R18** | One region dark: does only that region's stream lose writes? | answered — yes; a timeout is an unknown outcome — 2026-10-05 |

D03-R13 to D03-R18 are exercise 10
([`exercises/EXERCISE-10-TERMINAL-STEPS.md`](exercises/EXERCISE-10-TERMINAL-STEPS.md),
`lab/10-hub-meta-leader.sh`). Their evidence is
[`REPORT-10.md`](REPORT-10.md) and [`REPORT-10.html`](REPORT-10.html), made by
`lab/render-report-10.py` from kept runs.

### D03-R11 and D03-R12 — switching topology in place

**Result, two runs, `nats-server 2.14.6`.** The tested
stop–reconfigure–restart procedure has **not shown safe, repeatable
operation.** T4 → T5 failed to establish independent metadata groups on both
dates; the second run verified that the tested data was preserved. T5 → T4
gave different outcomes on the two dates: the clean-name conversion removed
the ZA stream data on 2026-09-28, but preserved it on 2026-09-29, with one
placement read left unresolved. The collision case showed replica deletion,
unavailable streams, and both regional lookups resolving to the other
region's stream. **The deletion mechanism and whether the data can be
recovered are not proved.** Alternative conversion and migration procedures
remain untested. A failed direct conversion does not make T4 or T5 a one-way
architectural choice.

**Run of 2026-09-29** — the check IDs below. Evidence:
`lab/run/evidence/09-20260929-093944/`. The verdict rows (`SA41`, `SB41`,
`SC59`) and the evidence checks (`SA40`, `SB40`, `SC58`) ran live.

| Run | Direction | Verdict | Rig | The checks that decide it |
|---|---|---|---|---|
| A | T4 → T5 | **failed** — topology | held, reader proved (`SA1`–`SA5`) | **Topology not established:** after the switch and after a restart every site still reported nine-member meta membership, and no site elected a meta leader in 289 s (`SA7`, `SA10`, `SA28`, `SA29`). **Data preserved**, for what was checked: every message, consumer position and KV key matched by Direct Get (`SA11`–`SA20`, `SA30`–`SA39`); formed stream groups kept taking acks and consumers resumed (`SA21`–`SA27`). |
| B1 | T5 → T4, clean names | **failed** — one unmet verification | held (`SB1`–`SB5`) | One shared group formed (`SB10`, `SB29`). Every message, consumer position and KV key matched after the switch and after the restart (`SB11`–`SB20`, `SB30`–`SB39`). `SB18`: the placement of `lb@za.SHARED_ODO` came back **unread** right after the switch — not wrong; it read `za` after the restart (`SB37`). This does **not** repeat 2026-09-28's ZA loss. |
| B2 | T5 → T4, LB name collision | **failed** | held (`SC1`–`SC5`) | **Replica deletion observed:** at the first T4 start za-2, then za-1, removed their copies of every ZA stream; za-3 kept its copy. The ZA streams took no new acks (`SC29`, `SC33`, `SC35`), their consumers were unreadable (`SC13`, `SC19`, `SC22`), and after the restart no replica answered for two of them (`SC42`, `SC48`) — integrity **inconclusive**. Files remain on za-3; recovery is untested, so irrecoverable loss is **not** established. **Lookup collision:** from the keep-working step on, both regional `ODOMETER` lookups resolved to the same surviving AU stream (`SC36`, `SC51`). **Test side effect:** that probe acked an AU message through the ZA name, moving the AU consumer on by one (`SC38`, `SC56`). |

**Run of 2026-09-28** — kept for comparison, not replaced. Its check IDs
were numbered by an older script and are not in `REPORT.md`; its evidence
(and its verdict rows, derived after the run) is in
`lab/run/evidence/09-20260928-202654/`.

| Run | Direction | Verdict | What it showed |
|---|---|---|---|
| A | T4 → T5 | **failed** | No independent meta groups. Formed stream groups kept taking acks. The data could not be read back: that reader used STREAM.MSG.GET, which answers 10008 with no meta leader. Integrity **unverified**, not disproved. |
| B1 | T5 → T4, clean names | **failed** | One shared group formed, but the ZA streams could not be read back, took no new acks, and their directories were gone from the kept stores. AU and the KV bucket came through. |
| B2 | T5 → T4, LB name collision | **failed** | The same ZA loss as B1. Two consumer failures were a side effect of the test's keep-working step. |

**What the 2026-09-29 debug logs show, and do not show.** In B2, each ZA
server that removed its copies did so within a fraction of a second after it
rolled its meta log back to the new AU meta leader's. za-3 rolled back too,
and kept its copy. So the roll-back alone does not explain the removal. The
cause stays **not proved**. `/jsz` is what each server reports; it is not a
full record of metadata decisions or of why something was deleted.

**How the run reads data back.** By Direct Get, at the sequence numbers the
manifest names. Direct Get is answered by the stream's own replicas, so it
needs no meta leader. It is read-only: no consumer and no ack. The reader is
proved on the source shape first, as two rig checks. If no replica answers
after the switch, the row is a NOTE — *data integrity inconclusive* — not a
data-loss FAIL, and the run cannot pass. Each run writes its evidence to its
own stamped folder, checks it was kept as a **rig** check, and writes the
verdict row itself. A run that stops early gets an *inconclusive* verdict and
keeps the logs and stores it had.

**How the report keeps rig and procedure apart.** Every row has a kind. A
*rig* check asks: did the rig build, seed and read back what it said? A rig
failure makes the verdict *inconclusive*, and the script exits non-zero. A
*procedure* check asks: did the switch keep the data and keep working? Its
failure stays a FAIL — it is the answer, not a broken rig — so
`run-all.sh` counts it apart and does not fail on it.

**The question.** With application traffic paused and existing storage
retained, can T4 be converted to T5, and T5 to T4, while preserving
acknowledged messages, consumer progress and KV state — and then resume
correct operation?

**First slice — convert in place only.**

- **Each direction starts fresh.** Seed a T4 and convert it to T5.
  Separately, seed a new T5 and convert it to T4. A T4 → T5 → T4 round trip
  does not stand for an established T5.
- **Every message carries its own ID.** Record each JetStream ack. After the
  switch, match every acked ID and payload. Equal counts can hide a lost
  message and a duplicate.
- **Writers and consumers are paused** for the switch. Record each consumer's
  acked position before it.
- **Check that it keeps working, not only that it kept the data.** After the
  switch: publish new messages, resume the consumers, update the KV bucket,
  and restart the destination once.
- **T5 → T4 runs twice:** once with clean names, and once with the same
  stream name in the same account on both sides. That collision is the
  restriction most likely to bite.
- One new script, `lab/09-switch-t4-t5.sh`, `t-` prefix, which keeps its
  store directories between the two shapes.

**Verdicts, per direction.** *Passed with a measured interruption*,
*failed*, or *inconclusive* — each with the procedure and the evidence. A
paused-traffic run can never show an online switch. A failure may show a
symptom without proving its cause.

**Not in this slice.** Live writers, a switch stopped partway, rollback
after new writes, and migration to a separately built system. If
conversion in place fails, that is the reason to measure migration — it
does not show that switching shape is impossible.

### Carried forward — not measured

- **D03-R8** — the export/import crossing. The one open claim in the matrix.
- **D03-R7** — a cross-domain mirror was measured over a **leaf** link, where it
  needs `external.api` or it sits at zero messages forever with no error. It was
  **not** measured across two **gateway**-joined clusters with different
  domains, and not for catch-up time at production volume.
- The hub as a **real JetStream store**, not a pass-through (T5).
- Leaf reconnect behaviour with **many** regions, not two.
- **D03-R11 / D03-R12** — only the stop–reconfigure–restart procedure is
  measured, and it failed. Still open: any other conversion (for example one
  site at a time), migration to separately built clusters, **why** T5 → T4
  removed ZA replicas (on 2026-09-28 in the clean-name run, on 2026-09-29 only
  in the collision run), and — a separate question — whether the surviving
  files can be recovered.

## Where the evidence is

- [`diagrams/combination-matrix.html`](diagrams/combination-matrix.html) —
  every combination wired, one row each, with the topology figures T1–T5 and
  the placement findings C1–C6.
- [`diagrams/meta-quorum-options.html`](diagrams/meta-quorum-options.html) —
  *"Who is still alive to take a write?"* Figures A–D, the scoreboard, and five
  config traps each measured the hard way.

Both pages cover demos **02 and 03** together. Demo 03 owns figures T1–T5,
C1–C6, and matrix rows 5, 6 and 7.

Rows say `measured`, `inferred` or `unmeasured`. Believe the labels.

## Source documents

Read from the NATS docs, not from memory:

- <https://docs.nats.io/learn/topologies/>
- <https://docs.nats.io/learn/clustering/>

Measured on **`nats-server 2.14.6`**, September 2026.

## Status

| Stage | State |
|---|---|
| 01 Define | this page |
| 02 Design | done — [`CLAUDE.md`](CLAUDE.md), the rig |
| 03 Validate | done — measured by hand 2026-09-11, made re-runnable 2026-09-17: [`lab/`](lab/), [`REPORT.md`](REPORT.md) and [`REPORT.html`](REPORT.html) |
| 04 Learn | written — pattern cards deck v0.6, 14 cards; card 14 is exercise 10 (2026-10-05) |

See [`demo-playbook.pdf`](../../demo-playbook.pdf) for what those stages mean.

## Re-run the evidence yourself

```bash
cd demos/03-multi-cluster-and-accounts/lab
./run-all.sh
```

That builds all eight topologies from nothing, measures them, tears them down,
and writes [`REPORT.md`](REPORT.md) and [`REPORT.html`](REPORT.html) — the
same findings, but the HTML edition draws each topology. It needs
`nats-server`, `nats`, `jq`, `curl` and `python3`, and nothing else — no
Docker, no trust chain.

Exercise 10 (hub meta-leader on T4) has its own run and its own report:

```bash
demos/03-multi-cluster-and-accounts/exercises/ex10-check.sh
python3 demos/03-multi-cluster-and-accounts/lab/render-report-10.py demos/03-multi-cluster-and-accounts/lab/run/evidence/10-<stamp> [more kept runs...]
```

The first run named is the cited run. Every run named is in the provenance
table of [`REPORT-10.md`](REPORT-10.md) and [`REPORT-10.html`](REPORT-10.html).
