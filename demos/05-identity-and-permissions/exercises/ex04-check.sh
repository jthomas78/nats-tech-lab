#!/usr/bin/env bash
# Exercise 04, end to end, with PASS / FAIL per check. Leaves nothing running.
#   exercises/ex04-check.sh
# 04a: the requester's inbox, allowed (AA), then denied (AD).
# 04b: the responder's reply, with no publish grant (BN), then with
#      allow_responses (BR).
#
# Each case has three groups of evidence. One group never stands in for another:
#   S  server enforcement  the server log for that part (lab/up.sh resets it)
#   D  delivery            what the requester, the responder hook and a
#                          listener actually got
#   C  CLI report          what the tool printed, and its exit code
# Every command keeps stdout, stderr and the exit code in separate files under
# .run/ex04/. A delivery check reads both streams, so a reply printed on stderr
# is not missed. A check about WHICH stream says something is a CLI prediction.
#
# This script has NOT been run yet. Every NATS check is [pred]. [rig] checks
# only test that the script captured what it needs.
# At the end, each denial case gets a verdict. A case counts as demonstrated
# only when its positive control, its captures, its S checks and its D checks
# all passed. C checks are reported, but never decide a verdict.
set -uo pipefail
source "$(dirname "$0")/../lab/lib.sh"

X="$D05_RUN/ex04"
HOOK="$D05_DIR/exercises/ex04-responder-hook.sh"

fails=0; pred_pass=0; pred_fail=0; passed=" "
pass() { echo "PASS  $1  $2"; passed+="$1 "; }
fail() { echo "FAIL  $1  $2"; fails=$((fails + 1)); }
check() { local id=$1 msg=$2; shift 2; if "$@"; then pass "$id" "$msg"; else fail "$id" "$msg"; fi; }
pred() { local id=$1 msg=$2; shift 2
  if "$@"; then pass "$id" "[pred] $msg"; pred_pass=$((pred_pass + 1))
  else fail "$id" "[pred] $msg"; pred_fail=$((pred_fail + 1)); fi; }
rig() { local id=$1 msg=$2; shift 2; check "$id" "[rig]  $msg" "$@"; }
info() { echo "info  $1"; }
ok() { [[ "$passed" == *" $1 "* ]]; }

cleanup() { "$D05_DIR/lab/down.sh" >/dev/null 2>&1 || true; kill "${resp:-}" "${lis:-}" 2>/dev/null || true; }
trap cleanup EXIT

if d05_port_busy; then
  echo "port 4522 is in use. Stop the other demo 05 server first." >&2
  exit 1
fi
[[ -x "$HOOK" ]] || { echo "$HOOK is missing or not executable" >&2; exit 1; }

rm -rf "$X"; mkdir -p "$X"
{
  echo "nats-server $(nats-server --version 2>&1)"
  echo "nats CLI $(nats --version 2>&1)"
  echo "os $(uname -sr)"
  echo "date $(date '+%Y-%m-%d %H:%M:%S %Z')"
} > "$X/versions.txt"
tr '\n' ' ' < "$X/versions.txt"; echo; echo

pw() { case $1 in order-svc) echo "$D05_ORDER_SVC_PASSWORD" ;; analytics-reader) echo "$D05_ANALYTICS_READER_PASSWORD" ;; esac; }

# capture <tag> <user> <nats args...>
#   writes $X/<tag>.stdout, .stderr, .exit and .secs (whole seconds, info only)
capture() {
  local tag=$1 user=$2 start; shift 2
  start=$(date +%s)
  d05_nats --user "$user" --password "$(pw "$user")" "$@" > "$X/$tag.stdout" 2> "$X/$tag.stderr"
  echo $? > "$X/$tag.exit"
  echo $(( $(date +%s) - start )) > "$X/$tag.secs"
}
captured() { [[ -f "$X/$1.stdout" && -f "$X/$1.stderr" ]] && grep -qE '^[0-9]+$' "$X/$1.exit" 2>/dev/null; }
said()     { captured "$1" && grep -qF -- "$2" "$X/$1.stdout" "$X/$1.stderr"; }   # either stream
not_said() { captured "$1" && ! grep -qF -- "$2" "$X/$1.stdout" "$X/$1.stderr"; }
on_stdout(){ captured "$1" && grep -qF -- "$2" "$X/$1.stdout"; }
exit_is()  { captured "$1" && [[ "$(cat "$X/$1.exit")" == "$2" ]]; }
show() { captured "$1" || { info "$1: not captured"; return; }
  info "$1 stdout: $(tr '\n' ' ' < "$X/$1.stdout")"
  info "$1 stderr: $(tr '\n' ' ' < "$X/$1.stderr")"
  info "$1 exit=$(cat "$X/$1.exit") after ~$(cat "$X/$1.secs")s"; }

# The responder: the same command in all four cases. The hook records what the
# responder itself received, before it tries to reply.
start_responder() {
  : > "$X/receipts.log"
  d05_nats --user order-svc --password "$D05_ORDER_SVC_PASSWORD" \
    reply orders.summary --command "$HOOK" > "$X/$1.stdout" 2> "$X/$1.stderr" & resp=$!
  sleep 0.7
}
stop_responder() { # <tag>   the exit code is the kill status (143), info only
  kill "$resp" 2>/dev/null; wait "$resp" 2>/dev/null; echo $? > "$X/$1.exit"; resp=
  cp "$X/receipts.log" "$X/$1.receipts"
}
receipts_ok() { [[ -f "$X/$1.receipts" ]]; }
# Exactly one request reached the responder: orders.summary, body 'how many?'.
received_once() { receipts_ok "$1" && [[ "$(cat "$X/$1.receipts")" == $'orders.summary\thow many?' ]]; }

# A listener on _INBOX.> (analytics-reader). It starts before the request, so
# it also gets the real reply: that is its own positive control.
start_listener() {
  d05_nats --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" \
    sub '_INBOX.>' --wait 3s > "$X/$1.stdout" 2> "$X/$1.stderr" & lis=$!
  sleep 0.7
}
stop_listener() { wait "$lis" 2>/dev/null; echo $? > "$X/$1.exit"; lis=; }

ask() { capture "$1" analytics-reader --timeout 2s request orders.summary 'how many?'; }
violations() { grep -c 'Violation' "$D05_LOG"; }
save_log() { cp "$D05_LOG" "$X/$1-server.log"; }
log_has() { grep -qE -- "$1" "$D05_LOG"; }
# all_captured <case> <tags...>: every tag has stdout, stderr and exit, and the
# responder's receipts were copied.
all_captured() { local c=$1 t; shift; for t in "$@"; do captured "$t" || return 1; done; receipts_ok "$c.responder"; }
forge_clean() { exit_is aa.forge 0 && not_said aa.forge 'Permissions Violation'; }

# --- AA: 04a, inbox allowed — the positive control for 04a --------------------
echo "== AA  04a, the requester's inbox allowed (positive control)"
"$D05_DIR/lab/up.sh" ex04-nats-inbox-allowed >/dev/null || { echo "AA server did not start" >&2; exit 1; }
source "$D05_SECRETS"

start_responder aa.responder
start_listener aa.listener
ask aa.request
capture aa.forge order-svc pub _INBOX.forged 'not a reply'
stop_listener aa.listener
stop_responder aa.responder
sleep 0.3
save_log aa
show aa.request; show aa.forge

rig  AA-R1 "captured: request, forge, responder, listener, receipts" \
  all_captured aa aa.request aa.forge aa.responder aa.listener
pred AA-S1 "server logged zero violations (responder, reply and forged reply all allowed)" [ "$(violations)" -eq 0 ]
pred AA-D1 "the requester got 'summary: 3 orders' (either stream)" said aa.request 'summary: 3 orders'
pred AA-D2 "the responder hook recorded exactly one request: orders.summary, 'how many?'" received_once aa.responder
pred AA-D3 "the listener got the real reply (its own positive control)" said aa.listener 'summary: 3 orders'
pred AA-D4 "broad grant: the forged 'not a reply' reached the listener" said aa.listener 'not a reply'
pred AA-C1 "the requester exited 0" exit_is aa.request 0
pred AA-C2 "the reply text is on the requester's stdout" on_stdout aa.request 'summary: 3 orders'
pred AA-C3 "the forged publish exited 0 with no Permissions Violation (either stream)" forge_clean
"$D05_DIR/lab/down.sh" >/dev/null

# --- AD: 04a, inbox denied ---------------------------------------------------
echo "== AD  04a, the requester's inbox denied"
"$D05_DIR/lab/up.sh" ex04-nats-inbox-denied >/dev/null || { echo "AD server did not start" >&2; exit 1; }

start_responder ad.responder
ask ad.request
sleep 0.5
stop_responder ad.responder
save_log ad
show ad.request

rig  AD-R1 "captured: request, responder, receipts" all_captured ad ad.request ad.responder
pred AD-S1 "server logged analytics-reader's Subscription Violation on an _INBOX. subject" \
  log_has 'analytics-reader.*Subscription Violation - Subject "_INBOX\.'
pred AD-S2 "server logged exactly 1 violation in this part (no Publish Violation)" [ "$(violations)" -eq 1 ]
pred AD-D1 "the requester got no 'summary: 3 orders' (neither stream)" not_said ad.request 'summary: 3 orders'
pred AD-D2 "the responder hook recorded exactly one request (only the reply was lost)" received_once ad.responder
# From the docs example only. Which stream holds which line is not claimed.
pred AD-C1 "docs example: the requester printed 'Sending request on' (either stream)" said ad.request 'Sending request on'
pred AD-C2 "docs example: the requester printed no 'error' (either stream)" not_said ad.request 'error'
pred AD-C3 "docs example: the requester exited 0" exit_is ad.request 0
"$D05_DIR/lab/down.sh" >/dev/null

# --- BN: 04b, no publish grant for the responder -----------------------------
echo "== BN  04b, the responder's reply with no publish grant"
"$D05_DIR/lab/up.sh" ex04-nats-no-responses >/dev/null || { echo "BN server did not start" >&2; exit 1; }

start_responder bn.responder
ask bn.request
sleep 0.5
stop_responder bn.responder
save_log bn
show bn.request; show bn.responder

rig  BN-R1 "captured: request, responder, receipts" all_captured bn bn.request bn.responder
pred BN-S1 "server logged order-svc's Publish Violation on an _INBOX. subject" \
  log_has 'order-svc.*Publish Violation - Subject "_INBOX\.'
pred BN-S2 "server logged exactly 1 violation in this part" [ "$(violations)" -eq 1 ]
pred BN-D1 "the requester got no 'summary: 3 orders' (neither stream)" not_said bn.request 'summary: 3 orders'
pred BN-D2 "the responder hook recorded exactly one request" received_once bn.responder
pred BN-C1 "the responder printed Permissions Violation for Publish to \"_INBOX. (either stream)" \
  said bn.responder 'Permissions Violation for Publish to "_INBOX.'
# The requester's own report here is UNKNOWN. It is recorded above (show), not
# checked, and not inferred from 04a: a denied reply is a different case.
info "BN requester report: recorded only, no check (unknown until the hand run)"
"$D05_DIR/lab/down.sh" >/dev/null

# --- BR: 04b, allow_responses ------------------------------------------------
echo "== BR  04b, the responder's reply with allow_responses"
"$D05_DIR/lab/up.sh" ex04-nats-allow-responses >/dev/null || { echo "BR server did not start" >&2; exit 1; }

start_responder br.responder
start_listener br.listener
ask br.request
capture br.forge order-svc pub _INBOX.forged 'not a reply'
stop_listener br.listener
stop_responder br.responder
sleep 0.3
save_log br
show br.request; show br.forge

rig  BR-R1 "captured: request, forge, responder, listener, receipts" \
  all_captured br br.request br.forge br.responder br.listener
pred BR-D1 "allow_responses beats deny '>': the requester got 'summary: 3 orders' (either stream)" said br.request 'summary: 3 orders'
pred BR-D2 "the responder hook recorded exactly one request" received_once br.responder
pred BR-D3 "the listener got the real reply (its own positive control)" said br.listener 'summary: 3 orders'
pred BR-D4 "the forged 'not a reply' did not reach the listener" not_said br.listener 'not a reply'
pred BR-S1 "server logged order-svc's Publish Violation on _INBOX.forged" \
  log_has 'order-svc.*Publish Violation - Subject "_INBOX\.forged"'
pred BR-S2 "every violation in this part is the forged one (the real reply logged none)" \
  bash -c "! grep 'Violation' '$D05_LOG' | grep -vq '_INBOX\.forged'"
pred BR-S3 "server logged exactly 1 violation in this part" [ "$(violations)" -eq 1 ]
pred BR-C1 "the requester exited 0" exit_is br.request 0
pred BR-C2 "the reply text is on the requester's stdout" on_stdout br.request 'summary: 3 orders'
pred BR-C3 "the forged publish printed Permissions Violation for Publish to \"_INBOX.forged\" (either stream)" \
  said br.forge 'Permissions Violation for Publish to "_INBOX.forged"'
info "BR forged publish exit code: recorded only (permission errors are asynchronous)"

# --- teardown ----------------------------------------------------------------
echo "== teardown"
"$D05_DIR/lab/down.sh" >/dev/null
rig T1 "no demo 05 server process left" bash -c "! pgrep -f 'nats-server -c $D05_CONFIGS/' >/dev/null"
rig T2 "port 4522 is free" bash -c "! lsof -nP -iTCP:4522 -sTCP:LISTEN >/dev/null 2>&1"

# --- verdicts ----------------------------------------------------------------
# verdict <name> <required ids...>   C checks are never in the list.
verdict() {
  local name=$1 missing=(); shift
  for id in "$@"; do ok "$id" || missing+=("$id"); done
  if [[ ${#missing[@]} -eq 0 ]]; then echo "CASE  $name: demonstrated"
  else echo "CASE  $name: NOT demonstrated (failed: ${missing[*]})"; fi
}
echo
echo "== verdicts (S and D checks, captures and positive controls only)"
AA_CONTROL=(AA-R1 AA-S1 AA-D1 AA-D2)
BR_CONTROL=(BR-R1 BR-D1 BR-D2 BR-S2)
verdict "AA  04a request/reply works (control)"            "${AA_CONTROL[@]}"
verdict "AA  04a broad grant lets a forged reply through"  "${AA_CONTROL[@]}" AA-D3 AA-D4
verdict "AD  04a denied inbox stops the reply"             "${AA_CONTROL[@]}" AD-R1 AD-S1 AD-S2 AD-D1 AD-D2
verdict "BR  04b allow_responses lets the reply through"   "${BR_CONTROL[@]}"
verdict "BN  04b no publish grant stops the reply"         "${BR_CONTROL[@]}" BN-R1 BN-S1 BN-S2 BN-D1 BN-D2
verdict "BR  04b allow_responses stops a forged reply"     "${BR_CONTROL[@]}" BR-D3 BR-D4 BR-S1 BR-S3

echo
echo "captures and versions: $X"
echo "predictions: $pred_pass passed, $pred_fail failed. Record both in EXERCISE_OBSERVATIONS.md."
if [[ $fails -eq 0 ]]; then echo "ALL PASS"; else echo "$fails FAILED"; fi
exit $(( fails > 0 ))
