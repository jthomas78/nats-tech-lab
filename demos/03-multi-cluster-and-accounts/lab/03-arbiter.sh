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
# Requirements exercised: D03-R1, D03-R2, D03-R4, D03-R5, D03-R9.

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
# --- THE SHARED ACCOUNT, ASKED FROM BOTH REGIONS ---------------------------
# The same scenario as T2's A26, on seven peers instead of six. The arbiter
# changes the QUORUM arithmetic and nothing else, so the answer must not move:
# one stream, placed in za, counted as 1 from both regions because a gateway
# joins one namespace -- it does not copy anything.
nats_as 4231 lb stream add SHARED_ODO --subjects "evt.shared.v1" --storage file \
        --replicas 3 --cluster za --defaults >/dev/null
nats_as 4231 lb pub "evt.shared.v1" '{"km":9.0}' >/dev/null 2>&1
sleep 2
check C13 D03-R2 "one publish in region ZA, shared account LB: messages seen from region ZA / region AU" \
      "1 / 1" "$(stream_msgs 4231 lb SHARED_ODO) / $(stream_msgs 4241 lb SHARED_ODO)" \
      A26
note  C13a D03-R2 "why that reads 1 / 1 and not 1 / 0" \
      "ONE stream in za, read over the WAN from au -- the arbiter did not change it" \
      A26

# --- DOES ANY OF IT LAND ON THE ARBITER? -----------------------------------
# C7-C10 asked that of placement: what happens when a client picks the site,
# by flag or by accident. This asks it of everything else. A26, A6, A9, A18,
# A22, A24 and A25 were all measured on T2/A's six peers, where there was no
# third site to drift onto. Re-asked here, every answer must be the same one.
#
# A third site changes the QUORUM arithmetic. If it also changed where data
# sits, the arbiter would be a data centre, not a vote.
nats_as 4231 za stream add ODOMETER --subjects "$SUBJECT" --storage file \
        --replicas 3 --cluster za --defaults >/dev/null 2>&1 || true
nats_as 4241 au stream add ODOMETER --subjects "$SUBJECT" --storage file \
        --replicas 3 --cluster au --defaults >/dev/null 2>&1 || true
sleep 2
check C14 D03-R2 "per-region accounts: LB_ZA / LB_AU ODOMETER land in" \
      "za / au" "$(stream_cluster 4231 za ODOMETER) / $(stream_cluster 4241 au ODOMETER)" \
      A6
check C15 D03-R5 "LB_ZA ODOMETER: replicas asked for / peers built" \
      "3 / 3" "$(stream_replicas 4231 za ODOMETER) / $(stream_peers 4231 za ODOMETER)" \
      A22
check C16 D03-R5 "LB_ZA / LB_AU ODOMETER: region holding the stream leader" \
      "za / au" "$(stream_leader_region 4231 za ODOMETER) / $(stream_leader_region 4241 au ODOMETER)" \
      A24
note  C16a D03-R5 "which server won the ZA stream election this run" \
      "$(stream_leader 4231 za ODOMETER)" A24a

# The one question T2/A could not ask, because T2/A had nowhere to drift to.
check C17 D03-R9 "sites actually carrying LB_ZA ODOMETER's three copies" \
      "za" "$(stream_peer_regions 4231 za ODOMETER)"

nats_as 4241 au kv add t7-vehicles --storage file --replicas 3 >/dev/null 2>&1 || true
check C18 D03-R4 "KV t7-vehicles created from AU, no placement flag, lands in" \
      "au" "$(stream_cluster 4241 au KV_t7-vehicles)" A9

nats_as 4241 au consumer add ODOMETER AU_READER --pull --deliver all \
        --ack explicit --defaults >/dev/null 2>&1 || true
check C19 D03-R4 "LB_AU's consumer, made from AU on AU's stream, lives in cluster" \
      "au" "$(consumer_cluster 4241 au ODOMETER AU_READER)" A18

check C20 D03-R5 "shared LB SHARED_ODO, seen from AU: peers / leader region" \
      "3 / za" "$(stream_peers 4241 lb SHARED_ODO) / $(stream_leader_region 4241 lb SHARED_ODO)" \
      A25
note  C20a D03-R9 "so the arbiter is a VOTE and not a data centre" \
      "it changes the quorum arithmetic only -- no copy, KV or consumer moved to it"

note C12 D03-R5 "slack after losing one region" \
     "none -- 4 of 7 is exactly the majority; one more node freezes it"

banner "$TOPOLOGY -- done"
