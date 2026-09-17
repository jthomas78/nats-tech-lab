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

## The five topologies

| ID | Shape | Meta group | Majority |
|---|---|---|---|
| **T1** | one cluster per region, **no link** | 3 + 3, separate | 2 and 2 |
| **T2** | one cluster per region, **gateway** | 6, shared | 4 |
| **T3** | T2 + a **1-instance** arbiter site | 7, shared | 4 |
| **T4** | T2 + a **3-instance** arbiter cluster | 9, shared | 5 |
| **T5** | 3-instance **hub** + a **leaf** cluster per region | 3 + 3 + 3, separate | 2, 2 and 2 |

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

## Requirements

Ranked by *uncertain and expensive to change*. The cheap ones are not here.

| ID | What we must know | State |
|---|---|---|
| **D03-R1** | Which topology lets **both** regions accept a JetStream write while the other is dark? | answered |
| **D03-R2** | Is it the **account**, the **cluster**, or the **domain** that lets a region own its own stream? | answered |
| **D03-R3** | What does `jetstream.domain` actually change, in each of the five topologies? | answered |
| **D03-R4** | Where do a stream, its **consumer** and its **KV bucket** physically land, and what does a read from the other region cost? | answered |
| **D03-R5** | What does losing a **region**, a **cluster** or a **single instance** do to quorum, and what error code does the client see? | answered |
| **D03-R6** | **Gateway or leaf node** for two regions — what does each one buy, and what does each one cost? | answered |
| **D03-R7** | How does data get a **second copy** in the other region, and what does that cost? | partly answered |
| **D03-R8** | Can two accounts share a subject **on purpose**, via export / import? | **open** |
| **D03-R9** | If we add an arbiter site, can real data land on it **by accident**? | answered |

### Carried forward — not measured

- **D03-R8** — the export/import crossing. The one open claim in the matrix.
- **D03-R7** — a cross-domain mirror was measured over a **leaf** link, where it
  needs `external.api` or it sits at zero messages forever with no error. It was
  **not** measured across two **gateway**-joined clusters with different
  domains, and not for catch-up time at production volume.
- The hub as a **real JetStream store**, not a pass-through (T5).
- Leaf reconnect behaviour with **many** regions, not two.

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
| 04 Learn | to write — pattern cards deck |

See [`demo-playbook.pdf`](../../demo-playbook.pdf) for what those stages mean.

## Re-run the evidence yourself

```bash
cd demos/03-multi-cluster-and-accounts/lab
./run-all.sh
```

That builds all five topologies from nothing, measures them, tears them down,
and writes [`REPORT.md`](REPORT.md) and [`REPORT.html`](REPORT.html) — the
same findings, but the HTML edition draws each topology. It needs
`nats-server`, `nats`, `jq`, `curl` and `python3`, and nothing else — no
Docker, no trust chain.
