#!/usr/bin/env python3
"""Turn exercise 10's kept runs into REPORT-10.md and REPORT-10.html.

Exercise 10 (lab/10-hub-meta-leader.sh) keeps every full run under
lab/run/evidence/10-<stamp>/. This script reads one or more of those folders.
The FIRST folder is the cited run: every figure and every per-step table comes
from it. The other folders appear in the provenance table and in the
cross-run table, so a count is always tied to named runs.

The same rule as render-report.py: the prose never states a number. It points
at a check ID, and the number sits in a table or a figure that the script
fills from the run.

Usage:  render-report-10.py <cited evidence dir> [more evidence dirs...]
Writes: ../REPORT-10.md and ../REPORT-10.html, beside the demo's README.md.
"""
import importlib.util, os, re, sys, textwrap

HERE = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location(
    "rr", os.path.join(HERE, "render-report.py"))
rr = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(rr)
esc, md, mark_of, is_proc = rr.esc, rr.md, rr.mark_of, rr.is_proc

LEAD = ("Hub leadership can be requested, but regional recovery can disturb "
        "it. Metadata quorum and stream availability remain separate.")

# The five return rounds. Each is (label, round start B, thaw R, verdict).
# A return verdict covers R..R+6; R+3 is "same leader", R+3b "same term".
RETURNS = (("step 3 · za returns, leader was in the hub", 42, 51, "ML58"),
           ("step 3 · au returns, leader was in the hub", 59, 68, "ML75"),
           ("step 4 · za returns, leader was elected elsewhere", 76, 85, "ML92"),
           ("step 4 · au returns, leader was elected elsewhere", 94, 103, "ML110"),
           ("step 5 · au returns after both were dark", 112, 129, "ML136"))

# The round's rig preconditions. A verdict whose precondition failed is
# inconclusive, whatever the run printed: runs before 2026-10-05 15:00 did
# not yet feed preconditions into the verdict, so this keeps old rows honest.
PRE = {"ML50": ["ML42"], "ML58": ["ML42"], "ML67": ["ML59"],
       "ML75": ["ML59"], "ML84": ["ML76"], "ML92": ["ML76"],
       "ML102": ["ML94"], "ML110": ["ML94"], "ML121": ["ML112"],
       "ML128": ["ML112", "ML122"], "ML136": ["ML112", "ML122"],
       "ML145": ["ML137"], "ML156": ["ML148"]}

VERDICTS = (("ML41", "S1 hub step-down"), ("ML50", "S2 za dark"),
            ("ML58", "S4 za returns"), ("ML67", "S2 au dark"),
            ("ML75", "S4 au returns"), ("ML84", "S3 za dark"),
            ("ML92", "S4 za returns"), ("ML102", "S3 au dark"),
            ("ML110", "S4 au returns"), ("ML121", "S5 both dark"),
            ("ML128", "S5 za back"), ("ML136", "S4 au returns"),
            ("ML145", "S6 za dark"), ("ML156", "S6 au dark"))

STEPS = (("Step 1 — build T4 and seed it", 1, 23),
         ("Step 2 — hub step-down (S1)", 24, 41),
         ("Step 3 — one region dark, leader in the hub (S2, S4)", 42, 75),
         ("Step 4 — one region dark, leader in that region (S3, S4)", 76, 111),
         ("Step 5 — both regions dark, then back one at a time (S5, S4)",
          112, 136),
         ("Step 6 — one region dark: only its stream stops (S6)", 137, 158))

REQS = {
 "D03-R13": ("Does a hub step-down put the meta leader in the hub, every time?",
   "Yes, in every trial of every kept run (`ML26`–`ML36`, verdict `ML41`), "
   "and again after a recovery (`ML93`, `ML111`). Restarting a hub server "
   "that is not the leader does not move it (`ML38`). The hub leader can be "
   "**requested**. Nothing in the config **pins** it there (`ML5a`)."),
 "D03-R14": ("One region dark, leader in the hub: does the leader stay, and "
   "do writes and metadata go on?",
   "Yes. The hub leader held through the dark window (`ML44`, `ML61`), the "
   "meta term did not move (`ML49a`, `ML66a`), and writes and the metadata "
   "probe worked (`ML45`–`ML49`, `ML62`–`ML66`). Verdicts `ML50` and `ML67`."),
 "D03-R15": ("One region dark, leader in that region: who wins, and how long "
   "does it take?",
   "A new leader is elected outside the dark region, and writes and "
   "metadata go on (`ML78`–`ML83`, `ML96`–`ML101`). Who wins and how long "
   "it took are notes, because nothing promises either (`ML78a`, `ML96a`). "
   "The winner is not always a hub server."),
 "D03-R16": ("When the region returns: does the leader stay, does the term "
   "stay, is the region current? Recovery and stability are reported apart.",
   "**Recovery** was met in every valid return round: all peers current, "
   "one leader, every message back, writes and metadata working. "
   "**Stability** was not: in some rounds the leader moved (`R+3`), or the "
   "same server came back in a higher term (`R+3b`). The cross-run table "
   "below names each round and each run. Why a return disturbs leadership "
   "is **not proved**."),
 "D03-R17": ("Both regions dark: does the hub lose the meta quorum even "
   "though it is healthy?",
   "Yes. No hub server names a leader (`ML116`, `ML117`), and creating a "
   "stream fails with `10008` (`ML120`, `ML120a`). But `ODOMETER_ARB` still "
   "takes writes, because its own three replicas are all live in arb "
   "(`ML118`, `ML119`). With one region back, quorum, a leader, writes and "
   "metadata return (`ML122`–`ML128`)."),
 "D03-R18": ("One region dark: does only that region's stream lose writes?",
   "Yes. A write to the dark region's stream is not acknowledged (`ML140`, "
   "`ML151`), while the meta leader holds and the other two streams and the "
   "metadata probe keep working (`ML139`, `ML141`–`ML144`, `ML150`, "
   "`ML152`–`ML155`). A timed-out publish has an **unknown** outcome: retry "
   "it with the same `Nats-Msg-Id` (`ML140a`, `ML146a`)."),
}

FINDINGS = [
 ("Hub leadership can be requested", "ML26, ML36, ML41, ML93, ML111, ML5a",
  "A step-down with `--cluster arb` put the meta leader in the hub in every "
  "trial. Start-up does not put it there: nothing in the config pins it."),
 ("A dark region does not move a hub leader", "ML44, ML61, ML49a, ML66a",
  "With the leader in the hub, one dark region left six voters, above the "
  "quorum. The leader held, the term did not move, and metadata and writes "
  "went on."),
 ("Regional recovery can disturb it", "ML54, ML54b, ML71, ML106, ML157a",
  "Recovery always came back. Leadership did not always stay put: on a "
  "return the leader sometimes moved out of the hub — in some rounds to "
  "the region that stayed up, in others to the returning region — and "
  "sometimes the same server came back in a higher term. The cause is "
  "not proved. Read the cross-run table before quoting a rate: the rounds "
  "are few."),
 ("Metadata quorum and stream availability are separate",
  "ML116, ML118, ML120, ML139, ML140",
  "Both regions dark: no meta leader and no create, yet the hub's own "
  "stream kept taking writes. One region dark: the meta group stayed "
  "healthy, yet that region's stream took no write. A vote does not carry "
  "data, and data does not carry a vote."),
 ("A timed-out publish is not a failed publish", "ML140a, ML146a, ML151a",
  "The CLI gives up and prints a timeout. The message may still be stored "
  "later. Retry with the same `Nats-Msg-Id`, so a stored copy is not "
  "stored twice."),
 ("A leader field can be stale", "ML116a",
  "After both regions go dark, the three hub servers stop naming the old "
  "leader one at a time, not together. Read every hub monitor, and wait."),
]

LIMITS = [
 "**`kill -STOP` is not a WAN cut.** Each dark region is a clean, total "
 "stop. A real partition, with both sides running, is not measured.",
 "**One machine, loopback only.** Every timing is a loopback timing. It "
 "says nothing about WAN latency.",
 "**Few rounds.** Each kept run has five return rounds. The counts below "
 "are counts of named rounds, not rates.",
 "**Causes are not proved.** Why a return can move the leader, or raise "
 "the term with the same leader, is not known. Do not write a cause down.",
 "**Not in these rows.** One unacknowledged write that was stored later "
 "was seen in a step-6 part run whose rows were not kept. It is recorded "
 "in `exercises/EXERCISE_OBSERVATIONS.md`, not here.",
 "**Hand runs are not here.** The terminal runs of `EXERCISE-10-TERMINAL-"
 "STEPS.md` are in `exercises/EXERCISE_OBSERVATIONS.md`.",
]


# Which script made each run. From 2026-10-05 15:20 the script writes its
# own sha256 and readiness rule into env.txt. The four runs before that are
# named here from the session record: the edits to wait_whole fell between
# them, and the script's own comments cite 14:47 and 15:08 as the reasons.
REVISIONS = {
    "10-20261005-144730": ("r1", "no all-nine /healthz wait before a round's start check"),
    "10-20261005-150304": ("r2", "wait_whole: 1 all-nine /healthz reading"),
    "10-20261005-150839": ("r2", "wait_whole: 1 all-nine /healthz reading"),
    "10-20261005-151454": ("r3", "wait_whole: 3 all-nine /healthz readings in a row"),
}


# ----------------------------------------------------------------- reading --

class Run:
    def __init__(self, path):
        self.path = path.rstrip("/")
        self.stamp = os.path.basename(self.path)
        self.rows, self.by = [], {}
        for line in open(os.path.join(self.path, "10-all.tsv")):
            parts = line.rstrip("\n").split("\t")
            if len(parts) == len(rr.COLS) - 1:
                parts.append("rig")
            if len(parts) != len(rr.COLS):
                continue
            r = dict(zip(rr.COLS, parts))
            self.rows.append(r)
            self.by[r["id"]] = r
        self.env = {}
        try:
            for line in open(os.path.join(self.path, "env.txt")):
                k, _, v = line.rstrip("\n").partition("\t")
                self.env.setdefault(k, v)
        except OSError:
            pass
        self.rel = os.path.relpath(self.path, os.path.dirname(HERE))

    def rev(self):
        """(label, readiness rule, source)."""
        if "script" in self.env:
            return (self.env["script"], self.env.get("readiness", "—"),
                    "env.txt")
        if self.stamp in REVISIONS:
            lbl, rule = REVISIONS[self.stamp]
            return (lbl, rule, "session record")
        return ("unknown", "—", "—")

    def a(self, i):
        return self.by.get(i, {}).get("actual", "")

    def st(self, i):
        return self.by.get(i, {}).get("status", "")

    def rig_fails(self):
        return [r["id"] for r in self.rows
                if not is_proc(r) and r["status"] == "FAIL"]

    def counts(self):
        return rr.counts(self.rows)

    def verdict(self, vid):
        """(word, why). A failed rig precondition makes it inconclusive."""
        r = self.by.get(vid)
        if not r:
            return ("not run", "")
        bad = [p for p in PRE.get(vid, []) if self.st(p) == "FAIL"]
        if bad and r["actual"] != "inconclusive":
            return ("inconclusive",
                    "rig precondition %s failed; the script of that time "
                    "printed “%s”" % (", ".join(bad), r["actual"]))
        return (r["actual"], r["expected"])


def nid(i):
    return "ML%d" % i


def secs(s):
    m = re.search(r"(\d+[.,]\d+)s", s)
    return m.group(1).replace(",", ".") + " s" if m else ""


def terms(s):
    m = re.search(r"(\d+) -> (\d+)", s)
    return (m.group(1), m.group(2)) if m else ("?", "?")


def cluster_of(name):
    m = re.match(r"t-(za|au|arb)-", name)
    return m.group(1) if m else ""


def stability(run, R):
    """('same' | 'term' | 'moved', moved-from) from R+3 and R+3b."""
    a3, a3b = run.a(nid(R + 3)), run.a(nid(R + 3) + "b")
    if a3.startswith("moved"):
        return ("moved", a3.split()[1])
    if a3b and a3b != "unchanged":
        return ("term", "")
    return ("same", "")


def ret_cell(run, R):
    seen, _, rest = run.a(nid(R + 3) + "a").partition(";")
    names = seen.split()
    kind, frm = stability(run, R)
    return {"leader": names[-1] if names else "?",
            "term": terms(rest)[1], "kind": kind, "from": frm,
            "ids": "%s %sb" % (nid(R + 3), nid(R + 3))}


def timeline(run):
    """One row per scenario: label, ids, and a cell per column."""
    rows = []
    for dark, B, v in (("za", 42, "ML50"), ("au", 59, "ML67")):
        t0, t1 = terms(run.a(nid(B + 7) + "a"))
        R = B + 9
        k, frm = stability(run, R)
        before = frm if k == "moved" else ret_cell(run, R)["leader"]
        held = run.a(nid(B + 2)).startswith("unchanged")
        rows.append(("S2 · %s dark" % dark, "leader in the hub",
                     "%s · %s" % (v, PRE_V[v]),
                     {"before": (before, t0),
                      "dark": (before if held else "?", t1),
                      "return": ret_cell(run, R)}))
    for dark, B, v in (("za", 76, "ML84"), ("au", 94, "ML102")):
        t0, t1 = terms(run.a(nid(B + 7) + "a"))
        R = B + 9
        new = run.a(nid(B + 2) + "a").split()
        sd = run.a(nid(B + 17) + "a").split("->")
        rows.append(("S3 · %s dark" % dark, "leader in %s" % dark,
                     "%s · %s" % (v, PRE_V[v]),
                     {"before": ("%s %s server" % ("an" if dark == "au" else "a", dark), t0),
                      "dark": (new[0] if new else "?", t1),
                      "return": ret_cell(run, R),
                      "stepdown": (sd[-1].strip() if len(sd) > 1 else "?",
                                   "")}))
    t0, t1 = terms(run.a("ML120a"))
    p = run.a("ML123a")
    rows.append(("S5 · both dark", "then za, then au back",
                 "ML121 · ML128 · ML136",
                 {"before": (run.a("ML114").split()[0] if run.a("ML114")
                             else "?", t0),
                  "both": ("no leader", t1),
                  "partial": (p.split()[0] if p else "?",
                              re.search(r"term (\d+)", p).group(1)
                              if re.search(r"term (\d+)", p) else "?"),
                  "return": ret_cell(run, 129)}))
    for dark, B, v in (("za", 137, "ML145"), ("au", 148, "ML156")):
        lead = run.a(nid(B + 2)).split()
        n = run.a(nid(B + 9) + "a")
        m = re.search(r"; (\S+) -> (\S+); term (\d+) -> (\d+)", n)
        if m:
            x, y, t0, t1 = m.groups()
            kind = ("moved" if x != y else "term" if t0 != t1 else "same")
        else:
            x = y = t0 = t1 = "?"
            kind = "same"
        rows.append(("S6 · %s dark" % dark, "only its stream stops",
                     "%s · note %sa" % (v, nid(B + 9)),
                     {"before": (x, t0),
                      "dark": (lead[0] if lead else "?", ""),
                      "return": {"leader": y, "term": t1, "kind": kind,
                                 "from": x, "ids": nid(B + 9) + "a",
                                 "note": True}}))
    return rows


PRE_V = {"ML50": "ML58", "ML67": "ML75", "ML84": "ML92", "ML102": "ML110"}

TCOLS = (("before", "BEFORE"), ("dark", "ONE DARK"), ("both", "BOTH DARK"),
         ("partial", "ZA BACK"), ("return", "ALL BACK"),
         ("stepdown", "HUB STEP-DOWN"))

KIND_TXT = {"same": ("same leader", "same term"),
            "term": ("same leader", "higher term"),
            "moved": ("moved from", None)}


# ----------------------------------------------------------- the figures --

CL = {"za": "cz", "au": "ca", "arb": "ch"}


def figure_states(run):
    """Figure 2's cells: (column, row) -> (expected text, [(label, ids)])."""
    ret = [R for _, _, R, _ in RETURNS]
    stab = [(nid(R + 3), nid(R + 3) + "b") for R in ret]
    return {
     ("healthy", "meta"): (
       "9 of 9 voters. One leader that all nine name. Create and delete work.",
       [("one leader, nine agree", ["ML3", "ML4"]),
        ("peers current", ["ML5"]),
        ("create, delete, gone", ["ML21", "ML22", "ML23"])]),
     ("healthy", "streams"): (
       "Each stream has 3 live replicas in its own cluster. All take writes.",
       [("placed as designed", ["ML10", "ML11", "ML12", "ML13"]),
        ("seed writes stored", ["ML14", "ML15", "ML16"]),
        ("read back by Direct Get", ["ML17", "ML18", "ML19"])]),
     ("dark", "meta"): (
       "6 of 9 voters, above quorum 5. A hub leader stays. A leader in the "
       "dark region is replaced.",
       [("hub leader stays", ["ML44", "ML61"]),
        ("new leader elected", ["ML78", "ML96"]),
        ("create, delete, gone", ["ML49", "ML66", "ML83", "ML101"])]),
     ("dark", "streams"): (
       "The dark region's stream has no live replica: no write. The other "
       "two streams take writes.",
       [("dark stream: no ack", ["ML140", "ML151"]),
        ("hub and other region write", ["ML45", "ML46", "ML62", "ML63"])]),
     ("both", "meta"): (
       "3 of 9 voters, below quorum. No leader. A create fails with 10008.",
       [("no leader on the hub", ["ML116", "ML117"]),
        ("create fails", ["ML120"])]),
     ("both", "streams"): (
       "ODOMETER_ARB keeps 3 live replicas in arb, so it takes writes. "
       "ZA and AU have none.",
       [("ARB writes stored", ["ML118"]),
        ("ARB reads back", ["ML119"]),
        ("ZA, AU writes: not probed", [])]),
     ("recovery", "meta"): (
       "All peers current. One leader. Wanted: the same leader in the same "
       "term.",
       [("recovery: current, one leader",
         [nid(R + 1) for R in ret] + [nid(R + 2) for R in ret]),
        ("create, delete, gone", [nid(R + 6) for R in ret])]
       + [("same leader+term, round %s" % v, list(p))
          for (_, _, _, v), p in zip(RETURNS, stab)]),
     ("recovery", "streams"): (
       "Every acked message comes back. All three streams take writes.",
       [("every message back", [nid(R + 4) for R in ret]),
        ("writes stored", [nid(R + 5) for R in ret])]),
    }


def glyph(run, ids):
    if not ids:
        return ("–", "lbl2")
    sts = [run.st(i) for i in ids]
    if any(s == "FAIL" for s in sts):
        return ("✗", "ro r")
    if all(s == "PASS" for s in sts):
        return ("✓", "ro g")
    return ("?", "lbl2")


FCOLS = (("healthy", "HEALTHY"), ("dark", "ONE REGION DARK"),
         ("both", "BOTH REGIONS DARK"), ("recovery", "ALL NINE BACK"))
FROWS = (("meta", "META GROUP", "who may change metadata"),
         ("streams", "STREAMS", "who may store a message"))


def svg_states(run):
    cells = figure_states(run)
    X0, W, GAP, TOP = 132, 206, 6, 58
    width = X0 + 4 * (W + GAP) + 6
    out, y = [], TOP
    for x_i, (_, title) in enumerate(FCOLS):
        x = X0 + x_i * (W + GAP)
        out.append(f'<text class="grp" x="{x + 10}" y="{TOP - 10}">{title}</text>')
    for rk, rtitle, rsub in FROWS:
        blocks, hmax = [], 0
        for ck, _ in FCOLS:
            exp, items = cells[(ck, rk)]
            lines = [("grp", "EXPECTED")]
            lines += [("lbl2", s) for s in textwrap.wrap(exp, 32)]
            lines.append(("gap", ""))
            lines.append(("grp", "MEASURED"))
            for lbl, ids in items:
                g, cls = glyph(run, ids)
                lines += [(cls, f"{g} {s}") if k == 0 else (cls, "  " + s)
                          for k, s in enumerate(textwrap.wrap(lbl, 29))]
                lines += [("lbl2", "  " + s)
                          for s in textwrap.wrap(" ".join(ids), 30)]
            blocks.append(lines)
            h = sum(6 if c == "gap" else 13 for c, _ in lines) + 22
            hmax = max(hmax, h)
        out.append(f'<text class="lbl" x="14" y="{y + 18}">{rtitle}</text>')
        for k, s in enumerate(textwrap.wrap(rsub, 18)):
            out.append(f'<text class="lbl2" x="14" y="{y + 34 + 13 * k}">'
                       f'{esc(s)}</text>')
        for x_i, lines in enumerate(blocks):
            x = X0 + x_i * (W + GAP)
            out.append(f'<rect class="nb clu" x="{x}" y="{y}" width="{W}" '
                       f'height="{hmax}" rx="3"/>')
            ty = y + 18
            for cls, s in lines:
                if cls == "gap":
                    ty += 6
                    continue
                out.append(f'<text class="{cls}" x="{x + 10}" y="{ty}">'
                           f'{esc(s)}</text>')
                ty += 13
        y += hmax + 12
    y += 4
    out.append(f'<text class="ro" x="14" y="{y + 10}">✓ every listed check '
               f'met  ✗ at least one not met  – not probed. Run '
               f'{esc(run.stamp)}.</text>')
    height = y + 24
    label = ("Exercise 10, failure states. Four columns: healthy, one region "
             "dark, both regions dark, all nine back. Two rows: the meta "
             "group, which decides metadata, and the streams, which store "
             "messages. Each cell gives the expected behaviour, then the "
             "check IDs measured in run %s, each marked met or not met. "
             "The meta group and the streams fail at different times."
             % run.stamp)
    return (f'<svg viewBox="0 0 {width} {height}" role="img" '
            f'aria-label="{esc(label)}">\n'
            f'<text class="grp g" x="14" y="22">FIGURE 2 &mdash; META GROUP '
            f'AND STREAMS, SHOWN APART</text>\n' + "\n".join(out) + "\n</svg>")


def svg_timeline(run):
    rows = timeline(run)
    X0, W, GAP, TOP, H = 168, 128, 4, 64, 66
    width = X0 + len(TCOLS) * (W + GAP) + 6
    out = []
    for i, (_, t) in enumerate(TCOLS):
        out.append(f'<text class="grp" x="{X0 + i * (W + GAP) + 8}" '
                   f'y="{TOP - 10}">{t}</text>')
    for j, (name, sub, ids, cells) in enumerate(rows):
        y = TOP + j * (H + 6)
        out.append(f'<text class="lbl" x="14" y="{y + 18}">{esc(name)}</text>')
        out.append(f'<text class="lbl2" x="14" y="{y + 32}">{esc(sub)}</text>')
        out.append(f'<text class="lbl2" x="14" y="{y + 45}">{esc(ids)}</text>')
        for i, (ck, _) in enumerate(TCOLS):
            x = X0 + i * (W + GAP)
            c = cells.get(ck)
            out.append(f'<rect class="nb clu" x="{x}" y="{y}" width="{W}" '
                       f'height="{H}" rx="3"/>')
            if c is None:
                out.append(f'<text class="lbl2" x="{x + W // 2}" y="{y + 37}" '
                           f'text-anchor="middle">&mdash;</text>')
                continue
            if isinstance(c, dict):
                lead, term = c["leader"], c["term"]
            else:
                lead, term = c
            cls = CL.get(cluster_of(lead), "")
            out.append(f'<text class="lbl {cls}" x="{x + 8}" y="{y + 18}">'
                       f'{esc(lead)}</text>')
            if term:
                out.append(f'<text class="lbl2" x="{x + 8}" y="{y + 31}">'
                           f'term {esc(term)}</text>')
            if isinstance(c, dict):
                a, b = KIND_TXT[c["kind"]]
                b = b or c["from"]
                rc = "ro g" if c["kind"] == "same" else "ro r"
                if c.get("note"):
                    rc = "ro" if c["kind"] == "same" else "ro a"
                out.append(f'<text class="{rc}" x="{x + 8}" y="{y + 45}">'
                           f'{esc(a)}</text>')
                out.append(f'<text class="{rc}" x="{x + 8}" y="{y + 58}">'
                           f'{esc(b)}{" (note)" if c.get("note") else ""}'
                           f'</text>')
    y = TOP + len(rows) * (H + 6) + 6
    out.append(f'<text class="ro" x="14" y="{y + 10}">Leader colour = its '
               f'cluster: <tspan class="ch">arb</tspan> '
               f'<tspan class="cz">za</tspan> <tspan class="ca">au</tspan>. '
               f'Wanted on return: same leader, same term. Run '
               f'{esc(run.stamp)}.</text>')
    out.append(f'<text class="ro" x="14" y="{y + 26}">S6 does not check the '
               f'leader on return; its return cell is a note, in amber when '
               f'the leader or term moved.</text>')
    height = y + 40
    label = ("Exercise 10, leadership timeline for run %s. One row per "
             "scenario: S2 with za and au dark while the leader is in the "
             "hub, S3 with the leader in the region that goes dark, S5 with "
             "both regions dark and then back one at a time, and S6. Columns "
             "show the meta leader and the meta term before, while dark, "
             "with both dark, with za back, with all nine back, and after a "
             "hub step-down. Each return cell says whether the same leader "
             "stayed in the same term, the same leader came back in a higher "
             "term, or the leader moved." % run.stamp)
    return (f'<svg viewBox="0 0 {width} {height}" role="img" '
            f'aria-label="{esc(label)}">\n'
            f'<text class="grp g" x="14" y="22">FIGURE 3 &mdash; WHO LEADS, '
            f'IN WHICH TERM, AT EACH STAGE</text>\n'
            + "\n".join(out) + "\n</svg>")


# ------------------------------------------------------------- shared bits --

def cross_run(runs):
    """[(stamp, [(word, why)...])] in VERDICTS order."""
    return [(r, [r.verdict(v) for v, _ in VERDICTS]) for r in runs]


def stability_tally(runs):
    """Rows of (run, round label, verdict id, outcome) and the totals."""
    rows, met, valid, excl = [], 0, 0, 0
    for run in runs:
        for lbl, B, R, v in RETURNS:
            word, why = run.verdict(v)
            if word == "inconclusive" or word == "not run":
                rows.append((run, lbl, v, "excluded — " + word, why))
                excl += 1
                continue
            k, frm = stability(run, R)
            valid += 1
            if k == "same":
                met += 1
                rows.append((run, lbl, v, "same leader, same term",
                             "%s, %sb" % (nid(R + 3), nid(R + 3))))
            elif k == "term":
                rows.append((run, lbl, v, "same leader, higher term",
                             "%s: %s" % (nid(R + 3) + "b",
                                         run.a(nid(R + 3) + "b"))))
            else:
                rows.append((run, lbl, v, "leader moved",
                             "%s: %s" % (nid(R + 3), run.a(nid(R + 3)))))
    return rows, met, valid, excl


SHORT = {"same leader, same term": "held", "leader moved": "moved",
         "same leader, higher term": "term rose"}


def by_scenario(runs):
    """[(round label, verdict, [(run, word)], held, valid)] — no pooling
    across scenarios: each return round is its own question."""
    rows, _, _, _ = stability_tally(runs)
    out = []
    for lbl, B, R, v in RETURNS:
        cells = [(run, SHORT.get(o, "inconclusive" if o.startswith("excluded")
                                 else o)) for run, l, _, o, _ in rows if l == lbl]
        held = sum(1 for _, w in cells if w == "held")
        valid = sum(1 for _, w in cells if w != "inconclusive")
        out.append((lbl, v, cells, held, valid))
    return out


SCEN_NOTE = ("Each row is one return scenario. The runs span script "
             "revisions (see Run provenance), so a column is not a repeat "
             "of the same script. The rounds are few. These counts say what "
             "happened in named runs; they cannot say how often leadership "
             "stays in the hub.")


def provenance(runs):
    out = []
    for i, r in enumerate(runs):
        rp, rf, n, pm, pn = r.counts()
        nv = sum(1 for x in r.rows if x["status"] == "VERDICT")
        if i == 0:
            role = "cited — every figure and per-step table"
        elif rf:
            role = ("not cited — rig check %s failed"
                    % ", ".join(r.rig_fails()))
        else:
            role = "not cited — counted in the cross-run tables"
        out.append({"run": r, "role": role, "rp": rp, "rf": rf, "n": n,
                    "pm": pm, "pn": pn, "nv": nv})
    return out


# --------------------------------------------------------------- markdown --

def render_md(runs, fh):
    run = runs[0]
    o = lambda s="": print(s, file=fh)
    o("# Demo 03 — exercise 10: hub meta-leader on T4")
    o()
    o("> GENERATED by `lab/render-report-10.py` from kept evidence. "
      "Do not edit by hand.")
    o()
    o(f"**{LEAD}**")
    o()
    o("Every number here comes from a kept run. The words never state a "
      "number — they point at a check ID. Expected behaviour and measured "
      "results are kept apart: **Expected** is the design, **Measured** is "
      "what the machine did.")
    o()
    o("## Run provenance")
    o()
    o("| Run | Role | Script | Readiness wait | When | Ended | Server | CLI "
      "| Machine | Rig checks | Procedure checks | Verdicts | Notes |")
    o("|---|---|---|---|---|---|---|---|---|---|---|---|---|")
    for p in provenance(runs):
        r = p["run"]
        rv = r.rev()
        o(f"| `{r.stamp}` | {p['role']} | {rv[0]} ({rv[2]}) | {rv[1]} "
          f"| {r.env.get('when', '—')} "
          f"| {r.env.get('ended', '—')} | {r.env.get('nats-server', '—')} "
          f"| {r.env.get('nats-cli', '—')} | {r.env.get('host', '—')} "
          f"| {p['rp']} passed, {p['rf']} failed | {p['pm']} met, "
          f"{p['pn']} not met | {p['nv']} | {p['n']} |")
    o()
    for r in runs:
        for k in ("kept", "gap"):
            if k in r.env:
                o(f"- `{r.stamp}` — **{k}:** {r.env[k]}")
    o()
    o("Evidence folders are local and gitignored: "
      + ", ".join(f"`{r.rel}/`" for r in runs) + ".")
    o()
    o("## Findings")
    o()
    for title, ids, body in FINDINGS:
        o(f"### {title}")
        o()
        o(body)
        o()
        o("*Evidence:* " + ", ".join(f"`{i.strip()}`"
                                     for i in ids.split(",")))
        o()
    o("## Figure 1 — the rig")
    o()
    o("T4: clusters `za`, `au` and the hub `arb`, three servers each, joined "
      "by gateways. One meta group of nine voters, quorum five. No "
      "`jetstream.domain` is set. `ODOMETER_ZA` lives in za, `ODOMETER_AU` "
      "in au, `ODOMETER_ARB` and KV `t7-vehicles` in arb, each with three "
      "replicas. The HTML edition draws it.")
    o()
    o(f"Measured in run `{run.stamp}`:")
    o()
    o("| | Check | Expected | Measured | What |")
    o("|---|---|---|---|---|")
    for i in ("ML1", "ML2", "ML4", "ML5", "ML5a", "ML10", "ML11", "ML12",
              "ML13"):
        r = run.by.get(i)
        if r:
            o(f"| {mark_of(r)[0]} | `{i}` | {r['expected']} "
              f"| `{r['actual']}` | {r['desc']} |")
    o()
    o("## Figure 2 — failure states: meta group and streams, shown apart")
    o()
    o(f"Measured in run `{run.stamp}`. ✓ every listed check met, ✗ at least "
      "one not met, – not probed.")
    o()
    cells = figure_states(run)
    for rk, rtitle, rsub in FROWS:
        o(f"**{rtitle}** — {rsub}")
        o()
        o("| | Expected | Measured |")
        o("|---|---|---|")
        for ck, ct in FCOLS:
            exp, items = cells[(ck, rk)]
            meas = "<br>".join(
                f"{glyph(run, ids)[0]} {lbl}"
                + (" (" + " ".join(f"`{i}`" for i in ids) + ")" if ids else "")
                for lbl, ids in items)
            o(f"| {ct[0] + ct[1:].lower()} | {exp} | {meas} |")
        o()
    o("## Figure 3 — leadership timeline: leader and term")
    o()
    o(f"Measured in run `{run.stamp}`. Wanted on return: the same leader in "
      "the same term. S6 does not check the leader on return; its return "
      "cell is a note.")
    o()
    o("| Scenario | Checks | " + " | ".join(t[0] + t[1:].lower() for _, t in TCOLS)
      + " |")
    o("|---|---|" + "---|" * len(TCOLS))
    for name, sub, ids, cells_ in timeline(run):
        row = [f"**{name}** — {sub}", ids]
        for ck, _ in TCOLS:
            c = cells_.get(ck)
            if c is None:
                row.append("—")
            elif isinstance(c, dict):
                a, b = KIND_TXT[c["kind"]]
                row.append(f"`{c['leader']}` term {c['term']} — {a} "
                           f"{b or c['from']}"
                           + (" (note)" if c.get("note") else ""))
            else:
                row.append(f"`{c[0]}`" + (f" term {c[1]}" if c[1] else ""))
        o("| " + " | ".join(row) + " |")
    o()
    md_cross(runs, o)
    o("## The answers, requirement by requirement")
    o()
    for rid in sorted(REQS, key=rr.req_key):
        q, a = REQS[rid]
        ids = [r["id"] for r in run.rows if r["req"] == rid]
        o(f"### {rid}")
        o()
        o(f"**Asks:** {q}")
        o()
        o(f"**The kept runs say:** {a}")
        o()
        o("*Evidence:* " + ", ".join(f"`{i}`" for i in ids))
        o()
    o(f"## Every check, run `{run.stamp}`")
    o()
    for title, lo, hi in STEPS:
        o(f"### {title}")
        o()
        o("| | Check | Kind | Expected | Measured | What |")
        o("|---|---|---|---|---|---|")
        for r in step_rows(run, lo, hi):
            exp = r["expected"] if r["status"] != "NOTE" else "—"
            kind = ("verdict" if r["status"] == "VERDICT"
                    else r["kind"])
            o(f"| {mark_of(r)[0]} | `{r['id']}` | {kind} | {exp} "
              f"| `{r['actual']}` | {r['desc']} |")
        o()
    o("## Limits")
    o()
    for s in LIMITS:
        o(f"- {s}")
    o()
    o("## Reproduce")
    o()
    o("```bash")
    for s in REPRO:
        o(s)
    o("```")


REPRO = ("demos/03-multi-cluster-and-accounts/exercises/ex10-check.sh",
         "python3 demos/03-multi-cluster-and-accounts/lab/render-report-10.py "
         "demos/03-multi-cluster-and-accounts/lab/run/evidence/10-<stamp> "
         "[more kept runs...]")


def step_rows(run, lo, hi):
    out = []
    for r in run.rows:
        m = re.match(r"ML(\d+)", r["id"])
        if m and lo <= int(m.group(1)) <= hi:
            out.append(r)
    return out


def md_cross(runs, o):
    o("## Across the kept runs")
    o()
    o("One verdict per scenario per run. A verdict whose rig precondition "
      "failed is **inconclusive**, even where the script of that time "
      "printed another word.")
    o()
    o("| Verdict | Scenario | " + " | ".join(f"`{r.stamp}`" for r in runs)
      + " |")
    o("|---|---|" + "---|" * len(runs))
    table = cross_run(runs)
    for k, (vid, name) in enumerate(VERDICTS):
        o(f"| `{vid}` | {name} | "
          + " | ".join(vs[k][0] for _, vs in table) + " |")
    o()
    for run, vs in table:
        for k, (vid, _) in enumerate(VERDICTS):
            if vs[k][0] == "inconclusive":
                o(f"- `{run.stamp}` `{vid}`: inconclusive — {vs[k][1]}.")
    o()
    rows, met, valid, excl = stability_tally(runs)
    o("### Leadership stability on return, round by round")
    o()
    o("Recovery and stability are reported apart. This table is stability "
      "only: was it the same leader, in the same term, after the region "
      "came back?")
    o()
    o("| Run | Round | Verdict | Outcome | From |")
    o("|---|---|---|---|---|")
    for run, lbl, v, out, why in rows:
        o(f"| `{run.stamp}` | {lbl} | `{v}` | {out} | {why} |")
    o()
    o("### By scenario")
    o()
    o(SCEN_NOTE)
    o()
    o("| Scenario | Verdict | " + " | ".join(
        f"`{r.stamp}` ({r.rev()[0]})" for r in runs) + " | Held / valid |")
    o("|---|---|" + "---|" * len(runs) + "---|")
    for lbl, v, cells, held, valid in by_scenario(runs):
        o(f"| {lbl} | `{v}` | " + " | ".join(w for _, w in cells)
          + f" | {held} of {valid} |")
    o()


# ------------------------------------------------------------------- html --

EXTRA_CSS = """<style>
.cz { fill: #4d94ff; } .ca { fill: #c4a5ff; } .ch { fill: #2dd4bf; }
.ro.a { fill: var(--warn); }
.lead { font-size: 15px; line-height: 22px; color: var(--text); max-width: 74ch; margin: 0; }
</style>
"""


def head():
    h = rr.HEAD.replace(
        "<title>Demo 03 &mdash; validation report</title>",
        "<title>Exercise 10 report</title>").replace(
        "GENERATED by lab/run-all.sh html.",
        "GENERATED by lab/render-report-10.py.")
    return h.replace("</head>", EXTRA_CSS + "</head>")


def render_html(runs, fh):
    run = runs[0]
    figs = rr.load_figures(os.path.join(HERE, "figures-10.html"))
    o = lambda s="": print(s, file=fh)
    o(head().rstrip("\n"))
    o('<header class="head"><div class="head-txt">')
    o('<div class="eyebrow">Demo 03 &middot; exercise 10 &middot; generated '
      'by lab/render-report-10.py</div>')
    o("<h1>Hub meta-leader on T4</h1>")
    o(f'<p class="lead"><strong>{esc(LEAD)}</strong></p>')
    o('<p class="sub">Every number on this page comes from a kept run. The '
      'words never state a number &mdash; they point at a check ID. '
      '<strong>Expected</strong> is the design. <strong>Measured</strong> '
      'is what the machine did.</p>')
    o("</div></header>")

    o('<section class="sec"><h2>Run provenance</h2>')
    o('<div class="card"><table><thead><tr><th>Run</th><th>Role</th>'
      '<th>Script</th><th>When</th><th>Server</th><th>CLI</th><th>Machine</th>'
      '<th class="num">Rig</th><th class="num">Procedure</th>'
      '<th class="num">Verdicts</th></tr></thead><tbody>')
    for p in provenance(runs):
        r = p["run"]
        rv = r.rev()
        o(f"<tr><td class='mono'>{esc(r.stamp)}</td><td>{esc(p['role'])}</td>"
          f"<td><b>{esc(rv[0])}</b> <span class='note'>({esc(rv[2])})</span>"
          f"<br>{esc(rv[1])}</td>"
          f"<td>{esc(r.env.get('when', '—'))}<br>to "
          f"{esc(r.env.get('ended', '—'))}</td>"
          f"<td><code>{esc(r.env.get('nats-server', '—'))}</code></td>"
          f"<td><code>{esc(r.env.get('nats-cli', '—'))}</code></td>"
          f"<td>{esc(r.env.get('host', '—'))}</td>"
          f"<td class='num'><span class='pass'>{p['rp']}</span> / "
          f"<span class='{'fail' if p['rf'] else 'note'}'>{p['rf']}</span></td>"
          f"<td class='num'>{p['pm']} / "
          f"<span class='{'fail' if p['pn'] else 'note'}'>{p['pn']}</span></td>"
          f"<td class='num'>{p['nv']}</td></tr>")
    o("</tbody></table></div>")
    o("<p>Rig and procedure columns read <em>passed / failed</em> and "
      "<em>met / not met</em>. A failed procedure check is an answer, not a "
      "broken rig.</p>")
    notes = [(r, k) for r in runs for k in ("kept", "gap") if k in r.env]
    if notes:
        o("<ul>" + "".join(
            f"<li><code>{esc(r.stamp)}</code> &mdash; <b>{k}:</b> "
            f"{esc(r.env[k])}</li>" for r, k in notes) + "</ul>")
    o("<p>Evidence folders are local and gitignored: "
      + ", ".join(f"<code>{esc(r.rel)}/</code>" for r in runs) + ".</p>")
    o("</section>")

    o('<section class="sec"><h2>Findings</h2>')
    for title, ids, body in FINDINGS:
        o(f'<div class="card"><h3>{md(title)}</h3><p>{md(body)}</p>'
          "<p><em>Evidence:</em> " + ", ".join(
              f"<code>{esc(i.strip())}</code>" for i in ids.split(","))
          + "</p></div>")
    o("</section>")

    o('<section class="sec"><h2>Figure 1 &mdash; the rig</h2>')
    if "topo" in figs:
        o('<figure><div class="fig-body">')
        o(figs["topo"])
        o("</div><figcaption><b>The design.</b> Three clusters, one meta "
          "group of nine, no JetStream domain. Each stream keeps all three "
          "replicas inside one cluster. The table under the figure is what "
          f"run <code>{esc(run.stamp)}</code> measured.</figcaption>"
          "</figure>")
    html_table(run, [run.by[i] for i in ("ML1", "ML2", "ML4", "ML5", "ML5a",
                                         "ML10", "ML11", "ML12", "ML13")
                     if i in run.by], o)
    o("</section>")

    o('<section class="sec"><h2>Figure 2 &mdash; failure states</h2>')
    o('<figure><div class="fig-body">')
    o(svg_states(run))
    o("</div><figcaption><b>The meta group and the streams fail at different "
      "times.</b> Both regions dark: no meta leader, yet the hub stream "
      "writes. One region dark: a healthy meta group, yet that region's "
      "stream takes no write. <b>Expected</b> is the design; "
      "<b>Measured</b> lists the check IDs of run "
      f"<code>{esc(run.stamp)}</code>.</figcaption></figure>")
    o("</section>")

    o('<section class="sec"><h2>Figure 3 &mdash; leadership timeline</h2>')
    o('<figure><div class="fig-body">')
    o(svg_timeline(run))
    o("</div><figcaption><b>Recovery comes back; leadership may not stay.</b> "
      "A term rises on every election attempt, won or not, so a higher term "
      "means a disturbance, not a count of elections. The leader names "
      "come from notes and checks of run "
      f"<code>{esc(run.stamp)}</code>.</figcaption></figure>")
    o("</section>")

    html_cross(runs, o)

    o('<section class="sec"><h2>The answers, requirement by requirement</h2>')
    for rid in sorted(REQS, key=rr.req_key):
        q, a = REQS[rid]
        ids = [r["id"] for r in run.rows if r["req"] == rid]
        o(f'<div class="card"><h3>{esc(rid)}</h3>'
          f"<p><strong>Asks:</strong> {md(q)}</p>"
          f"<p><strong>The kept runs say:</strong> {md(a)}</p>"
          "<p><em>Evidence:</em> " + ", ".join(
              f"<code>{esc(i)}</code>" for i in ids) + "</p></div>")
    o("</section>")

    o(f'<section class="sec"><h2>Every check, run '
      f'<code>{esc(run.stamp)}</code></h2>')
    for title, lo, hi in STEPS:
        o(f"<h3>{esc(title)}</h3>")
        html_table(run, step_rows(run, lo, hi), o)
    o("</section>")

    o('<section class="sec"><h2>Limits</h2><ul>')
    for s in LIMITS:
        o(f"<li>{md(s)}</li>")
    o("</ul></section>")
    o('<section class="sec"><h2>Reproduce</h2><pre>'
      + esc("\n".join(REPRO)) + "</pre></section>")
    o("</div>\n</body>\n</html>")


def html_table(run, rows, o):
    o('<div class="card"><table><thead><tr><th></th><th>Check</th>'
      "<th>Req</th><th>Expected</th><th>Measured</th><th>Extra info</th>"
      "</tr></thead><tbody>")
    for r in rows:
        mark, cls = mark_of(r)
        exp = esc(r["expected"]) if r["status"] != "NOTE" else "&mdash;"
        extra = rr.err_extra(r)
        o(f"<tr><td class='mark {cls}'>{mark}</td>"
          f"<td class='mono'>{esc(r['id'])}</td>"
          f"<td><code>{esc(r['req'])}</code></td><td>{exp}</td>"
          f"<td class='mono'>{esc(r['actual'])}</td>"
          f"<td class='extra'>{esc(extra) if extra else '&mdash;'}</td></tr>")
        tag = ("<strong>verdict</strong> &middot; " if r["status"] == "VERDICT"
               else "<em>procedure</em> &middot; " if is_proc(r) else "")
        o(f"<tr><td></td><td colspan='5'>{tag}{esc(r['desc'])}</td></tr>")
    o("</tbody></table></div>")


def html_cross(runs, o):
    o('<section class="sec"><h2>Across the kept runs</h2>')
    o("<p>One verdict per scenario per run. A verdict whose rig precondition "
      "failed is <strong>inconclusive</strong>, even where the script of "
      "that time printed another word.</p>")
    o('<div class="card"><table><thead><tr><th>Verdict</th><th>Scenario</th>'
      + "".join(f"<th>{esc(r.stamp)}</th>" for r in runs)
      + "</tr></thead><tbody>")
    table = cross_run(runs)
    for k, (vid, name) in enumerate(VERDICTS):
        cells = ""
        for _, vs in table:
            w = vs[k][0]
            cls = ("fail" if w == "failed" else
                   "note" if w in ("inconclusive", "not run") else "pass")
            cells += f"<td class='{cls}'>{esc(w)}</td>"
        o(f"<tr><td class='mono'>{vid}</td><td>{esc(name)}</td>{cells}</tr>")
    o("</tbody></table></div>")
    inc = [(run, vid, vs[k][1]) for run, vs in table
           for k, (vid, _) in enumerate(VERDICTS)
           if vs[k][0] == "inconclusive"]
    if inc:
        o("<ul>" + "".join(
            f"<li><code>{esc(r.stamp)}</code> <code>{v}</code>: inconclusive "
            f"&mdash; {esc(why)}.</li>" for r, v, why in inc) + "</ul>")
    rows, met, valid, excl = stability_tally(runs)
    o("<h3>Leadership stability on return, round by round</h3>")
    o("<p>Recovery and stability are reported apart. This table is "
      "stability only: was it the same leader, in the same term, after the "
      "region came back?</p>")
    o('<div class="card"><table><thead><tr><th>Run</th><th>Round</th>'
      "<th>Verdict</th><th>Outcome</th><th>From</th></tr></thead><tbody>")
    for run, lbl, v, out, why in rows:
        cls = ("pass" if out.startswith("same leader, same") else
               "note" if out.startswith("excluded") else "fail")
        o(f"<tr><td class='mono'>{esc(run.stamp)}</td><td>{esc(lbl)}</td>"
          f"<td class='mono'>{v}</td><td class='{cls}'>{esc(out)}</td>"
          f"<td>{esc(why)}</td></tr>")
    o("</tbody></table></div>")
    o("<h3>By scenario</h3>")
    o(f"<p>{esc(SCEN_NOTE)}</p>")
    o('<div class="card"><table><thead><tr><th>Scenario</th><th>Verdict</th>'
      + "".join(f"<th>{esc(r.stamp)}<br>{esc(r.rev()[0])}</th>" for r in runs)
      + '<th class="num">Held / valid</th></tr></thead><tbody>')
    for lbl, v, cells, held, valid in by_scenario(runs):
        tds = "".join(
            f"<td class='{'pass' if w == 'held' else 'note' if w == 'inconclusive' else 'fail'}'>"
            f"{esc(w)}</td>" for _, w in cells)
        o(f"<tr><td>{esc(lbl)}</td><td class='mono'>{v}</td>{tds}"
          f"<td class='num'>{held} of {valid}</td></tr>")
    o("</tbody></table></div>")
    o("</section>")


def main():
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    runs = [Run(p) for p in sys.argv[1:]]
    demo = os.path.dirname(HERE)
    with open(os.path.join(demo, "REPORT-10.md"), "w") as fh:
        render_md(runs, fh)
    with open(os.path.join(demo, "REPORT-10.html"), "w") as fh:
        render_html(runs, fh)
    print("wrote REPORT-10.md and REPORT-10.html from "
          + ", ".join(r.stamp for r in runs))


if __name__ == "__main__":
    main()
