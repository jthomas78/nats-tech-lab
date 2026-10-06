#!/usr/bin/env bash
# Exercise 04, end to end, with PASS / FAIL per check. Leaves nothing running.
#   exercises/ex04-check.sh
# 04a: the requester's inbox, allowed, then denied.
# 04b: the responder's reply, with no publish grant, then with allow_responses.
# A timeout alone proves nothing, so each part shows the request working first,
# on the same users. Every denial is judged from three sides: the client, the
# server log, and what the responder or a listener actually received.
# A check marked "prediction:" tests a docs claim that nobody has measured by
# hand yet. If it fails, that is a finding, not a script bug: check by hand.
set -uo pipefail
source "$(dirname "$0")/../lab/lib.sh"

fails=0
pass() { echo "PASS  $1  $2"; }
fail() { echo "FAIL  $1  $2"; fails=$((fails + 1)); }
check() { local id=$1 msg=$2; shift 2; if "$@"; then pass "$id" "$msg"; else fail "$id" "$msg"; fi; }
info() { echo "info  $1"; }

cleanup() { "$D05_DIR/lab/down.sh" >/dev/null 2>&1 || true; kill "${resp:-}" "${sub:-}" 2>/dev/null || true; }
trap cleanup EXIT

if d05_port_busy; then
  echo "port 4522 is in use. Stop the other demo 05 server first." >&2
  exit 1
fi

echo "nats-server $(nats-server --version | awk '{print $2}') · nats CLI $(nats --version) · $(date '+%Y-%m-%d %H:%M %Z')"
echo

as_order()  { d05_nats --user order-svc --password "$D05_ORDER_SVC_PASSWORD" "$@" 2>&1; echo "exit=$?"; }
as_reader() { d05_nats --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" "$@" 2>&1; echo "exit=$?"; }
ask() { as_reader --timeout 2s request orders.summary 'how many?'; }
no_violation() { ! grep -q 'Permissions Violation' <<<"$1"; }
violations() { grep -c 'Violation' "$D05_LOG"; }

# The responder runs in the background for one server, then is stopped.
start_responder() {
  d05_nats --user order-svc --password "$D05_ORDER_SVC_PASSWORD" \
    reply orders.summary 'summary: 3 orders' > "$1" 2>&1 & resp=$!
  sleep 0.7
}
stop_responder() { kill "$resp" 2>/dev/null; wait "$resp" 2>/dev/null; resp=; }

# A listener on _INBOX.>, then order-svc publishes a "reply" nobody asked for.
forge() {
  d05_nats --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" \
    sub '_INBOX.>' --count 1 --wait 3s > "$1" 2>&1 & sub=$!
  sleep 0.7
  r_forge=$(as_order pub _INBOX.forged 'not a reply')
  wait "$sub" 2>/dev/null; sub=
  sleep 0.5
}

# --- 04a: the requester's inbox ----------------------------------------------
echo "== 04a  the requester's inbox, allowed"
"$D05_DIR/lab/up.sh" ex04-nats-inbox-allowed >/dev/null || { echo "04a server did not start" >&2; exit 1; }
source "$D05_SECRETS"

out_r1="$D05_RUN/ex04a-responder-allowed.out"
start_responder "$out_r1"
r_ok=$(ask)
out_f1="$D05_RUN/ex04a-forge-listener.out"
forge "$out_f1"
stop_responder

check A1 "responder subscribed with no violation" no_violation "$(cat "$out_r1")"
check A2 "positive control: the request got 'summary: 3 orders', exit 0" \
  bash -c "grep -q 'summary: 3 orders' <<<\"\$1\" && grep -q 'exit=0' <<<\"\$1\"" _ "$r_ok"
# A3 assumes `nats reply` prints the request body. A9 and B2 rely on the same.
# If A3 fails, check that assumption before reading A9 or B2.
check A3 "the responder received the request" grep -q 'how many?' "$out_r1"
check A4 "broad grant: order-svc publish to _INBOX.forged accepted, no violation" \
  bash -c "grep -q 'exit=0' <<<\"\$1\" && ! grep -q 'Permissions Violation' <<<\"\$1\"" _ "$r_forge"
check A5 "broad grant: the forged 'reply' reached the inbox listener" grep -q 'not a reply' "$out_f1"
check A6 "server logged zero violations (the forged reply is silent)" [ "$(violations)" -eq 0 ]
cp "$D05_LOG" "$D05_RUN/ex04a-allowed-server.log"
"$D05_DIR/lab/down.sh" >/dev/null

echo "== 04a  the requester's inbox, denied"
"$D05_DIR/lab/up.sh" ex04-nats-inbox-denied >/dev/null || { echo "04a server did not start" >&2; exit 1; }

out_r2="$D05_RUN/ex04a-responder-denied.out"
start_responder "$out_r2"
r_deny=$(ask)
sleep 0.5
stop_responder
info "04a denied, the requester printed: $(tr '\n' ' ' <<<"$r_deny")"

check A7 "the request got no reply, exit not 0" \
  bash -c "! grep -q 'summary: 3 orders' <<<\"\$1\" && ! grep -q 'exit=0' <<<\"\$1\"" _ "$r_deny"
check A8 "prediction: the requester saw a timeout" grep -qi 'timeout' <<<"$r_deny"
check A9 "the responder still received the request (only the reply was lost)" grep -q 'how many?' "$out_r2"
check A10 "server logged analytics-reader's denied subscribe to an _INBOX subject" \
  grep -q 'analytics-reader.*Subscription Violation - Subject "_INBOX\.' "$D05_LOG"
check A11 "server logged no publish violation (the reply was allowed, then dropped)" \
  bash -c "! grep -q 'Publish Violation' '$D05_LOG'"
cp "$D05_LOG" "$D05_RUN/ex04a-denied-server.log"
"$D05_DIR/lab/down.sh" >/dev/null

# --- 04b: the responder's reply ----------------------------------------------
echo "== 04b  the responder's reply, no publish grant"
"$D05_DIR/lab/up.sh" ex04-nats-no-responses >/dev/null || { echo "04b server did not start" >&2; exit 1; }

out_r3="$D05_RUN/ex04b-responder-none.out"
start_responder "$out_r3"
r_none=$(ask)
sleep 0.5
stop_responder
info "04b no grant, the requester printed: $(tr '\n' ' ' <<<"$r_none")"

check B1 "the request got no reply, exit not 0" \
  bash -c "! grep -q 'summary: 3 orders' <<<\"\$1\" && ! grep -q 'exit=0' <<<\"\$1\"" _ "$r_none"
check B2 "the responder received the request" grep -q 'how many?' "$out_r3"
check B3 "server logged order-svc's denied publish to an _INBOX subject" \
  grep -q 'order-svc.*Publish Violation - Subject "_INBOX\.' "$D05_LOG"
check B4 "prediction: the responder was told Permissions Violation for Publish" \
  grep -q 'Permissions Violation for Publish to "_INBOX\.' "$out_r3"
cp "$D05_LOG" "$D05_RUN/ex04b-none-server.log"
"$D05_DIR/lab/down.sh" >/dev/null

echo "== 04b  the responder's reply, allow_responses"
"$D05_DIR/lab/up.sh" ex04-nats-allow-responses >/dev/null || { echo "04b server did not start" >&2; exit 1; }

out_r4="$D05_RUN/ex04b-responder-allow.out"
start_responder "$out_r4"
r_allow=$(ask)
out_f2="$D05_RUN/ex04b-forge-listener.out"
forge "$out_f2"
stop_responder

check B5 "prediction: allow_responses beats deny '>': the request got 'summary: 3 orders', exit 0" \
  bash -c "grep -q 'summary: 3 orders' <<<\"\$1\" && grep -q 'exit=0' <<<\"\$1\"" _ "$r_allow"
check B6 "prediction: a forged reply is denied: client told Permissions Violation for Publish" \
  grep -q 'Permissions Violation for Publish to "_INBOX.forged"' <<<"$r_forge"
check B7 "the forged 'reply' did not reach the inbox listener" bash -c "! grep -q 'not a reply' '$out_f2'"
check B8 "server logged the forged publish" \
  grep -q 'order-svc.*Publish Violation - Subject "_INBOX.forged"' "$D05_LOG"
check B9 "prediction: server logged exactly 1 violation (the real reply logged none)" [ "$(violations)" -eq 1 ]
cp "$D05_LOG" "$D05_RUN/ex04b-allow-server.log"

# --- teardown ----------------------------------------------------------------
echo "== teardown"
"$D05_DIR/lab/down.sh" >/dev/null
check T1 "no demo 05 server process left" bash -c "! pgrep -f 'nats-server -c $D05_CONFIGS/' >/dev/null"
check T2 "port 4522 is free" bash -c "! lsof -nP -iTCP:4522 -sTCP:LISTEN >/dev/null 2>&1"

echo
if [[ $fails -eq 0 ]]; then echo "ALL PASS"; else echo "$fails FAILED"; fi
exit $(( fails > 0 ))
