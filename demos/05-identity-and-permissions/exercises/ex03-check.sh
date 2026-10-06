#!/usr/bin/env bash
# Exercise 03, end to end, with PASS / FAIL per check. Leaves nothing running.
#   exercises/ex03-check.sh
# 03a: deny beats allow; what * and > match.
# 03b: an empty allow list.
# 03c: default_permissions, and whether a user's own block replaces them.
# 03d: a wildcard subscription that overlaps a deny.
# Every denial is judged next to a positive control from the same run, and
# from three sides: the client error, the server log, and what was delivered.
# A check marked "prediction:" tests a docs claim that nobody has measured by
# hand yet. If it fails, that is a finding, not a script bug: check by hand.
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

# 03a needs the observer password. An older secrets file does not have it.
"$D05_DIR/lab/secrets.sh" >/dev/null
source "$D05_SECRETS"
if [[ -z "${D05_AUDIT_OBSERVER_PASSWORD:-}" ]]; then
  echo "$D05_SECRETS has no D05_AUDIT_OBSERVER_PASSWORD. Run: lab/secrets.sh --rotate" >&2
  exit 1
fi

echo "nats-server $(nats-server --version | awk '{print $2}') · nats CLI $(nats --version) · $(date '+%Y-%m-%d %H:%M %Z')"
echo

as_order()  { d05_nats --user order-svc --password "$D05_ORDER_SVC_PASSWORD" "$@" 2>&1; echo "exit=$?"; }
as_reader() { d05_nats --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" "$@" 2>&1; echo "exit=$?"; }
no_violation() { ! grep -q 'Permissions Violation' <<<"$1"; }
violations() { grep -c 'Violation' "$D05_LOG"; }

# --- 03a: deny beats allow; * and > ------------------------------------------
echo "== 03a  deny beats allow; what * and > match"
"$D05_DIR/lab/up.sh" ex03-nats-allow-deny >/dev/null || { echo "03a server did not start" >&2; exit 1; }

# The observer may subscribe to all of orders.>. What it receives is what
# order-svc was allowed to publish.
out_a="$D05_RUN/ex03a-observer.out"
d05_nats --user audit-observer --password "$D05_AUDIT_OBSERVER_PASSWORD" \
  sub 'orders.>' --count 5 --wait 6s > "$out_a" 2>&1 & sub=$!
sleep 0.7

r_one=$(as_order pub orders.created 'one-token')
r_two=$(as_order pub orders.eu.created 'two-tokens')
r_bare=$(as_order pub orders 'bare')
r_both=$(as_order pub orders.internal.audit 'allow-and-deny')
r_star1=$(as_reader sub orders.created --wait 2s)
r_star2=$(as_reader sub orders.eu.created --wait 2s)
wait "$sub"
sleep 0.5

check A1 "positive control: order-svc publish to orders.created accepted, exit 0" grep -q 'exit=0' <<<"$r_one"
check A2 "> matches two tokens: orders.eu.created accepted, exit 0" grep -q 'exit=0' <<<"$r_two"
check A3 "positive control: observer received both allowed messages" \
  bash -c "grep -q 'one-token' '$out_a' && grep -q 'two-tokens' '$out_a'"
check A4 "> does not match bare orders: client told Permissions Violation for Publish" \
  grep -q 'Permissions Violation for Publish to "orders"' <<<"$r_bare"
check A5 "deny wins: orders.internal.audit, client told Permissions Violation for Publish" \
  grep -q 'Permissions Violation for Publish to "orders.internal.audit"' <<<"$r_both"
received=$(grep -c 'Received on' "$out_a")
check A6 "observer received exactly 2 messages, nothing denied" [ "$received" -eq 2 ]
check A7 "* matches one token: reader subscribe to orders.created, no violation" no_violation "$r_star1"
check A8 "* does not match two tokens: reader subscribe to orders.eu.created, client told Permissions Violation" \
  grep -q 'Permissions Violation for Subscription to "orders.eu.created"' <<<"$r_star2"
check A9 "server logged both publish denials" \
  bash -c "grep -q 'order-svc.*Publish Violation - Subject \"orders\"' '$D05_LOG' && grep -q 'order-svc.*Publish Violation - Subject \"orders.internal.audit\"' '$D05_LOG'"
check A10 "server logged the subscribe denial" \
  grep -q 'analytics-reader.*Subscription Violation - Subject "orders.eu.created"' "$D05_LOG"
check A11 "server logged exactly 3 violations" [ "$(violations)" -eq 3 ]
cp "$D05_LOG" "$D05_RUN/ex03a-server.log"
"$D05_DIR/lab/down.sh" >/dev/null

# --- 03b: an empty allow list ------------------------------------------------
echo "== 03b  an empty allow list"
"$D05_DIR/lab/up.sh" ex03-nats-empty-list >/dev/null || { echo "03b server did not start" >&2; exit 1; }

out_b="$D05_RUN/ex03b-listener.out"
d05_nats --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" \
  sub '>' --count 3 --wait 4s > "$out_b" 2>&1 & sub=$!
sleep 0.7

r_empty=$(as_order pub invoices.created 'empty-list')
r_live=$(as_order sub 'orders.>' --wait 2s)
wait "$sub"
sleep 0.5

check B1 "server accepted allow: [] and started" grep -q 'Server is ready' "$D05_LOG"
check B2 "empty list = no restriction: publish to invoices.created accepted, exit 0, no violation" \
  bash -c "grep -q 'exit=0' <<<\"\$1\" && ! grep -q 'Permissions Violation' <<<\"\$1\"" _ "$r_empty"
check B3 "the message outside orders.> was delivered" grep -q 'empty-list' "$out_b"
check B4 "control: the block is live, subscribe denied, client told Permissions Violation" \
  grep -q 'Permissions Violation for Subscription to "orders.>"' <<<"$r_live"
check B5 "server logged the subscribe denial" \
  grep -q 'order-svc.*Subscription Violation - Subject "orders.>"' "$D05_LOG"
check B6 "server logged exactly 1 violation (the publish logged nothing)" [ "$(violations)" -eq 1 ]
cp "$D05_LOG" "$D05_RUN/ex03b-server.log"
"$D05_DIR/lab/down.sh" >/dev/null

# --- 03c: default_permissions ------------------------------------------------
echo "== 03c  default_permissions"
"$D05_DIR/lab/up.sh" ex03-nats-defaults >/dev/null || { echo "03c server did not start" >&2; exit 1; }

# analytics-reader has no block of its own, so the defaults apply to it.
out_c="$D05_RUN/ex03c-listener.out"
d05_nats --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" \
  sub 'orders.>' --count 3 --wait 6s > "$out_c" 2>&1 & sub=$!
sleep 0.7

r_rpub=$(as_reader pub orders.created 'from-reader')
r_opub=$(as_order pub orders.created 'from-order-svc')
r_rsub=$(as_reader sub 'invoices.>' --wait 2s)

# The question: order-svc lists no subscribe side. The defaults allow only
# orders.>. Replaced -> invoices.> is open. Merged -> it is denied.
# Nobody here may publish to invoices.>, so the proof is the server's own
# subscription list, read while the subscription is live.
out_os="$D05_RUN/ex03c-sender-sub.out"
d05_nats --user order-svc --password "$D05_ORDER_SVC_PASSWORD" \
  sub 'invoices.>' --wait 3s > "$out_os" 2>&1 & sub2=$!
sleep 0.7
connz=$(curl -sf "$D05_MONITOR/connz?subs=1&auth=1")
wait "$sub" "$sub2" 2>/dev/null
sleep 0.5

check C1 "defaults allow the reader to subscribe orders.>, no violation" no_violation "$(cat "$out_c")"
check C2 "defaults deny the reader's publish: client told Permissions Violation" \
  grep -q 'Permissions Violation for Publish to "orders.created"' <<<"$r_rpub"
check C3 "positive control: order-svc publish accepted on its own block, exit 0" grep -q 'exit=0' <<<"$r_opub"
check C4 "listener received the order-svc message" grep -q 'from-order-svc' "$out_c"
check C5 "listener did not receive the reader's message" bash -c "! grep -q 'from-reader' '$out_c'"
check C6 "defaults limit the reader's subscribe: invoices.> denied, client told Permissions Violation" \
  grep -q 'Permissions Violation for Subscription to "invoices.>"' <<<"$r_rsub"
check C7 "prediction: own block REPLACES defaults: order-svc subscribe to invoices.>, no violation" \
  no_violation "$(cat "$out_os")"
check C8 "prediction: the server lists order-svc's invoices.> subscription as live" \
  grep -q '"invoices.>"' <<<"$connz"
check C9 "server logged the reader's publish and subscribe denials" \
  bash -c "grep -q 'analytics-reader.*Publish Violation - Subject \"orders.created\"' '$D05_LOG' && grep -q 'analytics-reader.*Subscription Violation - Subject \"invoices.>\"' '$D05_LOG'"
check C10 "prediction: server logged exactly 2 violations, none for order-svc" \
  bash -c "[ \"\$(grep -c 'Violation' '$D05_LOG')\" -eq 2 ] && ! grep -q 'order-svc.*Violation' '$D05_LOG'"
cp "$D05_LOG" "$D05_RUN/ex03c-server.log"
"$D05_DIR/lab/down.sh" >/dev/null

# --- 03d: a wildcard that overlaps a deny ------------------------------------
echo "== 03d  a wildcard subscription that overlaps a deny"
"$D05_DIR/lab/up.sh" ex03-nats-wildcard-overlap >/dev/null || { echo "03d server did not start" >&2; exit 1; }

out_d="$D05_RUN/ex03d-listener.out"
d05_nats --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" \
  sub 'orders.>' --count 3 --wait 5s > "$out_d" 2>&1 & sub=$!
sleep 0.7

r_sib=$(as_order pub orders.created 'sibling')
r_den=$(as_order pub orders.internal.audit 'denied-subject')
r_lit=$(as_reader sub orders.internal.audit --wait 2s)
wait "$sub"
sleep 0.5

check D1 "prediction: wildcard subscription orders.> accepted, no violation told to the client" \
  no_violation "$(cat "$out_d")"
check D2 "order-svc may publish both subjects: both exit 0, no violation" \
  bash -c "grep -q 'exit=0' <<<\"\$1\" && grep -q 'exit=0' <<<\"\$2\" && ! grep -q 'Permissions Violation' <<<\"\$1\$2\"" _ "$r_sib" "$r_den"
check D3 "positive control: listener received the sibling" grep -q 'sibling' "$out_d"
check D4 "prediction: the denied subject was filtered out at delivery" \
  bash -c "! grep -q 'denied-subject' '$out_d'"
check D5 "literal subscribe to orders.internal.audit: client told Permissions Violation" \
  grep -q 'Permissions Violation for Subscription to "orders.internal.audit"' <<<"$r_lit"
check D6 "server logged the literal subscribe denial" \
  grep -q 'analytics-reader.*Subscription Violation - Subject "orders.internal.audit"' "$D05_LOG"
check D7 "prediction: server logged exactly 1 violation (the filtering is silent)" [ "$(violations)" -eq 1 ]
cp "$D05_LOG" "$D05_RUN/ex03d-server.log"

# --- teardown ----------------------------------------------------------------
echo "== teardown"
"$D05_DIR/lab/down.sh" >/dev/null
check T1 "no demo 05 server process left" bash -c "! pgrep -f 'nats-server -c $D05_CONFIGS/' >/dev/null"
check T2 "port 4522 is free" bash -c "! lsof -nP -iTCP:4522 -sTCP:LISTEN >/dev/null 2>&1"

echo
if [[ $fails -eq 0 ]]; then echo "ALL PASS"; else echo "$fails FAILED"; fi
exit $(( fails > 0 ))
