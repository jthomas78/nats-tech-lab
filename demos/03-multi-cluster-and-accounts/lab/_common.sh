# Shared bits for the demo 03 topology labs. Sourced, never run on its own.
#
# Demo 03 asks ONE question, five times, once per topology:
#
#   When one region goes dark, who is still alive to take a JetStream write?
#
# So every script here does the same four things:
#
#   1. write its own config files       (always `t-` prefixed -- see below)
#   2. start its own servers            (bare nats-server, no Docker)
#   3. measure                          (meta group, stream results, error codes)
#   4. kill everything it started       (EXIT trap, always)
#
# Each script is self-contained. There is no `up.sh` to run first, unlike
# demo 02. That is deliberate: the whole point of this folder is that a
# stranger can re-run the evidence with nothing but the four host tools.

set -euo pipefail

# ---------------------------------------------------------------------------
# THE `t-` PREFIX RULE. Read demos/03-multi-cluster-and-accounts/CLAUDE.md.
#
# Every generated config file and every generated server_name starts `t-`, and
# every server is started from INSIDE $RUN_DIR with a RELATIVE path, so its
# command line reads `nats-server -c t-za-1.conf`. That is what makes the only
# kill pattern in this lab safe:
#
#     pkill -f "nats-server -c t-"
#
# It cannot match the six committed configs in the parent folder, which is how
# this lab once killed the user's live rig. Twice. Do not "tidy" this by using
# absolute paths -- the prefix would stop matching and the guard would be gone.
# ---------------------------------------------------------------------------

LAB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RUN_DIR="$LAB_DIR/run"
RESULTS="$RUN_DIR/results.tsv"
KILL_PATTERN='nats-server -c t-'

TOPOLOGY="${TOPOLOGY:-?}"

need_tools() {
  local missing=0 t
  for t in nats-server nats jq curl; do
    command -v "$t" >/dev/null 2>&1 || { echo "missing tool: $t" >&2; missing=1; }
  done
  [ "$missing" -eq 0 ] || {
    echo "install with: brew install nats-io/nats-tools/nats nats-io/nats-tools/nats-server jq" >&2
    exit 1
  }
}

lab_init() {
  need_tools
  rm -rf "$RUN_DIR"/{log,pid,js}
  rm -f "$RUN_DIR"/t-*.conf
  mkdir -p "$RUN_DIR"/{log,pid,js}
  trap lab_down EXIT INT TERM
  lab_down_quiet
}

# ---------------------------------------------------------------------------
# Config building
# ---------------------------------------------------------------------------

# The three business accounts plus $SYS, identical in every server of every
# topology. Held still on purpose -- the topology is the only variable.
#   LB     the SHARED account that spans both regions (the broken shape)
#   LB_ZA  ZA owns its own streams
#   LB_AU  AU owns its own streams
# user == password everywhere. Lab-only, 127.0.0.1 only, worthless elsewhere.
accounts_block() {
  cat <<'EOF'
accounts {
  $SYS { users: [ { user: admin, password: admin } ] }
  LB_ZA { jetstream: enabled, users: [ { user: za, password: za } ] }
  LB_AU { jetstream: enabled, users: [ { user: au, password: au } ] }
  LB    { jetstream: enabled, users: [ { user: lb, password: lb } ] }
}
EOF
}

# Open a config file and write the part every server shares.
#   $1 short name (za-1)   $2 client port   $3 monitor port   [$4 domain]
conf_head() {
  local name="$1" client="$2" http="$3" domain="${4:-}"
  local f="$RUN_DIR/t-$name.conf"
  {
    printf 'server_name: t-%s\n' "$name"
    printf 'listen: 127.0.0.1:%s\n' "$client"
    printf 'http: 127.0.0.1:%s\n' "$http"
    if [ -n "$domain" ]; then
      printf 'jetstream { store_dir: "./js/%s", domain: %s }\n' "$name" "$domain"
    else
      printf 'jetstream { store_dir: "./js/%s" }\n' "$name"
    fi
  } > "$f"
}

# Append a cluster block. $1 name  $2 cluster  $3 my route port  $4.. peer ports
conf_cluster() {
  local name="$1" cluster="$2" mine="$3"; shift 3
  local f="$RUN_DIR/t-$name.conf" routes="" p
  for p in "$@"; do routes+="nats://127.0.0.1:$p, "; done
  {
    printf 'cluster {\n  name: %s\n  listen: 127.0.0.1:%s\n' "$cluster" "$mine"
    printf '  routes: [ %s ]\n}\n' "${routes%, }"
  } >> "$f"
}

# Append a gateway block. $1 name  $2 my gw name  $3 my gw port
#                         $4 peer gw name  $5.. peer gw ports
conf_gateway() {
  local name="$1" gw="$2" mine="$3" peer="$4"; shift 4
  local f="$RUN_DIR/t-$name.conf" urls="" p
  for p in "$@"; do urls+="nats://127.0.0.1:$p, "; done
  {
    printf 'gateway {\n  name: %s\n  listen: 127.0.0.1:%s\n' "$gw" "$mine"
    printf '  gateways: [ { name: %s, urls: [ %s ] } ]\n}\n' "$peer" "${urls%, }"
  } >> "$f"
}

conf_accounts() { accounts_block >> "$RUN_DIR/t-$1.conf"; }

# ---------------------------------------------------------------------------
# Running servers
# ---------------------------------------------------------------------------

# Start one server. Relative path on purpose -- see the `t-` prefix rule above.
start_server() {
  local name="$1"
  pushd "$RUN_DIR" >/dev/null
  nats-server -c "t-$name.conf" > "log/$name.log" 2>&1 &
  echo $! > "$RUN_DIR/pid/$name.pid"
  popd >/dev/null
}

pid_of() { cat "$RUN_DIR/pid/$1.pid"; }

# Wait for /healthz on a monitor port. A JetStream cluster member is not ready
# the moment the process exists -- it still has to find a route and vote.
wait_ready() {
  local http="$1" tries="${2:-60}" i
  for ((i=0; i<tries; i++)); do
    if curl -fs "http://127.0.0.1:$http/healthz" >/dev/null 2>&1; then return 0; fi
    sleep 0.5
  done
  echo "server on monitor port $http never became healthy" >&2
  return 1
}

# MEASUREMENT TRAP, found 2026-09-17. `/jsz?meta=1` reports a STALE `leader`
# for tens of seconds after the peers holding the majority go dark. Read it too
# early and a frozen supercluster looks perfectly healthy. Always wait for the
# field to clear before recording "no leader", and treat a `stream add` timeout
# -- not only a 10008 -- as the real signal that management is frozen.
#
# Returns the whole seconds it took for the leader field to clear.
seconds_until_no_leader() {
  local http="$1" max="${2:-120}" i start
  start=$(date +%s)
  for ((i=0; i<max*2; i++)); do
    if [ "$(meta_leader "$http")" = "NONE" ]; then
      echo $(( $(date +%s) - start )); return 0
    fi
    sleep 0.5
  done
  echo "still-elected-after-${max}s"
  return 0
}

wait_no_leader() { seconds_until_no_leader "$@" >/dev/null; }

# Wait until the meta leader is a server that is still ALIVE. Needed because of
# the stale-leader trap above: right after a failure the field can still name a
# server that is already dark, so "a leader exists" is not the same as "a leader
# was re-elected". $2 is a regex the surviving leader's name must match.
wait_live_leader() {
  local http="$1" re="$2" max="${3:-120}" i l
  for ((i=0; i<max*2; i++)); do
    l="$(meta_leader "$http")"
    if [[ "$l" =~ $re ]]; then echo "$l"; return 0; fi
    sleep 0.5
  done
  echo "NONE"
  return 0
}

# Wait for a meta leader to be elected on this JetStream system.
wait_meta_leader() {
  local http="$1" tries="${2:-40}" i leader
  for ((i=0; i<tries; i++)); do
    leader="$(meta_leader "$http")"
    [ "$leader" != "NONE" ] && [ -n "$leader" ] && return 0
    sleep 0.5
  done
  return 1
}

lab_down_quiet() { pkill -f "$KILL_PATTERN" >/dev/null 2>&1 || true; sleep 1; }

lab_down() {
  local rc=$?
  pkill -CONT -f "$KILL_PATTERN" >/dev/null 2>&1 || true
  pkill -f "$KILL_PATTERN" >/dev/null 2>&1 || true
  sleep 1
  return $rc
}

# A region goes dark. kill -STOP on the PROCESSES is the only honest cut here.
# Never `docker network disconnect` -- it drops published host ports too, and
# every reading afterwards is a Docker artifact, not a NATS one.
freeze() { local n; for n in "$@"; do kill -STOP "$(pid_of "$n")" 2>/dev/null || true; done; sleep 3; }

# Prove a frozen region really stopped answering. A freeze that silently did
# nothing reads exactly like a finding, and that is how a lab lies to you.
wait_dark() {
  local http tries i ok
  for http in "$@"; do
    ok=1
    for ((i=0; i<20; i++)); do
      if curl -fs --max-time 1 "http://127.0.0.1:$http/healthz" >/dev/null 2>&1; then
        sleep 0.5
      else ok=0; break; fi
    done
    [ "$ok" -eq 0 ] || { echo "monitor port $http still answers -- the freeze did not work" >&2; return 1; }
  done
  return 0
}
thaw()   { local n; for n in "$@"; do kill -CONT "$(pid_of "$n")" 2>/dev/null || true; done; sleep 3; }
kill_server() { local n; for n in "$@"; do kill -TERM "$(pid_of "$n")" 2>/dev/null || true; done; sleep 3; }

# ---------------------------------------------------------------------------
# Reading the answer -- from the meta group, never from the logs
# ---------------------------------------------------------------------------

jsz() { curl -fs "http://127.0.0.1:$1/jsz?meta=1" 2>/dev/null; }

meta_leader() { jsz "$1" | jq -r '.meta_cluster.leader // "NONE"' 2>/dev/null || echo "NONE"; }
meta_size()   { jsz "$1" | jq -r '.meta_cluster.cluster_size // 0'  2>/dev/null || echo 0; }

# How many DISTINCT JetStream systems are these monitor ports in? One shared
# meta group of six reports the same leader from every port; three separate
# groups of three report three different leaders. That is the whole difference
# between a gateway and a leaf link, in one number.
meta_group_count() {
  local http leaders=""
  for http in "$@"; do leaders+="$(meta_leader "$http")"$'\n'; done
  printf '%s' "$leaders" | grep -v '^$' | sort -u | wc -l | tr -d ' '
}

# ---------------------------------------------------------------------------
# Talking to a server. No contexts in this demo -- every call names its server
# and its user, and user == password.
# ---------------------------------------------------------------------------
nats_as() {
  local port="$1" user="$2"; shift 2
  nats --server "nats://127.0.0.1:$port" --user "$user" --password "$user" "$@"
}

# Run a nats call that is EXPECTED to fail, and return the NATS error code it
# carried (10058, 10008, 10005 ...), or "ok" when it unexpectedly succeeded.
err_code() {
  local out rc=0 code
  out="$(nats_as "$@" 2>&1)" || rc=$?
  if [ "$rc" -eq 0 ]; then echo "ok"; return 0; fi
  # nats prints e.g. `... already in use with a different configuration (10058)`
  code="$(printf '%s' "$out" | grep -oE '\(1[0-9]{4}\)' | head -1 | tr -d '()')"
  if [ -n "$code" ]; then echo "$code"; else
    printf '%s' "$out" | tail -1 | cut -c1-60
  fi
}

# Did this call fail, whatever it failed with? Some refusals arrive as a NATS
# error code and some as a client timeout, and which one you get depends on
# whether the unreachable servers closed their sockets or merely stopped
# answering. The finding is "it failed"; the mode is recorded separately.
fails_or_ok() {
  if nats_as "$@" >/dev/null 2>&1; then echo "ok"; else echo "fails"; fi
}

stream_cluster() {
  local port="$1" user="$2" stream="$3"
  nats_as "$port" "$user" stream info "$stream" --json 2>/dev/null \
    | jq -r '.cluster.name // "?"' 2>/dev/null || echo "?"
}

# A durable consumer does NOT live with the client that made it. It lives with
# its stream, which is what makes a cross-region read a WAN read.
consumer_cluster() {
  local port="$1" user="$2" stream="$3" consumer="$4"
  nats_as "$port" "$user" consumer info "$stream" "$consumer" --json 2>/dev/null \
    | jq -r '.cluster.name // "?"' 2>/dev/null || echo "?"
}

# A stream is a RAFT group of its own, separate from the meta group. Three
# things about it can be read apart, and all three matter:
#
#   config.num_replicas   what was ASKED for
#   cluster.replicas      the peers actually carrying it, minus the leader
#   cluster.leader        the ONE server that takes the writes
#
# Asked-for and got are not the same number. A stream can be created with
# --replicas 3 and sit at one peer when placement had nowhere to put the rest.
stream_replicas() {
  local port="$1" user="$2" stream="$3"
  nats_as "$port" "$user" stream info "$stream" --json 2>/dev/null \
    | jq -r '.config.num_replicas // "?"' 2>/dev/null || echo "?"
}

# Peers actually in the stream's RAFT group: the leader plus its followers.
stream_peers() {
  local port="$1" user="$2" stream="$3"
  nats_as "$port" "$user" stream info "$stream" --json 2>/dev/null \
    | jq -r 'if .cluster then (1 + ((.cluster.replicas // []) | length)) else "?" end' \
      2>/dev/null || echo "?"
}

# Which REGION holds the stream's RAFT leader. Server names are t-za-1, t-au-2,
# t-hub-3 and so on, so the region is the middle token. The raw server name is
# a coin toss between the three peers and cannot be a check; the region can.
stream_leader_region() {
  local port="$1" user="$2" stream="$3" name
  name="$(nats_as "$port" "$user" stream info "$stream" --json 2>/dev/null \
          | jq -r '.cluster.leader // ""' 2>/dev/null)"
  case "$name" in
    ""|null) echo "NONE" ;;
    *) printf '%s\n' "$name" | sed -E 's/^t-//; s/-[0-9]+$//' ;;
  esac
}

# The raw leader name, for a note. Never for a check.
stream_leader() {
  local port="$1" user="$2" stream="$3"
  nats_as "$port" "$user" stream info "$stream" --json 2>/dev/null \
    | jq -r '.cluster.leader // "NONE"' 2>/dev/null || echo "NONE"
}

stream_msgs() {
  local port="$1" user="$2" stream="$3"
  nats_as "$port" "$user" stream info "$stream" --json 2>/dev/null \
    | jq -r '.state.messages // "?"' 2>/dev/null || echo "?"
}

# ---------------------------------------------------------------------------
# Recording the result
# ---------------------------------------------------------------------------

results_reset() { : > "$RESULTS"; }

# PROVENANCE -- the last argument of record, note and check.
#
# Some questions are worth asking on more than one rig. When a check here is
# the SAME question another topology already answered, it carries the id of
# that original as its last argument: `A4`, `A22`, and so on. The report then
# prints a "from" column, so a reader can put the two answers side by side and
# see what the topology changed.
#
# The argument is optional. Left out, it is written as `-` and the report
# shows nothing. It is never a citation of a document -- only of another check
# id measured in this same lab.

# record <id> <requirement> <what it checks> <expected> <actual> <PASS|FAIL> [from]
record() {
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$1" "$2" "$TOPOLOGY" "$3" "$4" "$5" "$6" "${7:--}" >> "$RESULTS"
}

# note <id> <requirement> <what was observed> <value> [from]
# An observation that is evidence but has no right answer to compare against.
note() {
  record "$1" "$2" "$3" "-" "$4" "NOTE" "${5:--}"
  printf '  \033[36mNOTE\033[0m  %-8s %s\n            %s\n' "$1" "$3" "$4"
  [ -n "${5:-}" ] && printf '            (same question as %s)\n' "$5"
  return 0
}

# check <id> <requirement> <what it checks> <expected> <actual> [from]
# PASS when the measured string equals the expected string, exactly.
check() {
  local id="$1" req="$2" desc="$3" want="$4" got="$5" from="${6:--}" status="FAIL"
  [ "$want" = "$got" ] && status="PASS"
  record "$id" "$req" "$desc" "$want" "$got" "$status" "$from"
  if [ "$status" = "PASS" ]; then
    printf '  \033[32mPASS\033[0m  %-8s %s\n            expected and got: %s\n' "$id" "$desc" "$got"
  else
    printf '  \033[31mFAIL\033[0m  %-8s %s\n            expected: %s\n            got:      %s\n' \
      "$id" "$desc" "$want" "$got"
  fi
  [ "$from" != "-" ] && printf '            (same question as %s)\n' "$from"
  return 0
}

banner() {
  printf '\n%s\n%s\n%s\n' \
    "==========================================================" "$1" \
    "=========================================================="
}
