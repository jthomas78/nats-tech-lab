#!/usr/bin/env bash
# T5 / FIGURE H -- HUB AND LEAF, WITH AN ACCOUNT PER REGION ON THE LINK.
#
# T5 / D built the hub and bound ONE account to the leaf link: LB. D16-D22
# measured what that leaves behind -- LB_ZA and LB_AU switched on at every
# site, reaching nothing, six separate islands for two account names.
#
# This is the same nine servers with one thing changed. Each region's leaf
# remote list gains a SECOND entry, binding that region's OWN account:
#
#     za servers:  { account: LB }  and  { account: LB_ZA }
#     au servers:  { account: LB }  and  { account: LB_AU }
#
# The hub now carries three account namespaces instead of one. That is the
# shape a real platform wants -- a region owns its data, and one hub can still
# reach both -- so the questions worth asking are about what the hub can and
# cannot do with data it did not create.
#
# The variable is the leaf BINDING. Everything else is held still: same nine
# servers, same ports, same domains hub / za / au, same accounts block.
#
# Requirements exercised: D03-R2, D03-R3, D03-R5, D03-R6, D03-R7.

source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

TOPOLOGY="T5 / H -- hub and leaf, account per region"
SUBJECT="evt.odo.v1"
HUB_HTTP=(8551 8552 8553); ZA_HTTP=(8231 8232 8233); AU_HTTP=(8241 8242 8243)

hub_listen() {                # $1 name  $2 leaf listen port
  printf 'leafnodes { listen: 127.0.0.1:%s }\n' "$2" >> "$RUN_DIR/t-$1.conf"
}

# TWO remotes per region server, over the SAME three hub leaf ports. A leaf
# remote binds exactly one account, so a second account needs a second entry.
# The user in each URL is the account's own user, because that is what tells
# the hub which side of the link it is.
#
# "Exactly one" is the parser's rule, not a style choice. `account` takes a
# string; docs.nats.io/reference/config/leafnodes/remotes calls it "the local
# account to bind to this remote server". Hand it a list and the config check
# refuses the file before any server starts (nats-server 2.14.6):
#
#     { urls: [ "nats-leaf://lb:lb@127.0.0.1:7560" ], account: [ LB, LB_ZA ] }
#
#     $ nats-server -c t-acctlist.conf -t
#     nats-server: t-acctlist.conf:5:53: interface conversion:
#                  interface {} is []interface {}, not string
#
# `urls` IS a list, but it is a list of addresses for ONE link -- failover, not
# fan-out. That is why both entries below repeat the same three hub ports.
leaf_remotes_2() {            # $1 name  $2 region account  $3 its user
  {
    printf 'leafnodes {\n  remotes: [\n'
    printf '    { urls: [ "nats-leaf://lb:lb@127.0.0.1:7560",\n'
    printf '              "nats-leaf://lb:lb@127.0.0.1:7561",\n'
    printf '              "nats-leaf://lb:lb@127.0.0.1:7562" ], account: LB },\n'
    printf '    { urls: [ "nats-leaf://%s:%s@127.0.0.1:7560",\n' "$3" "$3"
    printf '              "nats-leaf://%s:%s@127.0.0.1:7561",\n' "$3" "$3"
    printf '              "nats-leaf://%s:%s@127.0.0.1:7562" ], account: %s }\n' "$3" "$3" "$2"
    printf '  ]\n}\n'
  } >> "$RUN_DIR/t-$1.conf"
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
    leaf_remotes_2 "za-$i" LB_ZA za
    conf_accounts "za-$i"

    conf_head "au-$i" "424$i" "824$i" au
    conf_cluster "au-$i" au "624$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '624%s ' $j; done)
    leaf_remotes_2 "au-$i" LB_AU au
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
# Interest across a leaf link needs about 5 seconds to spread. Two links per
# region server, so give it longer than D's 8.
sleep 10

# --- The rig is unchanged where it must be ---------------------------------
# A second leaf remote is a TRANSPORT change. It must not touch the JetStream
# arithmetic at all. If either of these moves, the rest of the run is noise.
check H1 D03-R3 "distinct JetStream meta groups across hub, ZA and AU" \
      "3" "$(meta_group_count "${HUB_HTTP[@]}" "${ZA_HTTP[@]}" "${AU_HTTP[@]}")" D1
check H2 D03-R5 "meta group sizes: hub / ZA / AU (quorum 2 each)" \
      "3 / 3 / 3" "$(meta_size 8551) / $(meta_size 8231) / $(meta_size 8241)" D2

# --- Each region still owns its own streams --------------------------------
nats_as 4231 za stream add ODOMETER --subjects "$SUBJECT" --storage file \
        --replicas 3 --defaults >/dev/null 2>&1 || true
nats_as 4241 au stream add ODOMETER --subjects "$SUBJECT" --storage file \
        --replicas 3 --defaults >/dev/null 2>&1 || true
sleep 3
check H3 D03-R2 "LB_ZA ODOMETER lands in / LB_AU ODOMETER lands in" \
      "za / au" "$(stream_cluster 4231 za ODOMETER) / $(stream_cluster 4241 au ODOMETER)" D16
check H4 D03-R5 "LB_ZA ODOMETER: replicas asked for / peers built" \
      "3 / 3" "$(stream_replicas 4231 za ODOMETER) / $(stream_peers 4231 za ODOMETER)" D19
check H5 D03-R5 "LB_ZA / LB_AU ODOMETER: region holding the stream leader" \
      "za / au" "$(stream_leader_region 4231 za ODOMETER) / $(stream_leader_region 4241 au ODOMETER)" \
      D20

# --- The wall is still a wall ----------------------------------------------
# Both regions now reach the SAME hub. That does not join them to each other.
# D18 read 1 / 0 with no link at all; this must still read 1 / 0 with two.
nats_as 4231 za pub "$SUBJECT" '{"km":12.5}' >/dev/null 2>&1
sleep 3
check H6 D03-R6 "one publish in LB_ZA: messages in LB_ZA / LB_AU" \
      "1 / 0" "$(stream_msgs 4231 za ODOMETER) / $(stream_msgs 4241 au ODOMETER)" D18
note  H6a D03-R6 "a shared HUB is not a shared account" \
      "both regions reach the same three hub servers and still cannot see each other"

# --- What the hub can now do, and what it still cannot ---------------------
# This is the whole reason for the topology. The hub's LB_ZA is now the same
# ACCOUNT as ZA's LB_ZA -- but it is a different DOMAIN, and a domain is what
# owns stream names. D21 asked this with no link and got "fails".
check H7 D03-R3 "the HUB's LB_ZA asks for stream info on ZA's ODOMETER, by name alone" \
      "fails" "$(fails_or_ok 4551 za stream info ODOMETER --json)" D21
note  H7a D03-R3 "why a link did not make that work" \
      "the account now reaches the hub, but a stream name still resolves in the local DOMAIN"

# Subjects DO cross, and that is the difference. A subject has no domain.
nats_as 4551 za stream add HUB_COPY --subjects "$SUBJECT" --storage file \
        --replicas 3 --defaults >/dev/null 2>&1 || true
sleep 5
nats_as 4231 za pub "$SUBJECT" '{"km":13.5}' >/dev/null 2>&1
sleep 5
check H8 D03-R6 "a HUB stream on the SAME subject: messages it captured from ZA's publish" \
      "1" "$(stream_msgs 4551 za HUB_COPY)" D5
note  H8a D03-R6 "so the link carries SUBJECTS, not streams" \
      "the hub captured the publish live -- it did not read ZA's stream"

# And the account wall stands on the hub itself. Both regional accounts now
# terminate on the same three servers. They still cannot read each other.
check H9 D03-R2 "on the HUB, LB_AU asks for stream info on LB_ZA's HUB_COPY" \
      "fails" "$(fails_or_ok 4551 au stream info HUB_COPY --json)" A8
note  H9a D03-R2 "two regional accounts landing on ONE hub do not merge" \
      "the hub holds three separate namespaces, not one shared one"

# --- The cross-domain mirror, inside ONE regional account ------------------
# D7 and D9 measured this trap in the SHARED account. The question here is
# whether binding the account per region changes the answer. It must not:
# external.api names a DOMAIN, and the domains did not move.
cat > "$RUN_DIR/M_H_BROKEN.json" <<'JSON'
{ "name": "M_H_BROKEN", "num_replicas": 3, "mirror": { "name": "ODOMETER" } }
JSON
cat > "$RUN_DIR/M_H_OK.json" <<'JSON'
{ "name": "M_H_OK", "num_replicas": 3,
  "mirror": { "name": "ODOMETER", "external": { "api": "$JS.za.API" } } }
JSON
nats_as 4551 za stream add --config "$RUN_DIR/M_H_BROKEN.json" >/dev/null 2>&1 || true
nats_as 4551 za stream add --config "$RUN_DIR/M_H_OK.json" >/dev/null 2>&1 || true
sleep 10
check H10 D03-R7 "hub mirrors LB_ZA's ODOMETER by NAME only -- messages copied" \
      "0" "$(stream_msgs 4551 za M_H_BROKEN)" D7
check H11 D03-R7 "the same mirror WITH external.api \$JS.za.API -- messages copied" \
      "2" "$(stream_msgs 4551 za M_H_OK)" D8
note  H11a D03-R7 "so binding the account per region changed nothing here" \
      "external.api names a DOMAIN -- the account it runs in was never the problem"

# --- Kill the ENTIRE hub ----------------------------------------------------
# D10-D14 proved the regions survive when the hub is only carrying LB. They
# now carry their own account over the same link, so the question is worth
# asking again: does a region that depends on the hub for its OWN account
# still work when the hub is gone?
kill_server hub-1 hub-2 hub-3
sleep 6
check H12 D03-R1 "whole hub gone: ZA still has a meta leader" \
      "yes" "$([ "$(meta_leader 8231)" = "NONE" ] && echo no || echo yes)" D10
check H13 D03-R1 "whole hub gone: LB_ZA creates a NEW 3-replica stream" \
      "ok" "$(fails_or_ok 4231 za stream add T_ZA_NEW --subjects "evt.zn.v1" \
              --storage file --replicas 3 --defaults)" D12
check H14 D03-R1 "whole hub gone: LB_ZA still stores its own publishes" \
      "ok" "$(fails_or_ok 4231 za pub "$SUBJECT" '{"km":9}')" D14
note  H14a D03-R1 "so the hub is a READER, not a dependency" \
      "a region keeps full JetStream for its own account with all three hub nodes dead"

note H15 D03-R3 "the verdict" \
     "an account per region on the leaf link buys the hub a live SUBJECT feed and nothing more"

banner "$TOPOLOGY -- done"
