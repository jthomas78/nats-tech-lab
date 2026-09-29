#!/usr/bin/env bash
# D03-R11 / D03-R12 -- SWITCHING TOPOLOGY IN PLACE, T4 <-> T5.
#
# Every other script here builds its shape from an empty store. This one
# builds a shape, fills it, stops it, rewrites the configs to the OTHER shape
# over the SAME store directories, and starts it again. The question, from
# README.md: with traffic paused and stores kept, does every acknowledged
# message, consumer position and KV value survive -- and does it keep working?
#
# ---------------------------------------------------------------------------
# WHAT IS KEPT -- everything. Only the ROLE of each site changes.
# ---------------------------------------------------------------------------
#
#  * ALL NINE servers keep their server_name, client / monitor / route ports,
#    cluster name and store_dir. That includes the third site: in run A the
#    arbiter cluster (t-arb-*, cluster `arb`, 454x/654x) BECOMES the hub; in
#    run B the hub (t-hub-*, cluster `hub`, 455x/655x) BECOMES the arbiter.
#    The third site takes part in the meta group, so replacing it would be a
#    second variable. Replacing it is a later, separate procedure.
#  * The domain of a T5 third site is `hub`, whatever its cluster is called.
#  * A T4 third site gateways on 7<site>x (754x for arb, 755x for hub).
#    A T5 third site listens for leaf links on 7560-7562, as in 08.
#  * The T5 leaf binding is 08-hub-leaf-per-region.sh's: two remotes per
#    region server, LB plus the region's own account.
#
# ---------------------------------------------------------------------------
# THE SEED -- every message is traceable to one run, account, origin, stream
# ---------------------------------------------------------------------------
#
#  Streams, each R3, each keyed <account>@<origin>.<stream>:
#    za@za.ODOMETER   LB_ZA, in za, evt.odo.v1          (all runs)
#    au@au.ODOMETER   LB_AU, in au, evt.odo.v1          (all runs)
#    lb@za.SHARED_ODO LB,    in za, evt.shared.v1       (all runs, za ONLY)
#    lb@za.ODOMETER   LB,    in za, evt.lbodo.za        (B2 only)
#    lb@au.ODOMETER   LB,    in au, evt.lbodo.au        (B2 only)
#  B2's last two are THE collision: one name, one account, two domains.
#  Different subjects on purpose, so the name is the only clash.
#
#  * 50 publishes per stream with `nats pub -J`, which waits for the ack.
#    ID = <run>.<key>.<n>, sent as Nats-Msg-Id and inside the payload.
#    Only acked IDs enter the manifest, run/evidence/<stamp>/<run>/ids-<key>.tsv.
#  * Durable pull consumer READER on every stream: pull and ack 20.
#  * KV t7-vehicles, R3, LB_AU, in au: vehicle-1..5, then vehicle-1 twice
#    more. A revision is the BUCKET's stream sequence, not a per-key counter.
#
# ---------------------------------------------------------------------------
# THE PROCEDURE -- the same for every run, only the two shapes differ
# ---------------------------------------------------------------------------
#
#   1. lab_init (the ONLY one in the run), build the source shape, start it,
#      wait for its meta group(s). Check the shape.
#   2. Seed. Read the "before" state: each stream's cluster, each consumer's
#      ack floor / pending / ack pending, each KV key's value@revision.
#   3. STOP with TERM, and wait until each process is GONE: third site, then
#      au, then za. t0 is the first TERM.
#   4. Rewrite every config in place, same file names:
#        T4 -> T5  jetstream gains a domain (za / au / hub), the gateway
#                  block goes, region servers gain two leaf remotes, the
#                  third site gains a leaf listener.
#        T5 -> T4  the domain goes, the leaf blocks go, every server gains
#                  a three-way gateway (za / au / third site).
#      Both editions of every config are kept in the evidence folder.
#   5. START: third site first, then za, then au. Wait for each meta leader.
#   6. First acked publish into every stream -- the probe. Retries keep the
#      same Nats-Msg-Id, so a retry cannot store twice.
#   7. VERIFY against the manifests (checks, exact):
#        - the destination shape (meta groups and sizes)
#        - every stream: the stored (ID, payload) list equals the manifest,
#          in order -- nothing lost, nothing doubled, nothing changed
#        - every stream still sits in the same cluster
#        - every consumer: same ack floor, pending = manifest - floor,
#          nothing waiting for an ack
#        - every KV key: same value@revision
#   8. KEEP WORKING: 10 more acked publishes per stream; resume READER --
#      the next message must be manifest line 21; update vehicle-1 -- its
#      revision must be the bucket's last sequence + 1. Write the NEW
#      expected state (consumer floor 21, the new KV value@revision).
#   9. RESTART the destination once (TERM all nine, start again, same
#      configs) and repeat step 7 against the new expected state.
#  10. Stop. Keep the configs, logs and stores in run/evidence/<stamp>/<run>/.
#
#  Notes, never checks: seconds from t0 to each process gone, to each meta
#  leader, and to each first acked publish -- the OBSERVED interruption under
#  this stop/start procedure, which includes its own sequencing and waits --
#  and the [ERR] / [WRN] lines the servers log on the first start.
#
#  Runs:  A   T4 -> T5  (D03-R11)   check IDs SA*
#         B1  T5 -> T4  (D03-R12)   check IDs SB*   clean names
#         B2  T5 -> T4  (D03-R12)   check IDs SC*   the LB collision
#
# ---------------------------------------------------------------------------
# GUARDS
# ---------------------------------------------------------------------------
#
#  * lab_init runs once per run, before the build. Never after the seed --
#    it deletes run/js. Evidence goes to run/evidence/<stamp>/<run>/, which lab_init
#    does not touch. <stamp> is 09-<date-time>, one per whole run, so a
#    re-run never deletes an earlier run. The script copies itself there too.
#  * Every wait has a bound. A switch that never elects a leader is a RESULT:
#    the script carries on, the checks record what they find, and the stores
#    are kept. That is why `set -e` is off below.
#  * Only t-*.conf is ever written. The kill pattern stays `nats-server -c t-`.
#  * A check expects "nothing lost". Any other answer FAILS, and the verdict
#    for that direction is "failed" -- with the symptom, not a guessed cause.
#
# Requirements exercised: D03-R11 (run A), D03-R12 (runs B1 and B2).

source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

# A conversion that breaks the cluster is the thing being measured. It must
# reach the checks and the evidence copy, not abort half-way. Every wait below
# is bounded instead.
set +e

SEED_N=50        # acked publishes per stream before the switch
SEED_ACK=20      # of those, acked by READER before the switch
WORK_N=10        # acked publishes per stream after the switch
KV_BUCKET=t7-vehicles

# ---------------------------------------------------------------------------
# Config builders. One region server, one third-site server, either shape.
# Rewriting a shape = calling these again over the same file names.
# ---------------------------------------------------------------------------

# A three-way gateway. $1 name  $2 my gw name  $3 my gw port
#                      $4 third site gw name  $5 its gw port prefix (754|755)
gw_mesh() {
  local name="$1" gw="$2" mine="$3" third="$4" tp="$5" f="$RUN_DIR/t-$1.conf"
  {
    printf 'gateway {\n  name: %s\n  listen: 127.0.0.1:%s\n  gateways: [\n' "$gw" "$mine"
    [ "$gw" != za ] && printf '    { name: za, urls: [ nats://127.0.0.1:7231, nats://127.0.0.1:7232, nats://127.0.0.1:7233 ] },\n'
    [ "$gw" != au ] && printf '    { name: au, urls: [ nats://127.0.0.1:7241, nats://127.0.0.1:7242, nats://127.0.0.1:7243 ] },\n'
    [ "$gw" != "$third" ] && printf '    { name: %s, urls: [ nats://127.0.0.1:%s1, nats://127.0.0.1:%s2, nats://127.0.0.1:%s3 ] },\n' \
      "$third" "$tp" "$tp" "$tp"
    printf '  ]\n}\n'
  } >> "$f"
}

# Two leaf remotes per region server, as leaf_remotes_2 in 08.
# $1 name  $2 region account  $3 its user
leaf_remotes_2() {
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

# $1 region (za|au)  $2 index  $3 shape (t4|t5)
region_conf() {
  local r="$1" i="$2" shape="$3" n="$1-$2" p acct
  case "$r" in za) p=23; acct=LB_ZA ;; au) p=24; acct=LB_AU ;; esac
  if [ "$shape" = t4 ]; then
    conf_head "$n" "4$p$i" "8$p$i"
  else
    conf_head "$n" "4$p$i" "8$p$i" "$r"
  fi
  conf_cluster "$n" "$r" "6$p$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '6%s%s ' "$p" $j; done)
  if [ "$shape" = t4 ]; then
    gw_mesh "$n" "$r" "7$p$i" "$THIRD" "7$TP"
  else
    leaf_remotes_2 "$n" "$acct" "$r"
  fi
  conf_accounts "$n"
}

# $1 index  $2 shape. The third site keeps its own name, cluster and ports in
# both shapes; only its link and its domain change.
third_conf() {
  local i="$1" shape="$2" n="$THIRD-$1"
  if [ "$shape" = t4 ]; then
    conf_head "$n" "4$TP$i" "8$TP$i"
  else
    conf_head "$n" "4$TP$i" "8$TP$i" hub
  fi
  conf_cluster "$n" "$THIRD" "6$TP$i" $(for j in 1 2 3; do [ $j -ne $i ] && printf '6%s%s ' "$TP" $j; done)
  if [ "$shape" = t4 ]; then
    gw_mesh "$n" "$THIRD" "7$TP$i" "$THIRD" "7$TP"
  else
    printf 'leafnodes { listen: 127.0.0.1:756%s }\n' "$((i-1))" >> "$RUN_DIR/t-$n.conf"
  fi
  conf_accounts "$n"
}

write_shape() {                 # $1 t4|t5
  local i
  for i in 1 2 3; do
    third_conf "$i" "$1"
    region_conf za "$i" "$1"
    region_conf au "$i" "$1"
  done
  mkdir -p "$EVID/conf-$1"
  cp "$RUN_DIR"/t-*.conf "$EVID/conf-$1/"
}

# ---------------------------------------------------------------------------
# Stopping and starting, bounded
# ---------------------------------------------------------------------------

THIRD_NAMES() { echo "$THIRD-1 $THIRD-2 $THIRD-3"; }
ALL_HTTP() { echo "8${TP}1 8${TP}2 8${TP}3 8231 8232 8233 8241 8242 8243"; }

# Wait until a process has EXITED, not merely been signalled. A store is not
# safe to reopen under another config while its old owner is still flushing.
# Returns 1 if it had to be killed hard, which the caller records.
wait_gone() {
  local pid="$1" i
  for ((i=0; i<60; i++)); do
    kill -0 "$pid" 2>/dev/null || return 0
    sleep 0.5
  done
  kill -KILL "$pid" 2>/dev/null
  return 1
}

STOP_HARD=""
stop_group() {
  local n pid pids=""
  for n in "$@"; do
    pid="$(pid_of "$n")"
    kill -TERM "$pid" 2>/dev/null
    pids+="$n:$pid "
  done
  for n in $pids; do
    wait_gone "${n#*:}" || STOP_HARD+="${n%%:*} "
  done
}

# Third site, then au, then za. Sets STOP_SECS and STOP_HARD -- globals, not
# an echo, because a $(...) subshell would lose STOP_HARD.
stop_shape() {
  local t=$(date +%s)
  STOP_HARD=""
  stop_group $(THIRD_NAMES)
  stop_group au-1 au-2 au-3
  stop_group za-1 za-2 za-3
  STOP_SECS=$(( $(date +%s) - t ))
}

wait_up() {                     # $1 monitor port -- the process answers at all
  local i
  for ((i=0; i<60; i++)); do
    curl -fs "http://127.0.0.1:$1/varz" >/dev/null 2>&1 && return 0
    sleep 0.5
  done
  return 1
}

NOT_UP=""
start_shape() {
  local n i
  NOT_UP=""
  for n in $(THIRD_NAMES); do start_server "$n"; done
  for i in 1 2 3; do wait_up "8$TP$i" || NOT_UP+="$THIRD-$i "; done
  for n in za-1 za-2 za-3 au-1 au-2 au-3; do start_server "$n"; done
  for i in 1 2 3; do
    wait_up "823$i" || NOT_UP+="za-$i "
    wait_up "824$i" || NOT_UP+="au-$i "
  done
}

# Seconds from $T0 until each system names a LIVE leader, bounded at 90 s.
LEADER_T=""
wait_leaders() {                # $1 shape
  local l
  LEADER_T=""
  if [ "$1" = t4 ]; then
    l="$(wait_live_leader 8231 '^t-' 90)"
    LEADER_T="one group: $l after $(( $(date +%s) - T0 ))s"
  else
    l="$(wait_live_leader "8${TP}1" "^t-$THIRD" 90)"
    LEADER_T="third site: $l after $(( $(date +%s) - T0 ))s"
    l="$(wait_live_leader 8231 '^t-za' 90)"
    LEADER_T+="; za: $l after $(( $(date +%s) - T0 ))s"
    l="$(wait_live_leader 8241 '^t-au' 90)"
    LEADER_T+="; au: $l after $(( $(date +%s) - T0 ))s"
  fi
}

shape_now() {                   # $1 shape -- the string its check compares
  if [ "$1" = t4 ]; then
    echo "$(meta_group_count $(ALL_HTTP)) / $(meta_size 8231)"
  else
    echo "$(meta_group_count $(ALL_HTTP)) / $(meta_size "8${TP}1") / $(meta_size 8231) / $(meta_size 8241)"
  fi
}
shape_want() { [ "$1" = t4 ] && echo "1 / 9" || echo "3 / 3 / 3 / 3"; }
shape_desc() {
  [ "$1" = t4 ] && echo "T4: meta groups / size (one group of nine)" \
                || echo "T5: meta groups / size of third site, za, au (three of three)"
}

save_logs() {                   # $1 phase
  mkdir -p "$EVID/logs-$1"
  cp "$RUN_DIR"/log/*.log "$EVID/logs-$1/" 2>/dev/null
}

# [ERR] / [WRN] lines from one phase's logs: how many, and the first few
# distinct ones with the timestamp cut off. A note, never a check.
log_trouble() {                 # $1 phase
  local d="$EVID/logs-$1" n first
  n="$(cat "$d"/*.log 2>/dev/null | grep -cE '\[(ERR|WRN)\]')"
  first="$(cat "$d"/*.log 2>/dev/null | grep -E '\[(ERR|WRN)\]' \
           | sed -E 's/^.*\[(ERR|WRN)\] /[\1] /' | sort -u | head -3 | tr '\n' ' ' | cut -c1-300)"
  echo "$n lines${first:+ -- $first}"
}

# ---------------------------------------------------------------------------
# Ids, manifests and reading back
# ---------------------------------------------------------------------------

CN=0
nid() { CN=$((CN+1)); ID="$P$CN"; }

manifest() { echo "$EVID/ids-$1.tsv"; }
lines_of() { wc -l < "$1" | tr -d ' '; }

# One acked publish. Appends to the manifest ONLY when JetStream acked it.
# $1 key  $2 port  $3 user  $4 subject  $5 ordinal
acked_pub() {
  local id="$RUN.$1.$5" payload
  payload="{\"id\":\"$id\",\"n\":$5}"
  if nats_as "$2" "$3" pub -J --no-templates -H "Nats-Msg-Id:$id" "$4" "$payload" >/dev/null 2>&1; then
    printf '%s\t%s\n' "$id" "$payload" >> "$(manifest "$1")"
    return 0
  fi
  return 1
}

# Every stored message, in sequence order, as <Nats-Msg-Id> TAB <payload>.
dump_stream() {                 # $1 port  $2 user  $3 stream
  local info first last seq
  info="$(nats_as "$1" "$2" stream info "$3" --json 2>/dev/null)" || return 1
  first="$(jq -r '.state.first_seq' <<<"$info")"
  last="$(jq -r '.state.last_seq' <<<"$info")"
  [ "$last" -ge 1 ] 2>/dev/null || return 0
  for ((seq=first; seq<=last; seq++)); do
    nats_as "$1" "$2" stream get "$3" "$seq" --json 2>/dev/null \
      | jq -r '[ ([ .hdrs // "" | @base64d | scan("Nats-Msg-Id: ([^\r\n]*)") | .[0] ] | .[0] // "-"),
                 (.data // "" | @base64d) ] | @tsv' 2>/dev/null
  done
}

# "match", or what is wrong. Matches ID AND payload, in order -- never a count.
verify_msgs() {                 # $1 key  $2 port  $3 user  $4 stream  $5 phase
  local man got missing extra
  man="$(manifest "$1")"; got="$EVID/got-$1-$5.tsv"
  dump_stream "$2" "$3" "$4" > "$got" || { echo "stream unreadable"; return; }
  if cmp -s "$man" "$got"; then echo "match"; return; fi
  missing="$(comm -23 <(sort "$man") <(sort "$got") | wc -l | tr -d ' ')"
  extra="$(comm -13 <(sort "$man") <(sort "$got") | wc -l | tr -d ' ')"
  echo "differs: $(lines_of "$man") acked, $(lines_of "$got") stored, $missing missing, $extra extra"
}

cons_state() {                  # $1 port  $2 user  $3 stream
  local s
  s="$(nats_as "$1" "$2" consumer info "$3" READER --json 2>/dev/null \
       | jq -r '"floor \(.ack_floor.stream_seq) / pending \(.num_pending) / ack pending \(.num_ack_pending)"' 2>/dev/null)"
  echo "${s:-unreadable}"
}

kv_state() {
  local k s out=""
  for k in 1 2 3 4 5; do
    s="$(nats_as 4241 au stream get "KV_$KV_BUCKET" --last-for "\$KV.$KV_BUCKET.vehicle-$k" --json 2>/dev/null \
         | jq -r '"\(.data | @base64d)@\(.seq)"' 2>/dev/null)"
    out+="vehicle-$k=${s:-unreadable} "
  done
  echo "${out% }"
}

kv_last_seq() {
  nats_as 4241 au stream info "KV_$KV_BUCKET" --json 2>/dev/null | jq -r '.state.last_seq // "?"' 2>/dev/null
}

# ---------------------------------------------------------------------------
# The seed
# ---------------------------------------------------------------------------

# One line per stream:  key port user stream subject cluster-flag
set_streams() {
  STREAMS=(
    "za@za.ODOMETER   4231 za ODOMETER   evt.odo.v1    za"
    "au@au.ODOMETER   4241 au ODOMETER   evt.odo.v1    au"
    "lb@za.SHARED_ODO 4231 lb SHARED_ODO evt.shared.v1 za"
  )
  if [ "$RUN" = B2 ]; then
    STREAMS+=(
      "lb@za.ODOMETER 4231 lb ODOMETER evt.lbodo.za za"
      "lb@au.ODOMETER 4241 lb ODOMETER evt.lbodo.au au"
    )
  fi
}

seed() {
  local s key port user stream subj clu n flag acked=""
  for s in "${STREAMS[@]}"; do
    read -r key port user stream subj clu <<<"$s"
    : > "$(manifest "$key")"
    # T4 places by cluster name, as 06 does. T5 lands where the client is.
    flag=""; [ "$FROM" = t4 ] && flag="--cluster $clu"
    nats_as "$port" "$user" stream add "$stream" --subjects "$subj" --storage file \
            --replicas 3 $flag --defaults >/dev/null 2>&1
  done
  sleep 3
  for s in "${STREAMS[@]}"; do
    read -r key port user stream subj clu <<<"$s"
    for ((n=1; n<=SEED_N; n++)); do acked_pub "$key" "$port" "$user" "$subj" "$n"; done
    nats_as "$port" "$user" consumer add "$stream" READER --pull --deliver all \
            --ack explicit --defaults >/dev/null 2>&1
    nats_as "$port" "$user" consumer next "$stream" READER --count "$SEED_ACK" \
            --raw --timeout 10s >/dev/null 2>&1
    acked+="$(lines_of "$(manifest "$key")") / "
  done
  SEED_ACKED="${acked% / }"

  nats_as 4241 au kv add "$KV_BUCKET" --storage file --replicas 3 >/dev/null 2>&1
  for n in 1 2 3 4 5; do nats_as 4241 au kv put "$KV_BUCKET" "vehicle-$n" "$RUN-k$n-v1" >/dev/null 2>&1; done
  nats_as 4241 au kv put "$KV_BUCKET" vehicle-1 "$RUN-k1-v2" >/dev/null 2>&1
  nats_as 4241 au kv put "$KV_BUCKET" vehicle-1 "$RUN-k1-v3" >/dev/null 2>&1
  sleep 2
}

# Read the reference state. Cluster and consumer per stream, KV once.
before_state() {
  local s key port user stream subj clu
  for s in "${STREAMS[@]}"; do
    read -r key port user stream subj clu <<<"$s"
    stream_cluster "$port" "$user" "$stream" > "$EVID/cluster-$key"
    cons_state "$port" "$user" "$stream" > "$EVID/cons-$key"
  done
  KV_EXPECT="$(kv_state)"
  echo "$KV_EXPECT" > "$EVID/kv-before"
}

# ---------------------------------------------------------------------------
# The verification pass -- used after the switch and after the restart
# ---------------------------------------------------------------------------

# $1 phase label  $2 shape  $3 the consumer floor every READER must be at
verify_all() {
  local phase="$1" shape="$2" floor="$3" s key port user stream subj clu man
  nid; pcheck "$ID" "$REQ" "$phase: $(shape_desc "$shape")" "$(shape_want "$shape")" "$(shape_now "$shape")"
  for s in "${STREAMS[@]}"; do
    read -r key port user stream subj clu <<<"$s"
    man="$(manifest "$key")"
    nid; pcheck "$ID" "$REQ" "$phase: $key -- every acked ID and payload, in order" \
          "match" "$(verify_msgs "$key" "$port" "$user" "$stream" "$phase")"
    nid; pcheck "$ID" "$REQ" "$phase: $key -- still in the cluster it was in before" \
          "$(cat "$EVID/cluster-$key")" "$(stream_cluster "$port" "$user" "$stream")"
    nid; pcheck "$ID" "$REQ" "$phase: $key -- READER's ack floor / pending / ack pending" \
          "floor $floor / pending $(( $(lines_of "$man") - floor )) / ack pending 0" \
          "$(cons_state "$port" "$user" "$stream")"
  done
  nid; pcheck "$ID" "$REQ" "$phase: KV $KV_BUCKET -- every key's value@revision" \
        "$KV_EXPECT" "$(kv_state)"
}

# ---------------------------------------------------------------------------
# Rig checks and procedure checks
# ---------------------------------------------------------------------------
#
# Two questions, kept apart (see KIND in _common.sh):
#   rig        did this run build its source shape, seed it, and keep its
#              evidence? A rig FAIL makes the run inconclusive, and the
#              script exits non-zero -- the rig is broken, not the procedure.
#   procedure  did the switch keep every acked message, consumer position and
#              KV value, reach the target shape, and keep working? A FAIL
#              here stays a FAIL. It is the answer, not a broken rig.
# Each run ends with one VERDICT row worked out from both.

rcheck() {
  local k="$KIND"; KIND=rig; check "$@"; KIND="$k"
  [ "$4" = "$5" ] || RIG_FAIL=$((RIG_FAIL+1))
}
pcheck() {
  local k="$KIND"; KIND=procedure; check "$@"; KIND="$k"
  PROC_N=$((PROC_N+1))
  [ "$4" = "$5" ] || PROC_FAIL=$((PROC_FAIL+1))
}

# "complete", or what is missing. Run after the stores are moved in.
evidence_state() {
  local miss="" s key d
  for s in "${STREAMS[@]}"; do
    key="${s%% *}"
    [ -s "$(manifest "$key")" ] || miss+="manifest $key; "
  done
  for d in "conf-t4" "conf-t5"; do
    [ "$(ls "$EVID/$d"/t-*.conf 2>/dev/null | wc -l | tr -d ' ')" = 9 ] || miss+="$d; "
  done
  for d in "logs-1-$FROM" "logs-2-$TO" "logs-3-restart"; do
    ls "$EVID/$d"/*.log >/dev/null 2>&1 || miss+="$d; "
  done
  [ -d "$EVID/js" ] || miss+="stores; "
  echo "${miss:+missing: ${miss%; }}${miss:-complete}"
}

# ---------------------------------------------------------------------------
# One run
# ---------------------------------------------------------------------------

# $1 run (A|B1|B2)  $2 check prefix  $3 source shape (t4|t5)
# $4 third site name (arb|hub)  $5 its port digits (54|55)  $6 requirement
run_switch() {
  RUN="$1"; P="$2"; FROM="$3"; THIRD="$4"; TP="$5"; REQ="$6"; CN=0
  KIND=rig; RIG_FAIL=0; PROC_FAIL=0; PROC_N=0
  if [ "$FROM" = t4 ]; then TO=t5; else TO=t4; fi
  TOPOLOGY="$7"
  EVID="$RUN_DIR/evidence/$STAMP/$RUN"
  local s key port user stream subj clu n t_ok got secs probe_t="" ok

  lab_init
  mkdir -p "$EVID"
  banner "$TOPOLOGY"
  set_streams

  # --- 1. The source shape ---------------------------------------------------
  write_shape "$FROM"
  start_shape
  T0=$(date +%s); wait_leaders "$FROM"
  sleep 10
  nid; rcheck "$ID" "$REQ" "before: $(shape_desc "$FROM")" "$(shape_want "$FROM")" "$(shape_now "$FROM")"

  # --- 2. Seed and read the reference state ---------------------------------
  seed
  before_state
  nid; rcheck "$ID" "$REQ" "before: acked publishes per stream ($(for s in "${STREAMS[@]}"; do printf '%s ' "${s%% *}"; done| sed 's/ $//'))" \
        "$(for s in "${STREAMS[@]}"; do printf '%s / ' "$SEED_N"; done | sed 's| / $||')" "$SEED_ACKED"
  nid; rcheck "$ID" "$REQ" "before: READER on every stream -- ack floor / pending / ack pending" \
        "$(for s in "${STREAMS[@]}"; do printf 'floor %s / pending %s / ack pending 0; ' "$SEED_ACK" $((SEED_N-SEED_ACK)); done | sed 's/; $//')" \
        "$(for s in "${STREAMS[@]}"; do printf '%s; ' "$(cat "$EVID/cons-${s%% *}")"; done | sed 's/; $//')"
  nid; note "$ID" "$REQ" "before: KV $KV_BUCKET, key=value@revision" "$KV_EXPECT"

  # --- 3. Stop ---------------------------------------------------------------
  # From here to step 10, every row judges the procedure, not the rig.
  KIND=procedure
  T0=$(date +%s)
  stop_shape
  save_logs "1-$FROM"           # after the stop, so the shutdown lines are in
  nid; note "$ID" "$REQ" "stop: seconds from the first TERM until all nine exited" \
        "${STOP_SECS}s${STOP_HARD:+ -- had to SIGKILL: $STOP_HARD}"

  # --- 4. Rewrite, 5. start ---------------------------------------------------
  write_shape "$TO"
  start_shape
  [ -n "$NOT_UP" ] && { nid; note "$ID" "$REQ" "switch: servers that never answered /varz" "$NOT_UP"; }
  wait_leaders "$TO"
  nid; note "$ID" "$REQ" "switch: seconds from t0 to a live meta leader" "$LEADER_T"

  # --- 6. The probe: the first acked publish per stream -----------------------
  for s in "${STREAMS[@]}"; do
    read -r key port user stream subj clu <<<"$s"
    n=$(( $(lines_of "$(manifest "$key")") + 1 )); ok=no
    for _ in $(seq 1 30); do
      if acked_pub "$key" "$port" "$user" "$subj" "$n"; then ok=yes; break; fi
      sleep 2
    done
    if [ "$ok" = yes ]; then probe_t+="$key $(( $(date +%s) - T0 ))s; "
    else probe_t+="$key no ack; "; fi
  done
  nid; note "$ID" "$REQ" "switch: seconds from t0 to the first acked publish (observed interruption, this procedure)" \
        "${probe_t%; }"
  save_logs "2-$TO"
  nid; note "$ID" "$REQ" "switch: [ERR]/[WRN] lines logged on the first start in the new shape" \
        "$(log_trouble "2-$TO")"
  [ "$TO" = t5 ] && sleep 5     # leaf interest, for anything crossing the hub

  # --- 7. Verify --------------------------------------------------------------
  verify_all "after switch" "$TO" "$SEED_ACK"

  if [ "$RUN" = B2 ]; then
    nid; note "$ID" "$REQ" "collision: streams in account LB seen from za / from au" \
          "$(nats_as 4231 lb stream ls --names 2>/dev/null | sort | tr '\n' ' ')/ $(nats_as 4241 lb stream ls --names 2>/dev/null | sort | tr '\n' ' ')"
    nid; note "$ID" "$REQ" "collision: LB ODOMETER from za -- cluster, messages / from au -- cluster, messages" \
          "$(stream_cluster 4231 lb ODOMETER), $(stream_msgs 4231 lb ODOMETER) / $(stream_cluster 4241 lb ODOMETER), $(stream_msgs 4241 lb ODOMETER)"
  fi

  # --- 8. Keep working --------------------------------------------------------
  for s in "${STREAMS[@]}"; do
    read -r key port user stream subj clu <<<"$s"
    t_ok=0
    for ((n=1; n<=WORK_N; n++)); do
      acked_pub "$key" "$port" "$user" "$subj" $(( $(lines_of "$(manifest "$key")") + 1 )) && t_ok=$((t_ok+1))
    done
    nid; pcheck "$ID" "$REQ" "keep working: $key -- new publishes acked" "$WORK_N" "$t_ok"
    got="$(nats_as "$port" "$user" consumer next "$stream" READER --count 1 --raw --timeout 5s 2>/dev/null | head -1)"
    nid; pcheck "$ID" "$REQ" "keep working: $key -- READER resumes at the first un-acked message" \
          "$(sed -n "$((SEED_ACK+1))p" "$(manifest "$key")" | cut -f2)" "${got:-nothing}"
  done
  n="$(kv_last_seq)"
  nats_as 4241 au kv put "$KV_BUCKET" vehicle-1 "$RUN-k1-v4" >/dev/null 2>&1
  sleep 1
  got="$(kv_state | tr ' ' '\n' | grep '^vehicle-1=' | cut -d= -f2)"
  nid; pcheck "$ID" "$REQ" "keep working: vehicle-1 update -- value@revision (bucket's last sequence + 1)" \
        "$RUN-k1-v4@$(( n + 1 ))" "$got"
  # The NEW expected state: what the restart is compared against.
  KV_EXPECT="$(echo "$KV_EXPECT" | sed -E "s/vehicle-1=[^ ]*/vehicle-1=$RUN-k1-v4@$(( n + 1 ))/")"
  echo "$KV_EXPECT" > "$EVID/kv-after-work"

  # --- 9. Restart the destination once ---------------------------------------
  T0=$(date +%s)
  stop_shape
  save_logs "2-$TO"             # again, now with the shutdown lines
  start_shape
  wait_leaders "$TO"
  nid; note "$ID" "$REQ" "restart: stop seconds, then seconds from t0 to a live meta leader" \
        "stop ${STOP_SECS}s${STOP_HARD:+ (SIGKILL: $STOP_HARD)}; $LEADER_T${NOT_UP:+; never answered: $NOT_UP}"
  sleep 5
  verify_all "after restart" "$TO" "$((SEED_ACK+1))"

  # --- 10. Keep the evidence --------------------------------------------------
  stop_shape
  save_logs "3-restart"
  mv "$RUN_DIR/js" "$EVID/js"
  KIND=rig
  nid; rcheck "$ID" "$REQ" "evidence: manifests, both shapes' configs, logs of every phase, the stores" \
        "complete" "$(evidence_state)"

  # --- 11. The verdict ---------------------------------------------------------
  # inconclusive  the rig failed, so the run proves nothing either way
  # failed        the rig held, and at least one procedure check was not met
  # passed ...    the rig held, and every procedure check was met; the
  #               interruption is the switch notes above, not a pass mark
  local words
  if   [ "$RIG_FAIL" -gt 0 ];  then words="inconclusive"
  elif [ "$PROC_FAIL" -gt 0 ]; then words="failed"
  else                              words="passed with a measured interruption"; fi
  nid; verdict "$ID" "$REQ" "the stop-rewrite-restart procedure, $FROM to $TO" "$words" \
        "rig checks failed: $RIG_FAIL; procedure checks not met: $PROC_FAIL of $PROC_N"
  [ "$RIG_FAIL" -eq 0 ] || RIG_BROKEN+="$RUN "
  banner "$TOPOLOGY -- done"
}

RIG_BROKEN=""
STAMP="09-$(date +%Y%m%d-%H%M%S)"   # one folder per whole run; never reused, never deleted
mkdir -p "$RUN_DIR/evidence/$STAMP"
cp "${BASH_SOURCE[0]}" "$RUN_DIR/evidence/$STAMP/"
run_switch A  SA t4 arb 54 D03-R11 "T4 / S -- switch in place, T4 to T5"
run_switch B1 SB t5 hub 55 D03-R12 "T5 / S -- switch in place, T5 to T4, clean names"
run_switch B2 SC t5 hub 55 D03-R12 "T5 / S -- switch in place, T5 to T4, LB name collision"

# A failed procedure is an answer, so it exits 0. Only a broken rig does not.
if [ -n "$RIG_BROKEN" ]; then
  printf '\n  rig failed in run(s): %s-- those verdicts are inconclusive\n' "$RIG_BROKEN"
  exit 1
fi
