#!/usr/bin/env python3
"""Exercise 10 -- classify one regional return from per-server /raftz readings.

Used by 10-hub-meta-leader.sh. Tested by test-classify-10.py (no servers).

A round has three outcomes, reported apart:

  experiment   valid | invalid | inconclusive
               Did the rig do what the round asks? The region's processes
               stopped, then resumed; nothing else acted inside the window;
               the round's start check passed; a stable baseline existed.
  recovery     recovered | not recovered | inconclusive
               Servers, meta quorum, one agreed leader, peer catch-up, data,
               writes and the metadata probe -- within the deadline.
  stability    stable | disturbed | unknown
               Did the meta leader or the meta term change inside the window?

The window: a baseline read from the servers expected to be live, just before
the thaw, to a final reading from all nine at the end of the settle window.
A step-down made before the baseline or after the final reading is outside it.

Evidence rules:
  * A missing, malformed, stale or contradictory reading never passes.
  * No leader, unreachable and invalid are three different kinds.
  * A server that is frozen on purpose is EXPECTED to be unreachable. That is
    not a failure. A frozen server that answers makes the experiment invalid.
  * Missing evidence is neither recovery nor disturbance.
  * The term rises on every election attempt, won or not. A rise of N is not
    N elections. "Stable" means no election was observed during the window.

Commands (NAME=PORT pairs name each monitor, e.g. t-za-1=8231):
  snap OUT [--rounds N] NAME=PORT...       N poll rounds 0.5 s apart -> JSONL
  watch OUT STOPFILE NAME=PORT...          poll until STOPFILE exists -> JSONL
  kinds NAME=PORT...                       one token per server: the leader's
                                           name, NONE, UNREACHABLE or INVALID
  round [options]                          classify; prints CL_* shell lines
"""

import argparse
import concurrent.futures
import json
import os
import re
import shlex
import socket
import statistics
import sys
import time
import urllib.error
import urllib.request

# Revision of the classification rules. Bump it when a rule changes, so a
# report can say which rules produced an outcome.
#   c1  first rules (one full run, 10-20261005-162522).
#   c2  a no-leader interval is the GROUP's (no server says LEADER); a server
#       that sees no leader while another says LEADER is reported apart; a
#       poll with no leader confirmed and none denied is an evidence gap;
#       CANDIDATE readings are reported as election attempts.
#   c3  a leader name that does not say LEADER in that term is that server's
#       stale view, not a conflict; a conflict is two LEADERs in one term, or
#       a confirmed other leader in the baseline term.
#   c4  a poll that straddles the thaw is partial: its answers are
#       single-server views, never a group no-leader poll or a gap (c2 and
#       c3 read one as both).
#   c5  a straddling poll still checks its post-thaw answers for conflicts
#       (two LEADERs in one term, another leader in the baseline term); a
#       LIVE server that says no leader after the thaw, with no LEADER seen in
#       that poll, keeps the round from "stable"; a window with no complete
#       poll between the thaw and the final reading is "unknown", never
#       "stable".
REVISION = "c5"

FETCH_TIMEOUT_S = 1.0
BASELINE_MAX_AGE_S = 5.0

OK, NO_LEADER, UNREACHABLE, INVALID = "ok", "no_leader", "unreachable", "invalid"


# --- one reading -------------------------------------------------------------

def fetch(port, timeout=FETCH_TIMEOUT_S):
    """('body', text) | ('timeout', why) | ('refused', why) | ('http', code)."""
    url = f"http://127.0.0.1:{port}/raftz?group=_meta_"
    try:
        with urllib.request.urlopen(url, timeout=timeout) as r:
            return ("body", r.read().decode("utf-8", "replace"))
    except urllib.error.HTTPError as e:
        return ("http", str(e.code))
    except urllib.error.URLError as e:
        if isinstance(e.reason, (socket.timeout, TimeoutError)):
            return ("timeout", "no answer in %.1f s" % timeout)
        return ("refused", str(e.reason))
    except (socket.timeout, TimeoutError):
        return ("timeout", "no answer in %.1f s" % timeout)
    except OSError as e:
        return ("refused", str(e))


def parse_reading(server, port, t, rnd, fetched):
    """One server's /raftz answer -> a reading dict with a kind.

    ok         a leader name and a numeric term
    no_leader  leader null or empty, with a numeric term
    unreachable  no HTTP answer at all (timeout, refused)
    invalid    an answer that cannot be trusted (HTTP error, not JSON, no
               meta group, no numeric term, a leader id with no name, or the
               server's own name among its peers -- the wrong source)
    """
    r = {"round": rnd, "t": t, "server": server, "port": port,
         "kind": INVALID, "leader": None, "term": None, "state": None, "why": ""}
    how, val = fetched
    if how in ("timeout", "refused"):
        r.update(kind=UNREACHABLE, why=val)
        return r
    if how == "http":
        r["why"] = "HTTP " + val
        return r
    try:
        g = json.loads(val)["$SYS"]["_meta_"]
    except (ValueError, KeyError, TypeError):
        r["why"] = "malformed: no $SYS._meta_ in the answer"
        return r
    if not isinstance(g, dict):
        r["why"] = "malformed: _meta_ is not an object"
        return r
    term = g.get("term")
    if isinstance(term, bool) or not isinstance(term, int):
        r["why"] = "malformed: term %r is not a number" % (term,)
        return r
    r["term"] = term
    r["state"] = g.get("state")
    peers = g.get("peers") if isinstance(g.get("peers"), dict) else {}
    names = {pid: (p or {}).get("name") for pid, p in peers.items() if isinstance(p, dict)}
    if server in names.values():
        r["why"] = "source mismatch: %s is listed among its own peers" % server
        return r
    lid = g.get("leader")
    if lid in (None, ""):
        r["kind"] = NO_LEADER
        return r
    if lid == g.get("id"):
        r.update(kind=OK, leader=server)
        return r
    if names.get(lid):
        r.update(kind=OK, leader=names[lid])
        return r
    r["why"] = "leader id %r has no name in the peer list" % (lid,)
    return r


def poll(servers, rnd, timeout=FETCH_TIMEOUT_S, fetcher=fetch):
    """One round: every server at once. servers = [(name, port), ...].

    A reading's time `t` is when its ANSWER arrived, not when the round
    began. A frozen server accepts the request and answers only after the
    CONT, so its answer describes the state after the thaw (measured on the
    first scripted run of this classifier, 2026-10-05). `t0` is the round start.
    """
    t0 = time.time()

    def one(sp):
        f = fetcher(sp[1], timeout)
        return f, time.time()
    with concurrent.futures.ThreadPoolExecutor(max_workers=max(1, len(servers))) as ex:
        got = list(ex.map(one, servers))
    return [dict(parse_reading(s, p, rt, rnd, f), t0=t0)
            for (s, p), (f, rt) in zip(servers, got)]


# --- agreement over a set of servers -----------------------------------------

def agreement(readings, expected, now=None, max_age=None):
    """Do the EXPECTED servers name one leader in one term?

    outcome: agreed | no_leader | split | unconfirmed | contradictory |
             unreachable | stale | invalid | missing.
    "agreed" also needs the named leader to answer, and to call itself LEADER
    in that term: a follower can keep naming an old leader while its term
    rises (measured on this rig, 2026-10-05, hub with both regions frozen).
    """
    by = {r["server"]: r for r in readings}
    out = {"outcome": None, "leader": None, "term": None, "why": "",
           "sources": ",".join(sorted(s for s in expected if s in by))}
    miss = [s for s in expected if s not in by]
    if miss:
        out.update(outcome="missing", why="no reading from " + ",".join(miss))
        return out
    rs = [by[s] for s in expected]
    bad = [r for r in rs if r["kind"] == INVALID]
    if bad:
        out.update(outcome="invalid",
                   why="; ".join(f"{r['server']}: {r['why']}" for r in bad))
        return out
    if now is not None and max_age is not None:
        old = [r for r in rs if now - r["t"] > max_age]
        if old:
            out.update(outcome="stale", why="readings older than %.1f s from %s"
                       % (max_age, ",".join(r["server"] for r in old)))
            return out
    gone = [r for r in rs if r["kind"] == UNREACHABLE]
    if gone:
        out.update(outcome="unreachable",
                   why="no answer from " + ",".join(r["server"] for r in gone))
        return out
    claim = {}
    for r in rs:
        if r["state"] == "LEADER":
            claim.setdefault(r["term"], []).append(r["server"])
            if r["leader"] != r["server"]:
                out.update(outcome="contradictory",
                           why=f"{r['server']} says LEADER but names {r['leader']}")
                return out
    two = {t: s for t, s in claim.items() if len(s) > 1}
    if two:
        t, s = sorted(two.items())[0]
        out.update(outcome="contradictory",
                   why=f"{' and '.join(s)} both say LEADER in term {t}")
        return out
    if all(r["kind"] == NO_LEADER for r in rs):
        out.update(outcome="no_leader", term=max(r["term"] for r in rs),
                   why="no server names a leader")
        return out
    pairs = {(r["leader"], r["term"]) for r in rs}
    if len(pairs) > 1:
        out.update(outcome="split", why="readings differ: " + ", ".join(
            f"{r['server']}={r['leader'] or 'NONE'}/{r['term']}" for r in rs))
        return out
    (lead, term), = pairs
    own = by.get(lead)
    if lead not in expected or own is None:
        out.update(outcome="unconfirmed", leader=lead, term=term,
                   why=f"all name {lead}, but {lead} is not among the servers read")
        return out
    if own["state"] != "LEADER":
        out.update(outcome="unconfirmed", leader=lead, term=term,
                   why=f"all name {lead}, but {lead} says {own['state']}")
        return out
    out.update(outcome="agreed", leader=lead, term=term)
    return out


# --- one round ---------------------------------------------------------------

def _rounds(readings):
    """[(t, [readings])] in time order, one entry per (source, round).

    A round's time is its LAST answer: the moment every reading in it was in.
    """
    g = {}
    for r in readings:
        g.setdefault((r.get("src", ""), r["round"]), []).append(r)
    return sorted(((max(x["t"] for x in rs), rs) for rs in g.values()),
                  key=lambda p: p[0])


def _check_status(checks, ids):
    """(failed ids, missing ids) among ids, from {id: 'PASS'|'FAIL'|...}."""
    fail = [i for i in ids if checks.get(i) == "FAIL"]
    miss = [i for i in ids if checks.get(i) not in ("PASS", "FAIL")]
    return fail, miss


def _fmt(x):
    return "%.1f" % x


def check_baseline(baseline, live, frozen, thaw_t, max_age=BASELINE_MAX_AGE_S):
    """(leader, term, invalid reasons, inconclusive reasons).

    Every baseline round must show the LIVE servers agreed on one leader and
    term, and the same pair in every round. The FROZEN servers must not
    answer: that is the freeze working, not a failure.
    """
    invalid, inconc, seen = [], [], set()
    rounds = _rounds(baseline)
    if not rounds:
        return None, None, [], ["no baseline reading"]
    for bt, rs in rounds:
        if bt > thaw_t:
            inconc.append("a baseline reading was taken after the thaw")
        by = {r["server"]: r for r in rs}
        for s in frozen:
            r = by.get(s)
            if r is None:
                inconc.append(f"no baseline reading of frozen {s}")
            elif r["kind"] != UNREACHABLE:
                invalid.append(f"frozen {s} answered ({r['kind']}): the freeze did not hold")
        a = agreement(rs, live, now=thaw_t, max_age=max_age)
        if a["outcome"] != "agreed":
            inconc.append(f"baseline {a['outcome']}: {a['why']}")
        else:
            seen.add((a["leader"], a["term"]))
    if len(seen) > 1:
        inconc.append("baseline not stable: " + ", ".join(f"{l}/{t}" for l, t in sorted(seen)))
    invalid, inconc = list(dict.fromkeys(invalid)), list(dict.fromkeys(inconc))
    if len(seen) == 1 and not invalid and not inconc:
        (lead, term), = seen
        return lead, term, [], []
    return None, None, invalid, inconc


def classify_round(baseline, watch, final, live, frozen, checks, thaw_t,
                   deadline_s, process_ids=(), pre_ids=(), recovery_ids=(),
                   events=(), baseline_max_age=BASELINE_MAX_AGE_S):
    """Classify one return. Pure: readings and check statuses in, a dict out.

    baseline  readings just before the thaw (one or more rounds)
    watch     readings from the thaw to the end of the settle window
    final     one round from all nine, at the end of the window
    live      servers expected to answer in the baseline
    frozen    servers expected NOT to answer in the baseline
    checks    {check id: 'PASS'|'FAIL'} for the ids named below
    process_ids  the stop/resume checks (FAIL -> experiment invalid)
    pre_ids      the round's start checks (FAIL -> experiment inconclusive)
    recovery_ids the recovery checks (servers, peers, data, writes, metadata)
    events    [(t, label)] operator actions; one inside the window -> invalid
    """
    allsrv = list(live) + list(frozen)
    res = {"revision": REVISION}

    # Experiment validity. Invalid beats inconclusive.
    invalid, inconc = [], []
    pf, pm = _check_status(checks, process_ids)
    invalid += [f"{i} failed" for i in pf]
    inconc += [f"{i} has no result" for i in pm]
    qf, qm = _check_status(checks, pre_ids)
    inconc += [f"start check {i} failed" for i in qf]
    inconc += [f"start check {i} has no result" for i in qm]

    base_rounds = _rounds(baseline)
    final_t = max((r["t"] for r in final), default=None)
    for t, label in events:
        lo = min((bt for bt, _ in base_rounds), default=thaw_t)
        if lo <= t <= (final_t if final_t is not None else float("inf")):
            invalid.append(f"{label} at {_fmt(t - thaw_t)} s fell inside the window")

    L0, T0, b_inv, b_inc = check_baseline(baseline, live, frozen, thaw_t, baseline_max_age)
    invalid = list(dict.fromkeys(invalid + b_inv))
    inconc = list(dict.fromkeys(inconc + b_inc))
    if inconc or invalid:
        L0 = T0 = None
    if invalid:
        res.update(experiment="invalid", experiment_why="; ".join(invalid + inconc))
    elif inconc or L0 is None:
        res.update(experiment="inconclusive",
                   experiment_why="; ".join(inconc) or "no agreed baseline")
    else:
        res.update(experiment="valid",
                   experiment_why=f"stopped and resumed; baseline {L0} term {T0} "
                                  f"from {len(live)} live servers")
    res.update(baseline_leader=L0, baseline_term=T0)

    # The final reading, all nine.
    if not final:
        fa = {"outcome": "missing", "leader": None, "term": None, "why": "no final reading"}
    elif any(r["t"] < thaw_t for r in final):
        fa = {"outcome": "stale", "leader": None, "term": None,
              "why": "a final reading was taken before the thaw"}
    else:
        fa = agreement(final, allsrv)
    res.update(final_outcome=fa["outcome"], final_leader=fa["leader"],
               final_term=fa["term"], final_why=fa["why"])

    # When did all nine first agree?
    # A poll that straddles the thaw keeps only its answers after the thaw.
    # It is PARTIAL: the live servers answered before the CONT, so it cannot
    # say what the group saw after it (c4).
    rounds, partial, from_final = [], set(), set()
    final_ids = {id(r) for r in final}
    for t, rs in _rounds(watch + final):
        post = [r for r in rs if r["t"] >= thaw_t]
        if post:
            if len(post) < len(rs):
                partial.add(len(rounds))
            if any(id(r) in final_ids for r in rs):
                from_final.add(len(rounds))
            rounds.append((t, post))
    first = None
    for t, rs in rounds:
        if agreement(rs, allsrv)["outcome"] == "agreed":
            first = t - thaw_t
            break
    res["first_agreed_s"] = first
    gaps = [b - a for (a, _), (b, _) in zip(rounds, rounds[1:])]
    res["poll_gap_s"] = statistics.median(gaps) if gaps else None

    # Recovery.
    if res["experiment"] != "valid":
        res.update(recovery="inconclusive",
                   recovery_why=f"experiment {res['experiment']}: {res['experiment_why']}")
    else:
        no, unk = [], []
        rf, rm = _check_status(checks, recovery_ids)
        no += [f"{i} failed" for i in rf]
        unk += [f"{i} has no result" for i in rm]
        o = fa["outcome"]
        if o == "agreed":
            if first is None or first > deadline_s:
                no.append("deadline exceeded: all nine first agreed at %s s, limit %s s"
                          % ("?" if first is None else _fmt(first), deadline_s))
        elif o in ("missing", "invalid", "stale", "contradictory"):
            unk.append(f"final reading {o}: {fa['why']}")
        else:
            no.append(f"final reading {o}: {fa['why']}")
        if no:
            res.update(recovery="not recovered", recovery_why="; ".join(no + unk))
        elif unk:
            res.update(recovery="inconclusive", recovery_why="; ".join(unk))
        else:
            res.update(recovery="recovered",
                       recovery_why="all nine agreed at %s s (limit %s s); every recovery check passed"
                       % (_fmt(first), deadline_s))

    # Stability -- leader identity and term, baseline to final.
    # The last baseline round is the clean reading before the window, so a
    # no-leader interval in the first watch round still has an upper bound.
    # A GROUP no-leader poll: no server says LEADER, and some say no leader.
    # A server that sees no leader while another server says LEADER is that
    # server's own view (a returning server catching up), kept apart. So is
    # a server that names a leader which does not say LEADER in that term (a
    # stale leader field). Two servers saying LEADER in one term, or a name
    # that IS confirmed but is not the baseline leader in the baseline term,
    # is a conflict. A poll where nobody says LEADER and nobody says no
    # leader is an evidence gap: it can never support "stable". A CANDIDATE
    # reading is an election attempt, at any term, and is always reported.
    # A straddling poll (c5) is still checked for conflicts among its
    # post-thaw answers, and a live server's no leader in it is kept: either
    # can stop "stable". It never makes a group no-leader poll or a gap.
    leaders, stale, odd, gaps_n, views, cands = [], 0, [], 0, {}, []
    live_nl, complete_n = [], 0
    nl_rounds = [(base_rounds[-1][0], [])] if base_rounds and T0 is not None else []
    tmax = T0
    for n, (t, rs) in enumerate(rounds):
        if final_t is not None and t > final_t:
            continue
        cur = [r for r in rs if r["kind"] in (OK, NO_LEADER) and T0 is not None]
        for r in cur:
            if r.get("state") == "CANDIDATE":
                c_ = f"{r['server']} term {r['term']}"
                if c_ not in cands:
                    cands.append(c_)
        stale += sum(1 for r in cur if r["term"] < T0)
        cur = [r for r in cur if r["term"] >= T0]
        says = {}                      # term -> servers that say LEADER
        for r in cur:
            tmax = max(tmax, r["term"])
            if r["kind"] == OK and r.get("state") == "LEADER":
                says.setdefault(r["term"], []).append(r["server"])
        for term, who in says.items():
            if len(who) > 1:
                odd.append(f"{' and '.join(sorted(who))} all say LEADER in term {term}")
        if n in partial:
            # No group judgement: each answer is that server's own view.
            for r in cur:
                if r["kind"] == NO_LEADER:
                    views.setdefault(r["server"], []).append(
                        f"+{_fmt(t - thaw_t)} s no leader (poll straddled the thaw)")
                    if r["server"] in live and not says:
                        live_nl.append(f"{r['server']} +{_fmt(t - thaw_t)} s")
                elif r["leader"] in says.get(r["term"], []):
                    if r["term"] == T0 and r["leader"] != L0:
                        odd.append(f"{r['leader']} says LEADER in the baseline term {T0}, "
                                   f"which {L0} led")
                    if r["leader"] not in leaders:
                        leaders.append(r["leader"])
            continue
        if n not in from_final:
            complete_n += 1
        flag = []
        for r in cur:
            if r["kind"] == NO_LEADER:
                flag.append(r["server"])
            elif r["leader"] in says.get(r["term"], []):
                if r["term"] == T0 and r["leader"] != L0:
                    odd.append(f"{r['leader']} says LEADER in the baseline term {T0}, "
                               f"which {L0} led")
                if r["leader"] not in leaders:
                    leaders.append(r["leader"])
            else:
                views.setdefault(r["server"], []).append(
                    f"+{_fmt(t - thaw_t)} s named {r['leader']} in term {r['term']}, unconfirmed")
        if says and flag:
            for s_ in flag:
                views.setdefault(s_, []).append(f"+{_fmt(t - thaw_t)} s no leader")
            flag = []
        elif T0 is not None and not says and not flag:
            gaps_n += 1
        nl_rounds.append((t, flag))
    odd = list(dict.fromkeys(odd))
    res["leaders_seen"] = leaders
    res["stale_readings"] = stale
    res["no_leader"] = _intervals(nl_rounds, thaw_t)
    res["server_no_leader"] = ("; ".join(
        "%s: %s" % (s_, ", ".join(v)) for s_, v in sorted(views.items()))
        + " (single-server views, not a group interval)") if views else "none seen"
    res["candidates"] = ", ".join(cands) if cands else "none seen"
    res["leader_gaps"] = gaps_n
    res["complete_polls"] = complete_n
    res["live_no_leader"] = ", ".join(live_nl) if live_nl else "none seen"
    rise = (tmax - T0) if T0 is not None and tmax is not None else None
    res["term_rise"] = rise

    if res["experiment"] != "valid":
        res.update(stability="unknown",
                   stability_why=f"experiment {res['experiment']}: {res['experiment_why']}")
    else:
        dist = []
        if rise:
            dist.append(f"term {T0} -> {tmax} (+{rise}: every election attempt raises "
                        f"the term; +{rise} is not a count of elections)")
        if res["no_leader"]["intervals"]:
            dist.append("no-leader interval seen: " + res["no_leader"]["text"])
        if fa["outcome"] == "agreed":
            if fa["leader"] != L0:
                dist.append(f"leader {L0} -> {fa['leader']}")
            if fa["term"] < T0:
                odd.append(f"final term {fa['term']} is below the baseline {T0}")
        if dist:
            res.update(stability="disturbed", stability_why="; ".join(dist))
        elif odd:
            res.update(stability="unknown", stability_why="conflicting readings: " + "; ".join(odd))
        elif fa["outcome"] != "agreed":
            res.update(stability="unknown",
                       stability_why=f"final reading {fa['outcome']}: {fa['why']}")
        elif gaps_n:
            res.update(stability="unknown",
                       stability_why=f"no server confirmed a leader in {gaps_n} poll(s) of the window")
        elif live_nl:
            res.update(stability="unknown",
                       stability_why="a live server said no leader after the thaw in a poll that "
                                     "straddled it, and no server said LEADER in that poll: "
                                     + ", ".join(live_nl))
        elif not complete_n:
            res.update(stability="unknown",
                       stability_why="no complete poll between the thaw and the final reading")
        elif cands:
            res.update(stability="stable",
                       stability_why=f"leader and term held: {L0} term {T0} at the baseline, in every "
                                     f"poll and on all nine at the end; election attempts seen that "
                                     f"changed neither: {res['candidates']}")
        else:
            res.update(stability="stable",
                       stability_why=f"no election observed during the observation window: "
                                     f"{L0} term {T0} at the baseline and on all nine at the end")
    return res


def _intervals(rounds, thaw_t):
    """No-leader intervals from [(t, [servers with no leader])].

    A poll sees only its own instant. So each interval has a lower bound
    (first to last flagged poll) and an upper bound (the clean poll before to
    the clean poll after). An interval shorter than one poll gap can be missed.
    """
    out, cur = [], None
    for i, (t, flag) in enumerate(rounds):
        if flag:
            if cur is None:
                cur = {"i0": i, "i1": i, "servers": set(flag)}
            else:
                cur["i1"] = i
                cur["servers"] |= set(flag)
        elif cur is not None:
            out.append(cur)
            cur = None
    if cur is not None:
        out.append(cur)
    gaps = [b[0] - a[0] for a, b in zip(rounds, rounds[1:])]
    gap = statistics.median(gaps) if gaps else None
    items = []
    for c in out:
        t0, t1 = rounds[c["i0"]][0], rounds[c["i1"]][0]
        lo = t1 - t0
        hi = (rounds[c["i1"] + 1][0] - rounds[c["i0"] - 1][0]
              if c["i0"] > 0 and c["i1"] + 1 < len(rounds) else None)
        items.append({"from_s": t0 - thaw_t, "to_s": t1 - thaw_t, "min_s": lo,
                      "max_s": hi, "servers": sorted(c["servers"])})
    res = "polls every %s s" % ("?" if gap is None else _fmt(gap))
    if not items:
        text = f"none seen ({res}; a shorter gap could be missed)"
    else:
        text = "; ".join(
            "+%s..+%s s after the thaw on %s: at least %s s, at most %s" % (
                _fmt(x["from_s"]), _fmt(x["to_s"]), ",".join(x["servers"]),
                _fmt(x["min_s"]), "unbounded" if x["max_s"] is None else _fmt(x["max_s"]) + " s")
            for x in items) + f" ({res})"
    return {"intervals": items, "gap_s": gap, "text": text}


# --- command line ------------------------------------------------------------

def _pairs(args):
    out = []
    for a in args:
        n, _, p = a.partition("=")
        if not n or not p.isdigit():
            sys.exit(f"bad NAME=PORT: {a!r}")
        out.append((n, int(p)))
    return out


def _write(path, readings, src):
    with open(path, "a") as f:
        for r in readings:
            f.write(json.dumps(dict(r, src=src)) + "\n")


def load(path):
    rs = []
    if path and os.path.exists(path):
        with open(path) as f:
            for line in f:
                line = line.strip()
                if line:
                    rs.append(json.loads(line))
    return rs


def load_checks(path, ids):
    want, got = set(ids), {}
    if path and os.path.exists(path):
        with open(path) as f:
            for line in f:
                c = line.rstrip("\n").split("\t")
                if len(c) >= 7 and c[0] in want:
                    got[c[0]] = c[6]
    return got


LOG_LINE = re.compile(r"^\[\d+\] (\d{4}/\d\d/\d\d \d\d:\d\d:\d\d\.\d+) \[(\w+)\] (.*)$")
RAFT_SELF = re.compile(r"RAFT \[(\w+) - _meta_\]")


def elections(log_dir, obs_dir, before_s=1.0):
    """The meta group's lines around each thaw, from a diagnostic run's logs.

    A filter, not an analysis: every `_meta_` Raft line and every metadata
    leader line from all logs, from `before_s` before the thaw to the
    watcher's last reading, in time order, with the server that logged it.
    Peer ids are named from each server's own `RAFT [<id> - _meta_]` lines.
    Log times are the host clock, the same clock as the thaw time.
    """
    lines, names = [], {}
    for fn in sorted(os.listdir(log_dir)):
        if not fn.endswith(".log"):
            continue
        srv = "t-" + fn.split(".")[0]
        with open(os.path.join(log_dir, fn), errors="replace") as f:
            for raw in f:
                m = LOG_LINE.match(raw.rstrip("\n"))
                if not m or ("_meta_" not in m.group(3) and "metadata leader" not in m.group(3)):
                    continue
                t = time.mktime(time.strptime(m.group(1).split(".")[0], "%Y/%m/%d %H:%M:%S"))
                t += float("0." + m.group(1).split(".")[1])
                s = RAFT_SELF.search(m.group(3))
                if s:
                    names[s.group(1)] = srv
                lines.append((t, srv, m.group(2), m.group(3)))
    lines.sort()
    out, thaws = [], []
    for fn in os.listdir(obs_dir):
        if fn.endswith(".thaw"):
            with open(os.path.join(obs_dir, fn)) as f:
                thaws.append((float(f.read().strip()), fn[:-5]))
    for thaw, tag in sorted(thaws):  # thaw order, not file-name order
        end = max((r["t"] for r in load(os.path.join(obs_dir, tag + "-watch.jsonl"))), default=thaw + 30)
        out.append(f"== {tag}: thaw (first CONT) at {time.strftime('%H:%M:%S', time.localtime(thaw))}"
                   f"{('%.6f' % (thaw % 1))[1:]}; lines from -{before_s} s to the watcher's last "
                   f"reading, +{end - thaw:.1f} s")
        sel = [x for x in lines if thaw - before_s <= x[0] <= end]
        if not sel:
            out.append("   (no _meta_ lines in the window -- was the run in diagnostic mode?)")
        for t, srv, lvl, msg in sel:
            for pid, nm in names.items():
                msg = msg.replace(pid, f"{pid}={nm}")
            out.append(f"   {t - thaw:+8.3f}  {srv:<8} [{lvl}] {msg}")
    return "\n".join(out)


def _token(r):
    return {OK: r["leader"], NO_LEADER: "NONE", UNREACHABLE: "UNREACHABLE"}.get(r["kind"], "INVALID")


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = ap.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("snap"); s.add_argument("out"); s.add_argument("--rounds", type=int, default=1)
    s.add_argument("--src", default="snap"); s.add_argument("servers", nargs="+")
    w = sub.add_parser("watch"); w.add_argument("out"); w.add_argument("stop")
    w.add_argument("--interval", type=float, default=0.5); w.add_argument("--max", type=float, default=300)
    w.add_argument("servers", nargs="+")
    k = sub.add_parser("kinds"); k.add_argument("servers", nargs="+")
    g = sub.add_parser("agree"); g.add_argument("file")
    g.add_argument("--expect", required=True); g.add_argument("--frozen")
    r = sub.add_parser("round")
    for o in ("baseline", "watch", "final", "results"):
        r.add_argument("--" + o, required=True)
    r.add_argument("--live", required=True); r.add_argument("--frozen", required=True)
    r.add_argument("--thaw-t", type=float, required=True)
    r.add_argument("--deadline", type=float, required=True)
    for o in ("process", "pre", "recovery"):
        r.add_argument("--" + o, default="")
    e = sub.add_parser("elections"); e.add_argument("log_dir"); e.add_argument("obs_dir")
    a = ap.parse_args(argv)

    if a.cmd == "elections":
        print(elections(a.log_dir, a.obs_dir))
        return 0
    if a.cmd == "snap":
        sv = _pairs(a.servers)
        for i in range(a.rounds):
            if i:
                time.sleep(0.5)
            _write(a.out, poll(sv, i), a.src)
        return 0
    if a.cmd == "watch":
        sv, i, t0 = _pairs(a.servers), 0, time.time()
        while not os.path.exists(a.stop) and time.time() - t0 < a.max:
            start = time.time()
            _write(a.out, poll(sv, i), "watch")
            i += 1
            time.sleep(max(0.0, a.interval - (time.time() - start)))
        return 0
    if a.cmd == "kinds":
        print(" ".join(_token(x) for x in poll(_pairs(a.servers), 0)))
        return 0

    split = lambda v: [x for x in v.split(",") if x]
    if a.cmd == "agree":
        # With --frozen: a baseline (every round, frozen servers silent).
        # Without: one round, all --expect servers. Prints
        # "agreed <leader> <term>", or "<outcome>: <why>".
        rs = load(a.file)
        if a.frozen is not None:
            lead, term, inv, inc = check_baseline(rs, split(a.expect), split(a.frozen), time.time())
            print(f"agreed {lead} {term}" if lead else
                  ("invalid: " if inv else "inconclusive: ") + "; ".join(inv + inc))
        else:
            x = agreement(rs, split(a.expect))
            print(f"agreed {x['leader']} {x['term']}" if x["outcome"] == "agreed"
                  else f"{x['outcome']}: {x['why']}")
        return 0
    ids =split(a.process) + split(a.pre) + split(a.recovery)
    res = classify_round(
        load(a.baseline), load(a.watch), load(a.final), split(a.live), split(a.frozen),
        load_checks(a.results, ids), a.thaw_t, a.deadline,
        process_ids=split(a.process), pre_ids=split(a.pre), recovery_ids=split(a.recovery))
    flat = dict(res)
    flat["leaders_seen"] = ",".join(res["leaders_seen"]) or "-"
    flat["no_leader"] = res["no_leader"]["text"]
    for key, v in flat.items():
        if isinstance(v, float):
            v = _fmt(v)
        print(f"CL_{key}={shlex.quote('-' if v is None else str(v))}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
