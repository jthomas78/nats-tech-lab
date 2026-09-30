#!/usr/bin/env bash
# Exercise 01, end to end, with PASS / FAIL per check. Leaves nothing running.
#   lab/ex01-check.sh
# Every denial is judged next to a positive control from the same run, and
# from two sides: what the client reported, and what the server logged.
set -uo pipefail
source "$(dirname "$0")/lib.sh"

fails=0
pass() { echo "PASS  $1  $2"; }
fail() { echo "FAIL  $1  $2"; fails=$((fails + 1)); }
check() { local id=$1 msg=$2; shift 2; if "$@"; then pass "$id" "$msg"; else fail "$id" "$msg"; fi; }

cleanup() { "$D05_DIR/lab/down.sh" >/dev/null 2>&1 || true; kill "${sub:-}" 2>/dev/null || true; }
trap cleanup EXIT

if d05_port_busy; then
  echo "port 4522 is in use. Stop the other demo 05 server first." >&2
  exit 1
fi

echo "nats-server $(nats-server --version | awk '{print $2}') · nats CLI $(nats --version) · $(date '+%Y-%m-%d %H:%M %Z')"
echo

# --- 01a: open server ------------------------------------------------------
echo "== 01a  no authentication"
"$D05_DIR/lab/up.sh" ex01-a-open >/dev/null || { echo "01a server did not start" >&2; exit 1; }

out_a="$D05_RUN/ex01a-sub.out"
d05_nats sub 'orders.>' --count 1 --wait 5s > "$out_a" 2>&1 & sub=$!
sleep 0.5
d05_nats pub orders.created 'anonymous-order' >/dev/null 2>&1
wait "$sub"
check A1 "anonymous publish reached an anonymous subscriber" grep -q 'anonymous-order' "$out_a"
"$D05_DIR/lab/down.sh" >/dev/null

# --- 01b: users with passwords ---------------------------------------------
echo "== 01b  username / password"
"$D05_DIR/lab/up.sh" ex01-b-users >/dev/null || { echo "01b server did not start" >&2; exit 1; }
source "$D05_SECRETS"

check B0 "server warned about plaintext passwords" grep -q 'Plaintext passwords detected' "$D05_LOG"

# The subscriber is the positive control. It stays connected for the whole
# step, so anything a rejected client managed to send would show up here.
out_b="$D05_RUN/ex01b-sub.out"
d05_nats --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" \
  sub 'orders.>' --count 5 --wait 3s > "$out_b" 2>&1 & sub=$!
sleep 0.5

connz=$(curl -s "$D05_MONITOR/connz?auth=1")
check B1 "/connz shows the subscriber as analytics-reader" \
  grep -q '"authorized_user": "analytics-reader"' <<<"$connz"

# Rejected clients go first. If any got through, it arrives before the good one.
try() { d05_nats "$@" 2>&1; echo "exit=$?"; }
r_wrong=$(try --user order-svc --password not-the-password pub orders.created 'wrong-password-order')
r_none=$(try pub orders.created 'no-credentials-order')
r_unknown=$(try --user mallory --password whatever pub orders.created 'unknown-user-order')
r_good=$(try --user order-svc --password "$D05_ORDER_SVC_PASSWORD" pub orders.created 'order-svc-order')
wait "$sub"

rejected() { grep -q 'Authorization Violation' <<<"$1" && grep -q 'exit=1' <<<"$1"; }
check B2 "wrong password: client told 'Authorization Violation', exit 1" rejected "$r_wrong"
check B3 "no credentials: client told 'Authorization Violation', exit 1"  rejected "$r_none"
check B4 "unknown user: client told 'Authorization Violation', exit 1"    rejected "$r_unknown"
check B5 "correct password: publish accepted, exit 0" grep -q 'exit=0' <<<"$r_good"

check B6 "positive control: subscriber received the order-svc message" grep -q 'order-svc-order' "$out_b"
received=$(grep -c 'Received on' "$out_b")
check B7 "subscriber received exactly 1 message, none from rejected clients" [ "$received" -eq 1 ]

check B8 "server logged the wrong password, naming the user" grep -q 'authentication error - User "order-svc"' "$D05_LOG"
check B9 "server logged the unknown user, naming it"        grep -q 'authentication error - User "mallory"' "$D05_LOG"
no_user=$(grep 'authentication error' "$D05_LOG" | grep -vc 'User "')
check B10 "server logged the no-credentials attempt, with no user" [ "$no_user" -eq 1 ]
errors=$(grep -c 'authentication error' "$D05_LOG")
check B11 "server logged exactly 3 authentication errors" [ "$errors" -eq 3 ]

cp "$D05_LOG" "$D05_RUN/ex01b-server.log"

# --- teardown ----------------------------------------------------------------
echo "== teardown"
"$D05_DIR/lab/down.sh" >/dev/null
check T1 "no demo 05 server process left" bash -c "! pgrep -f 'nats-server -c $D05_CONFIGS/' >/dev/null"
check T2 "port 4522 is free" bash -c "! lsof -nP -iTCP:4522 -sTCP:LISTEN >/dev/null 2>&1"

echo
if [[ $fails -eq 0 ]]; then echo "ALL PASS"; else echo "$fails FAILED"; fi
exit $(( fails > 0 ))
