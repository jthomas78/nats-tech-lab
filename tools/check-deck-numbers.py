#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Check a clustering-findings deck against the numbers demo 03 last measured.

    python3 tools/check-deck-numbers.py demos/03-multi-cluster-and-accounts/docs/nats-clustering-findings-v0.9.html
    python3 tools/check-deck-numbers.py --report <REPORT.md> <deck.html>

The deck quotes counts, IDs, verdicts and a run timestamp that all come from
demos/03-multi-cluster-and-accounts/REPORT.md. That report is GENERATED --
every `lab/run-all.sh` rewrites it, and the deck then goes stale silently.
This script says what moved. It changes nothing.

The report keeps three things apart, and so does this script:
  rig checks        did the rig build, seed and read back?  "N passed, M failed"
  procedure checks  did a procedure under test meet its requirement?
                    "N met, M not met" -- a not-met row is an answer, not a
                    broken rig, so it never counts as a failed check
  verdicts          one per procedure run (a `◆` row): passed with a measured
                    interruption, failed, or inconclusive
A procedure run (a "T4 / S -- ..." section) is not a topology, so it is not
counted in "<n> topologies".

--report is for the fixture tests (tools/test-check-deck-numbers.py).

Exit code 0 = the deck matches the report. 1 = something moved. 2 = bad usage.
"""
import collections
import io
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
REPORT = os.path.join(ROOT, 'demos', '03-multi-cluster-and-accounts', 'REPORT.md')

# A slide label names a topology; the report names a check family. The label is
# read left to right, so each marker claims the next "<n> checks" after it.
# Add a row here when the deck grows a topology slide.
MARKERS = [
    ('T5 / H', 'H'),
    ('T2 / A', 'A'),
    ('T2 / E', 'E'),
    ('T6 / G', 'G'),
    ('T1', 'T1'),
    ('T3', 'C'),
    ('T4', 'F'),
    ('T5', 'D'),
    ('E ', 'E'),
]

ENT = [('&#183;', '·'), ('&#8212;', '—'), ('&#8211;', '–'),
       ('&#8594;', '→'), ('&amp;', '&')]


def plain(t):
    for a, b in ENT:
        t = t.replace(a, b)
    # a space, not nothing: </td><td> must not glue "arm64" onto "175 checks"
    return re.sub(r'\s+', ' ', re.sub(r'<[^>]+>', ' ', t))


VERDICT_MARK = '\u25c6'  # ◆
NOTE_MARK = '\U0001f4cb'  # 📋
VERDICT_WORDS = ('passed', 'failed', 'inconclusive')


def read_report(path):
    """-> (ids, checks_per_family, fails_per_family, notes_per_family, header,
    verdicts).

    ids[cid] = (mark, measured, kind) where kind is rig / procedure / verdict /
    note. checks and fails count RIG checks only, per family: a procedure row
    is an answer, not a slide's "all passed" claim. verdicts[cid] = (req, word).
    """
    ids, checks, fails = {}, collections.Counter(), collections.Counter()
    notes = collections.Counter()
    header, verdicts = {}, {}
    shapes, procedure_shapes, shape = set(), set(), None
    for line in io.open(path, encoding='utf-8'):
        m = re.match(r'^\|\s*(✅|❌|%s|%s)\s*`([A-Za-z0-9]+)`\s*\|(.*)$'
                     % (NOTE_MARK, VERDICT_MARK), line)
        if m:
            mark, cid, rest = m.groups()
            fam = re.match(r'^(T1|[A-Z])', cid).group(1)
            cols = [c.strip() for c in rest.split('|')]
            measured = ''
            for c in cols:
                if c.startswith('**') and c.endswith('**'):
                    measured = c.strip('*')
            if mark == VERDICT_MARK:
                kind = 'verdict'
                verdicts[cid] = (cols[1] if len(cols) > 1 else '', measured)
                if shape:
                    procedure_shapes.add(shape)
            elif mark == NOTE_MARK:
                kind = 'note'
                notes[fam] += 1
            elif cols and cols[0].startswith('*procedure*'):
                kind = 'procedure'
            else:
                kind = 'rig'
                checks[fam] += 1
                if mark == '❌':
                    fails[fam] += 1
            ids[cid] = (mark, measured, kind)
            continue
        m = re.match(r'^\|\s*(When|Checks|Rig checks|Procedure checks|'
                     r'Procedure verdicts|Recorded observations)\s*\|\s*(.+?)\s*\|\s*$', line)
        if m:
            header[m.group(1)] = m.group(2).replace('**', '')
        m = re.match(r'^###\s+(T\d[^-]*--.*)$', line)
        if m:
            shape = m.group(1).strip()
            shapes.add(shape)
    header['shapes'] = len(shapes - procedure_shapes)
    return ids, checks, fails, notes, header, verdicts


def header_counts(header):
    """-> dict of the run totals, from either report schema.

    Old schema: "Checks | N passed, M failed". New schema: "Rig checks | N
    passed, M failed" + "Procedure checks | N met, M not met" + "Procedure
    verdicts | N -- ...". A total the report does not state is None.
    """
    out = dict.fromkeys(('rig_pass', 'rig_fail', 'proc_met', 'proc_unmet',
                         'verdicts', 'notes'))
    rig = header.get('Rig checks', header.get('Checks', ''))
    m = re.match(r'^(\d+)\s+passed,\s*(\d+)\s+failed', rig)
    if m:
        out['rig_pass'], out['rig_fail'] = m.groups()
    m = re.match(r'^(\d+)\s+met,\s*(\d+)\s+not met', header.get('Procedure checks', ''))
    if m:
        out['proc_met'], out['proc_unmet'] = m.groups()
    m = re.match(r'^(\d+)', header.get('Procedure verdicts', ''))
    if m:
        out['verdicts'] = m.group(1)
    m = re.match(r'^(\d+)', header.get('Recorded observations', ''))
    if m:
        out['notes'] = m.group(1)
    return out


def main(argv):
    report = REPORT
    if len(argv) == 4 and argv[1] == '--report':
        report, argv = argv[2], [argv[0], argv[3]]
    if len(argv) != 2:
        sys.stderr.write(__doc__)
        return 2
    deck_path = argv[1]
    if not os.path.exists(deck_path):
        sys.stderr.write('no such deck: %s\n' % deck_path)
        return 2
    if not os.path.exists(report):
        sys.stderr.write('no report at %s -- run lab/run-all.sh first\n' % report)
        return 2

    deck = io.open(deck_path, encoding='utf-8').read()
    ids, checks, fails, notes, header, verdicts = read_report(report)
    want = header_counts(header)
    if want['rig_pass'] is None:
        sys.stderr.write('the report states no rig check total -- has its schema changed?\n')
        return 2
    bad, warn = [], []

    print('deck   %s' % os.path.basename(deck_path))
    print('report %s' % header.get('When', '?'))
    print('')

    # --- 1. the totals the deck states about the whole run -------------------
    flat = plain(deck)

    def compare(what, have, pat, groups=1):
        """Each match of pat must equal have (a tuple when groups > 1)."""
        for got in set(re.findall(pat, flat, re.I)):
            got = got if groups > 1 else (got,)
            if tuple(have) != tuple(got):
                bad.append('total %-17s deck says %-9s report says %s'
                           % (what, ' / '.join(got), ' / '.join(str(h) for h in have)))

    # "178 checks passed, 0 failed", "196 rig checks, 0 failed" -- rig only
    compare('rig checks', (want['rig_pass'], want['rig_fail']),
            r'(?<![\d-])(\d+)\s+(?:rig\s+)?checks(?:\s+passed)?,\s*(\d+)\s+failed', 2)
    if want['proc_met'] is not None:
        compare('procedure checks', (want['proc_met'], want['proc_unmet']),
                r'(?<![\d-])(\d+)\s+(?:procedure checks\s+)?met,\s*(\d+)\s+not met', 2)
    elif re.search(r'\bnot met\b', flat):
        bad.append('total procedure checks  deck states them, the report has none')
    compare('verdicts', (want['verdicts'] or '0',),
            r'(?<![\d-])(\d+)\s+procedure verdicts?')
    compare('topologies', (str(header['shapes']),), r'(?<![\d-])(\d+)\s+topologies')
    for pat in (r'plus\s+(\d+)\s+recorded observations',
                r'failed,\s*(\d+)\s+notes'):
        compare('notes', (want['notes'] or '?',), pat)
    if header.get('When', '') not in flat:
        bad.append('run timestamp   deck does not carry "%s"' % header.get('When'))

    # --- 1b. a verdict the deck states for a requirement --------------------
    # Every "D03-R11 ... failed" in the deck must name a verdict the report
    # holds for that requirement. Only the first verdict word after the ID, in
    # the same sentence, is read.
    by_req = collections.defaultdict(set)
    for req, word in verdicts.values():
        by_req[req].add(word.split()[0].lower() if word else '')
    for req in sorted(by_req):
        for m in re.finditer(re.escape(req) + r'\b([^.]{0,160})', flat):
            w = re.search(r'\b(' + '|'.join(VERDICT_WORDS) + r')\b', m.group(1), re.I)
            if w and w.group(1).lower() not in by_req[req]:
                bad.append('verdict %-9s deck says %-12s report says %s'
                           % (req, w.group(1).lower(), ' / '.join(sorted(by_req[req]))))

    # --- 2. the per-slide check counts --------------------------------------
    for raw in re.findall(r'<div class="fam-lbl">(.*?)</div>', deck, re.S):
        label = plain(raw)
        if 'checks' not in label:
            continue
        toks = []
        for m in re.finditer(r'(\d+)\s+checks', label):
            toks.append(('n', m.start(), m.group(1)))
        taken = []  # MARKERS is longest-first, so a later, shorter name that
        for name, fam in MARKERS:  # overlaps one already matched is ignored
            for m in re.finditer(re.escape(name), label):
                if any(m.start() < e and s_ < m.end() for s_, e in taken):
                    continue
                taken.append((m.start(), m.end()))
                toks.append(('f', m.start(), fam))
        toks.sort(key=lambda t: t[1])
        fam = None
        for kind, _, val in toks:
            if kind == 'f':
                fam = val
            elif fam is not None:
                if str(checks[fam]) != val:
                    bad.append('slide %-12s says %-4s checks, report has %s for family %s'
                               % (fam, val, checks[fam], fam))
                if 'passed' in label and fails[fam]:
                    bad.append('slide %-12s says passed, report has %s failed rig check(s) '
                               'in family %s' % (fam, fails[fam], fam))
                fam = None

    # --- 3. every check ID the deck cites must still exist -------------------
    # S<letter><n> are the procedure runs (09-switch-t4-t5.sh).
    cited = set()
    for cid in re.findall(r'<code>([A-Z]|T1)([A-Za-z0-9]{0,4})</code>', deck):
        cited.add(''.join(cid))
    for cid in sorted(c for c in cited if re.match(r'^(T1|[A-H]|S[A-Z])[0-9]', c)):
        if cid not in ids:
            bad.append('check ID        %s is cited but is no longer in the report' % cid)

    # --- 3b. a cited verdict ID must carry the report's verdict word ---------
    for cid in sorted(c for c in cited if c in verdicts):
        word = verdicts[cid][1].lower()
        near = [plain(x) for x in re.findall(r'<code>%s</code>(.{0,400})' % cid, deck, re.S)]
        for text in near:
            w = re.search(r'\b(' + '|'.join(VERDICT_WORDS) + r')\b', text, re.I)
            if w and not word.startswith(w.group(1).lower()):
                bad.append('verdict %-9s deck says %-12s report says %s'
                           % (cid, w.group(1).lower(), word))

    # --- 4. notes carry per-run values -- a human must read these -----------
    for cid in sorted(c for c in cited if c in ids and ids[c][2] == 'note'):
        val = ids[cid][1]
        if re.search(r'\d', val) and len(val) < 40:
            warn.append('%-5s now measures %s' % (cid, val))

    bad = list(dict.fromkeys(bad))  # two patterns can find the same stale number
    for b in bad:
        print('MOVED  %s' % b)
    if warn:
        print('')
        print('These are per-run observations. The deck should not quote them as a')
        print('number -- check the wording still holds:')
        for w in warn:
            print('  %s' % w)
    print('')
    print('%d mismatch(es).' % len(bad) if bad else 'The deck matches the report.')
    return 1 if bad else 0


if __name__ == '__main__':
    sys.exit(main(sys.argv))
