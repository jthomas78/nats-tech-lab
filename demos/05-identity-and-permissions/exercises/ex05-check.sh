#!/usr/bin/env bash
# Exercise 05, end to end, with PASS / FAIL per check. Leaves nothing running.
#   exercises/ex05-check.sh
# 05a: one shared token. Who is connected, and what may they do?
# 05b: NKeys. What does the server store, and do per-user limits still work?
# Every refusal is judged next to a positive control from the same run.
# A check marked "prediction:" tests a docs claim that nobody has measured by
# hand yet. If it fails, that is a finding, not a script bug: check by hand.
set -uo pipefail
source "$(dirname "$0")/../lab/lib.sh"

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

"$D05_DIR/lab/secrets.sh" >/dev/null
source "$D05_SECRETS"
if [[ -z "${D05_TOKEN:-}" ]]; then
  echo "$D05_SECRETS has no D05_TOKEN. Run: lab/secrets.sh --rotate" >&2
  exit 1
fi
"$D05_DIR/lab/nkeys.sh" >/dev/null || { echo "lab/nkeys.sh failed" >&2; exit 1; }
source "$D05_NKEYS"

echo "nats-server $(nats-server --version | awk '{print $2}') · nats CLI $(nats --version) · $(date '+%Y-%m-%d %H:%M %Z')"
echo

run() { d05_nats "$@" 2>&1; echo "exit=$?"; }
refused() { grep -q 'Authorization Violation' <<<"$1" && ! grep -q 'exit=0' <<<"$1"; }
auth_errors() { grep -c 'authentication error' "$D05_LOG"; }

# --- 05a: one shared token ---------------------------------------------------
echo "== 05a  one shared token"
r_mix=$(nats-server -t -c "$D05_CONFIGS/ex05-nats-token-and-users.conf" 2>&1)
check A1 "a token next to a users list is rejected: 'Can not have a token and a users array'" \
  grep -q 'Can not have a token and a users array' <<<"$r_mix"

"$D05_DIR/lab/up.sh" ex05-nats-token >/dev/null || { echo "05a server did not start" >&2; exit 1; }

out_a="$D05_RUN/ex05a-listener.out"
d05_nats --token "$D05_TOKEN" sub '>' --count 2 --wait 5s > "$out_a" 2>&1 & sub=$!
sleep 0.7

r_ok=$(run --token "$D05_TOKEN" pub orders.created 'with-token')
r_any=$(run --token "$D05_TOKEN" pub invoices.created 'anything-goes')
r_bad=$(run --token wrong pub orders.created 'bad-token')
r_none=$(run pub orders.created 'no-token')
connz_a=$(curl -sf "$D05_MONITOR/connz?auth=1")
wait "$sub" 2>/dev/null; sub=
sleep 0.5

check A2 "positive control: token publish to orders.created accepted, exit 0" grep -q 'exit=0' <<<"$r_ok"
check A3 "the listener received 'with-token'" grep -q 'with-token' "$out_a"
check A4 "no per-user limits: token publish to invoices.created accepted, exit 0" grep -q 'exit=0' <<<"$r_any"
check A5 "the listener received 'anything-goes'" grep -q 'anything-goes' "$out_a"
check A6 "wrong token refused: Authorization Violation, exit not 0" refused "$r_bad"
check A7 "no token refused: Authorization Violation, exit not 0" refused "$r_none"
check A8 "server logged exactly 2 authentication errors" [ "$(auth_errors)" -eq 2 ]
check A9 "server logged zero permission violations (there are no permissions)" \
  bash -c "! grep -q 'Violation' '$D05_LOG'"
check A10 "prediction: /connz names no user for a token client" \
  bash -c "! grep -q '\"authorized_user\": *\"[^\"]' <<<\"\$1\"" _ "$connz_a"
check A11 "the token is not in the server log" bash -c "! grep -qF \"\$1\" '$D05_LOG'" _ "$D05_TOKEN"
cp "$D05_LOG" "$D05_RUN/ex05a-server.log"
"$D05_DIR/lab/down.sh" >/dev/null

# --- 05b: NKeys --------------------------------------------------------------
echo "== 05b  NKeys"
seeds_ok=true
for n in order-svc analytics-reader stranger; do
  [[ "$(stat -f '%Lp' "$D05_NKEYS_DIR/$n.nk")" == 600 ]] || seeds_ok=false
  [[ "$(cut -c1-2 "$D05_NKEYS_DIR/$n.nk")" == SU ]] || seeds_ok=false
done
check B1 "three seeds in .run/nkeys/, each mode 600, each starting SU" $seeds_ok
check B2 "nkeys.env holds two public keys (U...) and no seed" \
  bash -c "[[ \"\$1\" == U* && \"\$2\" == U* ]] && ! grep -q '=SU' '$D05_NKEYS'" _ "$D05_ORDER_SVC_NKEY" "$D05_ANALYTICS_READER_NKEY"

r_mix=$(nats-server -t -c "$D05_CONFIGS/ex05-nats-nkey-with-password.conf" 2>&1)
check B3 "an NKey user with a password is rejected: 'Nkey users do not take usernames or passwords'" \
  grep -q 'Nkey users do not take usernames or passwords' <<<"$r_mix"

"$D05_DIR/lab/up.sh" ex05-nats-nkey >/dev/null || { echo "05b server did not start" >&2; exit 1; }

out_b="$D05_RUN/ex05b-listener.out"
d05_nats --nkey "$D05_NKEYS_DIR/analytics-reader.nk" sub 'orders.>' --count 3 --wait 5s > "$out_b" 2>&1 & sub=$!
sleep 0.7

r_ok=$(run --nkey "$D05_NKEYS_DIR/order-svc.nk" pub orders.created 'with-nkey')
r_rdr=$(run --nkey "$D05_NKEYS_DIR/analytics-reader.nk" pub orders.created 'reader-publishes')
r_str=$(run --nkey "$D05_NKEYS_DIR/stranger.nk" pub orders.created 'stranger')
r_none=$(run pub orders.created 'no-key')
connz_b=$(curl -sf "$D05_MONITOR/connz?auth=1")
wait "$sub" 2>/dev/null; sub=
sleep 0.5

check B4 "positive control: order-svc NKey publish accepted, exit 0" grep -q 'exit=0' <<<"$r_ok"
check B5 "the listener received 'with-nkey'" grep -q 'with-nkey' "$out_b"
check B6 "per-user limits work: the reader's publish told Permissions Violation" \
  grep -q 'Permissions Violation for Publish to "orders.created"' <<<"$r_rdr"
check B7 "the reader's message was not delivered" bash -c "! grep -q 'reader-publishes' '$out_b'"
check B8 "server logged the reader's denied publish" \
  grep -q 'Publish Violation - Subject "orders.created"' "$D05_LOG"
check B8a "prediction: that log line names the reader by its public key" \
  grep -q "$D05_ANALYTICS_READER_NKEY.*Publish Violation" "$D05_LOG"
check B9 "a key nobody listed is refused: Authorization Violation, exit not 0" refused "$r_str"
check B10 "no key refused: Authorization Violation, exit not 0" refused "$r_none"
check B11 "server logged exactly 2 authentication errors" [ "$(auth_errors)" -eq 2 ]
check B12 "server logged exactly 1 permission violation" [ "$(grep -c 'Violation' "$D05_LOG")" -eq 1 ]
check B13 "prediction: /connz names the reader by its public key" \
  grep -q "\"authorized_user\": *\"$D05_ANALYTICS_READER_NKEY\"" <<<"$connz_b"
seed_in_log=false
for n in order-svc analytics-reader stranger; do
  grep -qF "$(cat "$D05_NKEYS_DIR/$n.nk")" "$D05_LOG" && seed_in_log=true
done
check B14 "no seed appears in the server log" [ "$seed_in_log" = false ]
cp "$D05_LOG" "$D05_RUN/ex05b-server.log"

# --- teardown ----------------------------------------------------------------
echo "== teardown"
"$D05_DIR/lab/down.sh" >/dev/null
check T1 "no demo 05 server process left" bash -c "! pgrep -f 'nats-server -c $D05_CONFIGS/' >/dev/null"
check T2 "port 4522 is free" bash -c "! lsof -nP -iTCP:4522 -sTCP:LISTEN >/dev/null 2>&1"

echo
if [[ $fails -eq 0 ]]; then echo "ALL PASS"; else echo "$fails FAILED"; fi
exit $(( fails > 0 ))
