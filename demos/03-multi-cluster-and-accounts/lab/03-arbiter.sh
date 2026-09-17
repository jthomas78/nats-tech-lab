#!/usr/bin/env bash
# T3 / FIGURE C -- THE ARBITER, AND IT REALLY WORKS.
#
# T2's problem is arithmetic. A gateway makes one meta group of 6, majority 4,
# and 3 survivors is fewer than 4. Add ONE more voter in a third location and
# the sum changes: 7 peers, majority 4, and either regional loss leaves exactly
# 4. The freeze goes away.
#
# Two warnings this script also measures, because the arbiter is not free:
#
#   1. It has ZERO SLACK. 7 peers, majority 4, lose a region -> exactly 4 left.
#      One more node down after that and it freezes again. A three-instance
#      arbiter (T4) buys one node of slack. T4 has never been built, so every
#      T4 row in the matrix stays `inferred`.
#   2. A voting server with JetStream on is also a STORAGE server. Ordinary
#      traffic will not drift onto it -- placement follows the client -- but an
#      explicit `--cluster arb`, or any client that dials the arbiter's client
#      port directly, puts REAL unreplicated data in your smallest, least
#      protected site.
#
# The arbiter must sit somewhere independent of BOTH regions. One living in the
# ZA data centre is worth nothing.
#
# Requirements exercised: D03-R1, D03-R5, D03-R9.

source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

TOPOLOGY="T3 / C -- gateway + arbiter"
SUBJECT="evt.odo.v1"
ALL_HTTP=(8231 8232 8233 8241 8242 8243 8540)

# The shared conf_gateway helper joins ONE peer. Here every server has two.
gw3() {                       # $1 name  $2 my gw name  $3 my gw port
  local name="$1" gw="$2" mine="$3" f="$RUN_DIR/t-$1.conf"
  {
    printf 'gateway {\n  name: %s\n  listen: 127.0.0.1:%s\n  gateways: [\n' "$gw" "$mine"
    [ "$gw" != za ]  && printf '    { name: za,  urls: [ nats://127.0.0.1:7231, nats://127.0.0.1:7232, nats://127.0.0.1:7233 ] },\n'
    [ "$gw" != au ]  && printf '    { name: au,  urls: [ nats://127.0.0.1:7241, nats://127.0.0.1:7242, nats://127.0.0.1:7243 ] },\n'
    [ "$gw" != arb ] && printf '    { name: arb, urls: [ nats://127.0.0.1:7540 ] },\n'
    printf '  ]\n}\n'
  } >> "$f"
}

build() {
  local i
  for i in 1 2 3; do
    conf_head "za-$i" "423$i" "823$i"
    conf_cluster "za-$i" za "623$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '623%s ' $j; done)
    gw3 "za-$i" za "723$i"
    conf_accounts "za-$i"

    conf_head "au-$i" "424$i" "824$i"
    conf_cluster "au-$i" au "624$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '624%s ' $j; done)
    gw3 "au-$i" au "724$i"
    conf_accounts "au-$i"
  done
  # The arbiter. ONE instance, its own cluster, in a third location.
  # A one-node arbiter still has a cluster block, so it still needs a route --
  # pointed at itself. Without it, it is the only one of the seven that will
  # not start: "JetStream cluster requires configured routes or solicited
  # leafnode for the system account".
  conf_head arb 4540 8540
  conf_cluster arb arb 6540 6540
  gw3 arb arb 7540
  conf_accounts arb
}

lab_init
banner "$TOPOLOGY"

build
for i in 1 2 3; do start_server "za-$i"; start_server "au-$i"; done
start_server arb
for h in "${ALL_HTTP[@]}"; do wait_ready "$h"; done
wait_meta_leader 8231 || true
sleep 5

# --- Seven peers, one group -------------------------------------------------
check C1 D03-R5 "meta group size (majority is 4)" "7" "$(meta_size 8231)"
check C2 D03-R5 "distinct JetStream meta groups across all three sites" \
      "1" "$(meta_group_count "${ALL_HTTP[@]}")"

# --- Lose AU. 4 of 7 survive. ----------------------------------------------
freeze au-1 au-2 au-3
wait_dark 8241 8242 8243
LEADER_1="$(wait_live_leader 8231 '^t-(za|arb)')"
check C3 D03-R1 "AU dark: a leader is elected among the survivors" \
      "yes" "$([ "$LEADER_1" = "NONE" ] && echo no || echo yes)"
note  C3a D03-R1 "AU dark: which server took the lead" "$LEADER_1"
check C4 D03-R1 "AU dark: ZA creates a NEW 3-replica stream" \
      "ok" "$(fails_or_ok 4231 za stream add T_AFTER_AU --subjects "evt.a.v1" \
              --storage file --replicas 3 --cluster za --defaults)"

thaw au-1 au-2 au-3
wait_meta_leader 8241 || true
sleep 8

# --- Now lose ZA instead -- the side that held the leader. ------------------
freeze za-1 za-2 za-3
wait_dark 8231 8232 8233
LEADER_2="$(wait_live_leader 8241 '^t-(au|arb)')"
check C5 D03-R1 "ZA dark: a leader is elected among the survivors" \
      "yes" "$([ "$LEADER_2" = "NONE" ] && echo no || echo yes)"
note  C5a D03-R1 "ZA dark: which server took the lead" "$LEADER_2"
check C6 D03-R1 "ZA dark: AU creates a NEW 3-replica stream" \
      "ok" "$(fails_or_ok 4241 au stream add T_AFTER_ZA --subjects "evt.b.v1" \
              --storage file --replicas 3 --cluster au --defaults)"

thaw za-1 za-2 za-3
wait_meta_leader 8231 || true
sleep 8

# --- The arbiter is not automatically safe ---------------------------------
# Good news first: with no placement flag, nothing drifts onto the arbiter.
LANDED_ARB=0
for n in 1 2 3 4 5 6 7 8; do
  nats_as 4231 za stream add "P$n" --subjects "evt.p$n.v1" --storage file \
          --replicas 1 --defaults >/dev/null 2>&1 || true
  [ "$(stream_cluster 4231 za "P$n")" = "arb" ] && LANDED_ARB=$((LANDED_ARB+1))
done
check C7 D03-R9 "8 unplaced R1 streams from a ZA client that landed on the arbiter" \
      "0" "$LANDED_ARB"

# Bad news: ask for it and you get it. Real, unreplicated data on the arbiter.
nats_as 4231 za stream add ARB1 --subjects "evt.arb1.v1" --storage file \
        --replicas 1 --cluster arb --defaults >/dev/null 2>&1 || true
check C8 D03-R9 "an explicit --cluster arb R1 stream lands in" \
      "arb" "$(stream_cluster 4231 za ARB1)"
check C9 D03-R9 "an explicit --cluster arb R3 stream -- one node cannot hold 3" \
      "10005" "$(err_code 4231 za stream add ARB3 --subjects "evt.arb3.v1" \
                 --storage file --replicas 3 --cluster arb --defaults)"

# And a client that simply dials the arbiter's client port does it by accident.
nats_as 4540 za stream add DIRECT --subjects "evt.direct.v1" --storage file \
        --replicas 1 --defaults >/dev/null 2>&1 || true
check C10 D03-R9 "a client dialling the arbiter's port directly creates a stream in" \
      "arb" "$(stream_cluster 4540 za DIRECT)"

note C11 D03-R9 "so" \
     "keep clients off the arbiter's client port, or fence it with placement"
note C12 D03-R5 "slack after losing one region" \
     "none -- 4 of 7 is exactly the majority; one more node freezes it"

banner "$TOPOLOGY -- done"
