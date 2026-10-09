#!/usr/bin/env bash
# Exercise 03, end to end, with PASS / FAIL per check. Leaves nothing running.
#   exercises/ex03-check.sh
# DELETES .run/ first and builds a new chain (lab/chain.sh). Takes ~45 s.
# Expiry and revocation, each on a NEW connection and on an OPEN one.
set -uo pipefail
source "$(dirname "$0")/../lab/lib.sh"

fails=0
pass() { echo "PASS  $1  $2"; }
fail() { echo "FAIL  $1  $2"; fails=$((fails + 1)); }
check() { local id=$1 msg=$2; shift 2; if "$@"; then pass "$id" "$msg"; else fail "$id" "$msg"; fi; }

# Preflight, before the cleanup trap and before .run/ is deleted: an exit
# here must not stop a server that this run did not start.
if d06_port_busy; then
  echo "port 4922 is in use. Stop the other demo 06 server first." >&2
  exit 1
fi
command -v jq >/dev/null || { echo "ex03-check.sh needs jq (brew install jq)" >&2; exit 1; }
# EPOCHREALTIME (unix time with microseconds) needs bash 5.
(( BASH_VERSINFO[0] >= 5 )) || { echo "ex03-check.sh needs bash 5 (brew install bash)" >&2; exit 1; }

cleanup() { "$D06_DIR/lab/down.sh" >/dev/null 2>&1 || true; kill "${sub:-}" "${sub2:-}" "${ctl_pid:-}" "${new_pid:-}" 2>/dev/null || true; }
trap cleanup EXIT

echo "nats-server $(nats-server --version | awk '{print $2}') · nats CLI $(d06_nats --version) · $(date '+%Y-%m-%d %H:%M %Z')"
echo

"$D06_DIR/lab/down.sh" --clean >/dev/null
"$D06_DIR/lab/chain.sh" >/dev/null || { echo "chain.sh failed" >&2; exit 1; }
"$D06_DIR/lab/up.sh" ex01-nats-operator >/dev/null || { echo "server did not start" >&2; exit 1; }

S="-s $D06_URL"
OS="$D06_CREDS/order-svc.creds"
SYS="$D06_CREDS/sys.creds"
d06_nats auth account push ORDERS $S --creds "$SYS" >/dev/null 2>&1
try() { d06_nats "$@" 2>&1; echo "exit=$?"; }
now() { echo "$EPOCHREALTIME"; }
# Prefix every line a subscriber prints with the unix time it arrived.
stamp() { while IFS= read -r line; do echo "$EPOCHREALTIME $line"; done; }
# True when two unix times are at most 1 s apart.
within_1s() { awk -v a="$1" -v b="$2" 'BEGIN { d = a - b; exit !(d <= 1 && d >= -1) }'; }
# Unix time of the first line matching a pattern in a stamped file.
first_at() { grep -m1 "$2" "$1" | awk '{print $1}'; }
# The client prints `Authorization Violation` for many causes; the server log
# is the other evidence. log_mark = lines in the log now; auth_errors_since N
# = how many `authentication error` lines were written after line N.
# A cut listener retries every ~2 s and each try logs one such line, so count
# only after the cut listener has exited.
log_mark() { wc -l < "$D06_LOG" | tr -d ' '; }
auth_errors_since() { tail -n +$(( $1 + 1 )) "$D06_LOG" | grep -c 'authentication error'; }

# --- 03a: expiry --------------------------------------------------------------
echo "== 03a  a credential that expires in 10 s"
SHORT="$D06_CREDS/order-svc-10s.creds"
d06_nats auth user credential "$SHORT" order-svc ORDERS --expire 10s >/dev/null
exp=$(d06_claims "$SHORT" | jq -r .exp); iat=$(d06_claims "$SHORT" | jq -r .iat)
check E1 "the JWT says exp = iat + 10 s" [ $((exp - iat)) -eq 10 ]

out="$D06_RUN/ex03-expiry.out"
( d06_nats $S --creds "$SHORT" sub 'orders.>' --wait 20s 2>&1; echo "exit=$?" ) | stamp > "$out" & sub=$!
sleep 0.5
d06_nats $S --creds "$OS" pub orders.created 'before-expiry' >/dev/null 2>&1
sleep 0.5
check E2 "before expiry: the short credential connects and receives" grep -q 'before-expiry' "$out"

# Wait until 2 s after exp.
while [[ $(date +%s) -lt $((exp + 2)) ]]; do sleep 0.2; done
cut=$(first_at "$out" 'Disconnected')
check E3 "the OPEN connection was cut within 1 s of exp (cut at ${cut:-never})" \
  within_1s "${cut:-0}" "$exp"
# Positive control for E5: a valid subscriber must receive the same message,
# or "the cut subscriber received nothing" proves nothing.
ctl="$D06_RUN/ex03-control.out"
( d06_nats $S --creds "$OS" sub 'orders.>' --count 1 --wait 5s 2>&1 ) > "$ctl" & ctl_pid=$!
sleep 0.5
r=$(try $S --creds "$OS" pub orders.created 'after-expiry-sent')
wait "$ctl_pid" 2>/dev/null
wait "$sub" 2>/dev/null
check E5a "positive control: the publish succeeded and a valid subscriber received it" \
  bash -c "grep -q 'exit=0' <<<'$r' && grep -q 'after-expiry-sent' '$ctl'"
check E5 "the cut subscriber received nothing sent after expiry" bash -c "! grep -q 'after-expiry-sent' '$out'"
check E6 "trap: the cut subscriber still exited 0" grep -q 'exit=0' "$out"
# The cut subscriber has exited, so the log is quiet: one refusal, one line.
mark=$(log_mark)
r=$(try $S --creds "$SHORT" pub orders.created 'after-expiry')
check E4 "after expiry: a NEW connection is refused" grep -q 'Authorization Violation' <<<"$r"
check E4b "server logged exactly 1 authentication error for that refusal" \
  [ "$(auth_errors_since "$mark")" -eq 1 ]

# --- 03b: revocation ------------------------------------------------------------
echo "== 03b  revoke analytics-reader"
AR="$D06_CREDS/analytics-reader.creds"
d06_nats auth user add analytics-reader ORDERS --defaults \
  --sub-allow 'orders.>' --pub-deny '>' --credential "$AR" >/dev/null

out2="$D06_RUN/ex03-revoke.out"
( d06_nats $S --creds "$AR" sub 'orders.>' --wait 20s 2>&1; echo "exit=$?" ) | stamp > "$out2" & sub2=$!
sleep 0.5

d06_nats auth user rm analytics-reader ORDERS --revoke -f >/dev/null
# Drift: the local account JWT has the revocation; the server's copy does not.
local_copy=$(d06_nats auth account info ORDERS)
server_copy=$(d06_nats auth account query ORDERS $S --creds "$SYS")
check Q1 "not pushed: local copy says Revocations: 1, server copy says 0" \
  bash -c "grep -q 'Revocations: 1' <<<'$local_copy' && grep -q 'Revocations: 0' <<<'$server_copy'"
# A new connection counts as admitted only if it receives a message.
new="$D06_RUN/ex03-revoke-new.out"
( d06_nats $S --creds "$AR" sub 'orders.>' --count 1 --wait 5s 2>&1 ) > "$new" & new_pid=$!
sleep 0.5
d06_nats $S --creds "$OS" pub orders.created 'revoked-not-pushed' >/dev/null 2>&1
wait "$new_pid" 2>/dev/null
check R1 "revoked but NOT pushed: a new connection is still admitted (received a message)" \
  grep -q 'revoked-not-pushed' "$new"
check R2 "revoked but NOT pushed: the open connection still receives" grep -q 'revoked-not-pushed' "$out2"

pushed=$(now)
d06_nats auth account push ORDERS $S --creds "$SYS" >/dev/null 2>&1
sleep 1
cut2=$(first_at "$out2" 'Disconnected')
check R3 "after the push, the OPEN connection was cut within 1 s (cut at ${cut2:-never})" \
  within_1s "${cut2:-0}" "$pushed"
# The cut reader retries until its --wait 20s ends. Wait for that, so the
# log is quiet: one refusal, one line.
wait "$sub2" 2>/dev/null
mark=$(log_mark)
r=$(try $S --creds "$AR" sub 'orders.>' --count 1 --wait 1s)
check R4 "after the push, a NEW connection is refused" grep -q 'Authorization Violation' <<<"$r"
check R4b "server logged exactly 1 authentication error for that refusal" \
  [ "$(auth_errors_since "$mark")" -eq 1 ]
r=$(try $S --creds "$OS" pub orders.created 'after-revoke')
check R5 "positive control: order-svc, same account, still publishes" grep -q 'exit=0' <<<"$r"
check Q2 "after the push: server copy says Revocations: 1" \
  grep -q 'Revocations: 1' <<<"$(d06_nats auth account query ORDERS $S --creds "$SYS")"
# True when /accountz for account $1 lists user key $2 under revoked_user.
revoked_on_server() {
  [[ -n "$1" && -n "$2" ]] || return 1
  curl -sf "$D06_MONITOR/accountz?acc=$1" | jq -e --arg k "$2" '.account_detail.revoked_user | has($k)' >/dev/null
}
check R6 "the server lists the reader's key under revoked_user (/accountz)" \
  revoked_on_server "$(d06_claims "$OS" | jq -r .iss)" "$(d06_claims "$AR" | jq -r .sub)"

cp "$D06_LOG" "$D06_RUN/ex03-server.log"

# --- teardown ----------------------------------------------------------------
echo "== teardown"
"$D06_DIR/lab/down.sh" >/dev/null
check T1 "no demo 06 server process left" bash -c "! pgrep -f 'nats-server -c $D06_CONFIGS/' >/dev/null"
check T2 "port 4922 is free" bash -c "! lsof -nP -iTCP:4922 -sTCP:LISTEN >/dev/null 2>&1"

echo
if [[ $fails -eq 0 ]]; then echo "ALL PASS"; else echo "$fails FAILED"; fi
exit $(( fails > 0 ))
