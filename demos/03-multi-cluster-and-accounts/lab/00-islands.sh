#!/usr/bin/env bash
# T1 -- TWO ISLANDS. One cluster per region, and NOTHING between them.
#
# This is the control. It is the same six config files as T2 with the
# `gateway {}` block deleted, and it is the sharp contrast that makes the
# gateway's cost visible:
#
#   T2 (gateway): one meta group of 6, majority 4. Lose a region -> FROZEN.
#   T1 (nothing): two meta groups of 3, quorum 2 each. Lose a region -> the
#                 other side never notices.
#
# T1 also shows that the 10058 refusal is not a JetStream rule, it is a
# SUPERCLUSTER rule. With no link there is no shared namespace, so the same
# shared account LB can hold an ODOMETER in BOTH regions with no complaint.
#
# The price is total isolation. A publish cannot reach the other region at all,
# and nothing replicates by itself.
#
# Requirements exercised: D03-R1, D03-R2, D03-R5, D03-R6.

source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

TOPOLOGY="T1 -- no link"
SUBJECT="evt.odo.v1"
ZA_HTTP=(8231 8232 8233); AU_HTTP=(8241 8242 8243)

build() {
  local i
  for i in 1 2 3; do
    conf_head "za-$i" "423$i" "823$i"
    conf_cluster "za-$i" za "623$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '623%s ' $j; done)
    conf_accounts "za-$i"                       # <- no conf_gateway. That is T1.

    conf_head "au-$i" "424$i" "824$i"
    conf_cluster "au-$i" au "624$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '624%s ' $j; done)
    conf_accounts "au-$i"
  done
}

lab_init
banner "$TOPOLOGY"

build
for i in 1 2 3; do start_server "za-$i"; start_server "au-$i"; done
for h in "${ZA_HTTP[@]}" "${AU_HTTP[@]}"; do wait_ready "$h"; done
wait_meta_leader 8231 || true
wait_meta_leader 8241 || true
sleep 2

# --- Two JetStream systems, not one ----------------------------------------
check T1a D03-R5 "distinct JetStream meta groups across both regions" \
      "2" "$(meta_group_count "${ZA_HTTP[@]}" "${AU_HTTP[@]}")"
check T1b D03-R5 "ZA meta group size (quorum 2)" "3" "$(meta_size 8231)"
check T1c D03-R5 "AU meta group size (quorum 2)" "3" "$(meta_size 8241)"

# --- The same SHARED account can hold the same stream name twice ------------
# Over a gateway this is refused with 10058. With no link there is nothing to
# refuse it, because there is no shared namespace to collide in.
nats_as 4231 lb stream add ODOMETER --subjects "$SUBJECT" --storage file \
        --replicas 3 --defaults >/dev/null
check T1d D03-R2 "shared account LB: AU also asks for ODOMETER" \
      "ok" "$(fails_or_ok 4241 lb stream add ODOMETER --subjects "$SUBJECT" \
              --storage file --replicas 3 --defaults)"
check T1e D03-R2 "and they are two real streams, in" \
      "za / au" "$(stream_cluster 4231 lb ODOMETER) / $(stream_cluster 4241 lb ODOMETER)"

# --- A publish cannot cross. That is the price. -----------------------------
nats_as 4231 lb pub "$SUBJECT" '{"km":12.5}' >/dev/null 2>&1
sleep 2
check T1f D03-R6 "one publish in region ZA, shared account LB: messages in region ZA / region AU" \
      "1 / 0" "$(stream_msgs 4231 lb ODOMETER) / $(stream_msgs 4241 lb ODOMETER)"

# --- The region goes dark, and the other side never notices -----------------
freeze za-1 za-2 za-3
wait_dark "${ZA_HTTP[@]}"
sleep 3

check T1g D03-R1 "ZA dark: AU still has a meta leader" \
      "yes" "$([ "$(meta_leader 8241)" = "NONE" ] && echo no || echo yes)"
check T1h D03-R1 "ZA dark: AU creates a NEW 3-replica stream" \
      "ok" "$(fails_or_ok 4241 lb stream add T_NEW --subjects "evt.new.v1" \
              --storage file --replicas 3 --defaults)"
check T1i D03-R5 "ZA dark: AU meta group size, unchanged" "3" "$(meta_size 8241)"

thaw za-1 za-2 za-3
wait_meta_leader 8231 || true
sleep 3

# --- THE STREAM'S OWN RAFT GROUP -------------------------------------------
# The checks above ask WHERE a stream is. These ask WHAT it is made of. A
# stream is its own RAFT group, separate from the meta group, with its own
# peers and its own leader.
#
# Two numbers, not one. `num_replicas` is what was ASKED for. The peer count
# is what the cluster actually built. They agree here because each region has
# three servers, which is exactly what --replicas 3 needs.
check T1j D03-R5 "ZA ODOMETER: replicas asked for / peers built" \
      "3 / 3" "$(stream_replicas 4231 lb ODOMETER) / $(stream_peers 4231 lb ODOMETER)"
check T1k D03-R5 "AU ODOMETER: replicas asked for / peers built" \
      "3 / 3" "$(stream_replicas 4241 lb ODOMETER) / $(stream_peers 4241 lb ODOMETER)"

# WHICH of the three servers leads is a coin toss, so it cannot be a check.
# Which REGION leads is not a coin toss at all -- a stream's leader is always
# one of its own peers, so it can never sit in the other region.
check T1l D03-R5 "ZA / AU ODOMETER: region holding the stream leader" \
      "za / au" "$(stream_leader_region 4231 lb ODOMETER) / $(stream_leader_region 4241 lb ODOMETER)"
note  T1m D03-R5 "which server won the ZA stream election this run" \
      "$(stream_leader 4231 lb ODOMETER)"

banner "$TOPOLOGY -- done"
