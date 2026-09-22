#!/usr/bin/env bash
# T2 / E -- SHARING A SUBJECT ON PURPOSE. Export / import between accounts.
#
# Same six servers and same gateway as T2 / A. The only thing that changes is
# the accounts block, which is why this is a T2 variant and not a topology of
# its own.
#
# Every other script in this folder measures accidents: a name collision, a
# double capture nobody asked for, a mirror that copies the wrong stream in
# silence. This one measures the DELIBERATE version of the same thing.
#
# An account boundary is total by default. Nothing crosses it. `exports` and
# `imports` open exactly one hole, in exactly one direction, and the importing
# side chooses the subject the data arrives on:
#
#   LB_ZA  exports  evt.odo.>
#   LB_AU  imports  it with prefix `za`, so it arrives as za.evt.odo.>
#
# Three facts this measures, and all three matter for the platform:
#
#   1. The copy is REAL. One publish in ZA is stored in two accounts.
#   2. The copy is PREFIXED, not merged. The importing account never sees the
#      raw subject, so it can never confuse the two sources.
#   3. The hole is one-way, and it is a SUBJECT hole only. LB_AU still cannot
#      see, touch or name a JetStream stream that belongs to LB_ZA.
#
# The shape underneath is T2, the plain gateway -- so this also shows that an
# import crosses a gateway exactly like an ordinary subject does.
#
# Requirements exercised: D03-R2, D03-R3, D03-R5, D03-R8.

source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

TOPOLOGY="T2 / E -- export / import between accounts"
SUBJECT="evt.odo.v1"
ALL_HTTP=(8231 8232 8233 8241 8242 8243)

# The shared accounts_block has no exports or imports -- it is deliberately
# held still for every other topology. This is the ONE script that changes it,
# so it writes its own.
ei_accounts() {
  cat >> "$RUN_DIR/t-$1.conf" <<'EOF'
accounts {
  $SYS { users: [ { user: admin, password: admin } ] }
  LB_ZA {
    jetstream: enabled
    users: [ { user: za, password: za } ]
    exports: [ { stream: "evt.odo.>" } ]
  }
  LB_AU {
    jetstream: enabled
    users: [ { user: au, password: au } ]
    imports: [
      { stream: { account: LB_ZA, subject: "evt.odo.>" }, prefix: "za" }
    ]
  }
  LB { jetstream: enabled, users: [ { user: lb, password: lb } ] }
}
EOF
}

build() {
  local i
  for i in 1 2 3; do
    conf_head "za-$i" "423$i" "823$i"
    conf_cluster "za-$i" za "623$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '623%s ' $j; done)
    conf_gateway "za-$i" za "723$i" au 7241 7242 7243
    ei_accounts "za-$i"

    conf_head "au-$i" "424$i" "824$i"
    conf_cluster "au-$i" au "624$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '624%s ' $j; done)
    conf_gateway "au-$i" au "724$i" za 7231 7232 7233
    ei_accounts "au-$i"
  done
}

lab_init
banner "$TOPOLOGY"

build
for i in 1 2 3; do start_server "za-$i"; start_server "au-$i"; done
for h in "${ALL_HTTP[@]}"; do wait_ready "$h"; done
wait_meta_leader 8231 || true
sleep 5

# --- Three receivers, so one publish can be followed everywhere -------------
#   ODOMETER  in LB_ZA, on the raw subject          -- the origin
#   IMPORTED  in LB_AU, on the PREFIXED subject     -- the deliberate copy
#   RAW       in LB_AU, on the raw subject          -- must stay empty
nats_as 4231 za stream add ODOMETER --subjects "$SUBJECT" --storage file \
        --replicas 3 --cluster za --defaults >/dev/null
nats_as 4241 au stream add IMPORTED --subjects "za.evt.odo.>" --storage file \
        --replicas 3 --cluster au --defaults >/dev/null
nats_as 4241 au stream add RAW --subjects "$SUBJECT" --storage file \
        --replicas 3 --cluster au --defaults >/dev/null
sleep 3

# --- ONE publish, in ZA, into the exported subject --------------------------
nats_as 4231 za pub "$SUBJECT" '{"km":12.5}' >/dev/null 2>&1
sleep 5

check E1 D03-R8 "one publish in LB_ZA: the owning account stored it" \
      "1" "$(stream_msgs 4231 za ODOMETER)"
check E2 D03-R8 "the importing account LB_AU stored it too, across the gateway" \
      "1" "$(stream_msgs 4241 au IMPORTED)"
check E3 D03-R8 "and it arrived PREFIXED -- LB_AU's raw-subject stream" \
      "0" "$(stream_msgs 4241 au RAW)"
note  E3a D03-R8 "so the deliberate copy and the accidental one differ" \
      "the importing side renames the subject, so the two sources can never be confused"

# --- The hole is one-way ----------------------------------------------------
# LB_AU exports nothing. A publish there is its own business, and ZA's stream
# must not move. RAW is LB_AU's own subject space, so RAW is allowed to fill.
nats_as 4241 au pub "$SUBJECT" '{"km":99}' >/dev/null 2>&1
sleep 4
check E4 D03-R8 "LB_AU publishes the same subject: ZA's stream is unchanged" \
      "1" "$(stream_msgs 4231 za ODOMETER)"
check E5 D03-R8 "and LB_AU's own raw stream took that one" \
      "1" "$(stream_msgs 4241 au RAW)"

# --- A subject hole is NOT a JetStream hole ---------------------------------
# This is the line that makes export/import safe to use. The account boundary
# still owns the streams. LB_AU can receive ZA's messages and still cannot
# read, name or delete ZA's stream.
check E6 D03-R2 "LB_AU asks for stream info on ZA's ODOMETER" \
      "fails" "$(fails_or_ok 4241 au stream info ODOMETER --json)"
check E7 D03-R2 "LB_AU creates its OWN stream named ODOMETER -- no 10058" \
      "ok" "$(fails_or_ok 4241 au stream add ODOMETER --subjects "evt.dup.v1" \
              --storage file --replicas 3 --cluster au --defaults)"
check E8 D03-R2 "and they are two real streams, in" \
      "za / au" "$(stream_cluster 4231 za ODOMETER) / $(stream_cluster 4241 au ODOMETER)"

# --- THE SAME QUESTIONS T2 / B ASKED, ASKED AGAIN HERE ----------------------
# T2 / B is the warning next door: one small block in the config file left a
# whole cluster unable to elect, on a healthy machine, with no error anywhere
# until something tried to place a stream. This script changes a config block
# too -- the accounts block. So it re-asks B's questions, and each row carries
# the id of the B check it repeats.
#
# The first seven are controls. They should all say "normal", and saying so is
# the point: an exports/imports block changes exactly one thing.

za_leader="$(meta_leader 8231)"
au_leader="$(meta_leader 8241)"
live_n=0
[ "$za_leader" != "NONE" ] && live_n=$((live_n+1))
[ "$au_leader" != "NONE" ] && live_n=$((live_n+1))

# B1 got 1 here, on a healthy machine, because a domain per cluster blinded one
# side. An exports/imports block does not.
check E10 D03-R3 "sides that can name a meta leader, with NOTHING stopped (of 2)" \
      "2" "$live_n" \
      B1
# B2's mirror image. Over a gateway there is ONE meta group, so both sides must
# name the SAME server, not merely some server.
check E11 D03-R3 "and both sides name the SAME meta leader" \
      "yes" "$([ "$za_leader" = "$au_leader" ] && echo yes || echo no)" \
      B2
check E12 D03-R3 "meta group size seen from ZA / from AU" \
      "6 / 6" "$(meta_size 8231) / $(meta_size 8241)" \
      B3

# B4 and B5: placement. On B one of the two fails with 10005. Here both work,
# asked from one client, so the answer cannot be blamed on which side asked.
check E13 D03-R3 "shared account LB: placing a stream on cluster za" \
      "ok" "$(fails_or_ok 4231 lb stream add P_ZA --subjects "evt.p.za.v1" \
              --storage file --replicas 3 --cluster za --defaults)" \
      B5
check E14 D03-R3 "shared account LB: placing a stream on cluster au, from the SAME client" \
      "ok" "$(fails_or_ok 4231 lb stream add P_AU --subjects "evt.p.au.v1" \
              --storage file --replicas 3 --cluster au --defaults)" \
      B4

# B9 and B10: a stream is a RAFT group of its own. The exported stream is an
# ordinary three-peer group and the export does not stretch it across the WAN.
check E15 D03-R5 "the EXPORTED stream: replicas asked for / peers built" \
      "3 / 3" "$(stream_replicas 4231 za ODOMETER) / $(stream_peers 4231 za ODOMETER)" \
      B9
check E16 D03-R5 "the EXPORTED stream: region holding its leader" \
      "za" "$(stream_leader_region 4231 za ODOMETER)" \
      B10

# --- AND THE ONE NEW QUESTION: CAN A MIRROR CROSS THE ACCOUNT WALL? ---------
# B15 asks whether a mirror can be placed somewhere it must not go, and gets a
# clean 10005. This is the nastier version. E6 already shows LB_AU cannot read
# ZA's stream info. So what happens when LB_AU asks to MIRROR a stream called
# ODOMETER?
#
# It is allowed. There is no error. A mirror name is resolved inside the
# ASKING account, and E7 put an ODOMETER in LB_AU -- so LB_AU mirrors its own
# empty stream and never touches ZA's. The copy is silent, it is wrong, and
# nothing in the output says so. Compare A15/A16, where the same request over
# the same gateway copies real messages, because there both streams were in
# one account.
cat > "$RUN_DIR/M_E.json" <<'JSON'
{ "name": "M_E", "num_replicas": 3,
  "placement": { "cluster": "au" },
  "mirror": { "name": "ODOMETER" } }
JSON
check E17 D03-R8 "LB_AU asks to mirror a stream named ODOMETER -- is it refused?" \
      "ok" "$(fails_or_ok 4241 au stream add --config "$RUN_DIR/M_E.json")" \
      B15
sleep 6
check E18 D03-R8 "messages in that mirror / in the ZA stream it appears to name" \
      "0 / 1" "$(stream_msgs 4241 au M_E) / $(stream_msgs 4231 za ODOMETER)" \
      B16
note  E18a D03-R8 "so a mirror cannot cross an account wall, and does not say so" \
      "the name resolved inside LB_AU, so it copied LB_AU's own empty ODOMETER" \
      B16a

note E9 D03-R8 "the verdict" \
     "works, and it is the only sharing in this demo that is explicit, one-way and renamed"

banner "$TOPOLOGY -- done"
