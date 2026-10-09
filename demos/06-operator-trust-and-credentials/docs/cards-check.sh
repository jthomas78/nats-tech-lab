#!/usr/bin/env bash
# Shape guard for the pattern cards deck. PASS / FAIL per check.
#   docs/cards-check.sh
# It checks shape only: both editions exist, every card title is there, each
# card has a pro and a con panel and a verdict, and the provenance page names
# the versions and dates. It can never check that a card is true.
# Starts no server and touches nothing under .run/.
set -uo pipefail

DOCS="$(cd "$(dirname "$0")" && pwd)"
HTML="$DOCS/demo-06-pattern-cards.html"
PDF="$DOCS/demo-06-pattern-cards.pdf"

fails=0
pass() { echo "PASS  $1  $2"; }
fail() { echo "FAIL  $1  $2"; fails=$((fails + 1)); }
check() { local id=$1 msg=$2; shift 2; if "$@"; then pass "$id" "$msg"; else fail "$id" "$msg"; fi; }
has() { grep -qF -- "$1" "$HTML"; }
count() { grep -oF -- "$1" "$HTML" | wc -l | tr -d ' '; }
at_least() { [ "$(count "$1")" -ge "$2" ]; }

TITLES=(
  "The server trusts one key, not a user list"
  "An account must be pushed before its users get in"
  "A push report is not evidence of trust"
  "Permissions travel inside the user JWT"
  "A scoped signing key: the role wins"
  "Expiry ends access without a push"
  "A revocation is only as fast as its push"
  "The client cannot tell you why it was refused"
)
N=${#TITLES[@]}

check K1 "HTML edition exists" test -s "$HTML"
[ -s "$HTML" ] || { echo; echo "$fails FAILED"; exit 1; }
check K2 "PDF edition exists" test -s "$PDF"
check K3 "PDF is not older than the HTML" test ! "$PDF" -ot "$HTML"
check K4 "HTML has a doctype" has "<!doctype html>"
check K5 "A4 page size via @page" grep -qE '@page *\{ *size: *A4' "$HTML"

i=0
for t in "${TITLES[@]}"; do
  i=$((i + 1))
  check "T$i" "card title: $t" grep -qF "<h2>$t</h2>" "$HTML"
done

check K6 "at least $N pro panels" at_least 'class="panel pro"' "$N"
check K7 "at least $N con panels" at_least 'class="panel con"' "$N"
check K8 "at least $N decisions" at_least 'class="decision"' "$N"
check K9 "at least $N verdict pills" at_least '<span class="pill">Verdict</span>' "$N"
check K10 "a retraction is kept" has "Retraction"
check K11 "selection guide present" has "Which card answers your question"
check K12 "provenance page present" has "Where every number came from"
check K13 "provenance names nats-server v2.14.6" has "nats-server v2.14.6"
check K14 "provenance names nats CLI 0.4.0" has "CLI 0.4.0"
check K15 "provenance names the dates" has "2026-10-09"

echo
if [ "$fails" -eq 0 ]; then echo "ALL PASS"; else echo "$fails FAILED"; exit 1; fi
