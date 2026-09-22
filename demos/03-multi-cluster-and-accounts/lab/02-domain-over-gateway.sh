#!/usr/bin/env bash
# T2 / B -- THE POPULAR ADVICE THAT DOES NOT WORK.
#
# Same six servers and same gateway as T2 / A. The only thing that changes is
# a `jetstream.domain` per cluster, which is why this is a T2 variant and not
# a topology of its own.
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
# Requirements exercised: D03-R2, D03-R3, D03-R4, D03-R5, D03-R6, D03-R7.

source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

TOPOLOGY="T2 / B -- gateway + per-cluster domain"
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
  live_side=za; live_mon=8231; live_cli=4231; blind_side=au; blind_mon=8241; blind_cli=4241
else
  live_side=au; live_mon=8241; live_cli=4241; blind_side=za; blind_mon=8231; blind_cli=4231
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

# --- THE SHARED ACCOUNT, ASKED FROM BOTH SIDES -----------------------------
# A26 asks this of a healthy gateway and gets a clean 1 / 1. Here one side is
# already blind with nothing stopped, so only the LIVE half has a predictable
# answer. The blind half is recorded as a note, because the rig cannot say in
# advance which region is blind.
nats_as "$live_cli" lb pub "$SUBJECT" '{"km":9.0}' >/dev/null 2>&1 || true
sleep 2
check B7 D03-R3 "one publish on the LIVE side, shared account LB: messages seen from the LIVE side" \
      "1" "$(stream_msgs "$live_cli" lb ODOMETER)"
note  B7a D03-R3 "the same count asked of the BLIND side ($blind_side)" \
      "$(stream_msgs "$blind_cli" lb ODOMETER)"

# --- THE SAME QUESTIONS T2 / A ANSWERED, ASKED AGAIN ON THIS RIG ------------
# Everything below carries the id of the T2 / A check it repeats. Same six
# servers, same gateway, same slice -- the ONLY difference is the domain per
# cluster. So any answer that changes was changed by the domain, and the
# report can print the pair side by side.
#
# Nothing here names a region. The live side is whichever one won the race.

# A4 -- is the stream NAMESPACE still shared? B3 shows the meta GROUP is still
# one group of six. That is not the same claim. This one asks whether the name
# ODOMETER is still taken.
#
# The second ask must carry a DIFFERENT configuration, or the server treats it
# as a repeat of the first and answers ok. A4 gets its difference for free, by
# asking from the far region with `--cluster au`. Here the far region is blind,
# so the difference is the subject instead.
check B8 D03-R3 "shared account LB: the SAME stream name asked for with a different config" \
      "10058" "$(err_code "$live_cli" lb stream add ODOMETER --subjects "evt.other.v1" \
                 --storage file --replicas 3 --cluster "$live_side" --defaults)" \
      A4
# And aimed at the blind half. A4 gets 10058 because the name is checked before
# anything else. B4 gets 10005 because placement into the blind half fails.
# This asks both at once, and which one answers first is not knowable in
# advance, so it is a note.
note B8b D03-R3 "the same name aimed at the BLIND cluster -- name clash or placement?" \
     "$(err_code "$live_cli" lb stream add ODOMETER --subjects "evt.other.v1" \
        --storage file --replicas 3 --cluster "$blind_side" --defaults)" \
     A4
# A5 -- and the far side is handed a live handle to it. Over a healthy gateway
# that answer is the other region's name. Here the far side is blind, so the
# answer is not knowable in advance and this is a note.
note B8a D03-R3 "the same 'stream info' question asked of the BLIND side ($blind_side)" \
     "$(stream_cluster "$blind_cli" lb ODOMETER)" \
     A5

# A22 / A24 -- the stream's OWN raft group is a different thing from the meta
# group. A22 found it healthy over a plain gateway. The stream that DID get
# placed here is healthy too: a broken domain config breaks PLACEMENT, not an
# existing stream.
check B9 D03-R5 "the stream that was placed: replicas asked for / peers built" \
      "3 / 3" \
      "$(stream_replicas "$live_cli" lb ODOMETER) / $(stream_peers "$live_cli" lb ODOMETER)" \
      A22
check B10 D03-R5 "that stream's leader sits in the LIVE region" \
      "yes" \
      "$([ "$(stream_leader_region "$live_cli" lb ODOMETER)" = "$live_side" ] && echo yes || echo no)" \
      A24
note  B10a D03-R5 "which server won that stream election this run" \
      "$(stream_leader "$live_cli" lb ODOMETER)" \
      A24a

# A6 / A7 -- one account per region is the fix for the SHARED-name problem. It
# is not a fix for this one. A6 and A7 both land cleanly over a plain gateway.
# Here the live side still works and the blind side still refuses, because an
# account is a wall for data and the broken thing is placement.
check B11 D03-R2 "per-region account on the LIVE side: its own ODOMETER lands in the live region" \
      "yes" \
      "$([ "$(fails_or_ok "$live_cli" "$live_side" stream add ODOMETER --subjects "$SUBJECT" \
             --storage file --replicas 3 --cluster "$live_side" --defaults)" = "ok" ] \
         && echo yes || echo no)" \
      A6
check B12 D03-R2 "per-region account on the BLIND side, placed from a live client" \
      "10005" \
      "$(err_code "$live_cli" "$blind_side" stream add ODOMETER --subjects "$SUBJECT" \
         --storage file --replicas 3 --cluster "$blind_side" --defaults)" \
      A7

# A9 -- a new KV bucket with no placement flag follows the CLIENT. That still
# holds on the healthy half.
nats_as "$live_cli" lb kv add t7-vehicles --storage file --replicas 3 >/dev/null 2>&1
check B13 D03-R4 "KV t7-vehicles made from the LIVE side, no placement flag, lands in the live region" \
      "yes" \
      "$([ "$(stream_cluster "$live_cli" lb KV_t7-vehicles)" = "$live_side" ] && echo yes || echo no)" \
      A9

# A18 / A19 -- a durable consumer lives with its STREAM, not with the client
# that made it. A18 is the local case and still holds here.
nats_as "$live_cli" lb consumer add ODOMETER LIVE_READER --pull --deliver all \
        --ack explicit --defaults >/dev/null 2>&1 || true
check B14 D03-R4 "a consumer made on the LIVE side's stream lives in the live region" \
      "yes" \
      "$([ "$(consumer_cluster "$live_cli" lb ODOMETER LIVE_READER)" = "$live_side" ] \
         && echo yes || echo no)" \
      A18
# A19 is the cross-region case. Asked through the blind side's client it has no
# answer that can be stated in advance, so it is a note.
note B14a D03-R4 "the same consumer question asked through the BLIND side's client" \
     "$(consumer_cluster "$blind_cli" lb ODOMETER LIVE_READER)" \
     A19

# A15 / A16 -- the second copy in the other region. This is the open half of
# D03-R7: a mirror was measured over a LEAF link and over a plain gateway, but
# never across two gateway-joined clusters with DIFFERENT domains. It is
# measured here. Placement into the blind region fails the same way every other
# placement into it fails.
cat > "$RUN_DIR/M_B_BLIND.json" <<JSON
{ "name": "M_B_BLIND", "num_replicas": 3,
  "placement": { "cluster": "$blind_side" },
  "mirror": { "name": "ODOMETER" } }
JSON
check B15 D03-R7 "a mirror of the live stream, placed in the BLIND region" \
      "10005" "$(err_code "$live_cli" lb stream add --config "$RUN_DIR/M_B_BLIND.json")" \
      A15

# And the same mirror placed on the healthy half copies, with no external.api,
# exactly as A16 found over a plain gateway. So the mirror is not broken. The
# place you want to put it is.
cat > "$RUN_DIR/M_B_LIVE.json" <<JSON
{ "name": "M_B_LIVE", "num_replicas": 3,
  "placement": { "cluster": "$live_side" },
  "mirror": { "name": "ODOMETER" } }
JSON
nats_as "$live_cli" lb stream add --config "$RUN_DIR/M_B_LIVE.json" >/dev/null 2>&1 || true
sleep 6
check B16 D03-R7 "the same mirror placed on the LIVE cluster instead: messages copied" \
      "1" "$(stream_msgs "$live_cli" lb M_B_LIVE)" \
      A16
note  B16a D03-R7 "so, a domain over a gateway" \
      "does not break mirroring -- it breaks every placement into the blind half" \
      A17a

note B6 D03-R3 "verdict" \
     "does not work -- a domain name cannot split a supercluster"

banner "$TOPOLOGY -- done"
