#!/usr/bin/env python3
"""Turn lab/run/results.tsv into REPORT.md.

Every number in the report comes from the TSV, which comes from a live rig.
The prose below is the only hand-written part, and it is not allowed to state
a number -- it points at check IDs, and the reader finds the number in the
table. That way the words can never drift away from the measurement.

Usage:  render-report.py run/results.tsv run/env.txt > ../REPORT.md
"""
import sys, collections

COLS = ("id", "req", "topology", "desc", "expected", "actual", "status")

# What each requirement asks, and what this run said back. No numbers here.
REQS = {
 "D03-R1": (
   "Which topology lets **both** regions accept a JetStream write while the "
   "other is dark?",
   "Three of the five do: **T1** (no link at all), **T3** (gateway plus an "
   "arbiter site) and **T5** (hub and leaf). **T2**, the plain gateway, does "
   "not -- the six servers are one meta group, so a 3/3 split leaves nobody "
   "with a majority and both sides freeze. T3 fixes that with a seventh vote "
   "that sits outside both regions. T5 fixes it a different way: each region "
   "is its own JetStream system, so there is no shared vote to lose."),
 "D03-R2": (
   "Is it the **account**, the **cluster**, or the **domain** that lets a "
   "region own its own stream?",
   "The **account**. A cluster only decides *where* a stream is placed, and "
   "over a gateway a domain changes nothing at all. Two accounts is the only "
   "thing in this demo that gave two regions two real, separately-owned "
   "streams of the same name over one link."),
 "D03-R3": (
   "What does `jetstream.domain` actually change, in each of the five "
   "topologies?",
   "Over a **gateway** it changes nothing useful and quietly makes things "
   "worse -- the supercluster stays one meta group and one of the two regions "
   "stops electing a leader. Over a **leaf link** it is real: the hub, ZA and "
   "AU each become a separate JetStream system with its own vote, and the "
   "same stream name can live in all three at once. A domain is a leaf-link "
   "tool. A gateway is not a leaf link."),
 "D03-R4": (
   "Where do a stream, its **consumer** and its **KV bucket** physically "
   "land, and what does a read from the other region cost?",
   "Everything lands where the account's first request landed, unless you "
   "name a cluster with `--cluster`. A KV bucket follows exactly the same "
   "rule, because a bucket *is* a stream. **Partly answered:** this rig "
   "measured placement, not latency. Consumer placement and the cost in "
   "milliseconds of a cross-WAN read are still not measured here."),
 "D03-R5": (
   "What does losing a **region**, a **cluster** or a **single instance** do "
   "to quorum, and what error code does the client see?",
   "It is plain majority arithmetic over the meta group, and the group size "
   "is what the topology decides. When the majority is gone the client sees "
   "`10008 JetStream system temporarily unavailable` on any *change* -- but "
   "only once the old leader has aged out. Before that the client just hangs "
   "and times out. Writes into a stream that already exists keep working "
   "throughout, because a stream's replicas all sit inside one cluster."),
 "D03-R6": (
   "**Gateway or leaf node** for two regions -- what does each one buy, and "
   "what does each one cost?",
   "A **gateway** buys you one namespace: one name, one copy, no duplicates "
   "possible. It costs you a shared fate -- one meta group, so a WAN cut is a "
   "change freeze for everybody. A **leaf** link buys you real independence "
   "and survives the hub disappearing entirely. It costs you the double "
   "capture: one publish is genuinely stored twice, and nothing warns you. "
   "Nothing copies itself across a leaf link either -- every cross-region "
   "copy is something a person has to write down."),
 "D03-R7": (
   "How does data get a **second copy** in the other region, and what does "
   "that cost?",
   "With a **mirror**, and over a leaf link a mirror needs "
   "`mirror.external.api` pointing at the other domain. Leave it out and you "
   "get the worst outcome in this whole report: no error, no warning, a "
   "stream that reports healthy, and either zero messages or -- if a local "
   "stream happens to share the name -- a silent copy of the **wrong** "
   "stream. **Partly answered:** still not measured over a gateway, and no "
   "lag or bandwidth figure."),
 "D03-R8": (
   "Can two accounts share a subject **on purpose**, via export / import?",
   "**Still open.** This rig does not test it. It is the one unmeasured claim "
   "left in the matrix."),
 "D03-R9": (
   "If we add an arbiter site, can real data land on it **by accident**?",
   "Not by accident -- but it is not fenced off either. Unplaced streams "
   "never chose the arbiter on their own. But a stream *asked* to go there "
   "goes there, and a client that dials the arbiter's port directly creates "
   "its stream there. So the arbiter is safe by habit, not by rule. If you "
   "want it fenced, fence it yourself."),
}

# Findings this run produced that are NOT in the demo's existing evidence pages.
NEW = [
 ("The `/jsz` leader field goes stale, and it lies while it does",
  "A10a, A11a",
  "After the majority of a supercluster was frozen, the monitoring endpoint "
  "kept naming a leader for roughly a minute. Read it too early and a dead "
  "supercluster looks perfectly healthy. In that window the client does not "
  "get a clean refusal either -- it hangs and times out. The clean `10008` "
  "only appears once the old leader has aged out. Anyone timing a failover "
  "test needs to wait for the leader to actually go, not trust the first "
  "reading."),
 ("A name-only mirror can silently copy the WRONG stream",
  "D9, D9a",
  "A mirror with no `external.api` does not fail when the name it wants only "
  "exists in the other region -- it just sits at zero. Worse: if a stream of "
  "the same name also exists **locally**, the mirror copies that one instead, "
  "reports healthy, and looks like it is working. This run proved it by "
  "giving the two streams different message counts, so the number alone says "
  "which one was copied."),
 ("Which region goes blind is a coin toss",
  "B1, B1a, B2",
  "Put a domain on each cluster over a gateway and one of the two regions "
  "never elects a meta leader -- that part was already known. What was not "
  "known: **which** region loses is not fixed. Two runs of the same configs, "
  "minutes apart, nothing changed, and the blind side swapped. So a team that "
  "tests this shape once will write down the wrong region's name. The check "
  "in this rig deliberately refuses to name a side; it only asks that exactly "
  "one of the two elects, and records the winner as an observation."),
 ("A freeze that does not freeze reads exactly like a finding",
  "A10, A11",
  "Not a NATS fact -- a rig fact, and worth keeping. An early version of this "
  "harness recorded the wrong process id, so `kill -STOP` hit nothing and the "
  "\"frozen\" region stayed fully alive. The lab reported healthy behaviour "
  "and it looked like a result. Every freeze in these scripts now proves "
  "itself first by checking the monitor port has really gone dark."),
]

OPEN = [
 ("`D03-R8` -- export / import between two accounts", "No script covers it."),
 ("`D03-R7` over a gateway", "Mirrors were measured over a leaf link only."),
 ("`D03-R4` -- the cost of a cross-region read",
  "Placement is measured. Latency is not. No consumer placement test either."),
 ("**T4** -- gateway with a 3-node arbiter cluster",
  "Never built, here or anywhere. The README still marks it `inferred`."),
 ("The hub as a real store",
  "In T5 the hub only relays. Nothing measured what happens when it holds "
  "data of its own."),
 ("Leaf reconnect with many regions",
  "Two regions were tested. Nothing tested five."),
]


def main():
    rows = []
    with open(sys.argv[1]) as fh:
        for line in fh:
            line = line.rstrip("\n")
            if not line:
                continue
            parts = line.split("\t")
            if len(parts) != len(COLS):
                continue
            rows.append(dict(zip(COLS, parts)))

    env = {}
    try:
        for line in open(sys.argv[2]):
            k, _, v = line.rstrip("\n").partition("\t")
            env[k] = v
    except OSError:
        pass

    checks = [r for r in rows if r["status"] in ("PASS", "FAIL")]
    npass = sum(1 for r in checks if r["status"] == "PASS")
    nfail = sum(1 for r in checks if r["status"] == "FAIL")
    nnote = sum(1 for r in rows if r["status"] == "NOTE")

    # Topologies in the order the run produced them.
    topos, seen = [], set()
    for r in rows:
        if r["topology"] not in seen:
            seen.add(r["topology"])
            topos.append(r["topology"])

    by_topo = collections.defaultdict(list)
    for r in rows:
        by_topo[r["topology"]].append(r)
    by_req = collections.defaultdict(list)
    for r in rows:
        by_req[r["req"]].append(r["id"])

    o = print

    o("# Demo 03 — validation report")
    o("")
    o("<!-- GENERATED by lab/run-all.sh. Do not edit by hand. -->")
    o("")
    o("Demo 03's role is **validation**. The playbook says a validation demo "
      "fails when the rig is gone and cannot be re-run. This demo failed that "
      "test: the findings were measured on 2026-09-11 and they were real, "
      "but they were produced by hand in a terminal and never written down "
      "as anything runnable.")
    o("")
    o("This report is the answer. Every row below was produced by a script in "
      "`lab/`, on a rig those scripts build from nothing and tear down "
      "afterwards. To reproduce the whole thing:")
    o("")
    o("```bash")
    o("cd demos/03-multi-cluster-and-accounts/lab")
    o("./run-all.sh")
    o("```")
    o("")
    o("## The run that produced this")
    o("")
    o("| | |")
    o("|---|---|")
    o(f"| When | {env.get('when','—')} |")
    o(f"| Server | `nats-server {env.get('nats-server','—')}` |")
    o(f"| Client | `{env.get('nats-cli','—')}` |")
    o(f"| Machine | {env.get('host','—')} |")
    o(f"| Checks | **{npass} passed, {nfail} failed** |")
    o(f"| Recorded observations | {nnote} |")
    o("")
    o("A **check** has an expected answer and passes only on an exact match. "
      "A **note** has no expected answer — it records what the machine did so "
      "the number is on the record. Notes cannot pass or fail.")
    o("")

    o("## Per topology")
    o("")
    o("| Topology | Checks | Passed | Failed | Notes |")
    o("|---|---:|---:|---:|---:|")
    for t in topos:
        rs = by_topo[t]
        c = [r for r in rs if r["status"] in ("PASS", "FAIL")]
        p = sum(1 for r in c if r["status"] == "PASS")
        f = sum(1 for r in c if r["status"] == "FAIL")
        n = sum(1 for r in rs if r["status"] == "NOTE")
        o(f"| {t} | {len(c)} | {p} | {f} | {n} |")
    o("")

    o("## The answers, requirement by requirement")
    o("")
    for rid in sorted(REQS):
        q, a = REQS[rid]
        ids = by_req.get(rid, [])
        o(f"### {rid}")
        o("")
        o(f"**Asks:** {q}")
        o("")
        o(f"**This run says:** {a}")
        o("")
        if ids:
            o(f"*Evidence:* {', '.join('`%s`' % i for i in ids)}")
        else:
            o("*Evidence:* none — no check in this rig covers it.")
        o("")

    o("## Findings this rig added")
    o("")
    o("Four things came out of building the scripts that are **not** in the "
      "demo's existing evidence pages. Three are NATS behaviour. One is about "
      "measuring.")
    o("")
    for title, ids, body in NEW:
        o(f"### {title}")
        o("")
        o(body)
        o("")
        o(f"*Evidence:* {', '.join('`%s`' % i.strip() for i in ids.split(','))}")
        o("")

    o("## Still open")
    o("")
    o("| What | Why it is still open |")
    o("|---|---|")
    for what, why in OPEN:
        o(f"| {what} | {why} |")
    o("")

    o("## Every result")
    o("")
    for t in topos:
        o(f"### {t}")
        o("")
        o("| | Check | Req | Expected | Measured |")
        o("|---|---|---|---|---|")
        for r in by_topo[t]:
            mark = {"PASS": "✅", "FAIL": "❌", "NOTE": "📋"}.get(r["status"], "")
            exp = r["expected"] if r["status"] != "NOTE" else "—"
            o(f"| {mark} `{r['id']}` | {r['desc']} | {r['req']} | "
              f"{exp} | **{r['actual']}** |")
        o("")

    o("## What each script builds")
    o("")
    o("Every script starts its own servers from nothing, measures, and stops "
      "them. Nothing is left running. All servers are bare `nats-server` "
      "processes on `127.0.0.1` — no Docker, no `nsc`, no `nats` contexts.")
    o("")
    o("| Script | Topology | Servers |")
    o("|---|---|---|")
    o("| `00-islands.sh` | T1 — two regions, no link | 6 |")
    o("| `01-gateway.sh` | T2 / A — one gateway | 6 |")
    o("| `02-domain-over-gateway.sh` | B — a domain per cluster over a gateway | 6 |")
    o("| `03-arbiter.sh` | T3 / C — gateway plus a 1-node arbiter | 7 |")
    o("| `04-hub-and-leaf.sh` | T5 / D — a hub with two leaf clusters | 9 |")
    o("")
    o("Run one on its own the same way: `./01-gateway.sh`.")


main()
