#!/usr/bin/env bash
# T5 / FIGURE D -- HUB AND LEAF. The strongest isolation measured.
#
# A domain name may only change across a LEAF-NODE link. So this is the only
# shape in the demo where per-region domains are legal -- and here they do
# exactly what people wrongly expect them to do over a gateway.
#
# Three JetStream systems, not one: hub, za, au. Three meta groups of three,
# quorum two each. Lose a region and the other side never notices, because
# there was never a shared RAFT group to lose.
#
# It also produces the REAL double capture. Neither region can see the other's
# streams, so neither can refuse the overlap, and one publish is genuinely
# stored twice. Over a gateway that is impossible. (Demo 02 once claimed it was
# possible over a gateway. That page is superseded -- see CLAUDE.md.)
#
# The price is bookkeeping. NOTHING replicates by itself. Every cross-region
# copy is a mirror or source that somebody writes down and maintains -- and a
# cross-domain mirror that forgets `external.api` sits at zero messages
# forever, WITH NO ERROR. This script measures that silence.
#
# Requirements exercised: D03-R1, D03-R2, D03-R3, D03-R5, D03-R6, D03-R7.

source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

TOPOLOGY="T5 / D -- hub and leaf"
SUBJECT="evt.odo.v1"
HUB_HTTP=(8551 8552 8553); ZA_HTTP=(8231 8232 8233); AU_HTTP=(8241 8242 8243)

hub_listen() {                # $1 name  $2 leaf listen port
  printf 'leafnodes { listen: 127.0.0.1:%s }\n' "$2" >> "$RUN_DIR/t-$1.conf"
}

# EVERY region server lists ALL THREE hub leaf ports. That is what makes a
# hub-node failure self-heal: orphaned links move to another hub node by
# themselves. One URL makes one hub node a single point of failure.
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

build() {
  local i
  for i in 1 2 3; do
    conf_head "hub-$i" "455$i" "855$i" hub
    conf_cluster "hub-$i" hub "655$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '655%s ' $j; done)
    hub_listen "hub-$i" "756$((i-1))"
    conf_accounts "hub-$i"

    conf_head "za-$i" "423$i" "823$i" za
    conf_cluster "za-$i" za "623$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '623%s ' $j; done)
    leaf_remotes "za-$i"
    conf_accounts "za-$i"

    conf_head "au-$i" "424$i" "824$i" au
    conf_cluster "au-$i" au "624$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '624%s ' $j; done)
    leaf_remotes "au-$i"
    conf_accounts "au-$i"
  done
}

lab_init
banner "$TOPOLOGY"

build
for i in 1 2 3; do start_server "hub-$i"; done
for h in "${HUB_HTTP[@]}"; do wait_ready "$h"; done
for i in 1 2 3; do start_server "za-$i"; start_server "au-$i"; done
for h in "${ZA_HTTP[@]}" "${AU_HTTP[@]}"; do wait_ready "$h"; done
wait_meta_leader 8231 || true
wait_meta_leader 8241 || true
# Interest across a leaf link needs about 5 seconds to spread. A 2-second wait
# once read `0 received` and nearly got written up as a failure.
sleep 8

# --- Three JetStream systems ------------------------------------------------
check D1 D03-R3 "distinct JetStream meta groups across hub, ZA and AU" \
      "3" "$(meta_group_count "${HUB_HTTP[@]}" "${ZA_HTTP[@]}" "${AU_HTTP[@]}")"
check D2 D03-R5 "meta group sizes: hub / ZA / AU (quorum 2 each)" \
      "3 / 3 / 3" "$(meta_size 8551) / $(meta_size 8231) / $(meta_size 8241)"

# --- The same SHARED account holds ODOMETER twice --------------------------
nats_as 4231 lb stream add ODOMETER --subjects "$SUBJECT" --storage file \
        --replicas 3 --defaults >/dev/null
check D3 D03-R3 "shared account LB: AU also asks for ODOMETER (no 10058 here)" \
      "ok" "$(fails_or_ok 4241 lb stream add ODOMETER --subjects "$SUBJECT" \
              --storage file --replicas 3 --defaults)"
check D4 D03-R3 "and they are two real streams, in" \
      "za / au" "$(stream_cluster 4231 lb ODOMETER) / $(stream_cluster 4241 lb ODOMETER)"

# --- THE REAL DOUBLE CAPTURE -----------------------------------------------
# One publish. Both filters match it. Neither system can see the other, so
# neither can refuse it. Two stored copies from one send.
nats_as 4231 lb pub "$SUBJECT" '{"km":12.5}' >/dev/null 2>&1
sleep 6
check D5 D03-R6 "one publish in region ZA, shared account LB: messages STORED in region ZA / region AU" \
      "1 / 1" "$(stream_msgs 4231 lb ODOMETER) / $(stream_msgs 4241 lb ODOMETER)"
note  D5a D03-R6 "the same 1 / 1 as A26, C13 and F13 -- and the opposite meaning" \
      "over a gateway 1 / 1 is ONE stored copy read twice; here it is TWO stored copies of one send"

# --- A cross-domain mirror that forgets external.api ------------------------
# This is the trap, and it has TWO faces. Without `external.api` a mirror looks
# only inside its OWN domain. So it either finds nothing and sits at zero
# forever with no error -- or, worse, it finds a LOCAL stream that happens to
# share the name and silently mirrors the wrong data.
#
# Both are measured here, with deliberately different message counts, so the
# number alone says which stream was really copied.
#
#   AU_ONLY   exists only in AU        2 messages
#   SHADOW    exists in BOTH systems   ZA has 1, AU has 2

nats_as 4241 lb stream add AU_ONLY --subjects "evt.auonly.v1" --storage file \
        --replicas 3 --defaults >/dev/null
nats_as 4241 lb pub "evt.auonly.v1" 'a' >/dev/null 2>&1
nats_as 4241 lb pub "evt.auonly.v1" 'b' >/dev/null 2>&1

nats_as 4231 lb stream add SHADOW --subjects "evt.shadow.v1" --storage file \
        --replicas 3 --defaults >/dev/null
nats_as 4241 lb stream add SHADOW --subjects "evt.aushadow.v1" --storage file \
        --replicas 3 --defaults >/dev/null
nats_as 4231 lb pub "evt.shadow.v1" 'za-1' >/dev/null 2>&1
nats_as 4241 lb pub "evt.aushadow.v1" 'au-1' >/dev/null 2>&1
nats_as 4241 lb pub "evt.aushadow.v1" 'au-2' >/dev/null 2>&1
sleep 4
check D6 D03-R3 "two same-named SHADOW streams live at once: ZA has / AU has" \
      "1 / 2" "$(stream_msgs 4231 lb SHADOW) / $(stream_msgs 4241 lb SHADOW)"

mirror_json() {   # $1 mirror stream name  $2 source name  $3 external api or ""
  if [ -n "$3" ]; then
    printf '{ "name": "%s", "num_replicas": 3, "mirror": { "name": "%s", "external": { "api": "%s" } } }\n' "$1" "$2" "$3"
  else
    printf '{ "name": "%s", "num_replicas": 3, "mirror": { "name": "%s" } }\n' "$1" "$2"
  fi > "$RUN_DIR/$1.json"
  nats_as 4231 lb stream add --config "$RUN_DIR/$1.json" >/dev/null 2>&1 || true
}

mirror_json M_AUONLY_BROKEN AU_ONLY ""
mirror_json M_AUONLY_OK     AU_ONLY '$JS.au.API'
mirror_json M_SHADOW_BROKEN SHADOW  ""
sleep 10

# Face one: nothing to find at home, so it finds nothing. Forever. Silently.
check D7 D03-R7 "mirror of AU_ONLY WITHOUT external.api -- messages copied" \
      "0" "$(stream_msgs 4231 lb M_AUONLY_BROKEN)"
note  D7a D03-R7 "how that failure announced itself" \
      "it did not -- no error, no warning, and the stream reports healthy"

# Face two: the same mirror, told where to look, works.
check D8 D03-R7 "the same mirror WITH external.api \$JS.au.API -- messages copied" \
      "2" "$(stream_msgs 4231 lb M_AUONLY_OK)"

# Face three, and the nastier one. A local stream shares the name, so the
# mirror silently copies the WRONG data. 1 means it took ZA's local SHADOW;
# 2 would have meant it reached AU. It took the local one.
check D9 D03-R7 "mirror of SHADOW WITHOUT external.api -- messages copied" \
      "1" "$(stream_msgs 4231 lb M_SHADOW_BROKEN)"
note  D9a D03-R7 "what that means" \
      "a name-only mirror silently copied the LOCAL stream, not the remote one"

# --- THE TWO ACCOUNTS NOTHING BINDS ----------------------------------------
# LB_ZA and LB_AU are switched on, with JetStream, on all nine servers. They
# are in every topology's accounts block, held still on purpose.
#
# But `leaf_remotes` above binds ONE account: LB. So in this shape the two
# per-region accounts cross no link at all. They are not two islands -- they
# are SIX, one per account per site, and nobody had ever measured them here.
#
# A6, A7, A8, A22 and A24 asked these same questions over a gateway. The
# answers must change in exactly one place, and D18 is that place.
nats_as 4231 za stream add ODOMETER --subjects "$SUBJECT" --storage file \
        --replicas 3 --defaults >/dev/null 2>&1 || true
nats_as 4241 au stream add ODOMETER --subjects "$SUBJECT" --storage file \
        --replicas 3 --defaults >/dev/null 2>&1 || true
sleep 3
check D16 D03-R2 "per-region account LB_ZA: its ODOMETER lands in" \
      "za" "$(stream_cluster 4231 za ODOMETER)" A6
check D17 D03-R2 "per-region account LB_AU: its ODOMETER lands in" \
      "au" "$(stream_cluster 4241 au ODOMETER)" A7

# Over a gateway A8 read 1 / 0 because the account wall stopped it. Here the
# wall is not even needed -- there is no link to stop. Same number, and the
# contrast with D5's 1 / 1 in the SHARED account is the whole point of the row.
nats_as 4231 za pub "$SUBJECT" '{"km":12.5}' >/dev/null 2>&1
sleep 3
check D18 D03-R6 "one publish in LB_ZA: messages in LB_ZA / LB_AU" \
      "1 / 0" "$(stream_msgs 4231 za ODOMETER) / $(stream_msgs 4241 au ODOMETER)" A8
note  D18a D03-R6 "why this is 1 / 0 where D5 was 1 / 1" \
      "D5 shares ONE account across the leaf link -- these two share nothing"

check D19 D03-R5 "LB_ZA ODOMETER: replicas asked for / peers built" \
      "3 / 3" "$(stream_replicas 4231 za ODOMETER) / $(stream_peers 4231 za ODOMETER)" A22
check D20 D03-R5 "LB_ZA / LB_AU ODOMETER: region holding the stream leader" \
      "za / au" "$(stream_leader_region 4231 za ODOMETER) / $(stream_leader_region 4241 au ODOMETER)" \
      A24

# And the hub has its own LB_ZA, in a third domain, bound to nothing at all.
check D21 D03-R3 "the HUB's own LB_ZA asks for stream info on ZA's ODOMETER" \
      "fails" "$(fails_or_ok 4551 za stream info ODOMETER --json)"
check D22 D03-R3 "so the HUB's LB_ZA may create a THIRD ODOMETER, in" \
      "hub" "$(nats_as 4551 za stream add ODOMETER --subjects "$SUBJECT" \
               --storage file --replicas 3 --defaults >/dev/null 2>&1; \
               stream_cluster 4551 za ODOMETER)"
note  D22a D03-R3 "so the leaf link binds ONE account" \
      "LB reaches the hub; LB_ZA and LB_AU are six separate islands, one per site"

# --- Kill the ENTIRE hub ----------------------------------------------------
# Not one node. All three. Both regions must keep full JetStream.
kill_server hub-1 hub-2 hub-3
sleep 5
check D10 D03-R1 "whole hub gone: ZA still has a meta leader" \
      "yes" "$([ "$(meta_leader 8231)" = "NONE" ] && echo no || echo yes)"
check D11 D03-R1 "whole hub gone: AU still has a meta leader" \
      "yes" "$([ "$(meta_leader 8241)" = "NONE" ] && echo no || echo yes)"
check D12 D03-R1 "whole hub gone: ZA creates a NEW 3-replica stream" \
      "ok" "$(fails_or_ok 4231 lb stream add T_ZA_NEW --subjects "evt.zn.v1" \
              --storage file --replicas 3 --defaults)"
check D13 D03-R1 "whole hub gone: AU creates a NEW 3-replica stream" \
      "ok" "$(fails_or_ok 4241 lb stream add T_AU_NEW --subjects "evt.an.v1" \
              --storage file --replicas 3 --defaults)"
check D14 D03-R1 "whole hub gone: AU still stores its own publishes" \
      "ok" "$(fails_or_ok 4241 lb pub "$SUBJECT" '{"km":9}')"

note D15 D03-R6 "the cost" \
     "nothing replicates by itself -- every cross-region copy is hand-written"

banner "$TOPOLOGY -- done"
