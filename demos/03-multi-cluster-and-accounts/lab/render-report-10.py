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
_cspec = importlib.util.spec_from_file_location(
    "cls", os.path.join(HERE, "classify-10.py"))
CLS = importlib.util.module_from_spec(_cspec)
_cspec.loader.exec_module(CLS)

# The finding, word for word, in the exercise, the observations, this
# report and card 14. Change it in all four or in none.
FINDING = ("During some regional recoveries, the metadata group entered a new "
           "election term and sometimes elected a different leader. In one "
           "diagnostic run, logs showed returning servers requesting votes with "
           "higher terms, causing the incumbent leader to step down. Why those "
           "servers initiated elections remains unproved, and this sequence has "
           "not been confirmed in normal runs.")
SIM = ("Every dark region here is `kill -STOP` (SIGSTOP) on its three "
       "processes, and every return is `kill -CONT` (SIGCONT): not a real "
       "network partition, and not a restart.")
HYPOTHESIS = ("**Hypothesis, not a finding:** the returning servers' election "
              "timers ran out while they were stopped, so they campaign the "
              "moment they resume. The watcher's readings fit this, but fitting "
              "is not proof.")

LEAD = ("Hub leadership can be requested, but regional recovery can disturb "
        "it. " + FINDING + " Metadata quorum and stream availability remain "
        "separate.")

# The five S4 return rounds (step 6's two are STEP6). Each is (label, round start B, thaw R, verdict).
# R..R+6 are the round's checks; R+3 is "same leader", R+3b "same term".
# Runs with a classifier (env.txt line `classifier`) give three verdicts,
# R+7a experiment, R+7b recovery, R+7c stability. Older runs gave one
# combined verdict at R+7, kept as recorded and reassessed from their rows.
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

# The verdicts that are one word per run. The S4 returns are not here: they
# have three outcomes each, in their own table.
VERDICTS = (("ML41", "S1 hub step-down"), ("ML50", "S2 za dark"),
            ("ML67", "S2 au dark"), ("ML84", "S3 za dark"),
            ("ML102", "S3 au dark"), ("ML121", "S5 both dark"),
            ("ML128", "S5 za back"),
            ("ML145", "S6 za dark"), ("ML156", "S6 au dark"))

DARK = {51: "za", 68: "au", 85: "za", 103: "au", 129: "au"}

# The two step 6 returns: (label, round start B, thaw R). Step 6 asks about
# the region's stream (S6), not the leader. A run with a classifier still
# records the leader's three outcomes for it, as note ML<R>d. Its recovery
# word is META recovery only (ML<R>, then all nine agree); the stream's own
# recovery is check ML<R+1>.
STEP6 = (("step 6 · za returns, only its stream had stopped", 137, 146),
         ("step 6 · au returns, only its stream had stopped", 148, 157))
THAWS = {**DARK, 146: "za", 157: "au"}
ALL9 = ["t-%s-%d" % (c, i) for c in ("za", "au", "arb") for i in (1, 2, 3)]

# What a run without a classifier could not see. Said once per such run.
OLD_LIMITS = ("no ps check of stop and resume (the rig's dark check and "
              "nine answering monitors stood in); the leader before the thaw "
              "is the one that script read, not a two-reading baseline from "
              "all live servers; each term is one server's reading; no "
              "watcher, so a no-leader interval or a failed election attempt "
              "between the two readings is not seen")

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
   "**Stability** is a separate requirement, and it was not always met. "
   + FINDING + " A higher term with the same leader is still a disturbance: "
   "a higher term means at least one election was attempted, and the size "
   "of the rise is not a count of elections. Where no election was seen, "
   "the report says \"no election observed during the observation "
   "window\" — never \"no election\". The cross-run table below names "
   "each round and each run. " + SIM),
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
  FINDING + " Recovery came back in every valid round; stability is "
  "reported apart from it. A moved leader went to the region that stayed "
  "up in some rounds and to the returning region in others. Read the "
  "cross-run table before quoting a rate: the rounds are few."),
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
 "**SIGSTOP/SIGCONT, not a partition or a restart.** " + SIM + " A "
 "stopped process is a clean, total stop, timers included. A real "
 "partition, with both sides running, and a restart are not measured.",
 "**One machine, loopback only.** Every timing is a loopback timing. It "
 "says nothing about WAN latency.",
 "**Few rounds.** Each kept run has seven recovery windows: five S4 "
 "returns and two step 6 returns. The counts below "
 "are counts of named rounds, not rates.",
 "**Why returning servers start elections is not proved.** In the "
 "normal runs, the log level does not show which server starts the new "
 "term. A diagnostic run, where kept, matches each step-down to a "
 "higher-term vote request in its own section. It is not cited, its "
 "debug logging changes the timing, no normal run confirms its "
 "sequence, and it does not log why a returning server campaigns. "
 + HYPOTHESIS,
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

    def classifier(self):
        """The classifier revision that made the run's return verdicts."""
        m = re.search(r"revision (\w+)", self.env.get("classifier", ""))
        return m.group(1) if m else None

    def revisions(self):
        """Every classifier revision a row names, in order. A run can hold
        more than one: classify-10.py is run fresh for each round, so an
        edit during a run changes the rules for the rounds after it."""
        seen = []
        for x in self.rows:
            for c in re.findall(r"classifier (c\d+)", x["expected"] + " " + x["actual"]):
                if c not in seen:
                    seen.append(c)
        return seen or ([self.classifier()] if self.classifier() else [])

    def round_rev(self, R):
        """The revision that recorded return round R (from ML<R+7>a)."""
        m = re.search(r"classifier (c\d+)", self.by.get(nid(R + 7) + "a", {}).get("expected", ""))
        return m.group(1) if m else self.classifier()

    def diagnostic(self):
        return self.env.get("diagnostic", "off").startswith("on")

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


def reread(run, R, v):
    """Classify one round again, with today's rules, from its kept readings.

    Returns (result, limits) or None when the run kept no readings. A run
    that did not keep its thaw time opens the window at the watcher's first
    poll, which began before the CONT: the stability outcome is unaffected
    (those polls are before anything changed), but no time after the thaw
    is quoted from it.
    """
    tag = os.path.join(run.path, "obs", nid(R))
    w = CLS.load(tag + "-watch.jsonl")
    if not w:
        return None
    lim = "thaw time kept"
    try:
        thaw = float(open(tag + ".thaw").read().strip())
    except (OSError, ValueError):
        thaw = min(r["t0"] for r in w)
        lim = ("thaw time not kept: the window opens at the watcher's first "
               "poll, before the CONT; no time after the thaw is quoted")
    frozen = ["t-%s-%d" % (THAWS[R], i) for i in (1, 2, 3)]
    live = [s for s in ALL9 if s not in frozen]
    checks = {r["id"]: r["status"] for r in run.rows}
    if R in DARK:
        ids = ([nid(R) + "a", nid(R) + "c"], PRE.get(v, []) + [nid(R) + "b"],
               [nid(R + k) for k in (0, 1, 2, 4, 5, 6)])
    else:   # step 6: the IDs 10-hub-meta-leader.sh passes for ML<R>d
        B = [b_ for _, b_, r_ in STEP6 if r_ == R][0]
        ids = ([nid(R) + "b", nid(R) + "c"], [nid(B)], [nid(R)])
    res = CLS.classify_round(
        CLS.load(tag + "-baseline.jsonl"), w, CLS.load(tag + "-final.jsonl"),
        live, frozen, checks, thaw, 60, *ids)
    if lim != "thaw time kept":
        # The interval exists; its times are measured from the wrong zero.
        res["stability_why"] = re.sub(
            r"no-leader interval seen: [^;]*",
            "no-leader interval seen (times not quoted: thaw time not kept)",
            res["stability_why"])
    return res, lim


def recovery_from_rows(run, R, exp):
    if exp != "valid":
        return "inconclusive", "experiment " + exp
    ids = [nid(R + k) for k in (0, 1, 2, 4, 5, 6)]
    bad = [i for i in ids if run.st(i) == "FAIL"]
    miss = [i for i in ids if run.st(i) not in ("PASS", "FAIL")]
    if bad:
        return "not recovered", ", ".join(bad) + " not met"
    if miss:
        return "inconclusive", ", ".join(miss) + " has no result"
    return "recovered", "every recovery check met (%s–%s)" % (ids[0], ids[-1])


def outcomes(run, R, v):
    """The three outcomes of one return round, kept apart, plus their basis.

    kind is for figure 3: same | term | moved | gap | ends | unknown.
    """
    c = run.round_rev(R) if run.classifier() else None
    a3, a3b = run.a(nid(R + 3)), run.a(nid(R + 3) + "b")
    if c:
        rows = [run.by.get(nid(R + 7) + k) for k in "abc"]
        o = {}
        for key, r in zip(("experiment", "recovery", "stability"), rows):
            o[key] = r["actual"] if r else "not run"
            o[key + "_why"] = r["expected"] if r else ""
        o["recorded"] = "%s / %s / %s (classifier %s)" % (
            o["experiment"], o["recovery"], o["stability"], c)
        o["basis"] = "recorded, classifier %s" % c
        o["limits"] = ""
        if c != CLS.REVISION:
            got = reread(run, R, v)
            if got:
                res, lim = got
                was = o["stability"]
                o["stability"] = res["stability"]
                o["stability_why"] = res["stability_why"]
                o["basis"] = reassessed(c, was, res["stability"])
                o["limits"] = lim
            else:
                o["limits"] = "not reassessed: no kept readings"
        n = run.a(nid(R + 3) + "a")
        b = re.search(r"baseline (\S+) term (\S+)", n)
        f = re.search(r"final (\S+) term (\S+)", n)
        L0, T0 = b.groups() if b else ("?", "?")
        L1, T1 = f.groups() if f else ("?", "?")
        if o["experiment"] != "valid" or "?" in (L0, L1):
            kind = "unknown"
        elif L1 != L0:
            kind = "moved"
        elif T1 != T0:
            kind = "term"
        elif o["stability"] == "disturbed":
            kind = "gap"
        elif o["stability"] == "stable":
            kind = "same"
        else:
            kind = "unknown"
        o.update(kind=kind, before=L0, leader=L1, term=T1, frm=L0)
        return o

    # No classifier: that script's rows, read with today's three outcomes.
    pre = PRE.get(v, [])
    bad = [p for p in pre if run.st(p) == "FAIL"]
    miss = [p for p in pre if run.st(p) not in ("PASS", "FAIL")]
    if bad or miss:
        exp = "inconclusive"
        exp_why = "start check %s %s" % (", ".join(bad + miss),
                                         "failed" if bad else "has no result")
    else:
        exp, exp_why = "valid", "start check %s met; freeze proved by the rig" % ", ".join(pre)
    rec, rec_why = recovery_from_rows(run, R, exp)
    seen, _, rest = run.a(nid(R + 3) + "a").partition(";")
    names = seen.split()
    if exp != "valid":
        st, st_why, kind = "unknown", "experiment " + exp, "unknown"
    elif a3.startswith("moved"):
        st, st_why, kind = "disturbed", "%s: %s" % (nid(R + 3), a3), "moved"
    elif a3b and a3b != "unchanged":
        st, st_why, kind = ("disturbed", "%sb: %s (one server's reading)"
                            % (nid(R + 3), a3b), "term")
    elif a3 == "unchanged" and a3b == "unchanged":
        st, st_why, kind = ("unknown", "held at both readings (%s, %sb); "
                            "the window between them was not observed"
                            % (nid(R + 3), nid(R + 3)), "ends")
    else:
        st, st_why, kind = "unknown", "no leader or term reading", "unknown"
    r7 = run.by.get(nid(R + 7), {})
    return {"experiment": exp, "experiment_why": exp_why,
            "recovery": rec, "recovery_why": rec_why,
            "stability": st, "stability_why": st_why,
            "recorded": "%s (one combined verdict, %s)" % (
                r7.get("actual", "not run"), nid(R + 7)),
            "basis": "reassessed from the rows of script %s; no classifier"
                     % run.rev()[0],
            "limits": OLD_LIMITS, "kind": kind,
            "before": a3.split()[1] if a3.startswith("moved") else
                      (names[-1] if names else "?"),
            "leader": names[-1] if names else "?",
            "term": terms(rest)[1],
            "frm": a3.split()[1] if a3.startswith("moved") else ""}


NOTE6 = re.compile(r"experiment (\S+); meta (.+?); stability (\w+): (.*?); baseline ")


def outcomes6(run, R):
    """The three outcomes of one step 6 return, from note ML<R>d.

    A run without that note (no classifier) is excluded, and says why: its
    script recorded the leader and term at the round start and end only.
    """
    n = run.a(nid(R) + "d")
    m = NOTE6.match(n)
    st6 = run.st(nid(R + 1))
    stream = "%s %s" % (nid(R + 1), {"PASS": "met", "FAIL": "not met"}.get(
        st6, "has no result"))
    if not m:
        why = ("not assessed: script %s did not classify the step 6 return "
               "(no note %sd, no kept readings); %sa records the leader and "
               "term at the round start and end only: %s"
               % (run.rev()[0], nid(R), nid(R),
                  run.a(nid(R) + "a").partition("; ")[2] or "no reading"))
        return {"experiment": "not assessed", "experiment_why": why,
                "recovery": "not assessed (meta); stream: " + stream,
                "recovery_why": why,
                "stability": "not assessed", "stability_why": why,
                "recorded": "no note %sd" % nid(R),
                "basis": "excluded: no classifier at step 6",
                "limits": "", "kind": "unknown"}
    exp, rec, st, why = m.groups()
    c = re.search(r"classifier (c\d+)\s*$", n)
    c = c.group(1) if c else "?"
    o = {"experiment": exp, "experiment_why": "note %sd" % nid(R),
         "recovery": "%s (meta only); stream: %s" % (rec, stream),
         "recovery_why": "meta recovery only; the stream's own recovery is "
                         + stream,
         "stability": st, "stability_why": why,
         "recorded": "%s / meta %s / %s (note %sd, classifier %s)"
                     % (exp, rec, st, nid(R), c),
         "basis": "recorded, classifier %s" % c, "limits": "",
         "kind": "unknown"}
    if c != CLS.REVISION:
        got = reread(run, R, None)
        if got:
            res, lim = got
            was = o["stability"]
            o["stability"], o["stability_why"] = res["stability"], res["stability_why"]
            o["basis"] = reassessed(c, was, res["stability"])
            o["limits"] = lim
        else:
            o["limits"] = "not reassessed: no kept readings"
    return o


def reassessed(c, was, now):
    """The basis cell: the recorded revision and word beside today's."""
    return ("recorded by classifier %s: stability %s; reassessed with "
            "classifier %s from the kept readings: %s (%s)"
            % (c, was, CLS.REVISION, now,
               "same" if was == now else "changed"))


def all_returns():
    """[(label, thaw R, verdict id or None)]: the five S4 returns, then the
    two step 6 returns. Seven recovery windows per run."""
    return ([(lbl, R, v) for lbl, B, R, v in RETURNS]
            + [(lbl, R, None) for lbl, B, R in STEP6])


def any_outcomes(run, R, v):
    return outcomes(run, R, v) if v else outcomes6(run, R)


def stab_word(o):
    """One word for the by-scenario table."""
    if o["experiment"] == "not assessed":
        return "not assessed"
    if o["experiment"] != "valid":
        return "inconclusive"
    if o["kind"] == "ends":
        return "held at both readings"
    return o["stability"]


def ret_cell(run, R, v):
    o = outcomes(run, R, v)
    return {"leader": o["leader"], "term": o["term"], "kind": o["kind"],
            "from": o["frm"], "ids": "%sc" % nid(R + 7)}


def timeline(run):
    """One row per scenario: label, ids, and a cell per column."""
    rows = []
    for dark, B, v in (("za", 42, "ML50"), ("au", 59, "ML67")):
        t0, t1 = terms(run.a(nid(B + 7) + "a"))
        R = B + 9
        before = outcomes(run, R, PRE_V[v])["before"]
        held = run.a(nid(B + 2)).startswith("unchanged")
        rows.append(("S2 · %s dark" % dark, "leader in the hub",
                     "%s · %s" % (v, ret_ids(run, R)),
                     {"before": (before, t0),
                      "dark": (before if held else "?", t1),
                      "return": ret_cell(run, R, PRE_V[v])}))
    for dark, B, v in (("za", 76, "ML84"), ("au", 94, "ML102")):
        t0, t1 = terms(run.a(nid(B + 7) + "a"))
        R = B + 9
        new = run.a(nid(B + 2) + "a").split()
        sd = run.a(nid(B + 17) + "a").split("->")
        rows.append(("S3 · %s dark" % dark, "leader in %s" % dark,
                     "%s · %s" % (v, ret_ids(run, R)),
                     {"before": ("%s %s server" % ("an" if dark == "au" else "a", dark), t0),
                      "dark": (new[0] if new else "?", t1),
                      "return": ret_cell(run, R, PRE_V[v]),
                      "stepdown": (sd[-1].strip() if len(sd) > 1 else "?",
                                   "")}))
    t0, t1 = terms(run.a("ML120a"))
    p = run.a("ML123a")
    rows.append(("S5 · both dark", "then za, then au back",
                 "ML121 · ML128 · " + ret_ids(run, 129),
                 {"before": (run.a("ML114").split()[0] if run.a("ML114")
                             else "?", t0),
                  "both": ("no leader", t1),
                  "partial": (p.split()[0] if p else "?",
                              re.search(r"term (\d+)", p).group(1)
                              if re.search(r"term (\d+)", p) else "?"),
                  "return": ret_cell(run, 129, "ML136")}))
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


def ret_ids(run, R):
    """The return's verdict IDs: three in a classified run, one before."""
    return (nid(R + 7) + "a–c") if run.classifier() else nid(R + 7)

TCOLS = (("before", "BEFORE"), ("dark", "ONE DARK"), ("both", "BOTH DARK"),
         ("partial", "ZA BACK"), ("return", "ALL BACK"),
         ("stepdown", "HUB STEP-DOWN"))

KIND_TXT = {"same": ("same leader", "same term"),
            "term": ("same leader", "higher term"),
            "moved": ("moved from", None),
            "gap": ("same leader+term", "no leader briefly"),
            "ends": ("same at both", "readings only"),
            "unknown": ("not assessed", "")}
KIND_CLS = {"same": "ro g", "ends": "ro", "unknown": "ro"}


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
       + [("same leader+term, thaw %s" % nid(R), list(p))
          for R, p in zip(ret, stab)]),
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
                rc = KIND_CLS.get(c["kind"], "ro r")
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


def return_rows(runs):
    """[(run, label, R, outcomes)] for every return round of every run."""
    return [(run, lbl, R, any_outcomes(run, R, v))
            for run in runs for lbl, R, v in all_returns()]


def by_scenario(runs):
    """[(round label, thaw id, [(run, word)], disturbed, valid)] — no pooling
    across scenarios: each return round is its own question."""
    rows = return_rows(runs)
    out = []
    for lbl, R, v in all_returns():
        cells = [(run, stab_word(o)) for run, l, _, o in rows if l == lbl]
        dist = sum(1 for _, w in cells if w == "disturbed")
        valid = sum(1 for _, w in cells if w not in ("inconclusive", "not assessed"))
        out.append((lbl, nid(R), cells, dist, valid))
    return out


SCEN_NOTE = ("Each row is one return scenario. The runs span script and "
             "classifier revisions (see Run provenance), so a column is not "
             "a repeat of the same script. The rounds are few. These counts "
             "say what happened in named runs; they cannot say how often "
             "leadership stays in the hub. \u201cHeld at both readings\u201d "
             "is not \u201cstable\u201d: the window between the two readings "
             "was not observed.")

THREE_NOTE = ("Three outcomes per return, never merged. **Experiment**: was "
              "the round a valid test (stopped, resumed, start checks met, "
              "one stable baseline)? **Recovery**: did servers, peers, the "
              "agreed leader, data, writes and metadata come back within the "
              "deadline? **Stability**: did the same leader stay in the same "
              "term? An election with a full recovery is a disturbance, not "
              "a failed recovery. A run without a classifier is reassessed "
              "from its own rows, and its limits are listed under the table; "
              "its recorded single verdict is shown beside it, unchanged. "
              "**Step 6** has two returns as well. Its own question is the "
              "stream (S6), so its recovery word is **meta recovery only**; "
              "the stream's recovery is its own check, shown in the same "
              "cell. **Basis** names the classifier revision that recorded "
              "each word and, where the rules changed since, the current "
              "revision's stability from the same kept readings. A run whose script "
              "did not classify step 6 shows those two returns as **not "
              "assessed**, with the reason, and they are not counted.")


def cls_txt(run):
    c = run.revisions()
    if not c:
        return "none — return rounds reassessed from rows"
    if len(c) == 1:
        return run.env.get("classifier")
    return ("revisions %s — the rules changed during the run; each round "
            "names its own" % ", ".join(c))


def provenance(runs):
    out = []
    for i, r in enumerate(runs):
        rp, rf, n, pm, pn = r.counts()
        nv = sum(1 for x in r.rows if x["status"] == "VERDICT")
        if i == 0:
            role = "cited — every figure and per-step table"
        elif r.diagnostic():
            role = ("not cited — diagnostic run: debug logging changes the "
                    "timing; counted in the cross-run tables, labelled")
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
    o("| Run | Role | Script | Classifier | Diagnostic | Readiness wait "
      "| When | Ended | Server | CLI "
      "| Machine | Rig checks | Procedure checks | Verdicts | Notes |")
    o("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|")
    for p in provenance(runs):
        r = p["run"]
        rv = r.rev()
        o(f"| `{r.stamp}` | {p['role']} | {rv[0]} ({rv[2]}) "
          f"| {cls_txt(r)} | {r.env.get('diagnostic', 'off')} | {rv[1]} "
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
      "cell is a note. A return cell shows the stability outcome, never "
      "the recovery outcome. " + FINDING + " " + SIM)
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
    for r in runs:
        rounds = diag_rounds(r)
        if not rounds:
            continue
        o(f"## Diagnostic run `{r.stamp}` — the Raft debug log at each return")
        o()
        o("Not cited. " + DIAG_NOTE + " Times are seconds after the first "
          "SIGCONT, host clock.")
        o()
        o("| " + " | ".join(DIAG_COLS) + " |")
        o("|" + "---|" * len(DIAG_COLS))
        for d in rounds:
            o("| " + " | ".join(c.replace("|", "\\|") for c in diag_cells(d)) + " |")
        o()
        o(diag_shows(rounds, diag_other(r)))
        o()
        o("What the log does not show:")
        o()
        for s in DIAG_GAPS:
            o(f"- {s}")
        o()
        o(f"Source: `{r.rel}/elections.txt`.")
        o()
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
      "printed another word. The S4 returns are not one word: they have "
      "three outcomes each, in the table after this one.")
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
    o("### Return rounds: experiment, recovery, stability")
    o()
    o(THREE_NOTE)
    o()
    o("| Run | Round | Experiment | Recovery | Stability | Why (stability) "
      "| Basis | Recorded |")
    o("|---|---|---|---|---|---|---|---|")
    for run, lbl, R, x in return_rows(runs):
        o(f"| `{run.stamp}` | {lbl} | {x['experiment']} | {x['recovery']} "
          f"| **{x['stability']}** | {x['stability_why']} | {x['basis']} "
          f"| {x['recorded']} |")
    o()
    for run, lim in limits_by_run(runs):
        o(f"- `{run.stamp}` — **evidence limits:** {lim}.")
    o()
    o("### By scenario")
    o()
    o(SCEN_NOTE)
    o()
    o("| Scenario | Thaw | " + " | ".join(
        f"`{r.stamp}` ({r.rev()[0]})" for r in runs) + " | Disturbed / valid |")
    o("|---|---|" + "---|" * len(runs) + "---|")
    for lbl, t, cells, dist, valid in by_scenario(runs):
        o(f"| {lbl} | `{t}` | " + " | ".join(w for _, w in cells)
          + f" | {dist} of {valid} |")
    o()


# ------------------------------------------------------------ diagnostic --
# A diagnostic run (LAB_DEBUG=1) keeps the meta group's Raft debug lines
# around each thaw in elections.txt. The table is read from those lines
# only. It names what was logged, in order. It never names a reason that
# no line states.

EL_HEAD = re.compile(r"^== ML(\d+): thaw \(first CONT\) at (\S+);")
EL_LINE = re.compile(r"^\s+([+-]\d+\.\d+)\s+(\S+)\s+\[(\w+)\] (.*)$")
EL_VOTE = re.compile(r"Sending a voteResponse &\{term:(\d+) \S+ granted:(true|false)")
EL_ASK = re.compile(r"Sending out voteRequest \{term:(\d+)")

DIAG_NOTE = ("Read from the Raft debug lines of each diagnostic run, after "
             "the first SIGCONT of each return. **Candidates** are the "
             "servers that logged `Switching to candidate`, with the term of "
             "their own vote request. **Step-down** is the incumbent's own "
             "log line, quoted. **Votes** count the `granted` answers by the "
             "term each answer carries, which is the voter's term. An answer "
             "can reply to a request sent before the thaw. "
             "**Elected** is the server that logged `Self is new JetStream "
             "cluster metadata leader`. **Stability** is the run's own "
             "verdict or note for that return. **Matched in the incumbent's "
             "log** is read from that server's own log file, with its "
             "microsecond times: the step-down line, the vote request it "
             "received at the same term on the nearest Raft line before it, "
             "and the three Raft lines after it.")
# The step-down line names its own path. In nats-server v2.14.6, the version
# the rig runs (tag commit SRC_SHA), processVoteRequest logs "Received a
# voteRequest" (raft.go L5297), then, for a higher term on a non-follower,
# "Stepping down from <state>, detected higher term: X vs Y" (L5315), then
# calls stepdownLocked ("Stepping down", L1857; "Switching to follower",
# L5497) and cancelCatchup ("Canceling catchup subscription...", L3990,
# called at L5319). No other line in raft.go prints "Stepping down from
# leader"; the candidate's line (L3955) says "from candidate". The
# AppendEntry-response step-down writes "Detected another leader with higher
# term" (L4752) and does not call cancelCatchup. The proof is the matched
# sequence; the absence of that warning is only consistent with it.
SRC_SHA = "1aa10f9fe4e7a27b7d877af004a9c0022fdc4910"
SRC = "https://github.com/nats-io/nats-server/blob/%s/server/raft.go" % SRC_SHA
EL_AFTER = ("Stepping down", "Switching to follower",
            "Canceling catchup subscription since we are now up to date")
EL_OTHER = "Detected another leader with higher term"
EL_DOWN = re.compile(r"Stepping down from leader, detected higher term: (\d+) vs (\d+)")
EL_REQ = re.compile(r"Received a voteRequest &\{term:(\d+) .*candidate:(\w+)")
LOG_TS = re.compile(r"^\[\d+\] \S+ (\d\d:\d\d:\d\d\.\d+) \[\w+\] RAFT \[\w+(?:=\S+)? - _meta_\] (.*)$")

DIAG_GAPS = [
    "**Why the returning servers became candidates is not logged.** No line "
    "names an election timer or its deadline. The overdue-timer idea stays "
    "a hypothesis. The log fits it; fitting is not proof.",
    "**Why a vote was not granted is not logged.** A vote line says "
    "`granted:false` and nothing else.",
    "**A server can campaign after it accepts a leader.** The column "
    "*after accepting a leader* names such a server. The log does not say "
    "why it campaigned.",
    "**Debug logging changes the timing.** Every server writes many more "
    "lines. A normal run can order events differently. The normal runs show "
    "the same kind of term rise and leader move, but their log level does "
    "not show who started an election.",
    "**One diagnostic run.** It shows what happened in that run. It is not "
    "a rate, and it is never the cited run.",
]


def diag_rounds(run):
    """[{R, region, thaw, cands, downs, votes, elected, late, word}] from
    the run's elections.txt, in thaw order. [] when there is none."""
    path = os.path.join(run.path, "elections.txt")
    if not run.diagnostic() or not os.path.exists(path):
        return []
    secs_, cur = [], None
    for line in open(path, errors="replace"):
        h = EL_HEAD.match(line)
        if h:
            cur = {"R": int(h.group(1)), "thaw": h.group(2), "lines": []}
            secs_.append(cur)
            continue
        m = EL_LINE.match(line)
        if m and cur is not None:
            cur["lines"].append((float(m.group(1)), m.group(2), m.group(4)))
    out, names = [], diag_names(run)
    for s in sorted(secs_, key=lambda x: x["thaw"]):
        R, post = s["R"], [x for x in s["lines"] if x[0] >= 0]
        region = THAWS.get(R, "?")
        cands, asks, downs, votes, elected, late = [], {}, [], {}, None, []
        accepted = set()
        for off, srv, msg in post:
            a = EL_ASK.search(msg)
            if a:
                asks.setdefault(srv, a.group(1))
            if "Switching to candidate" in msg:
                cands.append((off, srv))
                if srv in accepted and srv not in late:
                    late.append(srv)
            if "new metadata leader:" in msg:
                accepted.add(srv)
            if "Stepping down from leader" in msg:
                why = msg.split("_meta_] ", 1)[-1]
                downs.append((off, srv, why))
            v = EL_VOTE.search(msg)
            if v:
                d = votes.setdefault(v.group(1), [0, 0])
                d[0 if v.group(2) == "true" else 1] += 1
            if "Self is new JetStream cluster metadata leader" in msg and not elected:
                elected = (off, srv, asks.get(srv, "?"))
        if R in DARK:
            row = run.by.get(nid(R + 7) + "c", {})
            word, txt = row.get("actual", ""), row.get("expected", "")
        else:
            txt = run.a(nid(R) + "d")
            m = re.search(r"stability (\w+)", txt)
            word = m.group(1) if m else ""
        m = (re.search(r"term (\d+) -> (\d+)", txt)
             or re.search(r"term (\d+) at the baseline", txt))
        if word and m:
            word += (f" (term {m.group(1)} → {m.group(2)})" if m.lastindex == 2
                     else f" (term {m.group(1)} held)")
        out.append({"R": R, "region": region, "thaw": s["thaw"],
                    "cands": [(o_, c, asks.get(c, "?")) for o_, c in cands],
                    "downs": downs, "votes": votes, "elected": elected,
                    "late": late, "word": word or "—",
                    "match": [diag_match(run, srv, why, names)
                              for _, srv, why in downs]})
    return out


def diag_names(run):
    """{raft id: server name}, from the id=name pairs in elections.txt."""
    names = {}
    for line in open(os.path.join(run.path, "elections.txt"), errors="replace"):
        for i, n in re.findall(r"(\w{8})=(t-(?:za|au|arb)-\d)", line):
            names[i] = n
    return names


def diag_other(run):
    """Servers whose log holds the AppendEntry-response step-down line."""
    d = os.path.join(run.path, "log")
    out = []
    for fn in sorted(os.listdir(d)) if os.path.isdir(d) else []:
        if EL_OTHER in open(os.path.join(d, fn), errors="replace").read():
            out.append(fn)
    return out


def diag_match(run, srv, why, names):
    """Match one step-down to the vote request just before it, in the
    incumbent's own log. Returns a dict; ok is False when no match."""
    m = EL_DOWN.search(why)
    res = {"server": srv, "ok": False, "text": "no match: step-down line not parsed"}
    if not m:
        return res
    hi, lo = m.groups()
    path = os.path.join(run.path, "log", srv[2:] + ".log")
    try:
        lines = [LOG_TS.match(x) for x in open(path, errors="replace")]
    except OSError:
        res["text"] = "no match: %s not kept" % os.path.basename(path)
        return res
    raft = [(x.group(1), x.group(2)) for x in lines if x]
    for k, (ts, msg) in enumerate(raft):
        d = EL_DOWN.search(msg)
        if not d or d.groups() != (hi, lo):
            continue
        for j in range(k - 1, -1, -1):
            q = EL_REQ.search(raft[j][1])
            if q and q.group(1) == hi:
                who = names.get(q.group(2), q.group(2))
                between = k - j - 1
                gap = _ms(raft[j][0], ts)
                after = tuple(x[1] for x in raft[k + 1:k + 1 + len(EL_AFTER)])
                res.update(ok=True, candidate=who, term=hi, adjacent=between == 0,
                           after=after == EL_AFTER,
                           text="%s's voteRequest term %s received %s; step-down "
                                "\u201c%s vs %s\u201d %s (%s ms later; %s)"
                                % (who, hi, raft[j][0], hi, lo, ts, gap,
                                   "the next Raft line" if between == 0 else
                                   "%d Raft lines between" % between)
                                + ("; then \u201cStepping down\u201d, \u201cSwitching "
                                   "to follower\u201d, \u201cCanceling catchup\u201d, as "
                                   "in the code" if after == EL_AFTER else
                                   "; the lines after do not follow the code"))
                return res
        res["text"] = "no match: no voteRequest at term %s before the step-down" % hi
        return res
    res["text"] = "no match: step-down line not in %s" % os.path.basename(path)
    return res


def _ms(a, b):
    """Milliseconds from clock time a to b (same day)."""
    f = lambda t: (lambda h, m, s_: int(h) * 3600 + int(m) * 60 + float(s_))(*t.split(":"))
    return "%.3f" % ((f(b) - f(a)) * 1000)


def diag_cells(d):
    """Plain-text cells for one diagnostic return."""
    ret = (f"ML{d['R']} · {d['region']} returns")
    cands = "; ".join(f"{c} +{o_:.3f} s (term {t})" for o_, c, t in d["cands"]) or "none logged"
    downs = "; ".join(f"{s} +{o_:.3f} s: \"{w}\"" for o_, s, w in d["downs"]) or "none logged"
    votes = "; ".join(f"term {t}: {g} granted, {n} not"
                      for t, (g, n) in sorted(d["votes"].items(), key=lambda x: int(x[0]))) or "none logged"
    el = (f"{d['elected'][1]} +{d['elected'][0]:.3f} s (term {d['elected'][2]})"
          if d["elected"] else "no election won after the thaw")
    late = ", ".join(d["late"]) or "—"
    match = "; ".join(x["text"] for x in d["match"]) or "no step-down to match"
    return [ret, cands, downs, match, votes, el, late, d["word"]]


def diag_shows(rounds, other):
    """The summary, built from the rows. It names a return only when that
    row bears the claim out; otherwise it says the rows do not agree."""
    dist, stab, bad = [], [], []
    for d in rounds:
        rid = "`ML%d`" % d["R"]
        held = re.search(r"term (\d+) held", d["word"])
        if d["word"].startswith("disturbed"):
            ok = d["match"] and all(
                x["ok"] and x["adjacent"] and x["after"]
                and x["candidate"].startswith("t-%s-" % d["region"])
                for x in d["match"])
            (dist if ok else bad).append(rid)
        elif d["word"].startswith("stable"):
            ok = (not d["downs"] and held
                  and any(c.startswith("t-%s-" % d["region"]) for _, c, _ in d["cands"])
                  and all(t != "?" and int(t) <= int(held.group(1))
                          for _, _, t in d["cands"]))
            (stab if ok else bad).append(rid)
        else:
            bad.append(rid)
    if bad or other:
        return ("The returns in this diagnostic run do not all follow one "
                "logged order (%s). Read each row on its own."
                % ", ".join(bad + other))
    out = ["What the log shows, in this diagnostic run only."]
    if dist:
        out.append(
            "**Disturbed returns (%s): a higher-term vote request, then the "
            "incumbent's step-down.** In each, a server of the returning "
            "region sent a vote request with a term above the incumbent's. "
            "The incumbent's own log shows the order that nats-server "
            "v2.14.6, the version the rig runs, writes in "
            "[`processVoteRequest`](%s#L5291-L5323): \u201cReceived a "
            "voteRequest\u201d at the higher term ([L5297](%s#L5297)), then on "
            "the next Raft line \u201cStepping down from leader, detected "
            "higher term\u201d at that term ([L5315](%s#L5315-L5317)), then "
            "\u201cStepping down\u201d ([L1857](%s#L1856-L1859)), \u201cSwitching "
            "to follower\u201d ([L5497](%s#L5497)) and \u201cCanceling catchup "
            "subscription\u201d ([L3990](%s#L3989-L3990), called at "
            "[L5319](%s#L5319)). No other line in that file prints "
            "\u201cStepping down from leader\u201d. This matched sequence is "
            "the evidence. The other higher-term step-down, on an "
            "AppendEntry response, writes \u201c%s\u201d "
            "([L4752](%s#L4745-L4754)); no log of this run holds it, which "
            "fits but proves nothing by itself. An election followed; "
            "*Elected* names the winner."
            % ((", ".join(dist),) + (SRC,) * 7 + (EL_OTHER, SRC)))
    if stab:
        out.append(
            "**Stable returns (%s): candidates, but no term above the "
            "incumbent's.** Servers of the returning region switched to "
            "candidate here too, but every candidate's term was at or below "
            "the incumbent's term. No incumbent stepped down." % ", ".join(stab))
    return " ".join(out)


DIAG_COLS = ("Return", "Candidates after the thaw", "Step-down",
             "Matched in the incumbent's log", "Votes", "Elected", "After accepting a leader", "Stability")


def limits_by_run(runs):
    """[(run, limits text)], once per run, for the runs that have limits."""
    out = []
    for run in runs:
        lims = list(dict.fromkeys(
            x["limits"] for r, _, _, x in return_rows([run]) if x["limits"]
            and x["limits"] != "thaw time kept"))
        if lims:
            out.append((run, "; ".join(lims)))
    return out


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
      '<th>Script</th><th>Classifier</th><th>When</th><th>Server</th><th>CLI</th><th>Machine</th>'
      '<th class="num">Rig</th><th class="num">Procedure</th>'
      '<th class="num">Verdicts</th></tr></thead><tbody>')
    for p in provenance(runs):
        r = p["run"]
        rv = r.rev()
        o(f"<tr><td class='mono'>{esc(r.stamp)}</td><td>{esc(p['role'])}</td>"
          f"<td><b>{esc(rv[0])}</b> <span class='note'>({esc(rv[2])})</span>"
          f"<br>{esc(rv[1])}</td>"
          f"<td>{esc(cls_txt(r))}"
          + (f"<br><b>diagnostic</b> {esc(r.env['diagnostic'])}"
             if r.diagnostic() else "") + "</td>"
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
      "means a disturbance, not a count of elections. A return cell is the "
      "stability outcome, kept apart from recovery. Each return is SIGCONT "
      "after SIGSTOP, not a partition or a restart; the event that starts "
      "the new term is unproved. The leader names come from notes and "
      f"checks of run <code>{esc(run.stamp)}</code>.</figcaption></figure>")
    o("</section>")

    html_cross(runs, o)

    for r in runs:
        rounds = diag_rounds(r)
        if not rounds:
            continue
        o(f'<section class="sec"><h2>Diagnostic run <code>{esc(r.stamp)}</code>'
          " &mdash; the Raft debug log at each return</h2>")
        o(f"<p><b>Not cited.</b> {md(DIAG_NOTE)} Times are seconds after the "
          "first SIGCONT, host clock.</p>")
        o('<div class="card"><table><thead><tr>'
          + "".join(f"<th>{esc(c)}</th>" for c in DIAG_COLS)
          + "</tr></thead><tbody>")
        for d in rounds:
            cells = diag_cells(d)
            o("<tr>" + "".join(
                f"<td class='{word_cls(c.split(' ')[0]) if i == 7 else 'mono' if i in (1, 2, 3, 5) else ''}'>"
                f"{esc(c)}</td>" for i, c in enumerate(cells)) + "</tr>")
        o("</tbody></table></div>")
        o(f"<p>{md(diag_shows(rounds, diag_other(r)))}</p>")
        o("<p>What the log does not show:</p><ul>"
          + "".join(f"<li>{md(s)}</li>" for s in DIAG_GAPS) + "</ul>")
        o(f"<p>Source: <code>{esc(r.rel)}/elections.txt</code>.</p>")
        o("</section>")

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
      "that time printed another word. The S4 returns are not one word: "
      "they have three outcomes each, in the table after this one.</p>")
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
    o("<h3>Return rounds: experiment, recovery, stability</h3>")
    o(f"<p>{md(THREE_NOTE)}</p>")
    o('<div class="card"><table><thead><tr><th>Run</th><th>Round</th>'
      "<th>Experiment</th><th>Recovery</th><th>Stability</th>"
      "<th>Why (stability)</th><th>Basis</th><th>Recorded</th>"
      "</tr></thead><tbody>")
    for run, lbl, R, x in return_rows(runs):
        o(f"<tr><td class='mono'>{esc(run.stamp)}</td><td>{esc(lbl)}</td>"
          f"<td class='{word_cls(x['experiment'])}'>{esc(x['experiment'])}</td>"
          f"<td class='{word_cls(x['recovery'])}'>{esc(x['recovery'])}</td>"
          f"<td class='{word_cls(x['stability'])}'><b>{esc(x['stability'])}</b></td>"
          f"<td>{esc(x['stability_why'])}</td><td>{esc(x['basis'])}</td>"
          f"<td class='note'>{esc(x['recorded'])}</td></tr>")
    o("</tbody></table></div>")
    lims = limits_by_run(runs)
    if lims:
        o("<ul>" + "".join(
            f"<li><code>{esc(r.stamp)}</code> &mdash; <b>evidence limits:</b> "
            f"{esc(l)}.</li>" for r, l in lims) + "</ul>")
    o("<h3>By scenario</h3>")
    o(f"<p>{esc(SCEN_NOTE)}</p>")
    o('<div class="card"><table><thead><tr><th>Scenario</th><th>Thaw</th>'
      + "".join(f"<th>{esc(r.stamp)}<br>{esc(r.rev()[0])}</th>" for r in runs)
      + '<th class="num">Disturbed / valid</th></tr></thead><tbody>')
    for lbl, t, cells, dist, valid in by_scenario(runs):
        tds = "".join(f"<td class='{word_cls(w)}'>{esc(w)}</td>"
                      for _, w in cells)
        o(f"<tr><td>{esc(lbl)}</td><td class='mono'>{t}</td>{tds}"
          f"<td class='num'>{dist} of {valid}</td></tr>")
    o("</tbody></table></div>")
    o("</section>")


def word_cls(w):
    w = w.split(" (")[0]
    if w in ("valid", "recovered", "stable"):
        return "pass"
    if w in ("invalid", "not recovered", "disturbed", "failed"):
        return "fail"
    return "note"


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
