#!/usr/bin/env bash
# FIGURE B -- THE POPULAR ADVICE THAT DOES NOT WORK.
#
# The idea is sound: stop sharing a meta group, give each region its own
# JetStream domain, and each side then votes alone. The METHOD is wrong.
#
# A JetStream domain names a JetStream SYSTEM, and the name may only change
# across a LEAF-NODE link. A gateway is not one. Setting a domain per cluster
# over a gateway does two things at once, both bad:
#
#   1. it suppresses JetStream traffic on the system account, so the clusters
#      stop learning each other's server names -- the link goes half-blind;
#   2. it does NOT split the RAFT group, because the gateway still binds all
#      six servers into one meta group.
#
# You end up with a shared meta group AND a broken link. ONE of the two
# clusters never elects a leader -- with nothing stopped, on a perfectly
# healthy machine -- and placement onto that cluster fails with `no suitable
# peers for placement (10005)`.
#
# WHICH cluster loses is a COIN TOSS. Measured 2026-09-17: demo 02 recorded au
# as the blind side, this script's first run agreed, and a run minutes later
# with the same configs and nothing changed left ZA blind instead. It is a
# start-up race, not a property of a region. So nothing below names a side. It
# asks only that exactly one of the two elects, and records the winner as a
# note.
#
# This is the second deliberate reproduction. Demo 02 hit it by accident in
# September 2026. Do not reintroduce a per-region domain.
#
# Requirements exercised: D03-R3, D03-R5, D03-R6.

source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

TOPOLOGY="B -- gateway + per-cluster domain"
SUBJECT="evt.odo.v1"

build() {
  local i
  for i in 1 2 3; do
    # The ONLY difference from 01-gateway.sh is the 4th argument: a domain.
    conf_head "za-$i" "423$i" "823$i" za
    conf_cluster "za-$i" za "623$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '623%s ' $j; done)
    conf_gateway "za-$i" za "723$i" au 7241 7242 7243
    conf_accounts "za-$i"

    conf_head "au-$i" "424$i" "824$i" au
    conf_cluster "au-$i" au "624$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '624%s ' $j; done)
    conf_gateway "au-$i" au "724$i" za 7231 7232 7233
    conf_accounts "au-$i"
  done
}

lab_init
banner "$TOPOLOGY"

build
for i in 1 2 3; do start_server "za-$i"; start_server "au-$i"; done
for h in 8231 8232 8233 8241 8242 8243; do wait_ready "$h"; done
# Either side may win the race. Wait for one of them to elect.
for _ in $(seq 1 20); do
  [ "$(meta_leader 8231)" != "NONE" ] && break
  [ "$(meta_leader 8241)" != "NONE" ] && break
  sleep 1
done
# Give the OTHER side a long, fair chance to elect too. It will not take it.
sleep 20

# Which side won is NOT fixed. Measured 2026-09-17: ZA won on one run and AU
# won on the next, same configs, nothing changed. So the checks below must
# never name a side -- they ask about the SHAPE. The side that won is recorded
# separately, as a note, because it is a different answer every time.
za_leader="$(meta_leader 8231)"
au_leader="$(meta_leader 8241)"

live_n=0
[ "$za_leader" != "NONE" ] && live_n=$((live_n+1))
[ "$au_leader" != "NONE" ] && live_n=$((live_n+1))

if [ "$za_leader" != "NONE" ]; then
  live_side=za; live_mon=8231; live_cli=4231; blind_side=au; blind_mon=8241
else
  live_side=au; live_mon=8241; live_cli=4241; blind_side=za; blind_mon=8231
fi

# --- Nothing is stopped. Nothing is broken. Look at both sides anyway. -----
check B1 D03-R3 "sides that elect a meta leader, with NOTHING stopped (of 2)" \
      "1" "$live_n"
note  B1a D03-R3 "which side won the race this run -- and it is a coin toss" \
      "$live_side won; $blind_side is blind"
check B2 D03-R3 "the losing side has a meta leader" \
      "no" "$([ "$(meta_leader "$blind_mon")" = "NONE" ] && echo no || echo yes)"

# The domains did not split the RAFT group. The gateway still binds all six.
check B3 D03-R3 "meta group size seen from the live side -- domains did NOT split it" \
      "6" "$(meta_size "$live_mon")"

# --- And placement into the half-blind side fails --------------------------
check B4 D03-R3 "placing a stream on the BLIND cluster, from a live client" \
      "10005" "$(err_code "$live_cli" lb stream add ODOMETER_X --subjects "evt.x.v1" \
                 --storage file --replicas 3 --cluster "$blind_side" --defaults)"
check B5 D03-R3 "placing a stream on the LIVE cluster still works" \
      "ok" "$(fails_or_ok "$live_cli" lb stream add ODOMETER --subjects "$SUBJECT" \
              --storage file --replicas 3 --cluster "$live_side" --defaults)"

note B6 D03-R3 "verdict" \
     "does not work -- a domain name cannot split a supercluster"

banner "$TOPOLOGY -- done"
