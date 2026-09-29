#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Fixture tests for tools/check-deck-numbers.py.

    python3 tools/test-check-deck-numbers.py

Builds a small report and a deck that matches it, then breaks one figure at a
time and checks the checker notices -- and names the right figure. Nothing
here reads the real REPORT.md. Exit 0 = every case behaved.
"""
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
CHECKER = os.path.join(HERE, 'check-deck-numbers.py')

REPORT_NEW = u"""# Demo 03 — validation report

| | |
|---|---|
| When | 2026-09-28 13:40 SAST |
| Rig checks | **7 passed, 0 failed** |
| Procedure checks | 2 met, **1 not met** |
| Procedure verdicts | 1 — see the next section |
| Recorded observations | 2 |

### T4 -- one cluster of nine

| | Check | Req | | Expected | Measured | |
|---|---|---|---|---|---|---|
| ✅ `F1` | meta group size | D03-R5 | — | 9 | **9** | — |
| ✅ `F2` | stream placed | D03-R5 | — | za | **za** | — |
| ✅ `F3` | leader region | D03-R5 | — | za | **za** | — |
| 📋 `F4a` | seconds to a leader | D03-R5 | — | — | **7s** | — |

### T5 -- hub and leaf

| | Check | Req | | Expected | Measured | |
|---|---|---|---|---|---|---|
| ✅ `D1` | meta groups | D03-R5 | — | 3 | **3** | — |
| ✅ `D2` | mirror copies | D03-R7 | — | 50 | **50** | — |

### T4 / S -- switch in place, T4 to T5

| | Check | Req | | Expected | Measured | |
|---|---|---|---|---|---|---|
| ✅ `SA1` | before: meta groups | D03-R11 | — | 1 / 9 | **1 / 9** | — |
| ✅ `SA2` | before: read back | D03-R11 | — | match | **match** | — |
| 📋 `SA3` | stop seconds | D03-R11 | — | — | **2s** | — |
| ❌ `SA4` | *procedure* · after switch: meta groups | D03-R11 | — | 3 / 3 | **1 / 9** | — |
| ✅ `SA5` | *procedure* · after switch: read back | D03-R11 | — | match | **match** | — |
| ✅ `SA6` | *procedure* · keep working | D03-R11 | — | 10 | **10** | — |
| ◆ `SA7` | **verdict** · the stop-rewrite-restart procedure, t4 to t5 | D03-R11 | — | rig checks failed: 0 | **failed** | — |
"""

# The same run, in the schema the report had before procedure checks existed.
REPORT_OLD = u"""# Demo 03 — validation report

| | |
|---|---|
| When | 2026-09-28 13:40 SAST |
| Checks | **5 passed, 0 failed** |
| Recorded observations | 1 |

### T4 -- one cluster of nine

| | Check | Req | | Expected | Measured | |
|---|---|---|---|---|---|---|
| ✅ `F1` | meta group size | D03-R5 | — | 9 | **9** | — |
| ✅ `F2` | stream placed | D03-R5 | — | za | **za** | — |
| ✅ `F3` | leader region | D03-R5 | — | za | **za** | — |
| 📋 `F4a` | seconds to a leader | D03-R5 | — | — | **7s** | — |

### T5 -- hub and leaf

| | Check | Req | | Expected | Measured | |
|---|---|---|---|---|---|---|
| ✅ `D1` | meta groups | D03-R5 | — | 3 | **3** | — |
| ✅ `D2` | mirror copies | D03-R7 | — | 50 | **50** | — |
"""

DECK = u"""<html><body>
<p>Measured 2026-09-28 13:40 SAST. 7 checks passed, 0 failed, plus 2 recorded
observations, across 2 topologies.</p>
<p>Procedure runs: 2 procedure checks met, 1 not met, 1 procedure verdict.</p>
<div class="fam-lbl">T4 &#183; 9 servers &#183; 3 checks &#8212; all passed</div>
<div class="fam-lbl">T5 &#183; 9 servers &#183; 2 checks &#8212; all passed</div>
<p>D03-R11 in place, T4 to T5: failed. See <code>SA7</code> failed, and
<code>SA4</code>, <code>F1</code>.</p>
</body></html>
"""

DECK_OLD = u"""<html><body>
<p>Measured 2026-09-28 13:40 SAST. 5 checks passed, 0 failed, plus 1 recorded
observations, across 2 topologies.</p>
<div class="fam-lbl">T4 &#183; 9 servers &#183; 3 checks &#8212; all passed</div>
</body></html>
"""

# name, report, deck edit (old -> new, or None), want exit, text the output must hold
CASES = [
    ('clean deck matches', REPORT_NEW, None, 0, 'The deck matches the report.'),
    ('old report schema still reads', REPORT_OLD, 'OLD', 0, 'The deck matches the report.'),
    ('rig checks passed moved', REPORT_NEW,
     ('7 checks passed, 0 failed', '8 checks passed, 0 failed'), 1, 'total rig checks'),
    ('rig checks failed moved', REPORT_NEW,
     ('7 checks passed, 0 failed', '7 checks passed, 1 failed'), 1, 'total rig checks'),
    ('procedure met moved', REPORT_NEW,
     ('2 procedure checks met, 1 not met', '3 procedure checks met, 1 not met'),
     1, 'total procedure checks'),
    ('procedure not met moved', REPORT_NEW,
     ('2 procedure checks met, 1 not met', '2 procedure checks met, 0 not met'),
     1, 'total procedure checks'),
    ('verdict count moved', REPORT_NEW,
     ('1 procedure verdict.', '3 procedure verdicts.'), 1, 'total verdicts'),
    ('a procedure run is not a topology', REPORT_NEW,
     ('across 2 topologies', 'across 3 topologies'), 1, 'total topologies'),
    ('notes moved', REPORT_NEW,
     ('plus 2 recorded', 'plus 3 recorded'), 1, 'total notes'),
    ('requirement verdict wrong', REPORT_NEW,
     ('T4 to T5: failed.', 'T4 to T5: passed.'), 1, 'verdict D03-R11'),
    ('cited verdict ID wrong', REPORT_NEW,
     ('<code>SA7</code> failed', '<code>SA7</code> inconclusive'), 1, 'verdict SA7'),
    ('cited procedure ID gone', REPORT_NEW,
     ('<code>SA4</code>', '<code>SA99</code>'), 1, 'SA99 is cited'),
    ('slide count moved', REPORT_NEW,
     ('T4 &#183; 9 servers &#183; 3 checks', 'T4 &#183; 9 servers &#183; 4 checks'),
     1, 'slide F'),
    ('run timestamp moved', REPORT_NEW,
     ('Measured 2026-09-28 13:40 SAST', 'Measured 2026-09-29 09:39 SAST'),
     1, 'run timestamp'),
]


def run(report, deck):
    d = tempfile.mkdtemp(prefix='deckcheck-')
    rp, dp = os.path.join(d, 'REPORT.md'), os.path.join(d, 'deck.html')
    with open(rp, 'w', encoding='utf-8') as f:
        f.write(report)
    with open(dp, 'w', encoding='utf-8') as f:
        f.write(deck)
    p = subprocess.run([sys.executable, CHECKER, '--report', rp, dp],
                       capture_output=True, text=True)
    return p.returncode, p.stdout + p.stderr


def main():
    failed = 0
    for name, report, edit, want_rc, want_text in CASES:
        if edit == 'OLD':
            deck = DECK_OLD
        elif edit is None:
            deck = DECK
        else:
            assert edit[0] in DECK, 'fixture edit does not apply: %r' % edit[0]
            deck = DECK.replace(edit[0], edit[1])
        rc, out = run(report, deck)
        ok = rc == want_rc and want_text in out
        failed += not ok
        print('%s  %s' % ('ok  ' if ok else 'FAIL', name))
        if not ok:
            print('      exit %d (want %d); output must hold %r' % (rc, want_rc, want_text))
            print('      ' + out.replace('\n', '\n      '))

    # A failed RIG check under a slide that says "all passed" must be caught.
    rc, out = run(REPORT_NEW.replace('| ✅ `F3` |', '| ❌ `F3` |')
                  .replace('7 passed, 0 failed', '6 passed, 1 failed'),
                  DECK.replace('7 checks passed, 0 failed', '6 checks passed, 1 failed'))
    ok = rc == 1 and 'says passed' in out
    failed += not ok
    print('%s  a failed rig check under "all passed"' % ('ok  ' if ok else 'FAIL'))
    if not ok:
        print('      ' + out.replace('\n', '\n      '))

    print('')
    print('all %d cases behaved' % (len(CASES) + 1) if not failed else '%d case(s) failed' % failed)
    return 1 if failed else 0


if __name__ == '__main__':
    sys.exit(main())
