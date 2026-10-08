#!/usr/bin/env bash
# Exercise 01, end to end, with PASS / FAIL per check. Leaves nothing running.
#   exercises/ex01-check.sh
# DELETES .run/ first and builds a new chain: old .creds files stop working.
# Every refusal is judged next to a positive control from the same run, and
# from two sides: what the client reported, and what the server logged.
set -uo pipefail
source "$(dirname "$0")/../lab/lib.sh"

fails=0
pass() { echo "PASS  $1  $2"; }
fail() { echo "FAIL  $1  $2"; fails=$((fails + 1)); }
check() { local id=$1 msg=$2; shift 2; if "$@"; then pass "$id" "$msg"; else fail "$id" "$msg"; fi; }

cleanup() { "$D06_DIR/lab/down.sh" >/dev/null 2>&1 || true; kill "${sub:-}" 2>/dev/null || true; }
trap cleanup EXIT

if d06_port_busy; then
  echo "port 4922 is in use. Stop the other demo 06 server first." >&2
  exit 1
fi

echo "nats-server $(nats-server --version | awk '{print $2}') · nats CLI $(nats --version) · $(date '+%Y-%m-%d %H:%M %Z')"
echo

"$D06_DIR/lab/down.sh" --clean >/dev/null
"$D06_DIR/lab/chain.sh" >/dev/null || { echo "chain.sh failed" >&2; exit 1; }
"$D06_DIR/lab/up.sh" ex01-nats-operator >/dev/null || { echo "server did not start" >&2; exit 1; }

S="-s $D06_URL"
OS="$D06_CREDS/order-svc.creds"
SYS="$D06_CREDS/sys.creds"
orders_key=$(d06_claim "$D06_RUN/xdg-data/nats/nsc/stores/D06/accounts/ORDERS/ORDERS.jwt" sub)
try() { d06_nats "$@" 2>&1; echo "exit=$?"; }
refused() { grep -q 'Authorization Violation' <<<"$1" && grep -q 'exit=1' <<<"$1"; }

# --- inspection only: these read claims and logs; they enforce nothing -------
echo "== inspection"
operator_key=$(d06_claim "$D06_RUN/xdg-data/nats/nsc/stores/D06/D06.jwt" sub)
check I1 "[inspect] boot log lists Trusted Operators, Operator \"D06\"" \
  bash -c "grep -q 'Trusted Operators' '$D06_LOG' && grep -q 'Operator: \"D06\"' '$D06_LOG'"
check I2 "[inspect] ORDERS account Issuer is the operator's key" \
  grep -q "Issuer: $operator_key" <<<"$(d06_nats auth account info ORDERS)"
check I3 "[inspect] --defaults put payload 1048576 in the order-svc JWT" \
  grep -q '"payload": 1048576' <<<"$(d06_claim "$OS" nats)"

# --- the server has never heard of ORDERS ---------------------------------
echo "== before the push"
r=$(try $S --creds "$OS" pub orders.created 'before-push')
check C1 "order-svc refused before ORDERS is pushed" refused "$r"
check C2 "server logged a timed-out fetch of the ORDERS account" \
  grep -q "Account \[$orders_key\] fetch took" "$D06_LOG"
r=$(try auth account query ORDERS $S --creds "$SYS")
check Q1 "account query before the push: the server has no copy (exit 1)" \
  bash -c "grep -q 'did not receive a valid token' <<<'$r' && grep -q 'exit=1' <<<'$r'"

# --- push, then the positive control --------------------------------------
echo "== after the push"
d06_nats auth account push ORDERS $S --creds "$SYS" >/dev/null 2>&1
check C3 "ORDERS account JWT now in the resolver directory" test -f "$D06_RESOLVER/$orders_key.jwt"
r=$(try auth account query ORDERS $S --creds "$SYS")
check Q2 "account query after the push: the server returns ORDERS" \
  bash -c "grep -q 'Account ORDERS ($orders_key)' <<<'$r' && grep -q 'exit=0' <<<'$r'"

# The subscriber stays connected for every refusal below. Anything a refused
# client managed to send would arrive here.
out="$D06_RUN/ex01-sub.out"
d06_nats $S --creds "$OS" sub 'orders.>' --count 5 --wait 15s > "$out" 2>&1 & sub=$!
sleep 0.5
connz=$(curl -s "$D06_MONITOR/connz?auth=1")
check C4 "/connz shows the subscriber in account ORDERS" grep -q "\"account\": \"$orders_key\"" <<<"$connz"

# --- refusals -----------------------------------------------------------
echo "== refusals"
r=$(try $S pub orders.created 'no-creds')
check C5 "no credentials: refused" refused "$r"

# A second operator, ROGUE, in its own store. Same account and user names.
rogue() { XDG_DATA_HOME="$D06_RUN/rogue/xdg-data" XDG_CONFIG_HOME="$D06_RUN/rogue/xdg-config" d06_nats "$@"; }
RC="$D06_CREDS/rogue-order-svc.creds"
rogue auth operator add ROGUE >/dev/null
rogue auth account add ORDERS --defaults >/dev/null
rogue auth user add order-svc ORDERS --defaults --credential "$RC" >/dev/null
rogue_key=$(d06_claim "$D06_RUN/rogue/xdg-data/nats/nsc/stores/ROGUE/accounts/ORDERS/ORDERS.jwt" sub)

r=$(try $S --creds "$RC" pub orders.created 'rogue-not-pushed')
check C6 "ROGUE user, account never pushed: refused" refused "$r"

p=$(rogue auth account push ORDERS $S --creds "$SYS" 2>&1)
check C7 "ROGUE account push: the CLI reports success (misleading)" grep -q 'Success 1 Failed 0' <<<"$p"
check C8 "ROGUE account JWT was written to the resolver directory" test -f "$D06_RESOLVER/$rogue_key.jwt"
check C9 "ROGUE account is not loaded (/accountz)" \
  bash -c "! curl -s '$D06_MONITOR/accountz' | grep -q '$rogue_key'"
before=$(grep -c "fetch took" "$D06_LOG")
r=$(try $S --creds "$RC" pub orders.created 'rogue-pushed')
check C10 "ROGUE user, account pushed: still refused" refused "$r"
after=$(grep -c "fetch took" "$D06_LOG")
check C11 "that refusal needed no account fetch (signature rejected locally)" [ "$before" -eq "$after" ]

# The real order-svc JWT, with the SYSTEM admin's seed: the nonce signature fails.
WS="$D06_CREDS/wrong-seed.creds"
python3 - "$OS" "$SYS" "$WS" <<'PY'
import re, sys
seed = lambda t: re.search(r"-----BEGIN USER NKEY SEED-----\n(.*?)\n------END", t, re.S).group(1)
a, b = open(sys.argv[1]).read(), open(sys.argv[2]).read()
open(sys.argv[3], "w").write(a.replace(seed(a), seed(b)))
PY
chmod 600 "$WS"
r=$(try $S --creds "$WS" pub orders.created 'wrong-seed')
check C12 "real order-svc JWT with the wrong seed: refused" refused "$r"

r=$(try $S --creds "$OS" pub orders.created 'order-svc-order')
check C13 "positive control: order-svc publish accepted, exit 0" grep -q 'exit=0' <<<"$r"
sleep 0.5
kill "$sub" 2>/dev/null; wait "$sub" 2>/dev/null

check C14 "positive control: subscriber received the order-svc message" grep -q 'order-svc-order' "$out"
received=$(grep -c 'Received on' "$out")
check C15 "subscriber received exactly 1 message, none from refused clients" [ "$received" -eq 1 ]

users=$(find "$D06_RESOLVER" -name 'U*' | wc -l | tr -d ' ')
check C16 "the server stores no user: no user JWT in the resolver directory" [ "$users" -eq 0 ]
# At least one per refused attempt (C1, C5, C6, C10, C12). Not "exactly 5":
# in 1 run of 3 on 2026-10-08, C1 alone logged 3 errors within 0.3 s, each
# after its own 1.9 s account fetch — the client connected more than once.
# Cause not proven; see EXERCISE_OBSERVATIONS.md.
errors=$(grep -c 'authentication error' "$D06_LOG")
check C17 "server logged at least 5 authentication errors (got $errors)" [ "$errors" -ge 5 ]

cp "$D06_LOG" "$D06_RUN/ex01-server.log"

# --- teardown ----------------------------------------------------------------
echo "== teardown"
"$D06_DIR/lab/down.sh" >/dev/null
check T1 "no demo 06 server process left" bash -c "! pgrep -f 'nats-server -c $D06_CONFIGS/' >/dev/null"
check T2 "port 4922 is free" bash -c "! lsof -nP -iTCP:4922 -sTCP:LISTEN >/dev/null 2>&1"

echo
if [[ $fails -eq 0 ]]; then echo "ALL PASS"; else echo "$fails FAILED"; fi
exit $(( fails > 0 ))
