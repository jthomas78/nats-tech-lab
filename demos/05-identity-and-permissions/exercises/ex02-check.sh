#!/usr/bin/env bash
# Exercise 02, end to end, with PASS / FAIL per check. Leaves nothing running.
#   exercises/ex02-check.sh
# 02a: each user lists ONE side. The other side is open (the hole).
# 02b: each user lists BOTH sides. The hole is closed.
# Every denial is judged next to a positive control from the same run, and
# from three sides: the client error, the server log, and what was delivered.
set -uo pipefail
source "$(dirname "$0")/../lab/lib.sh"

fails=0
pass() { echo "PASS  $1  $2"; }
fail() { echo "FAIL  $1  $2"; fails=$((fails + 1)); }
check() { local id=$1 msg=$2; shift 2; if "$@"; then pass "$id" "$msg"; else fail "$id" "$msg"; fi; }

cleanup() { "$D05_DIR/lab/down.sh" >/dev/null 2>&1 || true; kill "${sub:-}" "${sub2:-}" 2>/dev/null || true; }
trap cleanup EXIT

if d05_port_busy; then
  echo "port 4522 is in use. Stop the other demo 05 server first." >&2
  exit 1
fi

echo "nats-server $(nats-server --version | awk '{print $2}') · nats CLI $(nats --version) · $(date '+%Y-%m-%d %H:%M %Z')"
echo

as_order()  { d05_nats --user order-svc --password "$D05_ORDER_SVC_PASSWORD" "$@" 2>&1; echo "exit=$?"; }
as_reader() { d05_nats --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" "$@" 2>&1; echo "exit=$?"; }

# --- 02a: one side listed ----------------------------------------------------
echo "== 02a  one side listed (the hole)"
"$D05_DIR/lab/up.sh" ex02-nats-permissions-one-sided >/dev/null || { echo "02a server did not start" >&2; exit 1; }
source "$D05_SECRETS"

# The listener is the positive control. It stays connected for the whole step.
out_la="$D05_RUN/ex02a-listener.out"
d05_nats --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" \
  sub 'orders.>' --count 2 --wait 4s > "$out_la" 2>&1 & sub=$!
# The sender also listens. It has no subscribe entry, so this should work.
out_sa="$D05_RUN/ex02a-sender-sub.out"
d05_nats --user order-svc --password "$D05_ORDER_SVC_PASSWORD" \
  sub 'orders.>' --count 2 --wait 4s > "$out_sa" 2>&1 & sub2=$!
sleep 0.7

r_order=$(as_order pub orders.created 'a-from-order-svc')
r_reader=$(as_reader pub orders.created 'a-from-reader')
wait "$sub" "$sub2" 2>/dev/null

check A1 "positive control: order-svc publish accepted, exit 0" grep -q 'exit=0' <<<"$r_order"
check A2 "hole: analytics-reader publish accepted, exit 0 (nobody limited it)" grep -q 'exit=0' <<<"$r_reader"
check A3 "hole: the reader's message was delivered to the listener" grep -q 'a-from-reader' "$out_la"
check A4 "hole: order-svc could subscribe and received the reader's message" grep -q 'a-from-reader' "$out_sa"
check A5 "no Permissions Violation told to any client" bash -c "! grep -q 'Permissions Violation' <<<\"\$1\$2\$(cat '$out_sa')\"" _ "$r_order" "$r_reader"
violations=$(grep -c 'Violation' "$D05_LOG")
check A6 "server logged zero violations (the hole is silent)" [ "$violations" -eq 0 ]
cp "$D05_LOG" "$D05_RUN/ex02a-server.log"
"$D05_DIR/lab/down.sh" >/dev/null

# --- 02b: both sides listed --------------------------------------------------
echo "== 02b  both sides listed (the hole closed)"
"$D05_DIR/lab/up.sh" ex02-nats-permissions >/dev/null || { echo "02b server did not start" >&2; exit 1; }
source "$D05_SECRETS"

out_lb="$D05_RUN/ex02b-listener.out"
d05_nats --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" \
  sub 'orders.>' --count 5 --wait 6s > "$out_lb" 2>&1 & sub=$!
sleep 0.7

r_good=$(as_order pub orders.created 'b-from-order-svc')
r_pub_out=$(as_order pub invoices.created 'b-invoice')
r_rdr_pub=$(as_reader pub orders.created 'b-from-reader')
r_snd_sub=$(as_order sub 'orders.>' --wait 2s)
r_rdr_sub=$(as_reader sub 'invoices.>' --wait 2s)
wait "$sub"

check B1 "positive control: order-svc publish to orders.created accepted" grep -q 'exit=0' <<<"$r_good"
check B2 "order-svc publish to invoices.created: client told Permissions Violation for Publish" \
  grep -q 'Permissions Violation for Publish to "invoices.created"' <<<"$r_pub_out"
check B3 "analytics-reader publish: client told Permissions Violation for Publish" \
  grep -q 'Permissions Violation for Publish to "orders.created"' <<<"$r_rdr_pub"
check B4 "order-svc subscribe: client told Permissions Violation for Subscription" \
  grep -q 'Permissions Violation for Subscription to "orders.>"' <<<"$r_snd_sub"
check B5 "analytics-reader subscribe to invoices.>: client told Permissions Violation for Subscription" \
  grep -q 'Permissions Violation for Subscription to "invoices.>"' <<<"$r_rdr_sub"

check B6 "positive control: listener received the order-svc message" grep -q 'b-from-order-svc' "$out_lb"
received=$(grep -c 'Received on' "$out_lb")
check B7 "listener received exactly 1 message, nothing from the denied ones" [ "$received" -eq 1 ]
check B8 "nothing from the reader reached the listener" bash -c "! grep -q 'b-from-reader' '$out_lb'"

# Permission errors arrive after the command returns. Give the log a moment.
sleep 0.5
check B9  "server logged the order-svc publish to invoices.created" \
  grep -q 'order-svc.*Publish Violation - Subject "invoices.created"' "$D05_LOG"
check B10 "server logged the analytics-reader publish" \
  grep -q 'analytics-reader.*Publish Violation - Subject "orders.created"' "$D05_LOG"
check B11 "server logged the order-svc subscribe" \
  grep -q 'order-svc.*Subscription Violation - Subject "orders.>"' "$D05_LOG"
check B12 "server logged the analytics-reader subscribe" \
  grep -q 'analytics-reader.*Subscription Violation - Subject "invoices.>"' "$D05_LOG"
violations=$(grep -c 'Violation' "$D05_LOG")
check B13 "server logged exactly 4 violations" [ "$violations" -eq 4 ]
cp "$D05_LOG" "$D05_RUN/ex02b-server.log"

# --- teardown ----------------------------------------------------------------
echo "== teardown"
"$D05_DIR/lab/down.sh" >/dev/null
check T1 "no demo 05 server process left" bash -c "! pgrep -f 'nats-server -c $D05_CONFIGS/' >/dev/null"
check T2 "port 4522 is free" bash -c "! lsof -nP -iTCP:4522 -sTCP:LISTEN >/dev/null 2>&1"

echo
if [[ $fails -eq 0 ]]; then echo "ALL PASS"; else echo "$fails FAILED"; fi
exit $(( fails > 0 ))
