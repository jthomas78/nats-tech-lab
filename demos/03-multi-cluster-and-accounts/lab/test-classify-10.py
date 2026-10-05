#!/usr/bin/env python3
"""Fixture tests for classify-10.py. No servers; about 1 s.

    python3 demos/03-multi-cluster-and-accounts/lab/test-classify-10.py

Each case builds the /raftz readings a round would see, then asserts the
three outcomes: experiment, recovery and stability. The point is that
invalid evidence cannot pass, and that an election during a valid recovery
is a leadership disturbance -- never a rig failure or a failed recovery.
"""

import importlib.util
import json
import os
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("classify10", os.path.join(HERE, "classify-10.py"))
c = importlib.util.module_from_spec(spec)
spec.loader.exec_module(c)

ZA = ["t-za-1", "t-za-2", "t-za-3"]
AU = ["t-au-1", "t-au-2", "t-au-3"]
ARB = ["t-arb-1", "t-arb-2", "t-arb-3"]
ALL = ZA + AU + ARB
LIVE = AU + ARB          # za is the region that returns
THAW = 1000.0

PROCESS = ["ML51a", "ML51c"]
PRE = ["ML42"]
RECOVERY = ["ML51", "ML52", "ML53", "ML55", "ML56", "ML57"]


def rd(server, t, rnd, kind="ok", leader=None, term=7, state=None, why="", src="x"):
    """One parsed reading, as parse_reading would return it."""
    if kind == "ok" and state is None:
        state = "LEADER" if leader == server else "FOLLOWER"
    if kind in ("unreachable", "invalid"):
        term = None if kind == "unreachable" else term
    return {"round": rnd, "t": t, "server": server, "port": 0, "kind": kind,
            "leader": leader if kind == "ok" else None, "term": term,
            "state": state if kind in ("ok", "no_leader") else None, "why": why, "src": src}


def poll_round(t, rnd, leader, term, servers=ALL, down=(), src="x", override=None):
    """Every server in `servers` names `leader` in `term`; `down` do not answer."""
    out = []
    for s in servers:
        if s in down:
            out.append(rd(s, t, rnd, "unreachable", why="no answer", src=src))
        elif override and s in override:
            out.append(dict(override[s], round=rnd, t=t, server=s, src=src))
        else:
            out.append(rd(s, t, rnd, "ok", leader, term, src=src))
    return out


def baseline(leader="t-arb-1", term=7, override=None):
    """Two rounds just before the thaw: live servers answer, za is frozen."""
    return (poll_round(THAW - 1.0, 0, leader, term, down=ZA, src="b", override=override)
            + poll_round(THAW - 0.5, 1, leader, term, down=ZA, src="b", override=override))


def watch(rounds):
    """rounds = [(seconds after thaw, leader, term, down, override)]."""
    out = []
    for i, (dt, lead, term, down, ov) in enumerate(rounds):
        out += poll_round(THAW + dt, i, lead, term, down=down, src="w", override=ov)
    return out


def final(leader="t-arb-1", term=7, dt=20.0, override=None, down=()):
    return poll_round(THAW + dt, 0, leader, term, down=down, src="f", override=override)


def passing(**over):
    ch = {i: "PASS" for i in PROCESS + PRE + RECOVERY}
    ch.update(over)
    return ch


def run(b=None, w=None, f=None, checks=None, events=(), deadline=60):
    b = baseline() if b is None else b
    w = watch([(0.2, "t-arb-1", 7, ZA, None), (1.2, "t-arb-1", 7, (), None),
               (2.0, "t-arb-1", 7, (), None)]) if w is None else w
    f = final() if f is None else f
    return c.classify_round(b, w, f, LIVE, ZA, passing() if checks is None else checks,
                            THAW, deadline, PROCESS, PRE, RECOVERY, events)


GOOD_REC = {"recovered"}
GOOD_STAB = {"stable"}


class Parse(unittest.TestCase):
    """One /raftz answer -> one kind. No leader, unreachable, invalid apart."""

    def body(self, **g):
        base = {"id": "AAA", "state": "FOLLOWER", "term": 4, "leader": "BBB",
                "peers": {"BBB": {"name": "t-arb-1"}, "CCC": {"name": "t-au-1"}}}
        base.update(g)
        return ("body", json.dumps({"$SYS": {"_meta_": base}}))

    def p(self, fetched, server="t-za-1"):
        return c.parse_reading(server, 8231, 1.0, 0, fetched)

    def test_ok_names_the_leader_and_source(self):
        r = self.p(self.body())
        self.assertEqual((r["kind"], r["leader"], r["term"], r["server"], r["port"]),
                         ("ok", "t-arb-1", 4, "t-za-1", 8231))

    def test_self_leader(self):
        r = self.p(self.body(leader="AAA", state="LEADER"))
        self.assertEqual((r["kind"], r["leader"]), ("ok", "t-za-1"))

    def test_null_leader_is_no_leader_not_unreachable(self):
        self.assertEqual(self.p(self.body(leader=None))["kind"], "no_leader")
        self.assertEqual(self.p(self.body(leader=""))["kind"], "no_leader")

    def test_timeout_and_refused_are_unreachable(self):
        self.assertEqual(self.p(("timeout", "no answer"))["kind"], "unreachable")
        self.assertEqual(self.p(("refused", "conn refused"))["kind"], "unreachable")

    def test_missing_or_malformed_term_is_invalid(self):
        b = json.loads(self.body()[1]); del b["$SYS"]["_meta_"]["term"]
        self.assertEqual(self.p(("body", json.dumps(b)))["kind"], "invalid")
        self.assertEqual(self.p(self.body(term="4"))["kind"], "invalid")
        self.assertEqual(self.p(self.body(term=True))["kind"], "invalid")
        self.assertEqual(self.p(("body", "{not json"))["kind"], "invalid")
        self.assertEqual(self.p(("body", "{}"))["kind"], "invalid")
        self.assertEqual(self.p(("http", "503"))["kind"], "invalid")

    def test_unnamed_leader_id_is_invalid(self):
        self.assertEqual(self.p(self.body(leader="ZZZ"))["kind"], "invalid")

    def test_wrong_source_is_invalid(self):
        # A server never lists itself as a peer. If it does, the port answered
        # for a different server than the one we think we read.
        r = self.p(self.body(), server="t-arb-1")
        self.assertEqual(r["kind"], "invalid")
        self.assertIn("source mismatch", r["why"])


class Poll(unittest.TestCase):

    def test_a_reading_is_timed_by_its_answer(self):
        # A frozen server answers only after the CONT. Its reading must carry
        # the answer's time, or a post-thaw state lands before the thaw.
        import time as _t
        body = json.dumps({"$SYS": {"_meta_": {"id": "A", "state": "LEADER", "term": 3,
                                               "leader": "A", "peers": {}}}})

        def fetcher(port, timeout):
            if port == 2:
                _t.sleep(0.3)
            return ("body", body)
        rs = c.poll([("t-a", 1), ("t-b", 2)], 0, fetcher=fetcher)
        self.assertEqual(rs[0]["t0"], rs[1]["t0"])
        self.assertGreater(rs[1]["t"] - rs[0]["t"], 0.25)


class Agreement(unittest.TestCase):

    def test_agreed_needs_the_leader_to_say_leader(self):
        rs = poll_round(1, 0, "t-arb-1", 7)
        self.assertEqual(c.agreement(rs, ALL)["outcome"], "agreed")
        rs = [dict(r, state="FOLLOWER") for r in rs]
        self.assertEqual(c.agreement(rs, ALL)["outcome"], "unconfirmed")

    def test_a_stale_leader_field_is_not_agreement(self):
        # Measured: a hub follower kept naming the frozen za-1 while its term
        # rose. Naming a server that is not read cannot be agreement.
        rs = poll_round(1, 0, "t-za-1", 9, servers=ARB)
        self.assertEqual(c.agreement(rs, ARB)["outcome"], "unconfirmed")

    def test_all_no_leader_is_not_agreement(self):
        rs = [rd(s, 1, 0, "no_leader", term=9) for s in ARB]
        self.assertEqual(c.agreement(rs, ARB)["outcome"], "no_leader")

    def test_two_leaders_one_term_is_contradictory(self):
        rs = poll_round(1, 0, "t-arb-1", 7, override={"t-au-1": rd("t-au-1", 1, 0, "ok", "t-au-1", 7)})
        self.assertEqual(c.agreement(rs, ALL)["outcome"], "contradictory")

    def test_missing_server_is_missing(self):
        rs = poll_round(1, 0, "t-arb-1", 7)[1:]
        self.assertEqual(c.agreement(rs, ALL)["outcome"], "missing")

    def test_old_readings_are_stale(self):
        rs = poll_round(1, 0, "t-arb-1", 7)
        self.assertEqual(c.agreement(rs, ALL, now=100, max_age=5)["outcome"], "stale")


class Rounds(unittest.TestCase):
    """The required regression cases, plus the ones that guard the edges."""

    def test_01_same_leader_same_term(self):
        r = run()
        self.assertEqual((r["experiment"], r["recovery"], r["stability"]),
                         ("valid", "recovered", "stable"))
        self.assertIn("no election observed during the observation window", r["stability_why"])

    def test_02_same_leader_higher_term(self):
        w = watch([(0.2, "t-arb-1", 7, ZA, None), (1.2, "t-arb-1", 9, (), None)])
        r = run(w=w, f=final(term=9))
        self.assertEqual((r["experiment"], r["recovery"], r["stability"]),
                         ("valid", "recovered", "disturbed"))
        self.assertEqual(r["term_rise"], 2)
        self.assertIn("is not a count of elections", r["stability_why"])

    def test_03_different_leader_higher_term(self):
        w = watch([(0.2, "t-arb-1", 7, ZA, None), (1.2, "t-au-2", 8, (), None)])
        r = run(w=w, f=final("t-au-2", 8))
        self.assertEqual((r["experiment"], r["recovery"], r["stability"]),
                         ("valid", "recovered", "disturbed"))
        self.assertIn("leader t-arb-1 -> t-au-2", r["stability_why"])

    def test_04_temporary_no_leader_then_recovery(self):
        nl = {s: rd(s, 0, 0, "no_leader", term=8) for s in ALL}
        w = watch([(0.5, "t-arb-1", 7, (), None), (1.0, None, 8, (), nl),
                   (1.5, None, 8, (), nl), (2.0, "t-arb-2", 8, (), None)])
        r = run(w=w, f=final("t-arb-2", 8))
        self.assertEqual((r["recovery"], r["stability"]), ("recovered", "disturbed"))
        iv = r["no_leader"]["intervals"]
        self.assertEqual(len(iv), 1)
        self.assertAlmostEqual(iv[0]["min_s"], 0.5)      # first to last flagged poll
        self.assertAlmostEqual(iv[0]["max_s"], 1.5)      # clean poll before to after
        self.assertIn("at least 0.5 s, at most 1.5 s", r["no_leader"]["text"])

    def test_04b_no_leader_in_the_first_poll_is_bounded_by_the_baseline(self):
        # Measured shape: the first poll after the CONT already shows no
        # leader. The last baseline reading is the clean one before it.
        nl = {s: rd(s, 0, 0, "no_leader", term=8) for s in ALL}
        w = watch([(0.1, None, 8, (), nl), (0.6, "t-au-1", 9, (), None)])
        r = run(w=w, f=final("t-au-1", 9))
        iv = r["no_leader"]["intervals"]
        self.assertEqual(len(iv), 1)
        self.assertAlmostEqual(iv[0]["max_s"], 1.1)      # baseline -0.5 to +0.6
        self.assertNotIn("unbounded", r["no_leader"]["text"])

    def test_04c_returning_server_without_a_leader_is_its_own_view(self):
        # Measured shape (10-20261005-162522, ML85 and ML103): returning
        # servers say CANDIDATE or no leader at a term no higher than the
        # incumbent's, while the incumbent still says LEADER. The group never
        # lost its leader and the term did not rise.
        own = {"t-za-1": rd("t-za-1", 0, 0, "no_leader", term=7, state="CANDIDATE"),
               "t-za-2": rd("t-za-2", 0, 0, "no_leader", term=6, state="FOLLOWER")}
        w = watch([(0.1, "t-arb-1", 7, (), own), (0.6, "t-arb-1", 7, (), None)])
        r = run(w=w)
        self.assertEqual((r["recovery"], r["stability"]), ("recovered", "stable"))
        self.assertEqual(r["no_leader"]["intervals"], [])
        self.assertIn("t-za-1: +0.1 s no leader", r["server_no_leader"])
        self.assertNotIn("t-za-2", r["server_no_leader"])   # term 6 < 7: stale
        self.assertEqual(r["candidates"], "t-za-1 term 7")
        # An attempt was seen, so the text must not say "no election observed".
        self.assertNotIn("no election observed", r["stability_why"])
        self.assertIn("election attempts seen that changed neither", r["stability_why"])

    def test_04d_candidate_above_the_term_is_a_disturbance(self):
        # Measured shape (ML51, ML68): returning servers campaign at T0+1.
        cand = {s: rd(s, 0, 0, "no_leader", term=8, state="CANDIDATE") for s in ZA}
        cand["t-arb-1"] = rd("t-arb-1", 0, 0, "no_leader", term=8, state="FOLLOWER")
        w = watch([(0.1, "t-arb-1", 7, (), cand), (0.6, "t-au-1", 9, (), None)])
        r = run(w=w, f=final("t-au-1", 9))
        self.assertEqual((r["recovery"], r["stability"]), ("recovered", "disturbed"))
        self.assertEqual(len(r["no_leader"]["intervals"]), 1)
        self.assertIn("t-za-1 term 8", r["candidates"])

    def test_04f_poll_straddling_the_thaw_is_not_a_group_reading(self):
        # Measured shape (10-20261005-163907, ML85 and ML129): the watcher's
        # first poll began before the CONT. The live servers answered before
        # the thaw (incumbent LEADER); the returning ones answered after it,
        # with no leader. That poll says nothing about the group after the
        # thaw: no group interval, no gap. c2 read it as both.
        first = (poll_round(THAW - 0.43, 0, "t-arb-1", 7, servers=LIVE, src="w")
                 + [rd(s, THAW + 0.02, 0, "no_leader", term=7, state="CANDIDATE", src="w")
                    for s in ZA])
        w = first + [dict(r, round=1) for r in
                     poll_round(THAW + 0.06, 1, "t-arb-1", 7, src="w")]
        r = run(w=w)
        self.assertEqual((r["recovery"], r["stability"]), ("recovered", "stable"))
        self.assertEqual(r["no_leader"]["intervals"], [])
        self.assertEqual(r["leader_gaps"], 0)
        self.assertIn("t-za-1: +0.0 s no leader (poll straddled the thaw)",
                      r["server_no_leader"])
        self.assertIn("t-za-1 term 7", r["candidates"])

    def test_04g_straddling_poll_still_counts_a_term_rise(self):
        first = (poll_round(THAW - 0.43, 0, "t-arb-1", 7, servers=LIVE, src="w")
                 + [rd(s, THAW + 0.02, 0, "no_leader", term=8, state="CANDIDATE", src="w")
                    for s in ZA])
        w = first + poll_round(THAW + 0.06, 1, "t-arb-1", 7, src="w")
        r = run(w=w)
        self.assertEqual(r["stability"], "disturbed")
        self.assertEqual(r["term_rise"], 1)

    def test_04e_leader_unconfirmed_in_a_poll_cannot_be_stable(self):
        # The incumbent did not answer one poll and nobody denied a leader:
        # an evidence gap, not stability.
        w = watch([(0.5, "t-arb-1", 7, ("t-arb-1",), None), (1.5, "t-arb-1", 7, (), None)])
        r = run(w=w)
        self.assertEqual((r["recovery"], r["stability"]), ("recovered", "unknown"))
        self.assertIn("no server confirmed a leader in 1 poll", r["stability_why"])

    # -- A poll that spans the CONT (c5). It must neither prove an outage
    # -- nor hide one. 04f and 04g are the first two cases of this set.

    def straddle(self, pre=LIVE, post=None, pre_override=None):
        """Round 0 of the watcher: `pre` answered before the thaw, `post` after.

        post = {server: reading}. A server in neither list did not answer.
        """
        out = poll_round(THAW - 0.43, 0, "t-arb-1", 7, servers=pre, src="w",
                         override=pre_override)
        for s_, r in (post or {}).items():
            out.append(dict(r, round=0, t=THAW + 0.02, server=s_, src="w"))
        return out

    def test_04h_pre_thaw_no_leader_cannot_fabricate_an_outage(self):
        # Every live server said no leader -- but before the CONT. Only the
        # post-thaw answers count, and they are the returning servers' own
        # views. No group interval, no gap, not unknown.
        nl = {s: rd(s, 0, 0, "no_leader", term=7) for s in LIVE}
        post = {s: rd(s, 0, 0, "no_leader", term=7, state="CANDIDATE") for s in ZA}
        w = (self.straddle(pre_override=nl, post=post)
             + poll_round(THAW + 0.6, 1, "t-arb-1", 7, src="w"))
        r = run(w=w)
        self.assertEqual((r["recovery"], r["stability"]), ("recovered", "stable"))
        self.assertEqual(r["no_leader"]["intervals"], [])
        self.assertEqual(r["leader_gaps"], 0)
        self.assertEqual(r["live_no_leader"], "none seen")

    def test_04i_straddling_poll_two_leaders_in_one_term_is_not_stable(self):
        # c4 skipped this poll and called the round stable.
        post = {"t-za-1": rd("t-za-1", 0, 0, "ok", "t-za-1", 7),
                "t-za-2": rd("t-za-2", 0, 0, "ok", "t-za-2", 7)}
        w = (self.straddle(post=post)
             + poll_round(THAW + 0.6, 1, "t-arb-1", 7, src="w"))
        r = run(w=w)
        self.assertEqual(r["stability"], "unknown")
        self.assertIn("t-za-1 and t-za-2 all say LEADER in term 7", r["stability_why"])

    def test_04j_straddling_poll_other_leader_in_the_baseline_term_is_not_stable(self):
        post = {"t-za-1": rd("t-za-1", 0, 0, "ok", "t-za-1", 7),
                "t-za-2": rd("t-za-2", 0, 0, "ok", "t-za-1", 7)}
        w = (self.straddle(post=post)
             + poll_round(THAW + 0.6, 1, "t-arb-1", 7, src="w"))
        r = run(w=w)
        self.assertEqual(r["stability"], "unknown")
        self.assertIn("t-za-1 says LEADER in the baseline term 7", r["stability_why"])

    def test_04k_straddling_poll_new_leader_in_a_higher_term_is_disturbed(self):
        post = {s: rd(s, 0, 0, "ok", "t-za-1", 8) for s in ZA}
        w = (self.straddle(post=post)
             + poll_round(THAW + 0.6, 1, "t-za-1", 8, src="w"))
        r = run(w=w, f=final("t-za-1", 8))
        self.assertEqual(r["stability"], "disturbed")
        self.assertIn("t-za-1", r["leaders_seen"])
        self.assertIn("leader t-arb-1 -> t-za-1", r["stability_why"])

    def test_04l_live_server_no_leader_after_the_thaw_is_not_stable(self):
        # A live server answered after the CONT with no leader, and nobody in
        # that poll said LEADER. The poll cannot prove a group outage, so
        # there is no interval -- but the round cannot be stable either.
        pre = [s for s in LIVE if s != "t-au-1"]
        post = {"t-au-1": rd("t-au-1", 0, 0, "no_leader", term=7)}
        post.update({s: rd(s, 0, 0, "no_leader", term=7, state="CANDIDATE") for s in ZA})
        w = (self.straddle(pre=pre, post=post)
             + poll_round(THAW + 0.6, 1, "t-arb-1", 7, src="w"))
        r = run(w=w)
        self.assertEqual((r["recovery"], r["stability"]), ("recovered", "unknown"))
        self.assertEqual(r["no_leader"]["intervals"], [])
        self.assertIn("a live server said no leader after the thaw", r["stability_why"])
        self.assertIn("t-au-1 +0.0 s", r["live_no_leader"])

    def test_04m_live_server_no_leader_beside_a_post_thaw_leader_is_a_view(self):
        # Same poll, but the incumbent also answered after the CONT and said
        # LEADER. The live server's no leader is its own view.
        pre = [s for s in LIVE if s not in ("t-au-1", "t-arb-1")]
        post = {"t-au-1": rd("t-au-1", 0, 0, "no_leader", term=7),
                "t-arb-1": rd("t-arb-1", 0, 0, "ok", "t-arb-1", 7)}
        w = (self.straddle(pre=pre, post=post)
             + poll_round(THAW + 0.6, 1, "t-arb-1", 7, src="w"))
        r = run(w=w)
        self.assertEqual(r["stability"], "stable")
        self.assertEqual(r["live_no_leader"], "none seen")

    def test_04n_only_a_straddling_poll_and_the_final_is_unknown(self):
        # Insufficient evidence: nothing between the CONT and the final
        # reading was a complete poll. Unknown, not stable.
        post = {s: rd(s, 0, 0, "ok", "t-arb-1", 7) for s in ZA}
        r = run(w=self.straddle(post=post))
        self.assertEqual((r["recovery"], r["stability"]), ("recovered", "unknown"))
        self.assertIn("no complete poll between the thaw and the final reading",
                      r["stability_why"])
        self.assertEqual(r["complete_polls"], 0)

    def test_04o_no_watch_at_all_is_unknown(self):
        r = run(w=[])
        self.assertEqual(r["stability"], "unknown")
        self.assertIn("no complete poll", r["stability_why"])

    def test_05a_malformed_term_in_final_cannot_pass(self):
        f = final(override={"t-za-2": rd("t-za-2", 0, 0, "invalid", why="malformed: term '7'")})
        r = run(f=f)
        self.assertEqual((r["experiment"], r["recovery"], r["stability"]),
                         ("valid", "inconclusive", "unknown"))

    def test_05b_missing_term_in_baseline_makes_experiment_inconclusive(self):
        b = baseline(override={"t-arb-2": rd("t-arb-2", 0, 0, "invalid", why="malformed: term None")})
        r = run(b=b)
        self.assertEqual((r["experiment"], r["recovery"], r["stability"]),
                         ("inconclusive", "inconclusive", "unknown"))

    def test_05c_missing_server_in_final_cannot_pass(self):
        r = run(f=final()[:-1])
        self.assertEqual((r["recovery"], r["stability"]), ("inconclusive", "unknown"))

    def test_06a_frozen_servers_unreachable_in_baseline_is_expected(self):
        r = run()
        self.assertEqual(r["experiment"], "valid")

    def test_06b_live_server_unreachable_in_baseline(self):
        b = (poll_round(THAW - 1.0, 0, "t-arb-1", 7, down=ZA + ["t-au-3"], src="b")
             + poll_round(THAW - 0.5, 1, "t-arb-1", 7, down=ZA + ["t-au-3"], src="b"))
        r = run(b=b)
        self.assertEqual((r["experiment"], r["recovery"], r["stability"]),
                         ("inconclusive", "inconclusive", "unknown"))
        self.assertIn("t-au-3", r["experiment_why"])

    def test_06c_server_unreachable_at_the_end_is_not_recovered(self):
        r = run(f=final(down=["t-za-3"]))
        self.assertEqual((r["recovery"], r["stability"]), ("not recovered", "unknown"))

    def test_06d_unreachable_during_the_thaw_is_not_a_failure(self):
        # The frozen servers time out on the first watch polls after the CONT.
        w = watch([(0.0, "t-arb-1", 7, ZA, None), (1.0, "t-arb-1", 7, ZA, None),
                   (2.0, "t-arb-1", 7, (), None)])
        r = run(w=w)
        self.assertEqual((r["experiment"], r["recovery"], r["stability"]),
                         ("valid", "recovered", "stable"))

    def test_07a_conflicting_final_cannot_pass(self):
        f = final(override={"t-au-1": rd("t-au-1", 0, 0, "ok", "t-au-1", 7)})
        r = run(f=f)
        self.assertEqual(r["recovery"], "inconclusive")
        self.assertNotIn(r["stability"], GOOD_STAB)

    def test_07b_stale_lower_term_reading_is_ignored(self):
        # A returning za server first reports its pre-freeze view: lower term.
        old = {s: rd(s, 0, 0, "ok", "t-za-1", 5) for s in ZA}
        w = watch([(0.5, "t-arb-1", 7, (), old), (1.5, "t-arb-1", 7, (), None)])
        r = run(w=w)
        self.assertEqual(r["stability"], "stable")
        self.assertEqual(r["stale_readings"], 3)

    def test_07c_baseline_too_old_is_inconclusive(self):
        b = poll_round(THAW - 30, 0, "t-arb-1", 7, down=ZA, src="b")
        r = run(b=b)
        self.assertEqual(r["experiment"], "inconclusive")
        self.assertIn("stale", r["experiment_why"])

    def test_07d_frozen_server_that_answers_makes_the_experiment_invalid(self):
        b = poll_round(THAW - 0.5, 0, "t-arb-1", 7, down=["t-za-1", "t-za-2"], src="b")
        r = run(b=b)
        self.assertEqual((r["experiment"], r["recovery"], r["stability"]),
                         ("invalid", "inconclusive", "unknown"))

    def test_07e_baseline_that_moves_is_not_stable(self):
        b = (poll_round(THAW - 1.0, 0, "t-arb-1", 7, down=ZA, src="b")
             + poll_round(THAW - 0.5, 1, "t-arb-2", 8, down=ZA, src="b"))
        r = run(b=b)
        self.assertEqual(r["experiment"], "inconclusive")
        self.assertIn("baseline not stable", r["experiment_why"])

    def test_07f_two_leaders_in_the_baseline_term_is_conflicting(self):
        # t-au-1 really says LEADER in term 7, beside the baseline leader.
        odd = {"t-au-1": rd("t-au-1", 0, 0, "ok", "t-au-1", 7)}
        w = watch([(0.5, "t-arb-1", 7, (), odd), (1.5, "t-arb-1", 7, (), None)])
        r = run(w=w)
        self.assertEqual((r["recovery"], r["stability"]), ("recovered", "unknown"))
        self.assertIn("all say LEADER in term 7", r["stability_why"])

    def test_07g_stale_leader_field_is_that_servers_own_view(self):
        # Measured shape (10-20261005-162522, ML85): a returning server names
        # an old leader that does not say LEADER. Not a conflict, not a pass
        # by itself: the incumbent still says LEADER in that poll.
        old = {"t-za-3": rd("t-za-3", 0, 0, "ok", "t-za-2", 7, state="FOLLOWER")}
        w = watch([(0.5, "t-arb-1", 7, (), old), (1.5, "t-arb-1", 7, (), None)])
        r = run(w=w)
        self.assertEqual((r["recovery"], r["stability"]), ("recovered", "stable"))
        self.assertIn("t-za-3: +0.5 s named t-za-2 in term 7, unconfirmed",
                      r["server_no_leader"])

    def test_07h_unconfirmed_name_alone_cannot_be_stable(self):
        # Every server names t-arb-1, but t-arb-1 does not answer: no reading
        # confirms a leader in that poll.
        w = watch([(0.5, "t-arb-1", 7, ("t-arb-1",), None), (1.5, "t-arb-1", 7, (), None)])
        r = run(w=w)
        self.assertEqual(r["stability"], "unknown")

    def test_08_failed_starting_prerequisite(self):
        r = run(checks=passing(ML42="FAIL"))
        self.assertEqual((r["experiment"], r["recovery"], r["stability"]),
                         ("inconclusive", "inconclusive", "unknown"))
        self.assertIn("start check ML42 failed", r["recovery_why"])

    def test_09_recovery_deadline_exceeded(self):
        w = watch([(0.2, "t-arb-1", 7, ZA, None)] +
                  [(t, None, 7, (), {s: rd(s, 0, 0, "no_leader", term=7) for s in ALL})
                   for t in (10.0, 30.0, 61.0)] + [(65.0, "t-arb-1", 7, (), None)])
        r = run(w=w, f=final(dt=66.0), deadline=60)
        self.assertEqual(r["recovery"], "not recovered")
        self.assertIn("deadline exceeded", r["recovery_why"])
        self.assertEqual(r["experiment"], "valid")       # still a valid experiment

    def test_10_stepdown_outside_the_window(self):
        # A step-down before the baseline raised the term to 7; one after the
        # final reading raises it to 8. Neither is inside the window.
        late = poll_round(THAW + 40, 9, "t-arb-2", 8, src="w")
        w = watch([(0.2, "t-arb-1", 7, ZA, None), (1.2, "t-arb-1", 7, (), None)]) + late
        r = run(w=w, events=[(THAW - 20, "step-down --cluster arb"),
                             (THAW + 40, "step-down --cluster arb")])
        self.assertEqual((r["experiment"], r["stability"]), ("valid", "stable"))

    def test_10b_stepdown_inside_the_window_is_invalid(self):
        r = run(events=[(THAW + 1, "step-down --cluster arb")])
        self.assertEqual((r["experiment"], r["stability"]), ("invalid", "unknown"))

    def test_process_not_stopped_is_invalid(self):
        r = run(checks=passing(ML51a="FAIL"))
        self.assertEqual(r["experiment"], "invalid")

    def test_process_check_missing_is_inconclusive(self):
        ch = passing(); del ch["ML51c"]
        self.assertEqual(run(checks=ch)["experiment"], "inconclusive")

    def test_failed_data_check_is_not_recovered_and_stability_stays_separate(self):
        r = run(checks=passing(ML55="FAIL"))
        self.assertEqual((r["recovery"], r["stability"]), ("not recovered", "stable"))

    def test_missing_recovery_check_is_inconclusive(self):
        ch = passing(); del ch["ML57"]
        self.assertEqual(run(checks=ch)["recovery"], "inconclusive")

    def test_an_election_never_fails_recovery_or_the_rig(self):
        for f, w in [
            (final(term=9), watch([(1.0, "t-arb-1", 9, (), None)])),
            (final("t-au-1", 8), watch([(1.0, "t-au-1", 8, (), None)])),
            (final("t-za-2", 12), watch([(1.0, None, 10, (), {s: rd(s, 0, 0, "no_leader", term=10) for s in ALL}),
                                         (2.0, "t-za-2", 12, (), None)])),
        ]:
            r = run(w=w, f=f)
            self.assertEqual((r["experiment"], r["recovery"], r["stability"]),
                             ("valid", "recovered", "disturbed"))

    def test_invalid_evidence_never_passes(self):
        bad = rd("t-za-1", 0, 0, "invalid", why="malformed")
        cases = {
            "malformed final": dict(f=final(override={"t-za-1": bad})),
            "missing final": dict(f=[]),
            "unreachable final": dict(f=final(down=["t-za-1"])),
            "no-leader final": dict(f=final(override={s: rd(s, 0, 0, "no_leader", term=7) for s in ALL})),
            "malformed baseline": dict(b=baseline(override={"t-arb-1": bad})),
            "empty baseline": dict(b=[]),
            "missing checks": dict(checks={}),
            "final before thaw": dict(f=poll_round(THAW - 2, 0, "t-arb-1", 7, src="f")),
        }
        for name, kw in cases.items():
            r = run(**kw)
            self.assertFalse(r["recovery"] in GOOD_REC and r["stability"] in GOOD_STAB,
                             f"{name}: {r['recovery']} / {r['stability']}")
            self.assertNotIn(r["recovery"], GOOD_REC, name)


class Elections(unittest.TestCase):
    """The diagnostic filter: window, order, server, peer names."""

    def test_window_order_and_names(self):
        import tempfile, time as tm
        thaw = tm.mktime(tm.strptime("2026/10/05 16:00:10", "%Y/%m/%d %H:%M:%S")) + 0.5
        with tempfile.TemporaryDirectory() as d:
            logs, obs = os.path.join(d, "log"), os.path.join(d, "obs")
            os.makedirs(logs); os.makedirs(obs)
            with open(os.path.join(logs, "za-1.log"), "w") as f:
                f.write("[1] 2026/10/05 16:00:05.000000 [DBG] RAFT [AAA - _meta_] too early\n"
                        "[1] 2026/10/05 16:00:10.700000 [DBG] RAFT [AAA - _meta_] Switching to candidate\n"
                        "[1] 2026/10/05 16:00:10.800000 [DBG] RAFT [AAA - S-R3F-x] other group\n")
            with open(os.path.join(logs, "arb-1.log"), "w") as f:
                f.write("[2] 2026/10/05 16:00:10.750000 [DBG] RAFT [BBB - _meta_] vote from AAA\n"
                        "[2] 2026/10/05 16:00:11.000000 [INF] JetStream cluster new metadata leader: t-au-1/au\n")
            with open(os.path.join(obs, "ML51.thaw"), "w") as f:
                f.write(f"{thaw}\n")
            with open(os.path.join(obs, "ML51-watch.jsonl"), "w") as f:
                f.write(json.dumps({"t": thaw + 2}) + "\n")
            out = c.elections(logs, obs).splitlines()
        self.assertIn("== ML51", out[0])
        body = out[1:]
        self.assertEqual(len(body), 3)                       # too early and other group dropped
        self.assertIn("+0.200  t-za-1", body[0])
        self.assertIn("vote from AAA=t-za-1", body[1])       # peer id named
        self.assertIn("metadata leader: t-au-1", body[2])


if __name__ == "__main__":
    unittest.main(verbosity=1)
