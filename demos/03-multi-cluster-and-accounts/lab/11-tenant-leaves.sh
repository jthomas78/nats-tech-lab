#!/usr/bin/env bash
# T7 / FIGURE J -- T4 PLUS A TENANT LEAF PER REGION, ONE TENANT ACCOUNT PER REGION.
#
# T4 answered the meta-vote question: three sites, nine voters, lose a region
# and six are left against a majority of five. T7 keeps that core exactly and
# asks what it means for a TENANT, not for the platform.
#
#   za   3 servers   the ZA platform: the tenant's ZA backend and streams
#   au   3 servers   the AU platform: the tenant's AU backend and streams
#   arb  3 servers   the control / arbiter cluster -- a vote, no tenant traffic
#
#   za-leaf  1 server, leaf node into za   the tenant's ZA frontend connects here
#   au-leaf  1 server, leaf node into au   the tenant's AU frontend connects here
#
# The three clusters are joined by gateways, as in T4. Each tenant leaf is a
# plain leaf node: NO JetStream of its own, so it adds no vote and holds no
# store. It only relays its frontend's traffic into its own region.
#
# ONE TENANT ACCOUNT PER REGION. TENANT_ZA holds the ZA backend and the ZA
# streams; TENANT_AU holds the AU ones. The ZA leaf binds its local TENANT_ZA
# account to the cluster's TENANT_ZA, and the AU leaf does the same with
# TENANT_AU. Inside a region the frontend and the backend share one subject
# space: no export, no import. Between the regions there is no path at all --
# the same stream name lives in both (J6), and no backend answers for the
# other region (J14). Corrected 2026-10-18 on the user's instruction; the first
# version used one TENANT account for both regions.
#
# Both tenant accounts are defined on ALL nine servers, though each is used in
# one region only: the meta leader can sit in any cluster, and it must know
# every JetStream account to place that account's streams.
#
# The backend is a request/reply service (`nats reply`), one per region, each
# answering with its region's name. Who answers is the check.
#
# The script builds the shape TWICE, like 07-gateway-and-hub.sh:
#
#   phase 1  no `jetstream.domain` anywhere
#   phase 2  the SAME wiring, one domain `core` on all nine cluster servers
#
# Why phase 2 exists, found 2026-10-18 and read in nats-server 2.14.6,
# server/leafnode.go, the leaf JetStream deny block: a leaf link ALWAYS denies
# the plain JetStream API ($JS.API.>) to a non-system account. The only way a
# leaf client reaches the API is a domain on the hub side, which maps
# $JS.<domain>.API.> through. Without one, a frontend on a leaf can still
# publish with an ack (the STREAM sends the ack, not the API), but it cannot
# list a stream, read one through a consumer, or touch a KV bucket.
#
# Requirements exercised: D03-R1, D03-R5, D03-R33, D03-R34, D03-R35, D03-R36.

source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

TOPOLOGY="T7 / J -- T4 + a tenant leaf per region"
ZA_HTTP=(8231 8232 8233); AU_HTTP=(8241 8242 8243); ARB_HTTP=(8541 8542 8543)
LEAF_HTTP=(8571 8581)
ZA_LEAF=4571; AU_LEAF=4581          # the tenant frontends' client ports
SVC="tenant.svc.quote"              # the tenant backend's request subject
ORDERS="tenant.orders"              # the SAME subject in both tenant accounts
DOMAIN=core                         # phase 2's one domain, on all nine

# The lab's three business accounts plus $SYS, exactly as every other script
# has them, plus TENANT_ZA and TENANT_AU. Only T7 adds accounts; the others
# stay held still.
#
# system_account is not optional here. Measured 2026-10-18: a server with a
# leaf LISTENER and a gateway refuses to start without one --
#   "leaf nodes and gateways (both being defined) require a system account"
t7_accounts() {                       # $1 name
  {
    printf 'system_account: "$SYS"\n'   # quoted: bare $SYS is a config variable
    accounts_block | sed '$d'         # drop the closing brace, then add the tenants
    printf '  TENANT_ZA { jetstream: enabled, users: [ { user: tenant_za, password: tenant_za } ] }\n'
    printf '  TENANT_AU { jetstream: enabled, users: [ { user: tenant_au, password: tenant_au } ] }\n}\n'
  } >> "$RUN_DIR/t-$1.conf"
}

# Copied from 06-arbiter3.sh: every server lists the other two sites' gateways.
gw3() {                               # $1 name  $2 my gw name  $3 my gw port
  local name="$1" gw="$2" mine="$3" f="$RUN_DIR/t-$1.conf"
  {
    printf 'gateway {\n  name: %s\n  listen: 127.0.0.1:%s\n  gateways: [\n' "$gw" "$mine"
    [ "$gw" != za ]  && printf '    { name: za,  urls: [ nats://127.0.0.1:7231, nats://127.0.0.1:7232, nats://127.0.0.1:7233 ] },\n'
    [ "$gw" != au ]  && printf '    { name: au,  urls: [ nats://127.0.0.1:7241, nats://127.0.0.1:7242, nats://127.0.0.1:7243 ] },\n'
    [ "$gw" != arb ] && printf '    { name: arb, urls: [ nats://127.0.0.1:7541, nats://127.0.0.1:7542, nats://127.0.0.1:7543 ] },\n'
    printf '  ]\n}\n'
  } >> "$f"
}

# A regional server accepts leaf connections. The arbiter cluster does not.
leaf_listen() {                       # $1 name  $2 leaf listen port
  printf 'leafnodes { listen: 127.0.0.1:%s }\n' "$2" >> "$RUN_DIR/t-$1.conf"
}

# One tenant leaf node. No jetstream block at all, in both phases. It lists
# all three of ITS region's leaf ports and none of the other region's, and it
# binds its region's tenant account on both sides of the link.
tenant_leaf() {                       # $1 name  $2 client  $3 monitor  $4 account  $5 user  $6.. leaf ports
  local name="$1" client="$2" http="$3" acc="$4" user="$5" urls="" p; shift 5
  for p in "$@"; do urls+="\"nats-leaf://$user:$user@127.0.0.1:$p\", "; done
  cat > "$RUN_DIR/t-$name.conf" <<EOF
server_name: t-$name
listen: 127.0.0.1:$client
http: 127.0.0.1:$http
leafnodes {
  remotes: [ { urls: [ ${urls%, } ], account: $acc } ]
}
accounts {
  $acc { users: [ { user: fe, password: fe } ] }
}
EOF
}

# $1 is the domain: "" for phase 1, $DOMAIN for phase 2. The SAME name on all
# nine -- one JetStream system, one name. Not Figure B's name per cluster.
build() {
  local d="${1:-}" i j
  for i in 1 2 3; do
    conf_head "za-$i" "423$i" "823$i" "$d"
    conf_cluster "za-$i" za "623$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '623%s ' $j; done)
    gw3 "za-$i" za "723$i"
    leaf_listen "za-$i" "757$i"
    t7_accounts "za-$i"

    conf_head "au-$i" "424$i" "824$i" "$d"
    conf_cluster "au-$i" au "624$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '624%s ' $j; done)
    gw3 "au-$i" au "724$i"
    leaf_listen "au-$i" "758$i"
    t7_accounts "au-$i"

    conf_head "arb-$i" "454$i" "854$i" "$d"
    conf_cluster "arb-$i" arb "654$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '654%s ' $j; done)
    gw3 "arb-$i" arb "754$i"
    t7_accounts "arb-$i"
  done
  tenant_leaf za-leaf-1 "$ZA_LEAF" 8571 TENANT_ZA tenant_za 7571 7572 7573
  tenant_leaf au-leaf-1 "$AU_LEAF" 8581 TENANT_AU tenant_au 7581 7582 7583
}

# The leaves dial their region, so the regions go first.
start_all() {
  local i h
  for i in 1 2 3; do start_server "za-$i"; start_server "au-$i"; start_server "arb-$i"; done
  for h in "${ZA_HTTP[@]}" "${AU_HTTP[@]}" "${ARB_HTTP[@]}"; do wait_ready "$h"; done
  wait_meta_leader 8231 || true
  start_server za-leaf-1; start_server au-leaf-1
  for h in "${LEAF_HTTP[@]}"; do wait_ready "$h"; done
}

# --- The tenant backend -----------------------------------------------------
# `nats reply` processes are not nats-server processes, so the kill pattern
# cannot reach them. Track their pids and stop them on every exit.
ZA_BACKEND=""; AU_BACKEND=""

start_backend() {                     # $1 region  $2 client port  $3 user -> pid on stdout
  nats --server "nats://127.0.0.1:$2" --user "$3" --password "$3" \
       reply "$SVC" "$1" --queue tenant-svc >/dev/null 2>&1 &
  echo $!
}

stop_backends() {
  local p
  for p in $ZA_BACKEND $AU_BACKEND; do kill "$p" 2>/dev/null || true; done
  ZA_BACKEND=""; AU_BACKEND=""
}

# lab_down's body, plus the backends. Do not call lab_down from here: it reads
# $? on entry, and resetting $? with `( exit $rc )` trips `set -e` inside the
# trap and skips the kill -- that left three arbiter servers running once.
t7_down() {
  local rc=$?
  stop_backends
  pkill -CONT -f "$KILL_PATTERN" >/dev/null 2>&1 || true
  pkill -f "$KILL_PATTERN" >/dev/null 2>&1 || true
  sleep 1
  return "$rc"
}

# Ask the tenant service from a frontend, five times. Prints the distinct
# regions that answered, joined by "+", or "fails" when nothing answered.
# Five asks, not one: a locality rule that holds only sometimes is not a rule.
who_answers() {                       # $1 frontend (leaf) client port
  local i out got=""
  for i in 1 2 3 4 5; do
    out="$(nats_as "$1" fe request "$SVC" '' --raw --timeout 3s 2>/dev/null)" || out=""
    [ -n "$out" ] && got+="$out"$'\n'
  done
  if [ -z "$got" ]; then echo "fails"; return 0; fi
  printf '%s' "$got" | sort -u | paste -sd+ -
}

# Leaf connections bound to an account, summed over a cluster's monitor ports.
leaf_count() {                        # $1 account  $2.. monitor ports
  local acc="$1" n=0 h c; shift
  for h in "$@"; do
    c="$(curl -fs "http://127.0.0.1:$h/leafz" 2>/dev/null \
         | jq --arg a "$acc" '[.leafs[]? | select(.account == $a)] | length' 2>/dev/null)" || c=0
    n=$(( n + ${c:-0} ))
  done
  echo "$n"
}

# The stream names a frontend sees, sorted and joined by "+", or "fails".
fe_streams() {                        # $1 leaf client port  [$2.. extra nats flags]
  local port="$1" out; shift
  out="$(nats_as "$port" fe "$@" stream ls --names 2>/dev/null)" || { echo "fails"; return 0; }
  printf '%s\n' "$out" | grep -v '^$' | sort | paste -sd+ -
}

lab_init
trap t7_down EXIT INT TERM
banner "$TOPOLOGY"

# ===========================================================================
# PHASE 1 -- no domain anywhere
# ===========================================================================
build ""
start_all
ZA_BACKEND="$(start_backend za 4231 tenant_za)"
AU_BACKEND="$(start_backend au 4241 tenant_au)"
# Interest across a leaf link needs about 5 seconds to spread.
sleep 8

# --- The leaves add no vote -------------------------------------------------
check J1 D03-R35 "meta group size with both tenant leaves connected (majority is 5)" \
      "9" "$(meta_size 8231)" F1
check J2 D03-R35 "distinct JetStream meta groups across za, au and arb" \
      "1" "$(meta_group_count "${ZA_HTTP[@]}" "${AU_HTTP[@]}" "${ARB_HTTP[@]}")" F2
check J3 D03-R33 "leaf links: TENANT_ZA on za / TENANT_AU on au / any tenant on arb" \
      "1 / 1 / 0" \
      "$(leaf_count TENANT_ZA "${ZA_HTTP[@]}") / $(leaf_count TENANT_AU "${AU_HTTP[@]}") / $(( $(leaf_count TENANT_ZA "${ARB_HTTP[@]}") + $(leaf_count TENANT_AU "${ARB_HTTP[@]}") ))"
check J4 D03-R33 "leaf links crossed over: TENANT_AU on za / TENANT_ZA on au" \
      "0 / 0" \
      "$(leaf_count TENANT_AU "${ZA_HTTP[@]}") / $(leaf_count TENANT_ZA "${AU_HTTP[@]}")"
check J5 D03-R35 "the tenant leaves run JetStream: za-leaf / au-leaf" \
      "no / no" \
      "$(curl -fs http://127.0.0.1:8571/varz | jq -r 'if .jetstream.config then "yes" else "no" end') / $(curl -fs http://127.0.0.1:8581/varz | jq -r 'if .jetstream.config then "yes" else "no" end')"

# --- Each region's backend owns a stream of the SAME name --------------------
# In the shared LB account a second ODOMETER was refused (A4). Two accounts
# are two namespaces, so both create cleanly.
nats_as 4231 tenant_za stream add ORDERS --subjects "$ORDERS" --storage file \
        --replicas 3 --cluster za --defaults >/dev/null
nats_as 4241 tenant_au stream add ORDERS --subjects "$ORDERS" --storage file \
        --replicas 3 --cluster au --defaults >/dev/null
sleep 2
check J6 D03-R36 "stream ORDERS in TENANT_ZA / in TENANT_AU lands in" \
      "za / au" "$(stream_cluster 4231 tenant_za ORDERS) / $(stream_cluster 4241 tenant_au ORDERS)" A4

# The frontend writes through its leaf and waits for the JetStream ack.
check J7 D03-R33 "ZA frontend, through its leaf: acked JetStream write into ORDERS" \
      "ok" "$(fails_or_ok "$ZA_LEAF" fe pub "$ORDERS" '{"id":1}' --jetstream)"
check J8 D03-R33 "AU frontend, through its leaf: acked JetStream write into ORDERS" \
      "ok" "$(fails_or_ok "$AU_LEAF" fe pub "$ORDERS" '{"id":1}' --jetstream)"
nats_as "$ZA_LEAF" fe pub "$ORDERS" '{"id":2}' --jetstream >/dev/null 2>&1 || true
check J9 D03-R36 "two ZA writes, one AU write: messages in ORDERS, TENANT_ZA / TENANT_AU" \
      "2 / 1" "$(stream_msgs 4231 tenant_za ORDERS) / $(stream_msgs 4241 tenant_au ORDERS)"
# A stream name resolves in the ASKING account, not in the cluster the client
# happens to be connected to.
check J10 D03-R36 "a TENANT_AU user on a ZA server: its ORDERS lives in" \
      "au" "$(stream_cluster 4231 tenant_au ORDERS)"

# --- But the JetStream API does not cross the leaf --------------------------
check J11 D03-R33 "no domain: streams the ZA frontend lists through its leaf" \
      "fails" "$(fe_streams "$ZA_LEAF")"
note  J11a D03-R33 "how that refusal arrived" \
      "$(err_code "$ZA_LEAF" fe stream ls --names)"
note  J11b D03-R33 "why J7 works and J11 does not" \
      "the stream sends the publish ack itself; the API (\$JS.API.>) is denied on every leaf link without a domain"

# --- Who answers the tenant's request ---------------------------------------
check J12 D03-R34 "ZA frontend's requests (5): regions that answered" \
      "za" "$(who_answers "$ZA_LEAF")"
check J13 D03-R34 "AU frontend's requests (5): regions that answered" \
      "au" "$(who_answers "$AU_LEAF")"
kill "$ZA_BACKEND" 2>/dev/null || true; ZA_BACKEND=""
sleep 3
check J14 D03-R36 "ZA backend stopped, AU backend running: ZA frontend's requests answered by" \
      "fails" "$(who_answers "$ZA_LEAF")"
note  J14a D03-R36 "why AU does not answer for ZA" \
      "a queue group lives inside one account; the AU backend is in TENANT_AU, so it never hears TENANT_ZA's request"
ZA_BACKEND="$(start_backend za 4231 tenant_za)"
sleep 3
check J15 D03-R34 "ZA backend back: ZA frontend's requests answered by" \
      "za" "$(who_answers "$ZA_LEAF")"

# --- AU goes dark. 6 of 9 voters left. --------------------------------------
freeze au-1 au-2 au-3
wait_dark "${AU_HTTP[@]}"
LEADER_1="$(wait_live_leader 8231 '^t-(za|arb)')"
check J16 D03-R1 "AU dark (6 of 9 left): a leader is elected among the survivors" \
      "yes" "$([ "$LEADER_1" = "NONE" ] && echo no || echo yes)" F3
check J17 D03-R34 "AU dark: ZA frontend's requests answered by" \
      "za" "$(who_answers "$ZA_LEAF")"
check J18 D03-R1 "AU dark: ZA frontend's acked write into ORDERS" \
      "ok" "$(fails_or_ok "$ZA_LEAF" fe pub "$ORDERS" '{"id":3}' --jetstream)"
check J19 D03-R1 "AU dark: ZA backend creates a NEW 3-replica tenant stream" \
      "ok" "$(fails_or_ok 4231 tenant_za stream add ORDERS_NEW --subjects "tenant.new" \
              --storage file --replicas 3 --cluster za --defaults)" F4
check J20 D03-R34 "AU dark: AU frontend's requests answered by" \
      "fails" "$(who_answers "$AU_LEAF")"
note  J20a D03-R34 "why the AU frontend does not fail over to ZA" \
      "its leaf lists only AU's leaf ports, and its account has no backend in ZA; a frozen region keeps the link open and silent"

thaw au-1 au-2 au-3
wait_meta_leader 8231 || true
sleep 8
check J21 D03-R5 "after recovery: meta group size" "9" "$(meta_size 8231)" F10
check J22 D03-R34 "after recovery: AU frontend's requests answered by" \
      "au" "$(who_answers "$AU_LEAF")" J13

# --- ZA goes dark: the mirror image -----------------------------------------
freeze za-1 za-2 za-3
wait_dark "${ZA_HTTP[@]}"
LEADER_2="$(wait_live_leader 8241 '^t-(au|arb)')"
check J23 D03-R1 "ZA dark (6 of 9 left): a leader is elected among the survivors" \
      "yes" "$([ "$LEADER_2" = "NONE" ] && echo no || echo yes)" F22
check J24 D03-R34 "ZA dark: AU frontend's requests answered by" \
      "au" "$(who_answers "$AU_LEAF")" J17
check J25 D03-R1 "ZA dark: AU frontend's acked write into ORDERS" \
      "ok" "$(fails_or_ok "$AU_LEAF" fe pub "$ORDERS" '{"id":4}' --jetstream)" J18
check J26 D03-R34 "ZA dark: ZA frontend's requests answered by" \
      "fails" "$(who_answers "$ZA_LEAF")" J20

thaw za-1 za-2 za-3
wait_meta_leader 8231 || true
sleep 8
check J27 D03-R5 "after the second recovery: meta group size" "9" "$(meta_size 8231)" F25
check J28 D03-R33 "after both recoveries: messages in ORDERS, TENANT_ZA (J7 + 1 + J18) / TENANT_AU (J8 + J25)" \
      "3 / 2" "$(stream_msgs 4231 tenant_za ORDERS) / $(stream_msgs 4241 tenant_au ORDERS)"

# ===========================================================================
# PHASE 2 -- the SAME wiring, one domain `core` on all nine cluster servers
#
# The leaves stay as they were: no JetStream, no domain. The frontend asks
# for the domain by name (--js-domain core) and the region maps it through.
# ===========================================================================
stop_backends
lab_down_quiet
rm -f "$RUN_DIR"/t-*.conf
rm -rf "$RUN_DIR"/{log,pid,js}
mkdir -p "$RUN_DIR"/{log,pid,js}

build "$DOMAIN"
start_all
sleep 8

check J29 D03-R35 "one domain on all nine: meta group size (it did NOT split)" \
      "9" "$(meta_size 8231)" J1
check J30 D03-R35 "one domain on all nine: distinct meta groups across za, au and arb" \
      "1" "$(meta_group_count "${ZA_HTTP[@]}" "${AU_HTTP[@]}" "${ARB_HTTP[@]}")" J2

nats_as 4231 tenant_za stream add ORDERS --subjects "$ORDERS" --storage file \
        --replicas 3 --cluster za --defaults >/dev/null
nats_as 4241 tenant_au stream add ORDERS --subjects "$ORDERS" --storage file \
        --replicas 3 --cluster au --defaults >/dev/null
nats_as 4231 za stream add ODOMETER --subjects "evt.odo.v1" --storage file \
        --replicas 3 --cluster za --defaults >/dev/null
sleep 2

check J31 D03-R33 "domain set, frontend does NOT name it: streams it lists through its leaf" \
      "fails" "$(fe_streams "$ZA_LEAF")" J11
check J32 D03-R33 "domain set, frontend names it (--js-domain core): streams it lists" \
      "ORDERS" "$(fe_streams "$ZA_LEAF" --js-domain "$DOMAIN")" J11
check J33 D03-R36 "with the domain: the ORDERS each frontend sees lives in, ZA / AU" \
      "za / au" \
      "$(nats_as "$ZA_LEAF" fe --js-domain "$DOMAIN" stream info ORDERS --json 2>/dev/null | jq -r '.cluster.name // "?"' 2>/dev/null || echo "?") / $(nats_as "$AU_LEAF" fe --js-domain "$DOMAIN" stream info ORDERS --json 2>/dev/null | jq -r '.cluster.name // "?"' 2>/dev/null || echo "?")"
# The leaf binds TENANT_ZA and nothing else, so the platform's own account must
# stay invisible -- asked now that the API works, so a "fails" means isolation
# and not "the API is blocked".
check J34 D03-R33 "with the domain: ZA frontend asks for LB_ZA's ODOMETER" \
      "fails" "$(fails_or_ok "$ZA_LEAF" fe --js-domain "$DOMAIN" stream info ODOMETER)"

nats_as "$ZA_LEAF" fe pub "$ORDERS" '{"id":10}' --jetstream >/dev/null 2>&1 || true
check J35 D03-R33 "with the domain: ZA frontend makes a pull consumer on ORDERS" \
      "ok" "$(fails_or_ok "$ZA_LEAF" fe --js-domain "$DOMAIN" consumer add ORDERS FE_ZA \
              --pull --deliver all --ack explicit --defaults)"
check J36 D03-R33 "with the domain: ZA frontend reads its own write back through it" \
      "ok" "$(fails_or_ok "$ZA_LEAF" fe --js-domain "$DOMAIN" consumer next ORDERS FE_ZA \
              --count 1 --timeout 5s)"
check J37 D03-R33 "with the domain: FE_ZA lives in cluster" \
      "za" "$(consumer_cluster 4231 tenant_za ORDERS FE_ZA)" F19

# --- AU dark, with the domain ------------------------------------------------
freeze au-1 au-2 au-3
wait_dark "${AU_HTTP[@]}"
LEADER_3="$(wait_live_leader 8231 '^t-(za|arb)')"
check J38 D03-R1 "domain set, AU dark: a leader is elected among the survivors" \
      "yes" "$([ "$LEADER_3" = "NONE" ] && echo no || echo yes)" J16
nats_as "$ZA_LEAF" fe pub "$ORDERS" '{"id":11}' --jetstream >/dev/null 2>&1 || true
check J39 D03-R1 "domain set, AU dark: ZA frontend reads ORDERS through its consumer" \
      "ok" "$(fails_or_ok "$ZA_LEAF" fe --js-domain "$DOMAIN" consumer next ORDERS FE_ZA \
              --count 1 --timeout 5s)"
check J40 D03-R34 "domain set, AU dark: streams the AU frontend lists" \
      "fails" "$(fe_streams "$AU_LEAF" --js-domain "$DOMAIN" --timeout 5s)"
thaw au-1 au-2 au-3
wait_meta_leader 8231 || true
sleep 8
check J41 D03-R5 "domain set, after recovery: meta group size" "9" "$(meta_size 8231)" J21

note J42 D03-R35 "what a tenant leaf costs the meta vote" \
     "nothing -- no JetStream on the leaf, so T4's arithmetic (6 of 9 against 5) is unchanged"
note J43 D03-R34 "what a tenant leaf does when its own region goes dark" \
     "its frontend is cut off with that region; the other region's tenant keeps working"
note J44 D03-R33 "what the frontend needs for more than publish and request" \
     "a domain on the regional side and --js-domain in the client; one name on all nine is enough"
note J45 D03-R36 "what an account per region costs" \
     "no backend failover between regions; that would need an explicit export / import, which T7 does not configure"

banner "$TOPOLOGY -- done"
