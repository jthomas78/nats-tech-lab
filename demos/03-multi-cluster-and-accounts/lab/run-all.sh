#!/usr/bin/env bash
# Run every topology, in order, and write REPORT.md and REPORT.html.
#
# This is the whole point of the folder. Demo 03's role is VALIDATION, and the
# playbook says a validation demo fails when the rig is gone and cannot be
# re-run. Before this script existed, demo 03 had no scripts at all -- the
# original measurements in September 2026 were driven by hand through a
# terminal and never captured. This is that rig, written down.
#
#   ./run-all.sh          run every script, then write both reports
#   ./run-all.sh report   re-render both reports from the last run's results
#
# Takes about 40 minutes -- 09 alone is about 25. It starts and stops up to 9 nats-server processes at
# a time, all on 127.0.0.1, all with `t-` prefixed names.

set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

RUN_DIR="$PWD/run"
RESULTS="$RUN_DIR/results.tsv"

LABS=(00-islands.sh 01-gateway.sh 02-domain-over-gateway.sh 03-arbiter.sh
      04-hub-and-leaf.sh 05-export-import.sh 06-arbiter3.sh
      07-gateway-and-hub.sh 08-hub-leaf-per-region.sh 09-switch-t4-t5.sh)

# Two editions of one report, from the same numbers. The HTML one adds the
# nine hand-drawn topology figures from figures.html; the Markdown one does
# not. Both are generated -- never hand-edit either.
render() {
  python3 ./render-report.py "$RESULTS" "$RUN_DIR/env.txt" > ../REPORT.md
  python3 ./render-report.py "$RESULTS" "$RUN_DIR/env.txt" \
          --html ./figures.html > ../REPORT.html
}

if [ "${1:-}" = "report" ]; then
  render; echo "wrote ../REPORT.md and ../REPORT.html"; exit 0
fi

for t in nats-server nats jq curl python3; do
  command -v "$t" >/dev/null 2>&1 || { echo "missing tool: $t" >&2; exit 1; }
done

mkdir -p "$RUN_DIR"
: > "$RESULTS"

# Record the machine, so the report can say what produced the numbers.
{
  echo "when	$(date '+%Y-%m-%d %H:%M %Z')"
  echo "nats-server	$(nats-server --version | sed 's/^nats-server: //')"
  echo "nats-cli	$(nats --version)"
  echo "host	$(uname -srm)"
} > "$RUN_DIR/env.txt"

failed=0
for lab in "${LABS[@]}"; do
  echo
  echo ">>> $lab"
  if ! "./$lab"; then failed=$((failed+1)); echo "!!! $lab exited non-zero"; fi
done

render
echo
echo "=========================================================="
# Column 9 is the row's kind. A missing one is a rig row (older results).
# A procedure row answers "did the procedure work?" -- its FAIL is the answer,
# not a broken rig, so it is counted apart and never fails this script.
awk -F'\t' '$9!="procedure" && $7=="PASS"{p++} $9!="procedure" && $7=="FAIL"{f++}
  $9!="procedure" && $7=="NOTE"{n++}
  $9=="procedure" && $7=="PASS"{pm++} $9=="procedure" && $7=="FAIL"{pn++}
  END{printf "  rig checks:       %d passed, %d failed, %d notes\n", p, f, n
      printf "  procedure checks: %d met, %d not met\n", pm, pn}' "$RESULTS"
# A VERDICT row: $4 what was judged, $5 why (the counts), $6 the verdict words.
awk -F'\t' '$7=="VERDICT"{printf "  PROCEDURE VERDICT %-5s %s %s -- %s\n                    (%s)\n", $1, $2, $4, toupper($6), $5}' "$RESULTS"
echo "  report: demos/03-multi-cluster-and-accounts/REPORT.md"
echo "          demos/03-multi-cluster-and-accounts/REPORT.html"
echo "=========================================================="

# check() records a FAIL row and still returns 0, so a script can finish
# cleanly with failed checks in it. Count the rows, not just the exit codes.
failed_checks=$(awk -F'\t' '$9!="procedure" && $7=="FAIL"{n++} END{print n+0}' "$RESULTS")
if [ "$failed_checks" -ne 0 ]; then
  echo "  !!! $failed_checks rig check(s) FAILED -- see $RESULTS" >&2
fi
[ "$failed" -eq 0 ] && [ "$failed_checks" -eq 0 ]
