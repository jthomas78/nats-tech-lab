#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# test-verdict.sh -- fixture tests for 09's reporting logic. No servers.
#
# 09 takes about 25 minutes, so its verdict, evidence and exit-status rules
# are tested here first, with fake runs. Each case runs in a child bash that
# sources 09 (the guard at its end stops it running) and replays the rows a
# run would record, then checks:
#   the VERDICT row's words, the exit status, and the evidence left on disk.
#
# Nothing here starts or stops a server. lab_down is replaced with a no-op
# and KILL_PATTERN can match nothing, so the live lab is never touched.
# Everything is written under a mktemp folder, never under run/.
#
#   ./test-verdict.sh        run every case; exits non-zero on any mismatch
# ---------------------------------------------------------------------------
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---- child: one fake run ---------------------------------------------------
if [ "${1:-}" = child ]; then
  CASE="$2"; DIR="$3"
  # shellcheck source=/dev/null
  source "$HERE/09-switch-t4-t5.sh"
  KILL_PATTERN='t-fixture-this-matches-no-process-'
  lab_down() { return 0; }
  lab_down_quiet() { return 0; }
  RUN_DIR="$DIR"; RESULTS="$DIR/results.tsv"; STAMP=fixture
  RIG_BROKEN=""; IN_RUN=""

  # What a run leaves behind at the end of step 10.
  make_evidence() {
    local s d i
    for s in "${STREAMS[@]}"; do printf 'id\tpayload\n' > "$(manifest "${s%% *}")"; done
    for d in conf-t4 conf-t5; do
      mkdir -p "$EVID/$d"
      for i in 1 2 3 4 5 6 7 8 9; do : > "$EVID/$d/t-s$i.conf"; done
    done
    for d in "logs-1-$FROM" "logs-2-$TO" "logs-3-restart"; do
      mkdir -p "$EVID/$d"; echo log > "$EVID/$d/za-1.log"
    done
    for d in jsz-0-before jsz-1-after-start jsz-2-after-probe jsz-3-after-restart; do
      mkdir -p "$EVID/$d"
      for i in 1 2 3 4 5 6 7 8 9; do echo '{}' > "$EVID/$d/s$i.json"; done
    done
    cp -Rp "$RUN_DIR/js" "$EVID/js-0-before-switch"
    mv "$RUN_DIR/js" "$EVID/js"
  }

  # $1 run  $2 prefix  $3 what happens
  fake_run() {
    RUN="$1"; P="$2"; FROM=t4; TO=t5; REQ=D03-R11; CN=0
    KIND=rig; RIG_FAIL=0; PROC_FAIL=0; PROC_N=0; UNREAD=0
    TOPOLOGY="fixture $1"
    EVID="$RUN_DIR/evidence/$STAMP/$RUN"
    mkdir -p "$EVID" "$RUN_DIR/log" "$RUN_DIR/js/za-1"
    echo live > "$RUN_DIR/log/za-1.log"; echo store > "$RUN_DIR/js/za-1/f"
    trap 'stopped EXIT' EXIT; trap 'stopped INT' INT; trap 'stopped TERM' TERM
    IN_RUN="$RUN"; STEP=1
    set_streams
    if [ "$3" = setup-fail ]; then
      nid; rcheck "$ID" "$REQ" "before: shape" "want" "other"
    else
      nid; rcheck "$ID" "$REQ" "before: shape" "want" "want"
    fi
    STEP=3; KIND=procedure
    case "$3" in
      no-procedure) ;;
      unreadable)
        nid; icheck "$ID" "$REQ" "after switch: every acked ID" "match" "unreadable: no replica answered Direct Get at seq 1 of 3 (0 read)"
        nid; icheck "$ID" "$REQ" "after switch: KV" "k=v@1" "k=v@1" ;;
      unreadable-and-fail)
        nid; icheck "$ID" "$REQ" "after switch: every acked ID" "match" "unreadable: no replica answered Direct Get at seq 1 of 3 (0 read)"
        nid; icheck "$ID" "$REQ" "after switch: KV" "k=v@1" "k=v@9" ;;
      preservation-fail)
        nid; pcheck "$ID" "$REQ" "after switch: every acked ID" "match" "differs"
        nid; pcheck "$ID" "$REQ" "after switch: KV" "k=v@1" "k=v@1" ;;
      *)
        nid; pcheck "$ID" "$REQ" "after switch: every acked ID" "match" "match"
        nid; pcheck "$ID" "$REQ" "after switch: KV" "k=v@1" "k=v@1" ;;
    esac
    case "$3" in
      interrupted)  STEP=7; kill -TERM $$; sleep 5 ;;
      script-error) STEP=8; exit 2 ;;
    esac
    STEP=10
    make_evidence
    [ "$3" = missing-evidence ] && rm -rf "$EVID/logs-3-restart" "$EVID/jsz-2-after-probe/s9.json"
    STEP=11
    finish_run
  }

  # direct_get's parser, fed the replies `nats req` (natscli 0.4.0) printed
  # against nats-server 2.14.6 on 2026-09-29: a message, a KV value, a 404,
  # and no responders.
  if [ "$CASE" = parse ]; then
    trap - EXIT
    nats_as() { printf '%s\n' "$FAKE_REPLY"; }
    for FAKE_REPLY in \
'09:29:47 Sending request on "$JS.API.DIRECT.GET.ODOMETER"
09:29:47 Received with rtt 820.042µs
09:29:47 Nats-Sequence: 1
09:29:47 Nats-Time-Stamp: 2026-09-29T07:29:41.161659Z
09:29:47 Nats-Msg-Id: A.x.1
09:29:47 Nats-Stream: ODOMETER
09:29:47 Nats-Subject: evt.odo.v1
09:29:47
{"id":"A.x.1","n":1}' \
'09:29:47 Sending request on "$JS.API.DIRECT.GET.KV_t7-vehicles.$KV.t7-vehicles.vehicle-1"
09:29:47 Received with rtt 365.583µs
09:29:47 Nats-Time-Stamp: 2026-09-29T07:29:47.676411Z
09:29:47 Nats-Stream: KV_t7-vehicles
09:29:47 Nats-Subject: $KV.t7-vehicles.vehicle-1
09:29:47 Nats-Sequence: 7
09:29:47
A-k1-v3' \
'09:29:47 Sending request on "$JS.API.DIRECT.GET.ODOMETER"
09:29:47 Received with rtt 169.208µs
09:29:47 Status: 404
09:29:47 Description: Message Not Found
09:29:47
nil body' \
'09:29:48 Sending request on "$JS.API.DIRECT.GET.NOPE"
09:29:48 No responders are available'; do
      direct_get 1 u s b || echo "no answer"
    done
    exit 0
  fi

  case "$CASE" in
    two-runs) fake_run A SA preservation-fail; fake_run B1 SB success ;;
    *)        fake_run A SA "$CASE" ;;
  esac
  end_status
fi

# ---- parent: the cases -----------------------------------------------------
FAILS=0
ROOT="$(mktemp -d "${TMPDIR:-/tmp}/lab03-verdict.XXXXXX")"
trap 'rm -rf "$ROOT"' EXIT

ok()  { printf '  \033[32mok\033[0m    %s\n' "$1"; }
bad() { printf '  \033[31mFAIL\033[0m  %s\n' "$1"; FAILS=$((FAILS+1)); }
expect() { [ "$2" = "$3" ] && ok "$1: $3" || bad "$1: expected '$2', got '$3'"; }

# $1 case  $2 expected verdict words (last VERDICT row)  $3 expected exit status
run_case() {
  local c="$1" d="$ROOT/$1" rc words cols
  mkdir -p "$d"
  bash "$HERE/test-verdict.sh" child "$c" "$d" >"$d/out.txt" 2>&1; rc=$?
  printf '%s\n' "$c"
  words="$(awk -F'\t' '$7=="VERDICT"{w=$6} END{print w}' "$d/results.tsv" 2>/dev/null)"
  expect "verdict" "$2" "$words"
  expect "exit status" "$3" "$rc"
  cols="$(awk -F'\t' '{print NF}' "$d/results.tsv" 2>/dev/null | sort -u | tr '\n' ' ')"
  expect "every row has 9 columns" "9 " "$cols"
  expect "exactly one VERDICT row per run" "$4" \
    "$(awk -F'\t' '$7=="VERDICT"' "$d/results.tsv" 2>/dev/null | wc -l | tr -d ' ')"
}
has() { [ -e "$2" ] && ok "$1" || bad "$1 (missing: ${2#"$ROOT"/})"; }

printf '\n09 verdict logic -- fixture tests (no servers)\n\n'

run_case success           "passed with a measured interruption" 0 1
has "evidence kept: stores"  "$ROOT/success/evidence/fixture/A/js/za-1/f"

run_case preservation-fail "failed"       0 1
run_case setup-fail        "inconclusive" 1 1
run_case no-procedure      "inconclusive" 1 1

run_case missing-evidence  "inconclusive" 1 1
expect "evidence row names the gaps" "missing: logs-3-restart; jsz-2-after-probe" \
  "$(awk -F'\t' '$4 ~ /^evidence:/{print $6}' "$ROOT/missing-evidence/results.tsv")"

run_case interrupted       "inconclusive" 143 1
has "interrupted: logs kept"   "$ROOT/interrupted/evidence/fixture/A/logs-x-stopped/za-1.log"
has "interrupted: stores kept" "$ROOT/interrupted/evidence/fixture/A/js-x-stopped/za-1/f"
expect "interrupted: why names the step" "yes" \
  "$(grep -q 'stopped (TERM) at step 7' "$ROOT/interrupted/results.tsv" && echo yes || echo no)"

run_case script-error      "inconclusive" 2 1
has "script error: stores kept" "$ROOT/script-error/evidence/fixture/A/js-x-stopped/za-1/f"

run_case unreadable        "inconclusive" 0 1
expect "unreadable: a NOTE, not a FAIL" "NOTE" \
  "$(awk -F'\t' '$4 ~ /every acked ID/{print $7}' "$ROOT/unreadable/results.tsv")"
expect "unreadable: note says inconclusive" "yes" \
  "$(grep -q 'data integrity inconclusive' "$ROOT/unreadable/results.tsv" && echo yes || echo no)"
run_case unreadable-and-fail "failed"     0 1

printf 'parse\n'
PARSED="$(bash "$HERE/test-verdict.sh" child parse "$ROOT" 2>&1)"
expect "message"        "1	A.x.1	{\"id\":\"A.x.1\",\"n\":1}" "$(sed -n 1p <<<"$PARSED")"
expect "kv value"       "7	-	A-k1-v3"                     "$(sed -n 2p <<<"$PARSED")"
expect "404"            "status 404 Message Not Found"        "$(sed -n 3p <<<"$PARSED")"
expect "no responders"  "no answer"                           "$(sed -n 4p <<<"$PARSED")"

run_case two-runs          "passed with a measured interruption" 0 2
expect "two runs: first run's verdict" "failed" \
  "$(awk -F'\t' '$7=="VERDICT"{print $6; exit}' "$ROOT/two-runs/results.tsv")"

printf '\n'
if [ "$FAILS" -eq 0 ]; then echo "all fixture checks passed"; exit 0; fi
echo "$FAILS fixture check(s) failed -- outputs in $ROOT (kept)"; trap - EXIT; exit 1
