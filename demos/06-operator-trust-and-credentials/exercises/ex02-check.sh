#!/usr/bin/env bash
# Exercise 02, end to end, with PASS / FAIL per check. Leaves nothing running.
#   exercises/ex02-check.sh
# DELETES .run/ first and builds a new chain (lab/chain.sh).
# Permissions travel inside signed JWTs. The server config never changes and
# the server is never restarted; only the account JWT is pushed, once.
set -uo pipefail
source "$(dirname "$0")/../lab/lib.sh"

fails=0
pass() { echo "PASS  $1  $2"; }
fail() { echo "FAIL  $1  $2"; fails=$((fails + 1)); }
check() { local id=$1 msg=$2; shift 2; if "$@"; then pass "$id" "$msg"; else fail "$id" "$msg"; fi; }

# Preflight, before the cleanup trap: an exit here must not stop a server
# that this run did not start.
if d06_port_busy; then
  echo "port 4922 is in use. Stop the other demo 06 server first." >&2
  exit 1
fi
command -v jq >/dev/null || { echo "ex02-check.sh needs jq (brew install jq)" >&2; exit 1; }

cleanup() { "$D06_DIR/lab/down.sh" >/dev/null 2>&1 || true; kill "${sub:-}" "${sub2:-}" 2>/dev/null || true; }
trap cleanup EXIT

echo "nats-server $(nats-server --version | awk '{print $2}') · nats CLI $(d06_nats --version) · $(date '+%Y-%m-%d %H:%M %Z')"
echo

"$D06_DIR/lab/down.sh" --clean >/dev/null
"$D06_DIR/lab/chain.sh" >/dev/null || { echo "chain.sh failed" >&2; exit 1; }
"$D06_DIR/lab/up.sh" ex01-nats-operator >/dev/null || { echo "server did not start" >&2; exit 1; }
pid0=$(cat "$D06_PID")

S="-s $D06_URL"
OS="$D06_CREDS/order-svc.creds"
SYS="$D06_CREDS/sys.creds"
AR="$D06_CREDS/analytics-reader.creds"
GR="$D06_CREDS/greedy-reader.creds"
d06_nats auth account push ORDERS $S --creds "$SYS" >/dev/null 2>&1
try() { d06_nats "$@" 2>&1; echo "exit=$?"; }
# The client prints the same text for many causes; the server log tells them
# apart. log_mark = lines in the log now; auth_errors_since N = how many
# `authentication error` lines were written after line N.
log_mark() { wc -l < "$D06_LOG" | tr -d ' '; }
auth_errors_since() { tail -n +$(( $1 + 1 )) "$D06_LOG" | grep -c 'authentication error'; }

# --- 02a: permissions in the user JWT ---------------------------------------
echo "== 02a  user permissions, issued while the server runs"
d06_nats auth user add analytics-reader ORDERS --defaults \
  --sub-allow 'orders.>' --pub-deny '>' --credential "$AR" >/dev/null
ar_key=$(d06_claims "$AR" | jq -r .sub)
ar_perms=$(d06_claims "$AR" | jq -c '{pub: .nats.pub, sub: .nats.sub}')
check P1 "analytics-reader JWT carries sub allow orders.> and pub deny >" \
  [ "$ar_perms" = '{"pub":{"deny":[">"]},"sub":{"allow":["orders.>"]}}' ]

out="$D06_RUN/ex02-reader.out"
d06_nats $S --creds "$AR" sub 'orders.>' --count 5 --wait 10s > "$out" 2>&1 & sub=$!
sleep 0.5
r_rpub=$(try $S --creds "$AR" pub orders.created 'from-reader')
r_rsub=$(try $S --creds "$AR" sub 'invoices.>' --count 1 --wait 2s)
r_good=$(try $S --creds "$OS" pub orders.created 'from-order-svc')
sleep 0.5
kill "$sub" 2>/dev/null; wait "$sub" 2>/dev/null

check P2 "reader admitted with no push and no restart (received a message)" grep -q 'from-order-svc' "$out"
check P3 "reader publish denied: client told Permissions Violation" \
  grep -q 'Permissions Violation for Publish to "orders.created"' <<<"$r_rpub"
check P4 "reader subscribe invoices.> denied: client told Permissions Violation" \
  grep -q 'Permissions Violation for Subscription to "invoices.>"' <<<"$r_rsub"
check P4b "server logged the subscribe violation, naming the reader's key" \
  grep -q "jwt:$ar_key\" - Subscription Violation - Subject \"invoices.>\"" "$D06_LOG"
check P5 "server logged the publish violation, naming the user by public key" \
  grep -q "jwt:$ar_key\" - Publish Violation - Subject \"orders.created\"" "$D06_LOG"
check P6 "server log never names the user analytics-reader" \
  bash -c "! grep -q 'analytics-reader' '$D06_LOG'"
check P7 "reader received only the order-svc message, not its own" \
  bash -c "! grep -q 'from-reader' '$out'"

# --- 02b: a scoped signing key ----------------------------------------------
echo "== 02b  scoped signing key 'reader'"
d06_nats auth account keys add ORDERS reader --sub-allow 'orders.>' --pub-deny '>' \
  --description 'read orders only' >/dev/null
# Ask for more than the role allows. The CLI drops the request (measured).
d06_nats auth user add greedy-reader ORDERS --key reader --pub-allow '>' --defaults \
  --credential "$GR" >/dev/null
gr_pub=$(d06_claims "$GR" | jq -c .nats.pub)
check P8 "greedy-reader JWT has NO publish permission of its own (asked for >)" [ "$gr_pub" = "{}" ]

gr_key=$(d06_claims "$GR" | jq -r .sub)
mark=$(log_mark)
r=$(try $S --creds "$GR" pub orders.created 'greedy-before-push')
check P9 "greedy-reader refused before the push: server does not know the key" \
  grep -q 'Authorization Violation' <<<"$r"
check P9b "server logged exactly 1 authentication error for that refusal" \
  [ "$(auth_errors_since "$mark")" -eq 1 ]

d06_nats auth account push ORDERS $S --creds "$SYS" >/dev/null 2>&1
out2="$D06_RUN/ex02-greedy.out"
d06_nats $S --creds "$GR" sub 'orders.>' --count 5 --wait 10s > "$out2" 2>&1 & sub2=$!
sleep 0.5
r_gpub=$(try $S --creds "$GR" pub orders.created 'from-greedy')
r_good2=$(try $S --creds "$OS" pub orders.created 'for-greedy')
sleep 0.5
kill "$sub2" 2>/dev/null; wait "$sub2" 2>/dev/null

check P10 "after the push, greedy-reader is admitted and receives orders" grep -q 'for-greedy' "$out2"
check P11 "greedy-reader publish denied by the role, not by its own JWT" \
  grep -q 'Permissions Violation for Publish to "orders.created"' <<<"$r_gpub"
check P11b "server logged the publish violation, naming greedy-reader's key" \
  grep -q "jwt:$gr_key\" - Publish Violation - Subject \"orders.created\"" "$D06_LOG"
check P12 "positive control: order-svc publishes accepted, exit 0" \
  bash -c "grep -q 'exit=0' <<<'$r_good' && grep -q 'exit=0' <<<'$r_good2'"
check P13 "the same server process the whole time (no restart)" [ "$(cat "$D06_PID")" = "$pid0" ]

cp "$D06_LOG" "$D06_RUN/ex02-server.log"

# --- teardown ----------------------------------------------------------------
echo "== teardown"
"$D06_DIR/lab/down.sh" >/dev/null
check T1 "no demo 06 server process left" bash -c "! pgrep -f 'nats-server -c $D06_CONFIGS/' >/dev/null"
check T2 "port 4922 is free" bash -c "! lsof -nP -iTCP:4922 -sTCP:LISTEN >/dev/null 2>&1"

echo
if [[ $fails -eq 0 ]]; then echo "ALL PASS"; else echo "$fails FAILED"; fi
exit $(( fails > 0 ))
