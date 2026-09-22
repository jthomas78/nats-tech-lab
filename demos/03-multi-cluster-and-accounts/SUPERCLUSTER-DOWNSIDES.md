# Supercluster downsides — one JetStream domain across two regions

**Status: parked note, not a deliverable.** This is a holding place for a
general summary of the downsides of a NATS supercluster
([topologies](https://docs.nats.io/concepts/topologies#super-cluster)) run with
**one** JetStream domain. It is written down here so it is not lost. The proper
home for anything here that survives scrutiny is a **pattern card** (stage `04`
— see the `pattern-cards` skill).

Nothing below is hand-measured into `REPORT.md`. Where this rig *has* measured a
claim, the check ID is given. Where it has not, the row says so. Do not promote a
row without a run.

*Source:* a general summary from Codex, asked *"what are the possible downsides
of using a supercluster and using one JetStream domain?"*. Treated as an outside
opinion and checked against this demo's own evidence, not as fact.

---

## The eight claims, checked against this rig

| # | The claim | This rig says | Evidence |
|---|---|---|---|
| 1 | The JetStream **meta group** becomes global. Its RAFT quorum spans both regions, so a region loss or a WAN partition can stop every *change* everywhere. | **Measured, true.** A 3/3 split puts both sides under the majority of 4. Every `stream add` / `rm` / `edit` refuses with `10008`, in every account at once — because there is **one** meta group, not one per account. | `A2`, `A4`, `A10`, `A11a`, `A12` |
| 2 | Streams that already exist keep working. | **Measured, true.** Publishes into an existing stream keep being acked throughout a freeze (10 → 15 messages). A stream's replicas all sit inside one cluster, so the data plane is not the thing that froze. | `A11`, `A12` |
| 3 | The **control plane** now depends on the WAN. | **Measured, true**, and worse than it reads. `/jsz` keeps naming a dead meta leader for up to about a minute, and in that window the client does not get a clean refusal — it hangs and times out. The other edge is just as bad: a new leader is announced *before* it will take a change. | `A10a`, `A11a`, `F6a`, `F7a` |
| 4 | A plain `3 + 3` is an awkward shape. | **Measured, true.** 6 servers need a majority of 4, and neither region has 4. Adding **one** arbiter gives 7 and a majority of 4 — survivable, but with **zero slack**. Three arbiters give 9, a majority of 5, and **one node of slack**. | `C12`, `F11` |
| 5 | Adding regions changes the arithmetic again. | **Not measured here.** This rig only ever built two regions plus a hub. Listed as still open in `REPORT.md` ("Leaf reconnect with many regions"). | — |
| 6 | Stream **placement** starts to matter. | **Measured, true.** A stream lands in one cluster and stays there. `stream info` read from the far region reports the *other* cluster. The far side pays the WAN on every read — and when that region is dark, the read fails outright with `10008`, not just slowly. | `A8`, `A20`, `A21`, `A21a` |
| 7 | You lose independent regional namespaces. | **Measured, true.** One gateway = one namespace. A second `ODOMETER` is refused with `10058 stream name already in use`, overlapping subjects with `10065`. The wall that *does* work is the **account**: `LB_ZA` and `LB_AU` may each own an `ODOMETER`. | `A5`, `A6`, `E7`, `E8` |
| 8 | Regional maintenance becomes a global event. | **Inferred, not measured.** It follows from 1 and 4 — the rig never ran a rolling restart. Worth a run before it goes on a card. | — |

## The gap this rig found in the summary

The summary implies that **separate JetStream domains** are the escape hatch from
a shared-fate supercluster. On a gateway they are not.

A `jetstream.domain` name may only change across a **leaf-node** link. Put a
domain on each cluster of a gateway and one of the two regions simply never
elects a meta leader — and **which** region goes blind is a coin toss, not a
fixed side. Two runs of the same configs, minutes apart, and the blind side
swapped.

*Evidence:* `B1`, `B1a`, `B2`, `B9`–`B16`.

The same thing is true of the combined shape. A gateway **and** a hub leaf link
on the same nine servers gives **two** JetStream systems, not three: the hub on
its own, and both regions still fused by the gateway. Every property the leaf
link has alone is gone. A dark region still freezes creates with `10008`, and the
healthy hub cannot lend a vote.

*Evidence:* `G1`, `G4`, `G11a`, `G14a`, `G24` — *"the gateway decides."*

**So a leaf link added to a gateway buys nothing.** There is no gradual migration
from a gateway to a hub-and-leaf shape. It is a cutover.

## Where this goes next

1. Run the one unmeasured claim that matters — **8**, rolling regional
   maintenance on a shared meta group.
2. Turn what survives into pattern cards. Claims **1**, **3**, **4**, **6** and
   **7** are card-ready today; **5** and **8** are not.
3. Retract or relabel anything a later run contradicts. A note whose numbers only
   ever improve is not measuring.
