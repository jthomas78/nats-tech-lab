#!/usr/bin/env bash
# T6 / FIGURE G -- BOTH LINKS AT ONCE. A gateway AND a hub leaf link.
#
# Every other script in this folder gives the two regions ONE link. T2 joins
# them with a gateway, which MERGES their JetStream meta groups into one group
# of six. T5 gives each region a leaf link up to a hub instead, which does NOT
# merge anything -- three systems, three votes, and the same stream name may
# live in all three at once.
#
# So a reasonable person asks the obvious question: run BOTH. Keep the gateway
# for the shared namespace, add the hub leaf link for independence, and get the
# good half of each. This script is the measurement of that idea, and it is the
# first time the lab has built the shape.
#
# The script builds it TWICE, because a domain is the whole argument for the
# leaf link and the gateway is the whole argument against it:
#
#   phase 1  no `jetstream.domain` anywhere      (9 servers)
#   phase 2  the SAME wiring plus a domain per site: hub / za / au
#
# Phase 2 is a deliberate re-run of Figure B's question on a shape Figure B
# never had: the extra leaf link is exactly the thing a domain is allowed to
# cross, so the hub link is the strongest excuse anybody has ever had for
# per-region domains over a gateway. Its checks copy 02-domain-over-gateway.sh
# and, like that script, NEVER name a side -- which cluster goes blind is a
# start-up race, so only the SHAPE is checked and the winner is a note.
#
# Everything the hub link buys in T5 is measured again here, against T2's
# baseline, one check at a time: the meta group count (D1 vs G1), the second
# ODOMETER in the shared account (A4 / D3 vs G4), the double capture (D5 vs
# G6/G7), and what survives a dark region (A10/A11 vs G11/G12).
#
# Requirements exercised: D03-R1, D03-R2, D03-R3, D03-R6, D03-R10.

source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

TOPOLOGY="T6 / G -- gateway AND hub leaf"
SUBJECT="evt.odo.v1"
HUB_HTTP=(8551 8552 8553); ZA_HTTP=(8231 8232 8233); AU_HTTP=(8241 8242 8243)

hub_listen() {                # $1 name  $2 leaf listen port
  printf 'leafnodes { listen: 127.0.0.1:%s }\n' "$2" >> "$RUN_DIR/t-$1.conf"
}

# Copied from 04-hub-and-leaf.sh on purpose: every region server lists ALL
# THREE hub leaf ports, so a hub-node failure self-heals.
leaf_remotes() {              # $1 name
  cat >> "$RUN_DIR/t-$1.conf" <<'EOF'
leafnodes {
  remotes: [
    { urls: [ "nats-leaf://lb:lb@127.0.0.1:7560",
              "nats-leaf://lb:lb@127.0.0.1:7561",
              "nats-leaf://lb:lb@127.0.0.1:7562" ], account: LB }
  ]
}
EOF
}

# The whole topology, in one function. $1 is the domain mode:
#   ""        no domain anywhere      (phase 1)
#   "sites"   a domain per site       (phase 2)
# A region server carries a cluster block, a GATEWAY block to the other region
# and a LEAF remote up to the hub. That double link is the only thing this
# script adds to the rest of the lab.
build() {
  local mode="${1:-}" i j hd zd ad
  if [ "$mode" = "sites" ]; then hd=hub; zd=za; ad=au; else hd=""; zd=""; ad=""; fi
  for i in 1 2 3; do
    conf_head "hub-$i" "455$i" "855$i" "$hd"
    conf_cluster "hub-$i" hub "655$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '655%s ' $j; done)
    hub_listen "hub-$i" "756$((i-1))"
    conf_accounts "hub-$i"

    conf_head "za-$i" "423$i" "823$i" "$zd"
    conf_cluster "za-$i" za "623$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '623%s ' $j; done)
    conf_gateway "za-$i" za "723$i" au 7241 7242 7243
    leaf_remotes "za-$i"
    conf_accounts "za-$i"

    conf_head "au-$i" "424$i" "824$i" "$ad"
    conf_cluster "au-$i" au "624$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '624%s ' $j; done)
    conf_gateway "au-$i" au "724$i" za 7231 7232 7233
    leaf_remotes "au-$i"
    conf_accounts "au-$i"
  done
}

# Hub first, then both regions -- the leaf remotes need something to dial.
start_all() {
  for i in 1 2 3; do start_server "hub-$i"; done
  for h in "${HUB_HTTP[@]}"; do wait_ready "$h"; done
  for i in 1 2 3; do start_server "za-$i"; start_server "au-$i"; done
  for h in "${ZA_HTTP[@]}" "${AU_HTTP[@]}"; do wait_ready "$h"; done
}

lab_init
banner "$TOPOLOGY"

# ===========================================================================
# PHASE 1 -- both links, NO domain anywhere
# ===========================================================================
build ""
start_all
wait_meta_leader 8231 || true
wait_meta_leader 8551 || true
# Interest across a leaf link needs about 5 seconds to spread.
sleep 8

# --- How many JetStream systems are there? ---------------------------------
# T2 answered 1. T5 answered 3. Both links at once answers this.
check G1 D03-R10 "distinct JetStream meta groups across hub, ZA and AU" \
      "2" "$(meta_group_count "${HUB_HTTP[@]}" "${ZA_HTTP[@]}" "${AU_HTTP[@]}")" D1
check G2 D03-R10 "meta group sizes: hub / seen from ZA / seen from AU" \
      "3 / 6 / 6" "$(meta_size 8551) / $(meta_size 8231) / $(meta_size 8241)"
# The single number that says the gateway won: both regions name ONE leader.
check G3 D03-R10 "ZA and AU name the SAME meta leader" \
      "yes" "$([ "$(meta_leader 8231)" = "$(meta_leader 8241)" ] && echo yes || echo no)"

# --- The shared account LB, and the second ODOMETER ------------------------
# T5 lets AU have its own ODOMETER (D3 = ok, D4 = za / au). T2 refuses it
# (A4 = 10058, A5 = za). The leaf link is present here. It changes nothing.
nats_as 4231 lb stream add ODOMETER --subjects "$SUBJECT" --storage file \
        --replicas 3 --cluster za --defaults >/dev/null
check G4 D03-R2 "shared account LB: AU asks for ODOMETER on its own cluster" \
      "10058" "$(err_code 4241 lb stream add ODOMETER --subjects "$SUBJECT" \
                 --storage file --replicas 3 --cluster au --defaults)"
check G5 D03-R2 "shared account LB: AU's 'stream info ODOMETER' reports cluster" \
      "za" "$(stream_cluster 4241 lb ODOMETER)" A5

# --- Did the T5 double capture survive? ------------------------------------
# T5's D4 read `za / au` -- two real streams. If this reads `za / za` there is
# only ONE stream, seen twice, and the double capture is gone.
check G6 D03-R6 "Placement Cluster of the handle ZA holds / the handle AU holds" \
      "za / za" "$(stream_cluster 4231 lb ODOMETER) / $(stream_cluster 4241 lb ODOMETER)"
nats_as 4231 lb pub "$SUBJECT" '{"km":12.5}' >/dev/null 2>&1
sleep 6
check G7 D03-R6 "ONE publish in ZA: messages counted from ZA / from AU" \
      "1 / 1" "$(stream_msgs 4231 lb ODOMETER) / $(stream_msgs 4241 lb ODOMETER)"
note  G7a D03-R6 "so 1 / 1 here means the OPPOSITE of T5's D5" \
      "one stored copy read through two handles, not two stored copies"

# --- The hub is still its own JetStream system -----------------------------
# The leaf link did not die. It just cannot reach across the gateway's group.
nats_as 4551 lb stream add HUBONLY --subjects "evt.hub.v1" --storage file \
        --replicas 3 --defaults >/dev/null 2>&1 || true
check G8 D03-R10 "a stream created by a client on the HUB lands in" \
      "hub" "$(stream_cluster 4551 lb HUBONLY)"
check G9 D03-R10 "and ZA's view of that hub stream (? = it cannot see it)" \
      "?" "$(stream_cluster 4231 lb HUBONLY)"

# --- A whole region goes dark ----------------------------------------------
# T2's answer was: everything freezes, 3 of 6 is below the majority of 4.
# The hub link is present here, alive, and holds three healthy votes.
freeze au-1 au-2 au-3
wait_dark "${AU_HTTP[@]}"
note  G10a D03-R5 "seconds /jsz still named a meta leader after AU stopped answering" \
      "$(seconds_until_no_leader 8231 120)s"
check G10 D03-R10 "AU dark: meta leader seen from ZA (3 of 6 is below 4)" \
      "NONE" "$(meta_leader 8231)"
check G11 D03-R1 "AU dark: ZA tries to create a NEW stream" \
      "fails" "$(fails_or_ok 4231 lb stream add T_ZA_NEW --subjects "evt.zn.v1" \
                 --storage file --replicas 3 --cluster za --defaults)"
note  G11a D03-R1 "how the refusal arrived" \
      "$(err_code 4231 lb stream add T_ZA_NEW2 --subjects "evt.zn2.v1" \
         --storage file --replicas 3 --cluster za --defaults)"
# The half that still works, exactly as in T2: a WAN cut is a change freeze.
nats_as 4231 lb pub "$SUBJECT" '{"km":3.0}' >/dev/null 2>&1 || true
sleep 2
check G12 D03-R1 "AU dark: publish to ZA's EXISTING stream, messages now" \
      "2" "$(stream_msgs 4231 lb ODOMETER)"
# And the cruel part. The hub is perfectly healthy and cannot lend a vote.
check G13 D03-R10 "AU dark: the hub still has its own meta leader" \
      "yes" "$([ "$(meta_leader 8551)" = "NONE" ] && echo no || echo yes)"
check G14 D03-R10 "AU dark: a client on the HUB still creates a stream" \
      "ok" "$(fails_or_ok 4551 lb stream add HUBNEW --subjects "evt.hn.v1" \
              --storage file --replicas 3 --defaults)"
note  G14a D03-R10 "what G13 and G14 cost ZA" \
      "nothing -- a healthy hub cannot lend a vote across a gateway"

thaw au-1 au-2 au-3
wait_meta_leader 8231 || true
sleep 5
check G15 D03-R5 "after recovery: meta group size seen from ZA" \
      "6" "$(meta_size 8231)"

# --- The whole hub goes dark -----------------------------------------------
# T5 loses nothing here (D10-D14). Neither should this, for the same reason:
# the regions' votes were never in the hub's group to begin with.
kill_server hub-1 hub-2 hub-3
sleep 5
check G16 D03-R10 "whole hub gone: meta group size seen from ZA" \
      "6" "$(meta_size 8231)"
check G17 D03-R10 "whole hub gone: ZA creates a NEW 3-replica stream" \
      "ok" "$(fails_or_ok 4231 lb stream add T_NOHUB_ZA --subjects "evt.nz.v1" \
              --storage file --replicas 3 --cluster za --defaults)" D12
check G18 D03-R10 "whole hub gone: AU creates a NEW 3-replica stream" \
      "ok" "$(fails_or_ok 4241 lb stream add T_NOHUB_AU --subjects "evt.na.v1" \
              --storage file --replicas 3 --cluster au --defaults)" D13

# ===========================================================================
# PHASE 2 -- the SAME wiring, plus a domain per site
#
# A domain may only change across a leaf link, and this shape HAS a leaf link.
# That is the whole argument for trying it. The gateway is still there too.
# ===========================================================================
lab_down_quiet
rm -f "$RUN_DIR"/t-*.conf
rm -rf "$RUN_DIR"/{log,pid,js}
mkdir -p "$RUN_DIR"/{log,pid,js}

build sites
start_all
# Either side may win the race; wait for one, then give the other a fair
# chance it will not take. Copied from 02-domain-over-gateway.sh.
for _ in $(seq 1 20); do
  [ "$(meta_leader 8231)" != "NONE" ] && break
  [ "$(meta_leader 8241)" != "NONE" ] && break
  sleep 1
done
sleep 20

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

check G19 D03-R3 "a domain per site, nothing stopped: sides that elect a meta leader (of 2)" \
      "1" "$live_n"
note  G19a D03-R3 "which side won the race this run -- still a coin toss" \
      "$live_side won; $blind_side is blind"
check G20 D03-R3 "the losing side has a meta leader" \
      "no" "$([ "$(meta_leader "$blind_mon")" = "NONE" ] && echo no || echo yes)" B2
check G21 D03-R3 "meta group size seen from the live side -- domains did NOT split it" \
      "6" "$(meta_size "$live_mon")" B3
check G22 D03-R3 "placing a stream on the BLIND cluster, from a live client" \
      "10005" "$(err_code "$live_cli" lb stream add ODOMETER_X --subjects "evt.x.v1" \
                 --storage file --replicas 3 --cluster "$blind_side" --defaults)" B4
check G23 D03-R3 "the hub, which IS across a leaf link, still elects its own leader" \
      "yes" "$([ "$(meta_leader 8551)" = "NONE" ] && echo no || echo yes)"
note  G24 D03-R10 "verdict" \
      "the gateway decides -- adding a hub leaf link changes nothing it does"

banner "$TOPOLOGY -- done"
