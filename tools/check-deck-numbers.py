#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Check a clustering-findings deck against the numbers demo 03 last measured.

    python3 tools/check-deck-numbers.py nats-clustering-findings-v0.7.html

The deck quotes counts, IDs and a run timestamp that all come from
demos/03-multi-cluster-and-accounts/REPORT.md. That report is GENERATED --
every `lab/run-all.sh` rewrites it, and the deck then goes stale silently.
This script says what moved. It changes nothing.

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


def read_report():
    """-> (ids, checks_per_family, notes_per_family, header)."""
    ids, checks, notes = {}, collections.Counter(), collections.Counter()
    header, shapes = {}, set()
    for line in io.open(REPORT, encoding='utf-8'):
        m = re.match(r'^\|\s*(✅|❌|\U0001f4cb)\s*`([A-Za-z0-9]+)`\s*\|(.*)$', line)
        if m:
            mark, cid, rest = m.groups()
            fam = re.match(r'^(T1|[A-Z])', cid).group(1)
            (notes if mark == '\U0001f4cb' else checks)[fam] += 1
            cols = [c.strip() for c in rest.split('|')]
            measured = ''
            for c in cols:
                if c.startswith('**') and c.endswith('**'):
                    measured = c.strip('*')
            ids[cid] = (mark, measured)
            continue
        m = re.match(r'^\|\s*(When|Checks|Recorded observations)\s*\|\s*(.+?)\s*\|\s*$', line)
        if m:
            header[m.group(1)] = m.group(2).replace('**', '')
        m = re.match(r'^###\s+(T\d[^-]*--.*)$', line)
        if m:
            shapes.add(m.group(1).strip())
    header['shapes'] = len(shapes)
    return ids, checks, notes, header


def main():
    if len(sys.argv) != 2:
        sys.stderr.write(__doc__)
        return 2
    deck_path = sys.argv[1]
    if not os.path.exists(deck_path):
        sys.stderr.write('no such deck: %s\n' % deck_path)
        return 2
    if not os.path.exists(REPORT):
        sys.stderr.write('no report at %s -- run lab/run-all.sh first\n' % REPORT)
        return 2

    deck = io.open(deck_path, encoding='utf-8').read()
    ids, checks, notes, header = read_report()
    bad, warn = [], []

    print('deck   %s' % os.path.basename(deck_path))
    print('report %s' % header.get('When', '?'))
    print('')

    # --- 1. the totals the deck states about the whole run -------------------
    want_pass = re.match(r'^(\d+)\s+passed', header.get('Checks', '')).group(1)
    want_note = header.get('Recorded observations', '?')
    want_shape = str(header['shapes'])
    flat = plain(deck)
    for what, want, pat in [
            ('checks', want_pass, r'(?<![\d-])(\d+)\s+checks(?:\s+passed)?,\s*0\s+failed'),
            ('topologies', want_shape, r'(?<![\d-])(\d+)\s+topologies'),
            ('notes', want_note, r'plus\s+(\d+)\s+recorded observations'),
            ('notes', want_note, r'0 failed,\s*(\d+)\s+notes'),
    ]:
        for got in set(re.findall(pat, flat)):
            if got != want:
                bad.append('total %-11s deck says %-5s report says %s' % (what, got, want))
    if header.get('When', '') not in flat:
        bad.append('run timestamp   deck does not carry "%s"' % header.get('When'))

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
                fam = None

    # --- 3. every check ID the deck cites must still exist -------------------
    cited = set()
    for cid in re.findall(r'<code>([A-Z]|T1)([A-Za-z0-9]{0,4})</code>', deck):
        cited.add(''.join(cid))
    for cid in sorted(c for c in cited if re.match(r'^(T1|[A-H])[0-9]', c)):
        if cid not in ids:
            bad.append('check ID        %s is cited but is no longer in the report' % cid)

    # --- 4. notes carry per-run values -- a human must read these -----------
    for cid in sorted(c for c in cited if c in ids and ids[c][0] == '\U0001f4cb'):
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
    sys.exit(main())
