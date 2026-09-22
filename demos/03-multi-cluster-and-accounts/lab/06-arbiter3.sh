#!/usr/bin/env bash
# T4 / FIGURE F -- THE ARBITER WITH SLACK. Three voters, not one.
#
# T3 works, and T3 has ZERO slack. 7 peers, majority 4, lose a region and
# exactly 4 are left. One more node down after that -- an upgrade, a disk, a
# careless reboot -- and the whole supercluster freezes for changes again.
#
# T4 is the same idea with a three-node arbiter CLUSTER instead of one box:
# 9 peers, majority 5, lose a region and 6 are left. That is one spare node.
#
# Until this script existed, T4 had never been built -- here or anywhere else
# in the lab. Every T4 row in the demo's matrix was marked `inferred`, which
# means "the arithmetic says so and nobody checked". This measures it, and the
# interesting part is not F1 or F3. It is F4 and F5: the run walks the
# supercluster down one node at a time and finds the exact node that breaks it.
#
# The arbiter site still has to be independent of BOTH regions. Three arbiter
# nodes in the ZA data centre are worth nothing at all -- they die together.
#
# Requirements exercised: D03-R1, D03-R2, D03-R4, D03-R5, D03-R9.

source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

TOPOLOGY="T4 / F -- gateway + 3-node arbiter"
ALL_HTTP=(8231 8232 8233 8241 8242 8243 8541 8542 8543)

# Three gateway peers per server, like 03-arbiter.sh -- but the arbiter site
# now answers on three gateway ports instead of one.
gw3() {                       # $1 name  $2 my gw name  $3 my gw port
  local name="$1" gw="$2" mine="$3" f="$RUN_DIR/t-$1.conf"
  {
    printf 'gateway {\n  name: %s\n  listen: 127.0.0.1:%s\n  gateways: [\n' "$gw" "$mine"
    [ "$gw" != za ]  && printf '    { name: za,  urls: [ nats://127.0.0.1:7231, nats://127.0.0.1:7232, nats://127.0.0.1:7233 ] },\n'
    [ "$gw" != au ]  && printf '    { name: au,  urls: [ nats://127.0.0.1:7241, nats://127.0.0.1:7242, nats://127.0.0.1:7243 ] },\n'
    [ "$gw" != arb ] && printf '    { name: arb, urls: [ nats://127.0.0.1:7541, nats://127.0.0.1:7542, nats://127.0.0.1:7543 ] },\n'
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

    # The whole difference from T3 is here: a real 3-node cluster, so the
    # arbiter site is not itself a single point of failure.
    conf_head "arb-$i" "454$i" "854$i"
    conf_cluster "arb-$i" arb "654$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '654%s ' $j; done)
    gw3 "arb-$i" arb "754$i"
    conf_accounts "arb-$i"
  done
}

lab_init
banner "$TOPOLOGY"

build
for i in 1 2 3; do start_server "za-$i"; start_server "au-$i"; start_server "arb-$i"; done
for h in "${ALL_HTTP[@]}"; do wait_ready "$h"; done
wait_meta_leader 8231 || true
sleep 5

# --- Nine peers, one group --------------------------------------------------
check F1 D03-R5 "meta group size (majority is 5)" "9" "$(meta_size 8231)"
check F2 D03-R5 "distinct JetStream meta groups across all three sites" \
      "1" "$(meta_group_count "${ALL_HTTP[@]}")"

# --- Step 1: lose a whole region. 6 of 9 survive. ---------------------------
freeze au-1 au-2 au-3
wait_dark 8241 8242 8243
LEADER_1="$(wait_live_leader 8231 '^t-(za|arb)')"
check F3 D03-R1 "AU dark (6 of 9 left): a leader is elected among the survivors" \
      "yes" "$([ "$LEADER_1" = "NONE" ] && echo no || echo yes)"
check F4 D03-R1 "AU dark: ZA creates a NEW 3-replica stream" \
      "ok" "$(fails_or_ok 4231 za stream add T_AFTER_AU --subjects "evt.a.v1" \
              --storage file --replicas 3 --cluster za --defaults)"

# --- Step 2: THE SLACK. Take one arbiter node too. 5 of 9 -- still a majority.
# T3 has nothing left at this point. T4 does. This is the only reason to pay
# for two more arbiter boxes.
freeze arb-1
wait_dark 8541
LEADER_2="$(wait_live_leader 8231 '^t-(za|arb-[23])')"
check F5 D03-R5 "AU dark AND one arbiter node dark (5 of 9 -- exactly the majority)" \
      "yes" "$([ "$LEADER_2" = "NONE" ] && echo no || echo yes)"
note  F5a D03-R5 "which server held the lead on the last surviving majority" "$LEADER_2"
# A NEW meta leader is named before it can act. Measured here, 2026-09-17:
# ask the instant `/jsz` names the new leader and the create is REFUSED. This
# is the twin of the stale-leader trap in _common.sh -- that one reports a
# leader that is already gone, this one reports a leader that has not started.
# So do not sleep a magic number: ask repeatedly, and record how long it took.
F6_SECS=0; F6_GOT=fails
F6_T0=$(date +%s)
for _ in $(seq 1 20); do
  F6_GOT="$(fails_or_ok 4231 za stream add T_SLACK --subjects "evt.s.v1" \
            --storage file --replicas 3 --cluster za --defaults)"
  [ "$F6_GOT" = "ok" ] && break
  sleep 2
done
F6_SECS=$(( $(date +%s) - F6_T0 ))
check F6 D03-R1 "5 of 9: ZA still creates a NEW 3-replica stream" "ok" "$F6_GOT"
note  F6a D03-R5 "seconds after the new leader was named before it accepted a change" \
      "${F6_SECS}s"

# --- Step 3: one node further. 4 of 9 is below 5. It must freeze. -----------
freeze arb-2
wait_dark 8542
note  F7a D03-R5 "seconds /jsz still named a meta leader after the majority went" \
      "$(seconds_until_no_leader 8231)s"
check F7 D03-R5 "4 of 9 left: meta leader seen from ZA" \
      "NONE" "$(meta_leader 8231)"
check F8 D03-R1 "4 of 9 left: ZA tries to create a NEW stream" \
      "fails" "$(fails_or_ok 4231 za stream add T_TOOFAR --subjects "evt.t.v1" \
                 --storage file --replicas 3 --cluster za --defaults)"
check F9 D03-R1 "4 of 9 left: publishing into an EXISTING stream still works" \
      "ok" "$(fails_or_ok 4231 za pub "evt.a.v1" '{"km":1}')"

# --- Recovery ---------------------------------------------------------------
thaw arb-1 arb-2 au-1 au-2 au-3
wait_meta_leader 8231 || true
sleep 8
check F10 D03-R5 "after recovery: meta group size" "9" "$(meta_size 8231)"

# --- THE SHARED ACCOUNT, ASKED FROM BOTH REGIONS ---------------------------
# A26 on six peers, C13 on seven, this on nine. The arbiter cluster changes the
# quorum arithmetic and nothing else, so the answer must not move.
nats_as 4231 lb stream add SHARED_ODO --subjects "evt.shared.v1" --storage file \
        --replicas 3 --cluster za --defaults >/dev/null
nats_as 4231 lb pub "evt.shared.v1" '{"km":9.0}' >/dev/null 2>&1
sleep 2
check F13 D03-R2 "one publish in region ZA, shared account LB: messages seen from region ZA / region AU" \
      "1 / 1" "$(stream_msgs 4231 lb SHARED_ODO) / $(stream_msgs 4241 lb SHARED_ODO)" \
      A26
note  F13a D03-R2 "why that reads 1 / 1 and not 1 / 0" \
      "ONE stream in za, read over the WAN from au -- a 3-node arbiter did not change it" \
      A26

# --- DOES ANY OF IT LAND ON THE ARBITER CLUSTER? ---------------------------
# C14-C20 asked this of ONE arbiter box. Three of them is a real cluster: it
# can hold a 3-replica stream on its own, which a single box cannot (C9). So
# the drift question is sharper here than it was on T3.
#
# Every row below was first measured on T2/A's six peers, where there was no
# third site at all. The answers must not move.
nats_as 4231 za stream add ODOMETER --subjects "evt.odo.v1" --storage file \
        --replicas 3 --cluster za --defaults >/dev/null 2>&1 || true
nats_as 4241 au stream add ODOMETER --subjects "evt.odo.v1" --storage file \
        --replicas 3 --cluster au --defaults >/dev/null 2>&1 || true
sleep 2
check F14 D03-R2 "per-region accounts: LB_ZA / LB_AU ODOMETER land in" \
      "za / au" "$(stream_cluster 4231 za ODOMETER) / $(stream_cluster 4241 au ODOMETER)" \
      A6
check F15 D03-R5 "LB_ZA ODOMETER: replicas asked for / peers built" \
      "3 / 3" "$(stream_replicas 4231 za ODOMETER) / $(stream_peers 4231 za ODOMETER)" \
      A22
check F16 D03-R5 "LB_ZA / LB_AU ODOMETER: region holding the stream leader" \
      "za / au" "$(stream_leader_region 4231 za ODOMETER) / $(stream_leader_region 4241 au ODOMETER)" \
      A24
note  F16a D03-R5 "which server won the ZA stream election this run" \
      "$(stream_leader 4231 za ODOMETER)" A24a

# A 3-node arbiter CAN hold three copies. This asks whether it ever gets them
# without being told to.
check F17 D03-R9 "sites actually carrying LB_ZA ODOMETER's three copies" \
      "za" "$(stream_peer_regions 4231 za ODOMETER)" C17

nats_as 4241 au kv add t7-vehicles --storage file --replicas 3 >/dev/null 2>&1 || true
check F18 D03-R4 "KV t7-vehicles created from AU, no placement flag, lands in" \
      "au" "$(stream_cluster 4241 au KV_t7-vehicles)" A9

nats_as 4241 au consumer add ODOMETER AU_READER --pull --deliver all \
        --ack explicit --defaults >/dev/null 2>&1 || true
check F19 D03-R4 "LB_AU's consumer, made from AU on AU's stream, lives in cluster" \
      "au" "$(consumer_cluster 4241 au ODOMETER AU_READER)" A18

check F20 D03-R5 "shared LB SHARED_ODO, seen from AU: peers / leader region" \
      "3 / za" "$(stream_peers 4241 lb SHARED_ODO) / $(stream_leader_region 4241 lb SHARED_ODO)" \
      A25
note  F20a D03-R9 "so a 3-node arbiter is still a VOTE and not a data centre" \
      "it buys one node of slack -- it did not take a copy, a KV or a consumer" C20a

note F11 D03-R5 "slack after losing one region" \
     "one node -- 6 of 9 against a majority of 5; T3 in the same state has none"
note F12 D03-R1 "so T4 is no longer inferred" \
     "measured here: it survives a region plus one more node, and no further"

banner "$TOPOLOGY -- done"
