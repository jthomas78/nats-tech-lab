#!/usr/bin/env bash
# E -- SHARING A SUBJECT ON PURPOSE. Export / import between two accounts.
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
# Requirements exercised: D03-R2, D03-R8.

source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

TOPOLOGY="E -- export / import between accounts"
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

note E9 D03-R8 "the verdict" \
     "works, and it is the only sharing in this demo that is explicit, one-way and renamed"

banner "$TOPOLOGY -- done"
