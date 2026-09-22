#!/usr/bin/env python3
"""Turn lab/run/results.tsv into REPORT.md.

Every number in the report comes from the TSV, which comes from a live rig.
The prose below is the only hand-written part, and it is not allowed to state
a number -- it points at check IDs, and the reader finds the number in the
table. That way the words can never drift away from the measurement.

Usage:  render-report.py run/results.tsv run/env.txt > ../REPORT.md
"""
import sys, collections

COLS = ("id", "req", "topology", "desc", "expected", "actual", "status",
        "from")

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
   "stream. Over a **gateway** the same mirror needs no `external.api` at "
   "all, because a supercluster is one JetStream namespace -- it simply "
   "works, and it keeps up. **Partly answered:** no lag or bandwidth figure "
   "in either shape."),
 "D03-R8": (
   "Can two accounts share a subject **on purpose**, via export / import?",
   "Yes, and it is the only sharing in this demo that is safe by design. "
   "The exporting account opens one hole, in one direction, and the "
   "importing account renames what comes through it -- so the copy can never "
   "be mistaken for its own data. The hole is a **subject** hole only: the "
   "importing account still cannot read, name or delete a stream that "
   "belongs to the exporter, and it may use the same stream name for "
   "something else. That is the deliberate opposite of T5's silent double "
   "capture."),
 "D03-R10": (
   "If a system has a **gateway and a leaf link at the same time**, which one "
   "decides the JetStream shape?",
   "The **gateway**, completely. Wiring both links on the same servers gives "
   "**two** JetStream systems, not three: the hub keeps its own group, and "
   "the gateway still fuses the two regions into one. Every benefit the leaf "
   "link has on its own then disappears -- the second stream of the same name "
   "in a shared account is refused again, the double capture collapses to one "
   "stored copy, and a dark region still freezes every create while the hub "
   "sits beside it, healthy, unable to lend a vote. Deleting the hub entirely "
   "changes nothing. A per-site domain is still illegal here and still "
   "half-blinds one region, exactly as it does with no hub at all. **A leaf "
   "link added to a gateway buys nothing.**"),
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
 ("A new meta leader is named before it can take a change",
  "F6a, F7a",
  "The stale-leader trap has a twin, and it points the other way. After an "
  "election the monitoring endpoint names the new leader some seconds before "
  "that leader will accept a create -- ask immediately and the call is "
  "refused, on a supercluster that is in fact healthy. So neither edge of a "
  "failover can be trusted from one reading: the old leader lingers after it "
  "is gone, and the new one is announced before it is ready. The T4 script "
  "does not sleep a magic number for this -- it retries and records how long "
  "it waited."),
 ("Two links at once is not two shapes at once -- the gateway wins",
  "G1, G4, G6, G13",
  "A gateway and a hub leaf link were wired onto the same nine servers, to "
  "see whether a system could keep the gateway's single namespace and still "
  "get the leaf link's independent vote. It cannot. The result is two "
  "JetStream systems, not three: the hub in its own group, and both regions "
  "still fused into one by the gateway. Every property the leaf link has on "
  "its own is gone -- the duplicate stream name is refused again, the double "
  "capture collapses to a single stored copy, and a dark region freezes every "
  "create while the hub sits alongside it perfectly healthy and unable to "
  "help. The hub can be killed outright with no effect on either region. "
  "This is the first shape in the lab that costs real money and buys "
  "nothing."),
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
 ("`D03-R4` -- the cost of a cross-region read",
  "Placement is measured. Latency is not. No consumer placement test either."),
 ("Mirror lag and bandwidth",
  "Both shapes now prove a mirror copies. Neither says how fast, or at what "
  "cost on the wire."),
 ("The hub as a real store",
  "In T5 the hub only relays. Nothing measured what happens when it holds "
  "data of its own."),
 ("Leaf reconnect with many regions",
  "Two regions were tested. Nothing tested five."),
]


# --------------------------------------------------------------------------
# HTML mode.
#
# Same numbers, same prose, same rule: nothing here states a number. The
# difference is the five hand-drawn topology figures, which live in
# figures.html and are spliced in by key. Each figure carries the check IDs
# measured on that topology, so a reader can look at the picture and see
# which check proves each claim.
# --------------------------------------------------------------------------

# TSV topology string -> figure key in figures.html
FIGKEY = {
    "T1 -- no link":                       "t1",
    "T2 / A -- gateway":                   "a",
    "T2 / B -- gateway + per-cluster domain": "b",
    "T3 / C -- gateway + arbiter":         "c",
    "T5 / D -- hub and leaf":              "d",
    "T2 / E -- export / import between accounts": "e",
    "T4 / F -- gateway + 3-node arbiter":   "f",
    "T6 / G -- gateway AND hub leaf":       "g",
}

# One prose caption per figure. Claims only, no numbers.
FIGCAP = {
 "t1": ("Two islands.",
        "Nothing joins the regions. Each one runs its own JetStream meta "
        "group and votes alone, so neither can be stopped by the other. The "
        "price is total isolation: the same stream name means a different "
        "stream on each side, and no message ever crosses."),
 "a":  ("One gateway, one meta group.",
        "The gateway binds all six servers into a single RAFT group, so a "
        "shared account becomes one stream namespace across both regions — "
        "the second region is refused the name. A separate account per region "
        "is the wall that fixes it. Losing one region drops the survivors "
        "below the majority and freezes every create."),
 "b":  ("A domain per cluster over a gateway — the shape that does not work.",
        "A JetStream domain names a JetStream system, and the name may only "
        "change across a leaf link. Over a gateway it half-blinds the link "
        "without splitting the RAFT group. One side never elects a leader, "
        "with nothing stopped. Which side loses is a start-up race, not a "
        "property of a region."),
 "c":  ("A gateway plus a one-node arbiter.",
        "A third site breaks the tie, so losing a whole region still leaves a "
        "majority and both survivors keep writing. Two costs: the arbiter is "
        "a real JetStream server that will accept data if a client asks it "
        "to, and the surviving majority has no slack left."),
 "d":  ("A hub with two leaf clusters.",
        "Each region is its own JetStream system with its own domain, so each "
        "votes alone and the hub can die without stopping either. Nothing "
        "replicates by itself: every cross-region copy is hand-written, and a "
        "mirror written without an external API prefix copies the wrong "
        "stream, or nothing at all, and says so nowhere."),
 "e":  ("One hole in the account wall, opened on purpose.",
        "The exporting account offers a subject; the importing account takes "
        "it and renames it with a prefix of its own choosing. One publish is "
        "then stored in both accounts, on two different subjects, and it "
        "crosses the gateway like any other message. The wall still stands "
        "everywhere else: the importer cannot reach the exporter's streams, "
        "and may reuse the same stream name for its own."),
 "g":  ("Both links at once \u2014 a gateway and a hub leaf link.",
        "The obvious idea: keep the gateway for one namespace, add the hub "
        "leaf link for independence, take the good half of each. It does not "
        "work that way. The gateway still fuses both regions into one meta "
        "group, so the hub is a third JetStream system standing next to them "
        "with nothing to contribute \u2014 it cannot vote for them, cannot "
        "hold their streams, and can be deleted without either region "
        "noticing. A domain per site is no more legal here than it is without "
        "the hub."),
 "f":  ("A gateway plus a three-node arbiter cluster.",
        "The same idea as the one-node arbiter, with the arbiter site no "
        "longer a single point of failure. Nine voters instead of seven means "
        "losing a whole region still leaves a spare node — the slack the "
        "one-node shape does not have. One node further and it freezes, in "
        "exactly the way the plain gateway does."),
}


def req_key(rid):
    """Sort D03-R10 after D03-R9, not between R1 and R2."""
    return int(rid.rsplit("R", 1)[1])


def esc(s):
    return (s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;"))


def md(s):
    """The tiny bit of markdown the prose above uses: `code` and **bold**."""
    out, i, n = [], 0, len(s)
    while i < n:
        c = s[i]
        if c == "`":
            j = s.find("`", i + 1)
            if j == -1:
                out.append(esc(c)); i += 1; continue
            out.append("<code>" + esc(s[i + 1:j]) + "</code>")
            i = j + 1
        elif s.startswith("**", i):
            j = s.find("**", i + 2)
            if j == -1:
                out.append(esc(c)); i += 1; continue
            out.append("<strong>" + md(s[i + 2:j]) + "</strong>")
            i = j + 2
        else:
            out.append(esc(c)); i += 1
    return "".join(out)


def load_figures(path):
    """Read figures.html into {key: svg}. Missing file is not fatal."""
    figs = {}
    try:
        src = open(path).read()
    except OSError:
        return figs
    key = None
    buf = []
    for line in src.splitlines():
        st = line.strip()
        if st.startswith("<!--FIG ") and st.endswith("-->"):
            key = st[len("<!--FIG "):-3].strip()
            buf = []
        elif st == "<!--/FIG-->":
            if key:
                figs[key] = "\n".join(buf)
            key = None
        elif key is not None:
            buf.append(line)
    return figs


HEAD = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Demo 03 &mdash; validation report</title>
<!-- GENERATED by lab/run-all.sh html. Do not edit by hand. -->
<style>
:root {
  --text: #dee0e3; --muted: #b7bcc2; --dim: #737c87;
  --bg: #14171b; --panel: #1a1e23; --border: #2c3138;
  --accent: #006fff; --nested: #171c29;
  --sync: #4d94ff; --store: #2dd4bf; --evtl: #f0b429; --bad: #f87171; --hop: #8b93a1;
  --good: #2dd4bf; --warn: #f0b429; --dom: #f97316;
  --mono: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  --sans: 'Inter', -apple-system, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
}
* { box-sizing: border-box; }
body {
  margin: 0; background: var(--bg); color: var(--text);
  font-family: var(--sans); font-size: 13px; line-height: 20px;
  -webkit-font-smoothing: antialiased;
}
.wrap { max-width: 1180px; margin: 0 auto; padding: 24px 18px 64px; display: flex; flex-direction: column; gap: 26px; }
.head { display: flex; align-items: flex-end; gap: 16px; flex-wrap: wrap; }
.head-txt { flex: 1; min-width: 260px; display: flex; flex-direction: column; gap: 3px; }
.eyebrow { font-family: var(--mono); font-size: 10px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--dim); }
h1 { margin: 0; font-size: 20px; line-height: 26px; font-weight: 600; letter-spacing: -0.01em; text-wrap: balance; }
h2 { margin: 0; font-size: 15px; line-height: 21px; font-weight: 600; text-wrap: balance; }
h3 { margin: 0; font-size: 13px; line-height: 19px; font-weight: 600; }
.sub { margin: 0; color: var(--muted); max-width: 68ch; }
p { margin: 0; color: var(--muted); max-width: 74ch; }
p strong, li strong { color: var(--text); font-weight: 600; }

.sec { display: flex; flex-direction: column; gap: 11px; }
.card { background: var(--panel); border: 1px solid var(--border); border-radius: 4px; padding: 14px 16px; display: flex; flex-direction: column; gap: 9px; }
.card.nested { background: var(--nested); }

figure { margin: 0; background: var(--panel); border: 1px solid var(--border); border-radius: 4px; overflow: hidden; }
.fig-body { padding: 14px 14px 4px; overflow-x: auto; }
figure svg { display: block; width: 100%; min-width: 0; max-width: 1000px; height: auto; margin: 0 auto; color: var(--muted); }
figcaption { padding: 9px 14px 11px; border-top: 1px solid var(--border); color: var(--muted); font-size: 12px; line-height: 18px; }
figcaption b { color: var(--text); font-weight: 600; }
figcaption code, .sub code, li code, p code, td code, th code {
  font-family: var(--mono); font-size: 11px;
  background: color-mix(in srgb, var(--text) 8%, transparent);
  border-radius: 3px; padding: 0 4px;
}

table { border-collapse: collapse; width: 100%; font-size: 12px; line-height: 18px; }
th, td { text-align: left; padding: 6px 10px; border-bottom: 1px solid var(--border); vertical-align: top; }
th { color: var(--dim); font-family: var(--mono); font-size: 10px; text-transform: uppercase; letter-spacing: 0.07em; font-weight: 500; }
td { color: var(--muted); }
tr:last-child td { border-bottom: none; }
td.num, th.num { text-align: right; font-family: var(--mono); }
td.mark { width: 22px; text-align: center; }
td.mono { font-family: var(--mono); color: var(--text); }
.pass { color: var(--good); } .fail { color: var(--bad); } .note { color: var(--dim); }

ul { margin: 0; padding-left: 18px; color: var(--muted); display: flex; flex-direction: column; gap: 5px; max-width: 74ch; }
li b { color: var(--text); font-weight: 600; }
pre { margin: 0; background: var(--nested); border: 1px solid var(--border); border-radius: 4px; padding: 10px 12px; overflow-x: auto; font-family: var(--mono); font-size: 11.5px; line-height: 18px; color: var(--text); }

/* --- diagram primitives, identical to diagrams/combination-matrix.html --- */
.n  { fill: none; stroke: currentColor; stroke-width: 1; }
.nb { fill: none; stroke: currentColor; stroke-width: 1; stroke-dasharray: 4 3; stroke-opacity: 0.5; }
.nb.clu { stroke-dasharray: none; stroke-opacity: 0.32; }
.nb.dead { stroke: var(--bad); stroke-opacity: 0.55; }
.nb.acct { stroke: var(--warn); stroke-opacity: 0.7; stroke-dasharray: 7 3; }
.nb.dom  { stroke: var(--dom); stroke-opacity: 0.75; stroke-dasharray: 3 3; }
.lbl  { font-family: var(--mono); font-size: 11px; fill: var(--text); }
.lbl2 { font-family: var(--mono); font-size: 9.5px; fill: var(--dim); }
.grp  { font-family: var(--mono); font-size: 9.5px; fill: var(--dim); letter-spacing: 0.07em; }
.grp.g { fill: var(--good); } .grp.r { fill: var(--bad); } .grp.a { fill: var(--warn); }
.grp.d { fill: var(--dom); }
.edge { font-family: var(--mono); font-size: 9.5px; fill: var(--muted); }
.ro   { font-family: var(--mono); font-size: 10px; fill: var(--muted); }
.ro.g { fill: var(--good); } .ro.r { fill: var(--bad); } .ro.h { fill: var(--text); }
.ro.d { fill: var(--dom); }
.gw   { fill: none; stroke: var(--sync); stroke-width: 1.4; }
.lf   { fill: none; stroke: var(--store); stroke-width: 1.4; }
.dead { fill: none; stroke: var(--bad); stroke-width: 1.4; stroke-dasharray: 4 4; }
.mir  { fill: none; stroke: var(--evtl); stroke-width: 1.4; stroke-dasharray: 5 4; }
</style>
</head>
<body>
<div class="wrap">
"""


def render_html(env, rows, topos, by_topo, by_req, npass, nfail, nnote, figpath):
    figs = load_figures(figpath)
    o = print
    o(HEAD, end="")

    o('<header class="head"><div class="head-txt">')
    o('<div class="eyebrow">Demo 03 &middot; stage 03 Validate &middot; '
      'generated by lab/run-all.sh</div>')
    o("<h1>Multi-cluster and accounts &mdash; validation report</h1>")
    o('<p class="sub">Five topologies, built from nothing and measured by '
      'script. Every number on this page comes from one run of '
      '<code>lab/run-all.sh</code>. The words never state a number &mdash; '
      'they point at a check ID, and the number sits in the table beside '
      'it.</p>')
    o("</div></header>")

    # --- the run -----------------------------------------------------------
    o('<section class="sec">')
    o("<h2>The run that produced this</h2>")
    o('<div class="card"><table><tbody>')
    for k, v in (("When", esc(env.get("when", "—"))),
                 ("Server", "<code>nats-server %s</code>"
                            % esc(env.get("nats-server", "—"))),
                 ("Client", "<code>%s</code>" % esc(env.get("nats-cli", "—"))),
                 ("Machine", esc(env.get("host", "—")))):
        o(f"<tr><th>{k}</th><td>{v}</td></tr>")
    o(f'<tr><th>Checks</th><td><strong class="pass">{npass} passed</strong>, '
      f'<strong class="{"fail" if nfail else "note"}">{nfail} failed</strong>'
      f"</td></tr>")
    o(f"<tr><th>Recorded observations</th><td>{nnote}</td></tr>")
    o("</tbody></table></div>")
    o("<p>A <strong>check</strong> has an expected answer and passes only on "
      "an exact match. A <strong>note</strong> has no expected answer &mdash; "
      "it records what the machine did, so the number is on the record. A "
      "note can never fail, so it guards nothing.</p>")
    o("<p>To reproduce the whole thing:</p>")
    o("<pre>cd demos/03-multi-cluster-and-accounts/lab\n./run-all.sh</pre>")
    o("</section>")

    # --- the accounts, held still in every topology -------------------------
    # The topology is the variable. The account block is NOT -- it is byte for
    # byte the same in all nine config files of every run. A reader who does
    # not know that will read "shared account LB" as jargon.
    o('<section class="sec">')
    o("<h2>The four accounts</h2>")
    o("<p>An <strong>account</strong> is a wall around data. Two accounts "
      "cannot see each other&rsquo;s subjects, streams or consumers. The same "
      "four exist in every server of every topology on this page, so the "
      "account is never the variable &mdash; the topology is.</p>")
    o('<div class="card"><table><thead><tr>'
      "<th>Account</th><th>Who uses it</th><th>Why it is here</th>"
      "</tr></thead><tbody>")
    for acct, who, why in (
        ("LB", "<strong>shared</strong> &mdash; both regions log in to the "
               "same one",
               "the broken shape: one JetStream namespace across both "
               "regions, so a stream name can only exist once"),
        ("LB_ZA", "ZA only",
                  "ZA owns its own streams, and AU cannot see them at all"),
        ("LB_AU", "AU only",
                  "AU owns its own streams, and ZA cannot see them at all"),
        ("$SYS", "the servers",
                 "the system account, used for cluster and gateway traffic"),
    ):
        o(f"<tr><td><code>{esc(acct)}</code></td><td>{who}</td>"
          f"<td>{why}</td></tr>")
    o("</tbody></table></div>")
    o("<p>Users are plain user / password pairs, and the password equals the "
      "user name. That is lab-only, bound to <code>127.0.0.1</code>, and "
      "worthless anywhere else. In the diagrams an account is drawn as a "
      "<strong>yellow dashed box</strong> around everything it contains.</p>")
    o("<p>A <strong>JetStream domain</strong> is a different wall, and the "
      "diagrams draw it as an <strong>orange dashed box</strong>. It names "
      "one JetStream system &mdash; one meta group, one set of stream "
      "names. A gateway puts both regions inside <em>one</em> orange box, "
      "whatever you name it; only a <strong>leaf-node</strong> link can "
      "give each side an orange box of its own. That single fact is what "
      "topology <code>T2 / B</code> fails to beat.</p>")
    o("</section>")

    # --- summary -----------------------------------------------------------
    o('<section class="sec">')
    o("<h2>Per topology</h2>")
    o('<div class="card"><table><thead><tr>'
      '<th>Topology</th><th class="num">Checks</th><th class="num">Passed</th>'
      '<th class="num">Failed</th><th class="num">Notes</th>'
      "</tr></thead><tbody>")
    for t in topos:
        rs = by_topo[t]
        c = [r for r in rs if r["status"] in ("PASS", "FAIL")]
        p = sum(1 for r in c if r["status"] == "PASS")
        f = sum(1 for r in c if r["status"] == "FAIL")
        n = sum(1 for r in rs if r["status"] == "NOTE")
        o(f"<tr><td>{esc(t)}</td><td class='num'>{len(c)}</td>"
          f"<td class='num pass'>{p}</td>"
          f"<td class='num {'fail' if f else 'note'}'>{f}</td>"
          f"<td class='num note'>{n}</td></tr>")
    o("</tbody></table></div>")
    o("</section>")

    # --- one section per topology: the picture, then its checks ------------
    o('<section class="sec">')
    o("<h2>The five topologies</h2>")
    o("<p>Each diagram carries the IDs of the checks measured on it. Look at "
      "the picture, find the claim, then find the same ID in the table under "
      "it.</p>")
    o("<p>The <strong>From</strong> column is provenance. A row with a value "
      "there is the <em>same question</em> another topology already answered, "
      "re-asked on this rig; the value is that original check&rsquo;s ID. Put "
      "the two answers side by side and the difference is the topology, "
      "because nothing else moved. A row with no value is a question first "
      "asked here.</p>")
    o("</section>")

    for t in topos:
        key = FIGKEY.get(t)
        o('<section class="sec" id="topology-%s">' % (key or "x"))
        o(f"<h3>{esc(t)}</h3>")
        if key and key in figs:
            lead, body = FIGCAP.get(key, ("", ""))
            o("<figure>")
            o('<div class="fig-body">')
            o(figs[key])
            o("</div>")
            o(f"<figcaption><b>{esc(lead)}</b> {esc(body)}</figcaption>")
            o("</figure>")
        o('<div class="card"><table><thead><tr>'
          "<th></th><th>Check</th><th>Req</th><th>From</th><th>Expected</th>"
          "<th>Measured</th></tr></thead><tbody>")
        for r in by_topo[t]:
            mark, cls = {"PASS": ("✓", "pass"),
                         "FAIL": ("✗", "fail"),
                         "NOTE": ("○", "note")}.get(r["status"], ("", ""))
            exp = esc(r["expected"]) if r["status"] != "NOTE" else "&mdash;"
            src = r.get("from", "-")
            src_cell = ("&mdash;" if src in ("", "-")
                        else f"<code>{esc(src)}</code>")
            o(f"<tr><td class='mark {cls}'>{mark}</td>"
              f"<td class='mono'>{esc(r['id'])}</td>"
              f"<td><code>{esc(r['req'])}</code></td>"
              f"<td class='mono'>{src_cell}</td>"
              f"<td>{exp}</td>"
              f"<td class='mono'>{esc(r['actual'])}</td></tr>")
            o(f"<tr><td></td><td colspan='5'>{esc(r['desc'])}</td></tr>")
        o("</tbody></table></div>")
        o("</section>")

    # --- requirements ------------------------------------------------------
    o('<section class="sec">')
    o("<h2>The answers, requirement by requirement</h2>")
    for rid in sorted(REQS, key=req_key):
        q, a = REQS[rid]
        ids = by_req.get(rid, [])
        o('<div class="card">')
        o(f"<h3>{esc(rid)}</h3>")
        o(f"<p><strong>Asks:</strong> {md(q)}</p>")
        o(f"<p><strong>This run says:</strong> {md(a)}</p>")
        if ids:
            o("<p><em>Evidence:</em> "
              + ", ".join("<code>%s</code>" % esc(i) for i in ids) + "</p>")
        else:
            o("<p><em>Evidence:</em> none &mdash; no check in this rig covers "
              "it.</p>")
        o("</div>")
    o("</section>")

    # --- findings ----------------------------------------------------------
    o('<section class="sec">')
    o("<h2>Findings this rig added</h2>")
    o("<p>Five things came out of building the scripts that are <strong>not"
      "</strong> in the demo&rsquo;s existing evidence pages. Four are NATS "
      "behaviour. One is about measuring.</p>")
    for title, ids, body in NEW:
        o('<div class="card">')
        o(f"<h3>{md(title)}</h3>")
        o(f"<p>{md(body)}</p>")
        o("<p><em>Evidence:</em> "
          + ", ".join("<code>%s</code>" % esc(i.strip())
                      for i in ids.split(",")) + "</p>")
        o("</div>")
    o("</section>")

    # --- still open --------------------------------------------------------
    o('<section class="sec">')
    o("<h2>Still open</h2>")
    o("<p>These rows are <strong>not</strong> measured. Believe nothing in "
      "them. They are the list of what a later run has to build.</p>")
    o('<div class="card"><table><thead><tr><th>What</th>'
      "<th>Why it is still open</th></tr></thead><tbody>")
    for what, why in OPEN:
        o(f"<tr><td>{md(what)}</td><td>{md(why)}</td></tr>")
    o("</tbody></table></div>")
    o("</section>")

    # --- scripts -----------------------------------------------------------
    o('<section class="sec">')
    o("<h2>What each script builds</h2>")
    o("<p>Every script starts its own servers from nothing, measures, and "
      "stops them. Nothing is left running. All servers are bare "
      "<code>nats-server</code> processes on <code>127.0.0.1</code> &mdash; "
      "no Docker, no <code>nsc</code>, no <code>nats</code> contexts.</p>")
    o('<div class="card"><table><thead><tr><th>Script</th><th>Topology</th>'
      '<th class="num">Servers</th></tr></thead><tbody>')
    for s, t, n in (("00-islands.sh", "T1 &mdash; two regions, no link", 6),
                    ("01-gateway.sh", "T2 / A &mdash; one gateway", 6),
                    ("02-domain-over-gateway.sh",
                     "T2 / B &mdash; a domain per cluster over a gateway", 6),
                    ("03-arbiter.sh",
                     "T3 / C &mdash; gateway plus a 1-node arbiter", 7),
                    ("04-hub-and-leaf.sh",
                     "T5 / D &mdash; a hub with two leaf clusters", 9),
                    ("05-export-import.sh",
                     "T2 / E &mdash; export / import between two accounts", 6),
                    ("06-arbiter3.sh",
                     "T4 / F &mdash; gateway plus a 3-node arbiter", 9),
                    ("07-gateway-and-hub.sh",
                     "T6 / G &mdash; a gateway AND a hub leaf link", 9)):
        o(f"<tr><td><code>{s}</code></td><td>{t}</td>"
          f"<td class='num'>{n}</td></tr>")
    o("</tbody></table></div>")
    o("<p>Run one on its own the same way: <code>./01-gateway.sh</code>.</p>")
    o("</section>")

    o("</div>")
    o("</body>")
    o("</html>")


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

    if "--html" in sys.argv:
        i = sys.argv.index("--html")
        figpath = sys.argv[i + 1] if len(sys.argv) > i + 1 else "figures.html"
        render_html(env, rows, topos, by_topo, by_req,
                    npass, nfail, nnote, figpath)
        return

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

    o("## The four accounts")
    o("")
    o("An **account** is a wall around data. Two accounts cannot see each "
      "other's subjects, streams or consumers. The same four exist in every "
      "server of every topology below, so the account is never the variable "
      "— the topology is.")
    o("")
    o("| Account | Who uses it | Why it is here |")
    o("|---|---|---|")
    o("| `LB` | **shared** — both regions log in to the same one | "
      "the broken shape: one JetStream namespace across both regions, so a "
      "stream name can only exist once |")
    o("| `LB_ZA` | ZA only | ZA owns its own streams, and AU cannot see them "
      "at all |")
    o("| `LB_AU` | AU only | AU owns its own streams, and ZA cannot see them "
      "at all |")
    o("| `$SYS` | the servers | the system account, used for cluster and "
      "gateway traffic |")
    o("")
    o("Users are plain user / password pairs, and the password equals the "
      "user name. That is lab-only, bound to `127.0.0.1`, and worthless "
      "anywhere else. In `REPORT.html` an account is drawn as a **yellow "
      "dashed box** around everything it contains.")
    o("")
    o("A **JetStream domain** is a different wall, drawn in `REPORT.html` "
      "as an **orange dashed box**. It names one JetStream system — one "
      "meta group, one set of stream names. A gateway puts both regions "
      "inside *one* orange box, whatever you name it; only a **leaf-node** "
      "link can give each side an orange box of its own. That single fact "
      "is what topology `T2 / B` fails to beat.")
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
    for rid in sorted(REQS, key=req_key):
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
    o("Five things came out of building the scripts that are **not** in the "
      "demo's existing evidence pages. Four are NATS behaviour. One is about "
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
    o("The **From** column is provenance. A row with a value there is the "
      "*same question* another topology already answered, re-asked on this "
      "rig; the value is that original check's ID. Put the two answers side "
      "by side and the difference is the topology, because nothing else "
      "moved. A row with no value is a question first asked here.")
    o("")
    for t in topos:
        o(f"### {t}")
        o("")
        o("| | Check | Req | From | Expected | Measured |")
        o("|---|---|---|---|---|---|")
        for r in by_topo[t]:
            mark = {"PASS": "✅", "FAIL": "❌", "NOTE": "📋"}.get(r["status"], "")
            exp = r["expected"] if r["status"] != "NOTE" else "—"
            src = r.get("from", "-")
            src = "—" if src in ("", "-") else f"`{src}`"
            o(f"| {mark} `{r['id']}` | {r['desc']} | {r['req']} | {src} | "
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
    o("| `05-export-import.sh` | E — export / import between two accounts | 6 |")
    o("| `06-arbiter3.sh` | T4 / F — gateway plus a 3-node arbiter | 9 |")
    o("| `07-gateway-and-hub.sh` | T6 / G — a gateway AND a hub leaf link | 9 |")
    o("")
    o("Run one on its own the same way: `./01-gateway.sh`.")


main()
