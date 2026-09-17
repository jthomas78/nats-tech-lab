#!/usr/bin/env bash
# T2 / FIGURE A -- THE BASELINE. Two clusters, one gateway, no domain.
#
# This is the real demo 03 rig: za-1..3 and au-1..3 joined by one gateway, no
# `jetstream.domain` anywhere, accounts LB (shared), LB_ZA and LB_AU.
#
# The gateway does not just carry messages. It MERGES the two JetStream meta
# groups into one. Before it, two groups of 3 with majority 2. After it, one
# group of 6 with majority 4. Three survivors is fewer than four, so when a
# region goes dark the survivor cannot elect a meta leader and every JetStream
# MANAGEMENT call fails.
#
# But data keeps flowing. A WAN cut here is a CHANGE FREEZE, not an outage.
#
# The second half of the script asks the other question: inside ONE account,
# can both regions own a stream called ODOMETER? No. A supercluster is one
# JetStream namespace, so the far side is refused with 10058 -- and worse,
# `stream info` from the far side then SUCCEEDS and hands back a handle to the
# other region's stream. It looks local. It is not. Two accounts fix it.
#
# Requirements exercised: D03-R1, D03-R2, D03-R4, D03-R5.

source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

TOPOLOGY="T2 / A -- gateway"
SUBJECT="evt.odo.v1"
ZA_HTTP=(8231 8232 8233); AU_HTTP=(8241 8242 8243)

build() {
  local i
  for i in 1 2 3; do
    conf_head "za-$i" "423$i" "823$i"
    conf_cluster "za-$i" za "623$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '623%s ' $j; done)
    conf_gateway "za-$i" za "723$i" au 7241 7242 7243
    conf_accounts "za-$i"

    conf_head "au-$i" "424$i" "824$i"
    conf_cluster "au-$i" au "624$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '624%s ' $j; done)
    conf_gateway "au-$i" au "724$i" za 7231 7232 7233
    conf_accounts "au-$i"
  done
}

lab_init
banner "$TOPOLOGY"

build
for i in 1 2 3; do start_server "za-$i"; start_server "au-$i"; done
for h in "${ZA_HTTP[@]}" "${AU_HTTP[@]}"; do wait_ready "$h"; done
wait_meta_leader 8231 || true
sleep 3

# --- The meta group ---------------------------------------------------------
# One gateway, one meta group. Every monitor port must name the SAME leader.
check A1 D03-R5 "JetStream meta groups across both regions" \
      "1" "$(meta_group_count "${ZA_HTTP[@]}" "${AU_HTTP[@]}")"
check A2 D03-R5 "meta group size (majority is 4)" \
      "6" "$(meta_size 8231)"

# --- One shared account, two regions ---------------------------------------
nats_as 4231 lb stream add ODOMETER --subjects "$SUBJECT" --storage file \
        --replicas 3 --cluster za --defaults >/dev/null
check A3 D03-R2 "shared account LB: ODOMETER placed in" \
      "za" "$(stream_cluster 4231 lb ODOMETER)"
check A4 D03-R2 "shared account LB: AU asks for ODOMETER too" \
      "10058" "$(err_code 4241 lb stream add ODOMETER --subjects "$SUBJECT" \
                 --storage file --replicas 3 --cluster au --defaults)"
# The trap: the refusal is not the dangerous part. This is.
check A5 D03-R2 "shared account LB: AU's 'stream info ODOMETER' reports cluster" \
      "za" "$(stream_cluster 4241 lb ODOMETER)"
nats_as 4231 lb stream rm ODOMETER -f >/dev/null 2>&1 || true

# --- One account per region -------------------------------------------------
nats_as 4231 za stream add ODOMETER --subjects "$SUBJECT" --storage file \
        --replicas 3 --cluster za --defaults >/dev/null
nats_as 4241 au stream add ODOMETER --subjects "$SUBJECT" --storage file \
        --replicas 3 --cluster au --defaults >/dev/null
check A6 D03-R2 "LB_ZA ODOMETER lands in" "za" "$(stream_cluster 4231 za ODOMETER)"
check A7 D03-R2 "LB_AU ODOMETER lands in" "au" "$(stream_cluster 4241 au ODOMETER)"

# Independent message counts prove these are two streams, not one seen twice.
# The `created` timestamp does NOT prove it -- two different streams have been
# measured sharing one to the second.
nats_as 4231 za pub "$SUBJECT" '{"km":12.5}' >/dev/null 2>&1
sleep 1
check A8 D03-R2 "one publish in ZA: messages in LB_ZA / LB_AU" \
      "1 / 0" "$(stream_msgs 4231 za ODOMETER) / $(stream_msgs 4241 au ODOMETER)"

# --- Where does a NEW thing land? ------------------------------------------
# No placement flag. Placement follows the CLIENT.
nats_as 4241 au kv add t7-vehicles --storage file --replicas 3 >/dev/null 2>&1
check A9 D03-R4 "KV t7-vehicles created from AU, no placement flag, lands in" \
      "au" "$(stream_cluster 4241 au KV_t7-vehicles)"

# --- The region goes dark ---------------------------------------------------
# kill -STOP on the processes. Never a Docker network disconnect.
freeze za-1 za-2 za-3
wait_dark "${ZA_HTTP[@]}"

# `/jsz` lies for a while here -- see the trap note in _common.sh. Measure how
# long it lies, then read it. Reading it immediately says "healthy".
STALE_FOR="$(seconds_until_no_leader 8241 120)"
note A10a D03-R5 "seconds /jsz still named a meta leader after ZA stopped answering" \
      "${STALE_FOR}s"
check A10 D03-R5 "ZA dark: meta leader seen from AU (3 of 6 is below 4)" \
      "NONE" "$(meta_leader 8241)"

# The refusal arrives as a client timeout here, not as 10008. Both mean the
# same thing -- management is frozen -- so the check is "it failed" and the
# exact mode is recorded next to it.
check A11 D03-R1 "ZA dark: AU tries to create a NEW stream" \
      "fails" "$(fails_or_ok 4241 au stream add T_NEW --subjects "evt.new.v1" \
                 --storage file --replicas 3 --cluster au --defaults)"
note A11a D03-R1 "how the refusal arrived" \
      "$(err_code 4241 au stream add T_NEW --subjects "evt.new.v1" \
         --storage file --replicas 3 --cluster au --defaults)"
# The half that still works. This is why a WAN cut is a freeze, not an outage.
nats_as 4241 au pub "$SUBJECT" '{"km":3.0}' >/dev/null 2>&1 || true
sleep 1
check A12 D03-R1 "ZA dark: publish to AU's EXISTING stream, messages now" \
      "1" "$(stream_msgs 4241 au ODOMETER)"

# --- Recovery ---------------------------------------------------------------
thaw za-1 za-2 za-3
wait_meta_leader 8231 || true
sleep 3
check A13 D03-R5 "after recovery: meta group size" "6" "$(meta_size 8231)"
check A14 D03-R5 "after recovery: nothing lost, LB_ZA messages" \
      "1" "$(stream_msgs 4231 za ODOMETER)"

banner "$TOPOLOGY -- done"
