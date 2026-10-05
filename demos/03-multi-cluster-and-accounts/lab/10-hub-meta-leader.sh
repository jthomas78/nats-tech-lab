#!/usr/bin/env bash
# HUB META LEADER -- T4 with the `arb` cluster playing the hub.
#
#   ./10-hub-meta-leader.sh          every step, in order (step1..step6)
#   ./10-hub-meta-leader.sh step1    one step -- Step 1's checks
#   ./10-hub-meta-leader.sh step2    Step 2; on its own rig it runs step1 first
#   ./10-hub-meta-leader.sh step3    Step 3; on its own rig it runs step1 first
#   ./10-hub-meta-leader.sh step4    Step 4; on its own rig it runs step1 first
#   ./10-hub-meta-leader.sh step5    Step 5; on its own rig it runs step1 first
#   ./10-hub-meta-leader.sh step6    Step 6; on its own rig it runs step1 first
#
# Plan: ../HUB-META-LEADER-PLAN.md. Hand steps: exercise 10,
# ../exercises/EXERCISE-10-TERMINAL-STEPS.md -- part `stepN` here checks its
# Step N. ../exercises/ex10-check.sh is a wrapper that calls this script with
# the same arguments. The checks live only here.
#
# THE RIG -- one way to start and stop it, rig-t4.sh.
#   * No t- server running: this script starts its own rig with
#     `rig-t4.sh up` and tears it down at the end, on failure and on interrupt.
#   * A rig already running (started by hand): ONE part may attach to it. The
#     part checks its starting state first and refuses if it is wrong. It
#     never calls lab_init, thaws anything it froze, and leaves the rig up.
#   * The full run (no part named) refuses to attach.
#
# WHERE THE ROWS GO. record() appends to $RESULTS, and only run-all.sh clears
# results.tsv. So a run on its own writes to run/10-<part>.tsv (run/10-all.tsv
# for every part) and can never reach REPORT.md. Only LAB_RUN_ALL=1 -- to be
# set by run-all.sh when 10 joins its LABS list -- writes to results.tsv.
#
# FIXTURES (part step1) -- all in the shared account LB (user lb), R3, file:
#   ODOMETER_ARB  evt.odo.arb.v1  --cluster arb   seeds seed.arb.1..3
#   ODOMETER_ZA   evt.odo.za.v1   --cluster za    seeds seed.za.1..3
#   ODOMETER_AU   evt.odo.au.v1   --cluster au    seeds seed.au.1..3
#   KV t7-vehicles                --cluster arb   vehicle-1 = seed-k1-v1
# Payload: {"id":"seed.<site>.<n>","vehicle":"v-<n>","km":<1000+n>}, sent
# with Nats-Msg-Id = id, read back by Direct Get.
#
# Check IDs: ML1 upward, never reused. Requirements: D03-R13..R18 in the plan.
#   step1  ML1-ML23   all rig
#   step2  ML24-ML41  rig, except the trial outcomes and the restart (procedure);
#                     ML41 is the step's verdict
#   step3  ML42-ML58  za dark: ML50 verdict S2 (D03-R14), ML58 verdict S4 (D03-R16)
#          ML59-ML75  au dark: ML67 verdict S2, ML75 verdict S4
#   step4  ML76-ML93  leader in za, za dark: ML84 verdict S3 (D03-R15), ML92
#                     verdict S4, ML93 the hub step-down after recovery
#          ML94-ML111 the same for au: ML102 S3, ML110 S4, ML111 step-down
#   step5  ML112-ML121 both dark: ML121 verdict S5 (D03-R17)
#          ML122-ML128 za back, au dark: ML128 verdict, the partial recovery check
#          ML129-ML136 au back: ML136 verdict S4 (D03-R16)
#   step6  ML137-ML147 za dark: ML145 verdict S6 (D03-R18); ML146-ML147 the
#                      thaw (rig), with the unacked write's fate as a note
#          ML148-ML158 the same for au: ML156 verdict S6
#   Every return also has a "meta term unchanged during recovery" check, added later with a
#   `b` suffix: ML54b, ML71b, ML88b, ML106b, ML132b. It counts in its S4 verdict.
#
# EXIT STATUS: non-zero only for a failed RIG check. A failed procedure check
# is the answer, not a broken rig -- it stays FAIL in the rows and the summary.

source "$(dirname "${BASH_SOURCE[0]}")/rig-t4.sh"   # SERVERS, http_of, _common.sh

TOPOLOGY="T4 / ML -- hub meta-leader (arb as hub)"
RIG="$LAB_DIR/rig-t4.sh"
PARTS=(step1 step2 step3 step4 step5 step6)

# Client ports used for fixtures: the first server of each cluster.
port_of() { case "$1" in arb) echo 4541 ;; za) echo 4231 ;; au) echo 4241 ;; esac; }
SITES=(arb za au)
stream_of() { printf 'ODOMETER_%s' "$(printf '%s' "$1" | tr '[:lower:]' '[:upper:]')"; }

usage() { echo "usage: $0 [${PARTS[*]}]" >&2; exit 2; }

# --- which part, and where its rows go --------------------------------------

PART="${1:-all}"
if [ "$PART" != all ]; then
  ok=0; for p in "${PARTS[@]}"; do [ "$p" = "$PART" ] && ok=1; done
  [ "$ok" -eq 1 ] || usage
fi

need_tools
mkdir -p "$RUN_DIR"
# Emptied only after every refusal has passed, so a refused run keeps the
# rows of the last good one.
[ "${LAB_RUN_ALL:-}" = 1 ] || RESULTS="$RUN_DIR/10-$PART.tsv"

# --- the rig: attach, or start our own --------------------------------------

probe() { curl -fs --max-time 1 "http://127.0.0.1:$1/$2" 2>/dev/null; }

answering() {                   # how many of the nine monitors answer
  local s n=0
  for s in "${SERVERS[@]}"; do probe "$(http_of "$s")" healthz >/dev/null && n=$((n+1)); done
  echo "$n"
}

stream_exists() { nats_as "$(port_of arb)" lb stream info "$1" >/dev/null 2>&1; }

if pgrep -f "$KILL_PATTERN" >/dev/null 2>&1; then
  if [ "$PART" = all ]; then
    echo "refusing: a rig is running, and the full run starts its own." >&2
    echo "run ./lab/rig-t4.sh down first, or name one part." >&2
    exit 1
  fi
  MODE=attached
  # Whatever happens, leave nothing frozen. Never stop the rig.
  trap 'pkill -CONT -f "$KILL_PATTERN" >/dev/null 2>&1 || true' EXIT INT TERM
  n="$(answering)"
  if [ "$n" != 9 ] || [ "$(meta_size 8541)" != 9 ]; then
    echo "refusing: attached rig is not whole ($n of 9 monitors answer)." >&2
    echo "check with ./lab/rig-t4.sh status" >&2
    exit 1
  fi
else
  MODE=own
  trap lab_down EXIT INT TERM
  if ! up_out="$("$RIG" up 2>&1)"; then
    printf '%s\n' "$up_out" >&2
    echo "rig-t4.sh up failed" >&2
    exit 1
  fi
fi

echo "nats-server $(nats-server --version | sed 's/^nats-server: //') · nats CLI $(nats --version) · $(date '+%Y-%m-%d %H:%M %Z')"
echo "part: $PART · rig: $MODE · rows: $RESULTS"

# --- helpers for the parts ---------------------------------------------------

# 09's Direct Get reader, copied. Prints "<seq>\t<msg-id>\t<body>", or
# "status <code> <description>" when the server answered with a status.
direct_get() {                  # $1 port  $2 user  $3 subject  $4 request body
  local out
  out="$(nats_as "$1" "$2" req --timeout 3s "$3" "$4" 2>&1)" || true
  awk '
    !inbody && /^[0-9][0-9]:[0-9][0-9]:[0-9][0-9]( |$)/ {
      line = substr($0, 10)
      if (line ~ /^Received with rtt/) { got = 1; next }
      if (!got) next
      if (line == "")                 { inbody = 1; next }
      if (line ~ /^Status: /)         st  = substr(line, 9)
      if (line ~ /^Description: /)    st  = st " " substr(line, 14)
      if (line ~ /^Nats-Msg-Id: /)    id  = substr(line, 14)
      if (line ~ /^Nats-Sequence: /)  seq = substr(line, 16)
      next
    }
    inbody && $0 != "" { body = body (nb++ ? "\n" : "") $0 }
    END {
      if (!got) exit 1
      if (st != "") { print "status " st; exit 0 }
      printf "%s\t%s\t%s\n", seq, (id == "" ? "-" : id), body
    }' <<<"$out"
}

seed_payload() { printf '{"id":"seed.%s.%s","vehicle":"v-%s","km":%s}' "$1" "$2" "$2" $((1000 + $2)); }

# Acked publish. Prints the stored sequence, or "fail".
acked_seq() {                   # $1 site  $2 n
  local out
  out="$(nats_as "$(port_of "$1")" lb pub -J --no-templates -H "Nats-Msg-Id:seed.$1.$2" \
         "evt.odo.$1.v1" "$(seed_payload "$1" "$2")" 2>&1)" || { echo fail; return 0; }
  printf '%s' "$out" | grep -oE 'Sequence: [0-9]+' | tail -1 | awk '{print $2}' | grep . || echo fail
}

allow_direct() {
  nats_as "$(port_of arb)" lb stream info "$1" --json 2>/dev/null \
    | jq -r '.config.allow_direct // false' 2>/dev/null || echo "?"
}

# Peers the leader sees as current AND heard from in the last 5 s. `current`
# alone lags a freeze by several seconds (measured by hand, 2026-10-05).
current_peers() {
  local lead http
  lead="$(meta_leader 8541)"
  [ "$lead" = NONE ] && { echo 0; return 0; }
  http="$(http_of "${lead#t-}")"
  probe "$http" 'jsz?meta=1' \
    | jq '[.meta_cluster.replicas[]? | select(.current == true and .active < 5000000000)] | length' \
      2>/dev/null || echo 0
}

all_meta_ports() { local s; for s in "${SERVERS[@]}"; do http_of "$s"; done; }

# --- part step1: set up the rig and the fixtures -----------------------------
# Hand steps: exercises/EXERCISE-10-TERMINAL-STEPS.md, Step 1.

# Starting state. An attached rig must be fresh: step1 creates the fixtures.
ready_step1() {
  local f
  for f in ODOMETER_ARB ODOMETER_ZA ODOMETER_AU KV_t7-vehicles PROBE_META; do
    if stream_exists "$f"; then
      echo "refusing: $f already exists. step1 needs a fresh rig:" >&2
      echo "  ./lab/rig-t4.sh down, then run step1 again (it starts its own rig)" >&2
      exit 1
    fi
  done
}

part_step1() {
  banner "Step 1 -- set up: nine servers, one meta group, fixtures, seed"

  # Steps 1.2-1.4: the rig and its meta group.
  check ML1 D03-R13 "all nine monitors answer" "9" "$(answering)"
  check ML2 D03-R13 "meta group size, read from arb-1 (majority 5)" "9" "$(meta_size 8541)"
  local lead
  lead="$(wait_live_leader 8541 '^t-(za|au|arb)-[123]$' 30)"
  check ML3 D03-R13 "a live meta leader, one of the nine" "yes" \
    "$([ "$lead" != NONE ] && echo yes || echo no)"
  # shellcheck disable=SC2046
  check ML4 D03-R13 "all nine servers name the same meta leader" "1" \
    "$(meta_group_count $(all_meta_ports))"
  check ML5 D03-R13 "peers the leader sees as current, active < 5 s" "8" "$(current_peers)"
  note  ML5a D03-R13 "meta leader after start-up -- nothing pins it" "$lead"

  # Steps 1.5-1.7: the fixtures, each placed in one cluster.
  # Check IDs are fixed per site, in SITES order: arb, za, au, then the KV.
  local i site s
  for i in 0 1 2; do
    site="${SITES[$i]}"; s="$(stream_of "$site")"
    check "ML$((6+i))" D03-R13 "create $s, R3, --cluster $site" "ok" \
      "$(fails_or_ok "$(port_of "$site")" lb stream add "$s" --subjects "evt.odo.$site.v1" \
           --storage file --replicas 3 --cluster "$site" --defaults)"
  done
  check ML9 D03-R13 "create KV t7-vehicles, R3, --cluster arb" "ok" \
    "$(fails_or_ok "$(port_of arb)" lb kv add t7-vehicles --storage file --replicas 3 --cluster arb)"

  for i in 0 1 2; do
    site="${SITES[$i]}"; s="$(stream_of "$site")"
    check "ML$((10+i))" D03-R13 "$s: every peer is in $site, 3 peers, Direct Get on" \
      "$site 3 true" \
      "$(stream_peer_regions "$(port_of arb)" lb "$s") $(stream_peers "$(port_of arb)" lb "$s") $(allow_direct "$s")"
  done
  check ML13 D03-R13 "KV_t7-vehicles: every peer is in arb, 3 peers, Direct Get on" \
    "arb 3 true" \
    "$(stream_peer_regions "$(port_of arb)" lb KV_t7-vehicles) $(stream_peers "$(port_of arb)" lb KV_t7-vehicles) $(allow_direct KV_t7-vehicles)"

  # Step 1.8: three acked messages per stream. The ack carries the sequence.
  local n seqs
  for i in 0 1 2; do
    site="${SITES[$i]}"; seqs=""
    for n in 1 2 3; do seqs="$seqs $(acked_seq "$site" "$n")"; done
    check "ML$((14+i))" D03-R13 "acked publish seed.$site.1..3: stored sequences" "1 2 3" "${seqs# }"
  done

  # Step 1.9: read every seeded message back by Direct Get -- id AND payload.
  local want got matched
  for i in 0 1 2; do
    site="${SITES[$i]}"; s="$(stream_of "$site")"; matched=0
    for n in 1 2 3; do
      want="$(printf '%s\tseed.%s.%s\t%s' "$n" "$site" "$n" "$(seed_payload "$site" "$n")")"
      got="$(direct_get "$(port_of "$site")" lb "\$JS.API.DIRECT.GET.$s" "{\"seq\":$n}")"
      [ "$got" = "$want" ] && matched=$((matched+1))
    done
    check "ML$((17+i))" D03-R13 "$s: Direct Get seq 1..3 returns the seeded id and payload" "3" "$matched"
  done
  note ML19a D03-R13 "Direct Get of a sequence that does not exist (ODOMETER_ARB seq 4)" \
    "$(direct_get "$(port_of arb)" lb '$JS.API.DIRECT.GET.ODOMETER_ARB' '{"seq":4}' || echo 'no reply')"

  # Step 1.10: one KV key.
  check ML20 D03-R13 "KV put vehicle-1, then get it back" "seed-k1-v1" \
    "$(nats_as "$(port_of arb)" lb kv put t7-vehicles vehicle-1 seed-k1-v1 >/dev/null 2>&1
       nats_as "$(port_of arb)" lb kv get t7-vehicles vehicle-1 --raw 2>/dev/null || echo fail)"

  # Step 1.11: the metadata probe -- only the meta leader can place a new stream.
  check ML21 D03-R13 "metadata probe: create PROBE_META (R1, --cluster arb)" "ok" \
    "$(fails_or_ok "$(port_of arb)" lb --timeout 10s stream add PROBE_META --subjects probe.meta \
         --storage memory --replicas 1 --cluster arb --defaults)"
  check ML22 D03-R13 "metadata probe: delete PROBE_META" "ok" \
    "$(fails_or_ok "$(port_of arb)" lb stream rm PROBE_META -f)"
  check ML23 D03-R13 "PROBE_META is gone after the delete" "gone" \
    "$(stream_exists PROBE_META && echo present || echo gone)"
}

# --- part step2: placement trials (plan test S1) -----------------------------
# Hand steps: exercises/EXERCISE-10-TERMINAL-STEPS.md, Step 2.
#
# Each trial starts from a CONFIRMED regional leader (a rig check), then asks
# for the hub with `step-down --cluster arb` (a procedure check). The CLI's
# "New leader elected" line is not evidence; -j prints the same text on CLI
# 0.4.0 (measured by hand). The leader is read from /jsz on all nine.

TRIALS=6
ELECTION_S=30     # plan rule 6: a live leader after a loss that keeps quorum
RECOVERY_S=60     # plan rule 6: a returned server is current again

# step2 needs step1's fixtures. On our own rig it runs step1 first.
PREREQ_step2=(step1)

stepdown() { nats_as "$(port_of arb)" admin server cluster step-down --cluster "$1" >/dev/null 2>&1; }

region_of() { case "$1" in NONE|"") echo NONE ;; *) printf '%s\n' "$1" | sed -E 's/^t-//; s/-[0-9]+$//' ;; esac; }

now() { date +%s.%N; }
secs_since() { awk -v a="$1" -v b="$(now)" 'BEGIN{printf "%.2f", b - a}'; }

# Wait until all nine name ONE leader that matches $1 (a regex) and is not $2.
# Prints the leader, or NONE past $3 seconds.
wait_agreed_leader() {
  local re="$1" not="$2" max="$3" i l
  for ((i=0; i<max*5; i++)); do
    l="$(meta_leader 8541)"
    # shellcheck disable=SC2046
    if [[ "$l" =~ $re ]] && [ "$l" != "$not" ] && [ "$(meta_group_count $(all_meta_ports))" = 1 ]; then
      echo "$l"; return 0
    fi
    sleep 0.2
  done
  echo NONE
}

# Starting state for an attached rig: step1's fixtures are there, its probe is
# gone. The nine monitors and meta size 9 were checked on attach.
ready_step2() {
  local f
  for f in ODOMETER_ARB ODOMETER_ZA ODOMETER_AU KV_t7-vehicles; do
    if ! stream_exists "$f"; then
      echo "refusing: $f is missing. step2 starts where step1 ends:" >&2
      echo "  run ./exercises/ex10-check.sh step1 on this rig first" >&2
      exit 1
    fi
  done
  if stream_exists PROBE_META; then
    echo "refusing: PROBE_META still exists (a step1 probe left behind)." >&2
    exit 1
  fi
}

part_step2() {
  banner "Step 2 -- placement trials: can step-down put the leader in the hub? (S1)"

  # Step 2.1: the starting state.
  local lead
  lead="$(wait_agreed_leader '^t-(za|au|arb)-[123]$' "" "$ELECTION_S")"
  check ML24 D03-R13 "starting state: monitors answering, meta size, distinct leaders" "9 9 1" \
    "$(answering) $(meta_size 8541) $([ "$lead" != NONE ] && echo 1 || echo 0)"

  # Steps 2.2-2.4: the trials. Regions alternate za, au, za, ...
  local t r start t0 after secs log=""
  for ((t=1; t<=TRIALS; t++)); do
    if [ $((t % 2)) -eq 1 ]; then r=za; else r=au; fi

    stepdown "$r" || true
    start="$(wait_agreed_leader "^t-$r-[123]\$" "" "$ELECTION_S")"
    check "ML$((23 + 2*t))" D03-R13 "trial $t: confirmed regional leader before the hub step-down" \
      "$r" "$(region_of "$start")"

    KIND=procedure
    t0="$(now)"
    stepdown arb || true
    after="$(wait_agreed_leader '^t-(za|au|arb)-[123]$' "$start" "$ELECTION_S")"
    secs="$(secs_since "$t0")"
    check "ML$((24 + 2*t))" D03-R13 "trial $t: step-down --cluster arb -- leader's cluster, distinct leaders on nine" \
      "arb 1" "$(region_of "$after") $([ "$after" != NONE ] && echo 1 || echo 0)"
    KIND=rig
    log="$log t$t ${start}->${after} ${secs}s;"
  done
  note ML36a D03-R13 "per trial: start -> leader after, seconds until all nine agree (includes the CLI call)" "$(log="${log# }"; printf '%s' "${log%;}")"

  # Step 2.5: a hub step-down with the leader already in the hub. Not a trial.
  start="$(meta_leader 8541)"
  stepdown arb || true
  after="$(wait_agreed_leader '^t-(za|au|arb)-[123]$' "$start" "$ELECTION_S")"
  note ML36b D03-R13 "step-down --cluster arb with the leader already in the hub" "$start -> $after"

  # Step 2.6: restart one hub server that is NOT the meta leader.
  local s victim="" sl_before
  lead="$(meta_leader 8541)"
  for s in arb-1 arb-2 arb-3; do [ "t-$s" != "$lead" ] && { victim="$s"; break; }; done
  sl_before="$(stream_leader "$(port_of arb)" lb ODOMETER_ARB)"
  "$RIG" restart "$victim" >/dev/null 2>&1 || true
  check ML37 D03-R13 "restart t-$victim (not the leader): all nine monitors answer again" "9" "$(answering)"

  KIND=procedure
  after="$(wait_agreed_leader '^t-(za|au|arb)-[123]$' "" "$ELECTION_S")"
  check ML38 D03-R13 "meta leader after restarting a non-leader hub server" "unchanged" \
    "$([ "$after" = "$lead" ] && echo unchanged || echo "moved $lead -> $after")"
  KIND=rig

  local i cur=0
  for ((i=0; i<RECOVERY_S; i++)); do
    cur="$(current_peers)"; [ "$cur" = 8 ] && break; sleep 1
  done
  check ML39 D03-R13 "peers the leader sees as current after the restart, active < 5 s" "8" "$cur"
  check ML40 D03-R13 "ODOMETER_ARB messages after the restart" "3" \
    "$(stream_msgs "$(port_of arb)" lb ODOMETER_ARB)"
  note ML40a D03-R13 "ODOMETER_ARB's own stream leader, before -> after the restart" \
    "$sl_before -> $(stream_leader "$(port_of arb)" lb ODOMETER_ARB)"

  # One verdict for the step, from its own rows (ML24-ML40).
  local rig_fail proc_fail proc_n word
  read -r rig_fail proc_fail proc_n < <(awk -F'\t' '
    $1 ~ /^ML(2[4-9]|3[0-9]|40)$/ {
      if ($9 == "rig"       && $7 == "FAIL") rf++
      if ($9 == "procedure")                 { pn++; if ($7 == "FAIL") pf++ }
    } END { print rf+0, pf+0, pn+0 }' "$RESULTS")
  if   [ "$rig_fail" -gt 0 ] || [ "$proc_n" -eq 0 ]; then word=inconclusive
  elif [ "$proc_fail" -gt 0 ];                       then word=failed
  else                                                    word="passed with a measured interruption"; fi
  verdict ML41 D03-R13 "hub step-down puts the meta leader in the hub, every trial; a hub restart does not move it" \
    "$word" "rig checks failed: $rig_fail; procedure checks not met: $proc_fail of $proc_n; settle times in ML36a"
}

# --- part step3: one region dark, leader in the hub, then recovery (S2 + S4) --
# Hand steps: exercises/EXERCISE-10-TERMINAL-STEPS.md, Step 3.
#
# Two rounds: za dark (ML42-ML58), then au dark (ML59-ML75). Each round has
# two verdicts: S2 while dark (D03-R14), S4 after the thaw (D03-R16).
#
# A FROZEN server accepts the TCP connection and never answers, and jsz() in
# _common.sh has no --max-time. So while a region is frozen, read only the
# live servers' monitors -- never all_meta_ports or wait_agreed_leader.
#
# The ledger: every message this part expects in a stream, one line per
# message, "<seq>\t<msg-id>\t<payload>" -- exactly what direct_get prints. It
# starts with step1's seeds and grows with every ACKED write. A read-back
# compares each line by Direct Get, so it proves identity, not only a count.

PREREQ_step3=(step1)
HOLD_S=15         # how long the leader must stay put while a region is dark
SETTLE_S=10       # after the peers are current: the window an election shows in

ready_step3() {
  ready_step2
  local site got=""
  for site in "${SITES[@]}"; do got="$got $(stream_msgs "$(port_of "$site")" lb "$(stream_of "$site")")"; done
  if [ "${got# }" != "3 3 3" ]; then
    echo "refusing: step3 needs exactly step1's 3 messages per stream (arb za au: ${got# })." >&2
    echo "  ./lab/rig-t4.sh down, then run step3 again (it starts its own rig)" >&2
    exit 1
  fi
}

ledger() { printf '%s/ledger-%s' "$RUN_DIR" "$1"; }

ledger_init() {
  local site n
  for site in "${SITES[@]}"; do
    : > "$(ledger "$site")"
    for n in 1 2 3; do
      printf '%s\tseed.%s.%s\t%s\n' "$n" "$site" "$n" "$(seed_payload "$site" "$n")" >> "$(ledger "$site")"
    done
  done
}

ledger_total() { wc -l < "$(ledger "$1")" | tr -d ' '; }

# The next $2 sequences the ledger expects for site $1: "4 5 6".
next_seqs() { local t; t="$(ledger_total "$1")"; seq $((t+1)) $((t+$2)) | paste -sd ' ' -; }

# Acked publish with an id of our own. Prints the stored sequence and adds the
# message to the ledger, or prints "fail" and adds nothing.
pub_ack() { pub_ack_via "$(port_of "$1")" "$@"; }   # $1 site  $2 msg id  $3 km

# The same, sent through client port $1 -- step6 must reach a dark region's
# stream through a live server.
pub_ack_via() {                 # $1 port  $2 site  $3 msg id  $4 km
  local body out seq
  body="$(printf '{"id":"%s","vehicle":"v-1","km":%s}' "$3" "$4")"
  out="$(nats_as "$1" lb pub -J --no-templates -H "Nats-Msg-Id:$3" \
         "evt.odo.$2.v1" "$body" 2>&1)" || { echo fail; return 0; }
  seq="$(printf '%s' "$out" | grep -oE 'Sequence: [0-9]+' | tail -1 | awk '{print $2}' || true)"
  [ -n "$seq" ] || { echo fail; return 0; }
  printf '%s\t%s\t%s\n' "$seq" "$3" "$body" >> "$(ledger "$2")"
  echo "$seq"
}

# Three acked writes to site $1, ids <tag>.<site>.1..3. Prints the sequences.
pub_three() {                   # $1 site  $2 tag  $3 km base
  local n seqs=""
  for n in 1 2 3; do seqs="$seqs $(pub_ack "$1" "$2.$1.$n" $(($3 + n)))"; done
  echo "${seqs# }"
}

# Every ledger line read back by Direct Get from site $1's own port: "<matched>/<total>".
readback() {
  local s line seq m=0 t=0
  s="$(stream_of "$1")"
  while IFS= read -r line <&3; do
    t=$((t+1)); seq="${line%%$'\t'*}"
    [ "$(direct_get "$(port_of "$1")" lb "\$JS.API.DIRECT.GET.$s" "{\"seq\":$seq}")" = "$line" ] && m=$((m+1))
  done 3< "$(ledger "$1")"
  echo "$m/$t"
}

meta_probe() {
  echo "$(fails_or_ok "$(port_of arb)" lb --timeout 10s stream add PROBE_META --subjects probe.meta \
            --storage memory --replicas 1 --cluster arb --defaults)" \
       "$(fails_or_ok "$(port_of arb)" lb stream rm PROBE_META -f)" \
       "$(stream_exists PROBE_META && echo present || echo gone)"
}

# The meta group's Raft term. It rises on every election ATTEMPT, won or not,
# even when the same server wins (Raft paper, sections 5.1-5.2). So a rise
# proves the term advanced; it does not count successful elections.
meta_term() {
  probe 8541 'raftz?group=_meta_' | jq -r '.["$SYS"]._meta_.term // "?"' 2>/dev/null || echo "?"
}

# Monitor ports of the servers NOT in cluster $1.
live_ports() { local s; for s in "${SERVERS[@]}"; do [ "${s%-*}" = "$1" ] || http_of "$s"; done; }

dark_count() { local p n=0; for p in "$@"; do probe "$p" healthz >/dev/null || n=$((n+1)); done; echo "$n"; }

# Leaders seen, in order of first sight, read from arb-1 (never frozen here).
SEEN=""
seen_add() { case " $SEEN " in *" $1 "*) ;; *) SEEN="${SEEN:+$SEEN }$1" ;; esac; }

# One verdict from the PASS/FAIL rows ML<lo>..ML<hi>, suffixed checks
# (ML57b) included. Notes do not count: they are neither PASS nor FAIL.
range_verdict() {               # $1 lo  $2 hi  -> "<word>|<why>"
  local rf pf pn word
  read -r rf pf pn < <(awk -F'\t' -v lo="$1" -v hi="$2" '
    $1 ~ /^ML[0-9]+[a-z]?$/ && ($7 == "PASS" || $7 == "FAIL") {
      n = substr($1, 3); sub(/[a-z]$/, "", n); n += 0; if (n < lo || n > hi) next
      if ($9 == "rig" && $7 == "FAIL") rf++
      if ($9 == "procedure")           { pn++; if ($7 == "FAIL") pf++ }
    } END { print rf+0, pf+0, pn+0 }' "$RESULTS")
  if   [ "$rf" -gt 0 ] || [ "$pn" -eq 0 ]; then word=inconclusive
  elif [ "$pf" -gt 0 ];                  then word=failed
  else                                        word="passed with a measured interruption"; fi
  echo "$word|rig checks failed: $rf; procedure checks not met: $pf of $pn"
}

# "met" if every listed ID is a PASS row, else "not met". For a verdict's
# <why>, so two parts of one scenario are reported apart.
ids_met() {
  awk -F'\t' -v ids=" $* " '
    index(ids, " " $1 " ") && ($7 == "PASS" || $7 == "FAIL") { if ($7 == "FAIL") bad = 1 }
    END { print (bad ? "not met" : "met") }' "$RESULTS"
}

# One round: freeze cluster $1, probe from the survivors, thaw, check recovery.
# $2 is the other region, $3 the round's first check number.
step3_round() {
  local dark="$1" other="$2" B="$3" i cur lead l moved term0 term1 t0 secs after v
  local live; live="$(live_ports "$dark")"
  id() { echo "ML$((B + $1))"; }

  # Step 3.1: leader in the hub, nine up, eight peers current.
  lead="$(wait_agreed_leader '^t-(za|au|arb)-[123]$' "" "$ELECTION_S")"
  if [ "$(region_of "$lead")" != arb ]; then
    stepdown arb || true
    lead="$(wait_agreed_leader '^t-arb-[123]$' "" "$ELECTION_S")"
  fi
  for ((i=0; i<RECOVERY_S; i++)); do cur="$(current_peers)"; [ "$cur" = 8 ] && break; sleep 1; done
  check "$(id 0)" D03-R14 "$dark round: start -- leader's cluster, monitors answering, peers current" \
    "arb 9 8" "$(region_of "$lead") $(answering) $cur"
  term0="$(meta_term)"

  # Step 3.2: freeze the region, and prove it.
  "$RIG" freeze "$dark" >/dev/null 2>&1 || true
  # shellcheck disable=SC2046
  check "$(id 1)" D03-R14 "freeze $dark: its three monitors do not answer" "3" \
    "$(dark_count $(http_of "$dark-1") $(http_of "$dark-2") $(http_of "$dark-3"))"

  # The leader must hold for HOLD_S, read from a live port only.
  KIND=procedure
  moved=0; SEEN=""
  for ((i=0; i<HOLD_S*2; i++)); do
    l="$(meta_leader 8541)"; seen_add "$l"; [ "$l" = "$lead" ] || moved=1; sleep 0.5
  done
  # shellcheck disable=SC2086
  check "$(id 2)" D03-R14 "$dark dark: meta leader for ${HOLD_S} s, and the six live servers agree" \
    "unchanged 1" "$([ "$moved" -eq 0 ] && echo unchanged || echo "moved: $SEEN") $(meta_group_count $live)"

  # Step 3.3: acked writes to the two surviving streams, then read them back.
  check "$(id 3)" D03-R14 "$dark dark: 3 acked writes to ODOMETER_ARB -- stored sequences" \
    "$(next_seqs arb 3)" "$(pub_three arb "s3.${dark}dark" 2000)"
  check "$(id 4)" D03-R14 "$dark dark: 3 acked writes to $(stream_of "$other") -- stored sequences" \
    "$(next_seqs "$other" 3)" "$(pub_three "$other" "s3.${dark}dark" 2000)"
  check "$(id 5)" D03-R14 "$dark dark: ODOMETER_ARB, every expected message by Direct Get" \
    "$(ledger_total arb)/$(ledger_total arb)" "$(readback arb)"
  check "$(id 6)" D03-R14 "$dark dark: $(stream_of "$other"), every expected message by Direct Get" \
    "$(ledger_total "$other")/$(ledger_total "$other")" "$(readback "$other")"

  # Step 3.4: the metadata probe.
  check "$(id 7)" D03-R14 "$dark dark: metadata probe -- create, delete, gone" "ok ok gone" "$(meta_probe)"
  term1="$(meta_term)"
  KIND=rig
  note "$(id 7)a" D03-R14 "$dark dark: meta term before the freeze -> after the probes" "$term0 -> $term1"

  v="$(range_verdict $((B)) $((B + 7)))"
  verdict "$(id 8)" D03-R14 "$dark dark, leader in the hub: leader holds, writes and metadata go on (S2)" \
    "${v%%|*}" "${v#*|}"

  # Steps 3.5-3.7: thaw, then recovery.
  recovery_checks "$dark" $((B + 9)) "$lead" "$term0" s3
}

# S4 -- thaw cluster $1, then every recovery check. Shared by step3 and step4.
# $2 is the first check number (8 numbers: 7 checks and the verdict), $3 the
# leader just before the thaw, $4 the meta term before the freeze, $5 the
# prefix of the write-probe ids.
recovery_checks() {
  local dark="$1" R="$2" lead="$3" term0="$4" tag="$5" i cur t0 secs after v term_thaw term_after
  term_thaw="$(meta_term)"
  t0="$(now)"
  "$RIG" thaw "$dark" >/dev/null 2>&1 || true
  check "ML$R" D03-R16 "thaw $dark: all nine monitors answer" "9" "$(answering)"

  KIND=procedure
  SEEN="$lead"
  for ((i=0; i<RECOVERY_S; i++)); do
    seen_add "$(meta_leader 8541)"; cur="$(current_peers)"; [ "$cur" = 8 ] && break; sleep 1
  done
  secs="$(secs_since "$t0")"
  check "ML$((R + 1))" D03-R16 "after the thaw: peers current, active < 5 s, within ${RECOVERY_S} s" "8" "$cur"
  # An election on the return shows up a few seconds later (measured by hand).
  for ((i=0; i<SETTLE_S*2; i++)); do seen_add "$(meta_leader 8541)"; sleep 0.5; done
  after="$(wait_agreed_leader '^t-(za|au|arb)-[123]$' "" "$ELECTION_S")"
  seen_add "$after"
  check "ML$((R + 2))" D03-R16 "after the thaw: one live leader that all nine name" "1" \
    "$([ "$after" != NONE ] && echo 1 || echo 0)"
  check "ML$((R + 3))" D03-R16 "after the thaw: the meta leader is the one from just before the thaw" "unchanged" \
    "$([ "$after" = "$lead" ] && echo unchanged || echo "moved $lead -> $after")"
  # The same server can come back as leader in a later term, so the leader's
  # name alone does not show the term held (measured 2026-10-05: term +2, same
  # server). The window: from just before the thaw to the end of the settle
  # window above (peers current, then SETTLE_S, then an agreed leader). The
  # term only rises, so its value at the end covers the whole window. Both
  # readings must be numbers; a missing reading is a rig failure (verdict
  # inconclusive), never a match of "?" with "?". ID suffix `b`: added after
  # ML42-ML111 were numbered, and IDs are never renumbered.
  term_after="$(meta_term)"
  if [[ "$term_thaw" =~ ^[0-9]+$ && "$term_after" =~ ^[0-9]+$ ]]; then
    check "ML$((R + 3))b" D03-R16 "after the thaw: meta term unchanged during recovery (just before the thaw -> end of the settle window)" "unchanged" \
      "$([ "$term_after" = "$term_thaw" ] && echo unchanged || echo "term $term_thaw -> $term_after")"
  else
    KIND=rig
    check "ML$((R + 3))b" D03-R16 "after the thaw: meta term unchanged during recovery -- both readings must be numbers" "unchanged" \
      "no reading: '$term_thaw' -> '$term_after'"
  fi
  KIND=rig
  note "ML$((R + 3))a" D03-R16 "after the thaw: leaders seen in order; meta term before the freeze -> after; seconds from thaw to peers current" \
    "$SEEN; term $term0 -> $term_after; ${secs}s"

  # Every message back, in all three streams, plus the KV key.
  KIND=procedure
  local want="" got="" site
  for site in "${SITES[@]}"; do
    want="$want $(ledger_total "$site")/$(ledger_total "$site")"; got="$got $(readback "$site")"
  done
  check "ML$((R + 4))" D03-R16 "after the thaw: arb za au, every expected message by Direct Get; then KV vehicle-1" \
    "${want# } seed-k1-v1" \
    "${got# } $(nats_as "$(port_of arb)" lb kv get t7-vehicles vehicle-1 --raw 2>/dev/null || echo fail)"

  # One write to each stream, then the metadata probe.
  want=""; got=""
  for site in "${SITES[@]}"; do
    want="$want $(next_seqs "$site" 1)"; got="$got $(pub_ack "$site" "$tag.${dark}back.$site.1" 3001)"
  done
  check "ML$((R + 5))" D03-R16 "after the thaw: one acked write to each of arb za au -- stored sequences" \
    "${want# }" "${got# }"
  check "ML$((R + 6))" D03-R16 "after the thaw: metadata probe -- create, delete, gone" "ok ok gone" "$(meta_probe)"
  KIND=rig

  # One verdict row for the scenario. Its <why> reports the two parts apart:
  # recovery (peers current, a leader, data, writes, metadata) and leadership
  # stability (same leader, term unchanged).
  v="$(range_verdict "$R" $((R + 6)))"
  verdict "ML$((R + 7))" D03-R16 "$dark returns: all current, same leader, meta term unchanged, every message back, writes and metadata work (S4)" \
    "${v%%|*}" "${v#*|}; recovery: $(ids_met "ML$((R + 1))" "ML$((R + 2))" "ML$((R + 4))" "ML$((R + 5))" "ML$((R + 6))"); leadership stability: $(ids_met "ML$((R + 3))" "ML$((R + 3))b")"
}

part_step3() {
  banner "Step 3 -- one region dark, leader in the hub, then recovery (S2 + S4)"
  ledger_init
  step3_round za au 42
  step3_round au za 59
}

# --- part step4: one region dark, leader in that region, then recovery (S3 + S4)
# Hand steps: exercises/EXERCISE-10-TERMINAL-STEPS.md, Step 4.
#
# Two rounds: za (ML76-ML93), then au (ML94-ML111). Each round: S3 while dark
# (D03-R15, verdict at +8), S4 after the thaw (D03-R16, verdict at +16), then
# a hub step-down at +17, measured apart from both (plan, S4).
#
# The new leader is never named in advance: the check asks only that one is
# elected outside the dark region, within ELECTION_S. Its name and cluster are
# notes. Same frozen-server rule as step3: while dark, read live ports only.

PREREQ_step4=(step1)

# step4 runs after step1, step2 or step3, so the message counts vary.
ready_step4() { ready_step2; }

# The ledger, read from the streams themselves: whatever is there now is what
# must still be there, byte for byte, after the return. All nine must be up.
ledger_from_streams() {
  local site s last q
  for site in "${SITES[@]}"; do
    s="$(stream_of "$site")"; : > "$(ledger "$site")"
    last="$(nats_as "$(port_of "$site")" lb stream info "$s" --json 2>/dev/null | jq -r '.state.last_seq')"
    for ((q=1; q<=last; q++)); do
      direct_get "$(port_of "$site")" lb "\$JS.API.DIRECT.GET.$s" "{\"seq\":$q}" >> "$(ledger "$site")" || true
    done
  done
}

step4_round() {
  local dark="$1" other="$2" B="$3" i cur l lead new term0 term1 t0 secs v
  local live; live="$(live_ports "$dark")"
  id() { echo "ML$((B + $1))"; }

  # Step 4.1: leader in the region that is about to go dark.
  stepdown "$dark" || true
  lead="$(wait_agreed_leader "^t-$dark-[123]\$" "" "$ELECTION_S")"
  for ((i=0; i<RECOVERY_S; i++)); do cur="$(current_peers)"; [ "$cur" = 8 ] && break; sleep 1; done
  check "$(id 0)" D03-R15 "$dark round: start -- leader's cluster, monitors answering, peers current" \
    "$dark 9 8" "$(region_of "$lead") $(answering) $cur"
  term0="$(meta_term)"

  # Step 4.2: freeze the region. NOT `rig-t4.sh freeze`: it sleeps 3 s and
  # probes each monitor before it returns, which adds about 4 s to the
  # election time (seen on the first scripted run: 9.2 s against 4-5 s in the
  # logs by hand). So send the STOP here, start the clock with it, and prove
  # the freeze after the election instead.
  local s
  t0="$(now)"
  for s in "$dark-1" "$dark-2" "$dark-3"; do kill -STOP "$(pid_of "$s")" 2>/dev/null || true; done

  # Step 4.3: a new leader, outside the dark region, read from a live port.
  # The leader field stays stale until the election, so wait for a live name.
  new=NONE
  for ((i=0; i<ELECTION_S*5; i++)); do
    l="$(meta_leader 8541)"
    [[ "$l" =~ ^t-(arb|$other)-[123]$ ]] && { new="$l"; break; }
    sleep 0.2
  done
  secs="$(secs_since "$t0")"

  check "$(id 1)" D03-R15 "freeze $dark: its three monitors do not answer" "3" \
    "$(dark_count $(http_of "$dark-1") $(http_of "$dark-2") $(http_of "$dark-3"))"
  KIND=procedure
  # shellcheck disable=SC2086
  check "$(id 2)" D03-R15 "$dark dark: a live leader outside $dark within ${ELECTION_S} s, and the six live servers agree" \
    "yes 1" "$([ "$new" != NONE ] && echo yes || echo no) $(meta_group_count $live)"
  KIND=rig
  note "$(id 2)a" D03-R15 "$dark dark: new leader, its cluster, seconds from the STOP until arb-1 names it (polled every 0.2 s)" \
    "$new $(region_of "$new") ${secs}s"

  # Step 4.4: acked writes to the two surviving streams, then read them back.
  KIND=procedure
  check "$(id 3)" D03-R15 "$dark dark: 3 acked writes to ODOMETER_ARB -- stored sequences" \
    "$(next_seqs arb 3)" "$(pub_three arb "s4.${dark}dark" 2000)"
  check "$(id 4)" D03-R15 "$dark dark: 3 acked writes to $(stream_of "$other") -- stored sequences" \
    "$(next_seqs "$other" 3)" "$(pub_three "$other" "s4.${dark}dark" 2000)"
  check "$(id 5)" D03-R15 "$dark dark: ODOMETER_ARB, every expected message by Direct Get" \
    "$(ledger_total arb)/$(ledger_total arb)" "$(readback arb)"
  check "$(id 6)" D03-R15 "$dark dark: $(stream_of "$other"), every expected message by Direct Get" \
    "$(ledger_total "$other")/$(ledger_total "$other")" "$(readback "$other")"

  # Step 4.5: the metadata probe.
  check "$(id 7)" D03-R15 "$dark dark: metadata probe -- create, delete, gone" "ok ok gone" "$(meta_probe)"
  term1="$(meta_term)"
  KIND=rig
  note "$(id 7)a" D03-R15 "$dark dark: meta term before the freeze -> after the probes" "$term0 -> $term1"

  v="$(range_verdict "$B" $((B + 7)))"
  verdict "$(id 8)" D03-R15 "$dark dark, leader was in $dark: a new leader elected, writes and metadata go on (S3)" \
    "${v%%|*}" "${v#*|}"

  # Steps 4.6-4.8: thaw, then recovery. "Same leader" means the one elected
  # while dark: does it keep the job when the old leader's region returns?
  recovery_checks "$dark" $((B + 9)) "$(meta_leader 8541)" "$term0" s4

  # Step 4.9: a hub step-down, measured apart from the recovery above.
  KIND=procedure
  lead="$(meta_leader 8541)"
  stepdown arb || true
  new="$(wait_agreed_leader '^t-(za|au|arb)-[123]$' "$lead" "$ELECTION_S")"
  check "$(id 17)" D03-R13 "after recovery: step-down --cluster arb -- leader's cluster, distinct leaders on nine" \
    "arb 1" "$(region_of "$new") $([ "$new" != NONE ] && echo 1 || echo 0)"
  KIND=rig
  note "$(id 17)a" D03-R13 "after recovery: hub step-down, leader before -> after" "$lead -> $new"
}

part_step4() {
  banner "Step 4 -- one region dark, leader in that region, then recovery (S3 + S4)"
  ledger_from_streams
  step4_round za au 76
  step4_round au za 94
}

# --- part step5: both regions dark, then recovery one region at a time -------
# Hand steps: Step 5. Plan test S5 (D03-R17); the last return is S4 (D03-R16).
#
# Order: leader in the hub, freeze za (6 of 9, quorum held), freeze au (3 of 9,
# quorum lost), probe, thaw za (the plan's partial recovery check), thaw au
# (recovery_checks). While au is frozen, read live ports only: hub ports
# throughout, za ports once za is back.

PREREQ_step5=(step1)
QUORUM_LOSS_S=90  # plan rule 6: the leader field goes NONE after quorum is lost

ready_step5() { ready_step2; }

# Peers the leader sees as current and active, counting only servers NOT in
# cluster $1 (still frozen). The leader is not in its own list.
live_current_peers() {
  local lead http
  lead="$(meta_leader 8541)"
  [ "$lead" = NONE ] && { echo 0; return 0; }
  http="$(http_of "${lead#t-}")"
  probe "$http" 'jsz?meta=1' \
    | jq --arg d "t-$1-" '[.meta_cluster.replicas[]? | select((.name | startswith($d)) | not)
                           | select(.current == true and .active < 5000000000)] | length' \
      2>/dev/null || echo 0
}

hub_ports() { echo 8541 8542 8543; }

part_step5() {
  banner "Step 5 -- both regions dark, then recovery one region at a time (S5 + S4)"
  ledger_from_streams
  local B=112 i cur l lead term0 term1 t0 secs none hold s err v
  id() { echo "ML$((B + $1))"; }

  # Step 5.1: leader in the hub, nine up, eight peers current.
  lead="$(wait_agreed_leader '^t-(za|au|arb)-[123]$' "" "$ELECTION_S")"
  if [ "$(region_of "$lead")" != arb ]; then
    stepdown arb || true
    lead="$(wait_agreed_leader '^t-arb-[123]$' "" "$ELECTION_S")"
  fi
  for ((i=0; i<RECOVERY_S; i++)); do cur="$(current_peers)"; [ "$cur" = 8 ] && break; sleep 1; done
  check "$(id 0)" D03-R17 "start -- leader's cluster, monitors answering, peers current" \
    "arb 9 8" "$(region_of "$lead") $(answering) $cur"
  term0="$(meta_term)"

  # Step 5.2: freeze za. 6 of 9 -- the group keeps quorum and its leader.
  "$RIG" freeze za >/dev/null 2>&1 || true
  # shellcheck disable=SC2046
  check "$(id 1)" D03-R17 "freeze za: its three monitors do not answer" "3" \
    "$(dark_count $(http_of za-1) $(http_of za-2) $(http_of za-3))"
  # shellcheck disable=SC2046
  check "$(id 2)" D03-R17 "za dark: the hub leader holds, and the six live servers agree" \
    "$lead 1" "$(meta_leader 8541) $(meta_group_count $(live_ports za))"

  # Step 5.3: freeze au with a direct STOP, so the clock starts with it (see
  # step4_round). Then wait until ALL THREE hub monitors' leader fields are
  # NONE. One field can lag another by many seconds: the first run read
  # arb-1 NONE while arb-3 still named the old leader (stale-leader field),
  # and the old leader's log lagged arb-1's field by about 20 s by hand.
  t0="$(now)"
  for s in au-1 au-2 au-3; do kill -STOP "$(pid_of "$s")" 2>/dev/null || true; done
  local first=""
  none=no
  for ((i=0; i<QUORUM_LOSS_S*5; i++)); do
    l="$(for p in $(hub_ports); do meta_leader "$p"; done | paste -sd ' ' -)"
    [ -z "$first" ] && [[ "$l" == *NONE* ]] && first="$(secs_since "$t0")"
    [ "$l" = "NONE NONE NONE" ] && { none=yes; break; }
    sleep 0.2
  done
  secs="$(secs_since "$t0")"
  # shellcheck disable=SC2046
  check "$(id 3)" D03-R17 "freeze au: its three monitors do not answer" "3" \
    "$(dark_count $(http_of au-1) $(http_of au-2) $(http_of au-3))"

  KIND=procedure
  # shellcheck disable=SC2046
  check "$(id 4)" D03-R17 "both dark: no meta leader on the three hub monitors within ${QUORUM_LOSS_S} s" \
    "yes NONE NONE NONE" "$none $(for p in $(hub_ports); do meta_leader "$p"; done | paste -sd ' ' -)"
  KIND=rig
  note "$(id 4)a" D03-R17 "both dark: seconds from the au STOP until the first, then all three, hub leader fields are NONE (polled every 0.2 s)" \
    "first ${first:-never}s; all ${secs}s"

  # No site may elect while 3 of 9 are reachable. The regions are frozen, so
  # only the hub could. Once all three fields are NONE, a name that appears
  # is a new claim, not a stale one. Watch for HOLD_S.
  KIND=procedure
  hold=none
  for ((i=0; i<HOLD_S*2; i++)); do
    for p in $(hub_ports); do l="$(meta_leader "$p")"; [ "$l" = NONE ] || hold="named: $l"; done
    sleep 0.5
  done
  check "$(id 5)" D03-R17 "both dark: no hub monitor names a leader for ${HOLD_S} s more" "none" "$hold"

  # Step 5.4: acked writes to the hub stream, then read them back.
  check "$(id 6)" D03-R17 "both dark: 3 acked writes to ODOMETER_ARB -- stored sequences" \
    "$(next_seqs arb 3)" "$(pub_three arb s5.bothdark 4000)"
  check "$(id 7)" D03-R17 "both dark: ODOMETER_ARB, every expected message by Direct Get" \
    "$(ledger_total arb)/$(ledger_total arb)" "$(readback arb)"

  # Step 5.5: the metadata probe must FAIL. Its error is kept as a note.
  err="$(nats_as "$(port_of arb)" lb --timeout 10s stream add PROBE_META --subjects probe.meta \
           --storage memory --replicas 1 --cluster arb --defaults 2>&1 >/dev/null)" && err=ok
  check "$(id 8)" D03-R17 "both dark: creating a stream fails -- no meta leader" "fails" \
    "$([ "$err" = ok ] && echo created || echo fails)"
  term1="$(meta_term)"
  KIND=rig
  note "$(id 8)a" D03-R17 "both dark: the create's error; meta term before the freezes -> now; arb-1 raft state" \
    "$(printf '%s' "$err" | tail -1); term $term0 -> $term1; $(probe 8541 'raftz?group=_meta_' | jq -r '.["$SYS"]._meta_.state // "?"' 2>/dev/null)"

  v="$(range_verdict "$B" $((B + 8)))"
  verdict "$(id 9)" D03-R17 "both dark, hub healthy: no meta leader, hub stream writes go on, metadata frozen (S5)" \
    "${v%%|*}" "${v#*|}"

  # Step 5.6: thaw za -- the partial recovery check. au stays frozen.
  t0="$(now)"
  "$RIG" thaw za >/dev/null 2>&1 || true
  # shellcheck disable=SC2046
  check "$(id 10)" D03-R17 "thaw za: monitors answering; au's three still dark" "6 3" \
    "$(answering) $(dark_count $(http_of au-1) $(http_of au-2) $(http_of au-3))"

  KIND=procedure
  lead=NONE
  for ((i=0; i<RECOVERY_S*5; i++)); do
    l="$(meta_leader 8541)"
    # shellcheck disable=SC2046
    if [[ "$l" =~ ^t-(za|arb)-[123]$ ]] && [ "$(meta_group_count $(live_ports au))" = 1 ]; then lead="$l"; break; fi
    sleep 0.2
  done
  secs="$(secs_since "$t0")"
  check "$(id 11)" D03-R17 "za back: one live leader that the six name, within ${RECOVERY_S} s" \
    "yes" "$([ "$lead" != NONE ] && echo yes || echo no)"
  KIND=rig
  note "$(id 11)a" D03-R17 "za back: leader, its cluster, seconds from the thaw; meta term" \
    "$lead $(region_of "$lead") ${secs}s; term $(meta_term)"

  KIND=procedure
  for ((i=0; i<RECOVERY_S; i++)); do cur="$(live_current_peers au)"; [ "$cur" = 5 ] && break; sleep 1; done
  check "$(id 12)" D03-R17 "za back: the five live peers current on the leader's view, within ${RECOVERY_S} s" "5" "$cur"
  check "$(id 13)" D03-R17 "za back: arb za, every expected message by Direct Get; then KV vehicle-1" \
    "$(ledger_total arb)/$(ledger_total arb) $(ledger_total za)/$(ledger_total za) seed-k1-v1" \
    "$(readback arb) $(readback za) $(nats_as "$(port_of arb)" lb kv get t7-vehicles vehicle-1 --raw 2>/dev/null || echo fail)"
  check "$(id 14)" D03-R17 "za back: one acked write to each of arb za -- stored sequences" \
    "$(next_seqs arb 1) $(next_seqs za 1)" \
    "$(pub_ack arb s5.zaback.arb.1 5001) $(pub_ack za s5.zaback.za.1 5001)"
  check "$(id 15)" D03-R17 "za back: metadata probe -- create, delete, gone" "ok ok gone" "$(meta_probe)"
  KIND=rig

  v="$(range_verdict $((B + 10)) $((B + 15)))"
  verdict "$(id 16)" D03-R17 "za back, au still dark: quorum, a leader, data, writes and metadata return (partial recovery)" \
    "${v%%|*}" "${v#*|}"

  # Steps 5.7-5.8: thaw au -- a full return, the same checks as Step 3.
  recovery_checks au $((B + 17)) "$(meta_leader 8541)" "$term0" s5
}

# --- part step6: one region dark, only its stream stops ----------------------
# Hand steps: Step 6. Plan test S6 (D03-R18). The question is DATA, not the
# meta group: the meta leader stays in the hub, and each ODOMETER_* stream is
# its own R3 group inside one cluster. Where the meta leader goes on the
# return is a note -- steps 3-5 measure that.

PREREQ_step6=(step1)
DARK_PUB_S=5      # the cap on the acked write to the dark region's stream

ready_step6() { ready_step2; }

# One round: freeze cluster $1. $2 is the other region, $3 the first check
# number (11 numbers).
step6_round() {
  local dark="$1" other="$2" B="$3" i cur lead term0 t0 secs err ds held extra q v
  id() { echo "ML$((B + $1))"; }
  ds="$(stream_of "$dark")"

  # Step 6.1: leader in the hub, nine up, eight peers current.
  lead="$(wait_agreed_leader '^t-(za|au|arb)-[123]$' "" "$ELECTION_S")"
  if [ "$(region_of "$lead")" != arb ]; then
    stepdown arb || true
    lead="$(wait_agreed_leader '^t-arb-[123]$' "" "$ELECTION_S")"
  fi
  for ((i=0; i<RECOVERY_S; i++)); do cur="$(current_peers)"; [ "$cur" = 8 ] && break; sleep 1; done
  check "$(id 0)" D03-R18 "$dark round: start -- leader's cluster, monitors answering, peers current" \
    "arb 9 8" "$(region_of "$lead") $(answering) $cur"
  term0="$(meta_term)"

  # Step 6.2: freeze the region; the meta group must not notice.
  "$RIG" freeze "$dark" >/dev/null 2>&1 || true
  # shellcheck disable=SC2046
  check "$(id 1)" D03-R18 "freeze $dark: its three monitors do not answer" "3" \
    "$(dark_count $(http_of "$dark-1") $(http_of "$dark-2") $(http_of "$dark-3"))"
  KIND=procedure
  # shellcheck disable=SC2046
  check "$(id 2)" D03-R18 "$dark dark: the meta leader is still the hub leader, and the six live servers agree" \
    "$lead 1" "$(meta_leader 8541) $(meta_group_count $(live_ports "$dark"))"

  # Step 6.3: an acked write to the dark region's stream, through the hub.
  # Not added to the ledger: nothing acknowledged it.
  t0="$(now)"
  err="$(nats_as "$(port_of arb)" lb --timeout "${DARK_PUB_S}s" pub -J --no-templates \
           -H "Nats-Msg-Id:s6.${dark}dark.$dark.1" "evt.odo.$dark.v1" \
           "{\"id\":\"s6.${dark}dark.$dark.1\",\"vehicle\":\"v-1\",\"km\":7001}" 2>&1)" && err="acked: $err"
  secs="$(secs_since "$t0")"
  check "$(id 3)" D03-R18 "$dark dark: an acked write to $ds through arb (cap ${DARK_PUB_S} s) -- not acknowledged" \
    "no ack" "$(case "$err" in acked:*) echo acked ;; *) echo "no ack" ;; esac)"
  KIND=rig
  note "$(id 3)a" D03-R18 "$dark dark: the write's last line; seconds until the CLI gave up" \
    "$(printf '%s' "$err" | tail -1); ${secs}s"

  # Step 6.4: the two other streams, and metadata.
  KIND=procedure
  check "$(id 4)" D03-R18 "$dark dark: 3 acked writes to ODOMETER_ARB -- stored sequences" \
    "$(next_seqs arb 3)" "$(pub_three arb "s6.${dark}dark" 7000)"
  check "$(id 5)" D03-R18 "$dark dark: 3 acked writes to $(stream_of "$other") -- stored sequences" \
    "$(next_seqs "$other" 3)" "$(pub_three "$other" "s6.${dark}dark" 7000)"
  check "$(id 6)" D03-R18 "$dark dark: ODOMETER_ARB and $(stream_of "$other"), every expected message by Direct Get" \
    "$(ledger_total arb)/$(ledger_total arb) $(ledger_total "$other")/$(ledger_total "$other")" \
    "$(readback arb) $(readback "$other")"
  check "$(id 7)" D03-R18 "$dark dark: metadata probe -- create, delete, gone" "ok ok gone" "$(meta_probe)"
  KIND=rig

  v="$(range_verdict "$B" $((B + 7)))"
  verdict "$(id 8)" D03-R18 "$dark dark, meta group healthy: only $ds stops taking writes (S6)" \
    "${v%%|*}" "${v#*|}"

  # Step 6.5: thaw. Was the unacknowledged write stored after all? That is a
  # note: nothing promises either answer. If it was, it joins the ledger, so
  # the rest of the run checks what the stream really holds.
  "$RIG" thaw "$dark" >/dev/null 2>&1 || true
  check "$(id 9)" D03-R18 "thaw $dark: all nine monitors answer" "9" "$(answering)"
  for ((i=0; i<RECOVERY_S; i++)); do cur="$(current_peers)"; [ "$cur" = 8 ] && break; sleep 1; done
  held=""
  for ((i=0; i<RECOVERY_S; i++)); do
    held="$(nats_as "$(port_of "$dark")" lb stream info "$ds" --json 2>/dev/null | jq -r '.state.last_seq // empty')"
    [ -n "$held" ] && break; sleep 1
  done
  extra=$(( ${held:-0} - $(ledger_total "$dark") ))
  for ((q=$(ledger_total "$dark")+1; q<=${held:-0}; q++)); do
    direct_get "$(port_of "$dark")" lb "\$JS.API.DIRECT.GET.$ds" "{\"seq\":$q}" >> "$(ledger "$dark")" || true
  done
  note "$(id 9)a" D03-R18 "after the thaw: $ds messages beyond the acked ones (the unacked write stored later?); meta leader before -> after; meta term" \
    "${held:-?} held, $extra beyond the acked; $lead -> $(meta_leader 8541); term $term0 -> $(meta_term)"

  # The region's stream works again, so the next round starts whole. A retry
  # reuses the msg id, so the dedup window keeps it to one stored copy.
  local want got=fail
  want="$(next_seqs "$dark" 1)"
  for ((i=0; i<10; i++)); do
    got="$(pub_ack "$dark" "s6.${dark}back.$dark.1" 7101)"; [ "$got" != fail ] && break; sleep 1
  done
  check "$(id 10)" D03-R18 "after the thaw: $ds takes an acked write, and every message reads back by Direct Get" \
    "$want $(( $(ledger_total "$dark") ))/$(ledger_total "$dark")" "$got $(readback "$dark")"
}

part_step6() {
  banner "Step 6 -- one region dark: only the stream placed there stops (S6)"
  ledger_from_streams
  step6_round za au 137
  step6_round au za 148
}

# --- run ---------------------------------------------------------------------

# One part on our own rig runs the parts it needs first (PREREQ_<part>). An
# attached rig must already be in the part's starting state (ready_<part>).
if [ "$PART" = all ]; then
  RUN_PARTS=("${PARTS[@]}")
elif [ "$MODE" = own ]; then
  prereq="PREREQ_$PART[@]"
  RUN_PARTS=(${!prereq+"${!prereq}"} "$PART")
else
  RUN_PARTS=("$PART")
fi
# Only an attached rig can be in the wrong state; our own rig is fresh.
[ "$MODE" = attached ] && "ready_$PART"
[ "${LAB_RUN_ALL:-}" = 1 ] || : > "$RESULTS"
for p in "${RUN_PARTS[@]}"; do "part_$p"; done

echo
awk -F'\t' '$7=="PASS"{p++} $7=="FAIL"{f++; if ($9=="procedure") pf++} $7=="NOTE"{n++}
  END{printf "  %d passed, %d failed (%d of them procedure checks), %d notes\n", p, f, pf, n}' "$RESULTS"
if [ "$MODE" = attached ]; then
  echo "  rig left running (attached). ./lab/rig-t4.sh status to look, down to stop."
else
  echo "  rig stopped (started by this run)."
fi
fails=$(awk -F'\t' '$7=="FAIL" && $9=="rig"{n++} END{print n+0}' "$RESULTS")
[ "$fails" -eq 0 ]
