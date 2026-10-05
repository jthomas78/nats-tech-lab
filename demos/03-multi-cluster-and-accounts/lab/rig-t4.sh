#!/usr/bin/env bash
# T4 RIG FOR HAND RUNS -- start it, leave it running, stop it.
#
#   ./lab/rig-t4.sh up       start the nine servers from exercises/config/; leave them running
#   ./lab/rig-t4.sh status   read-only: who answers, who leads, who is current
#   ./lab/rig-t4.sh restart arb-1   stop one server cleanly, start it again
#   ./lab/rig-t4.sh freeze za       kill -STOP a cluster (or servers); prove it is dark
#   ./lab/rig-t4.sh thaw za         kill -CONT them; wait until they answer
#   ./lab/rig-t4.sh down     thaw anything frozen, then stop every t- server
#
# Every other script in this folder starts its servers, measures, and kills
# them on exit. That is right for evidence and wrong for a person at a
# terminal, who needs the rig to stay up between commands. This script is the
# one place that is allowed to leave servers running. It exists for
# HUB-META-LEADER-PLAN.md: exercise 10 (../exercises/EXERCISE-10-TERMINAL-STEPS.md) uses it by hand, and
# 10-hub-meta-leader.sh uses the same configs.
#
# The wiring is T4 / Figure F, copied from 06-arbiter3.sh (left untouched,
# because it is measured): za + au + a three-node third cluster `arb`, joined
# by gateways only, one nine-member meta group, majority 5. In the plan the
# `arb` cluster plays the HUB. The cluster keeps the name `arb` -- every
# command and placement uses `arb`; "hub" is only the role.
#
# NOT in run-all.sh's LABS list, on purpose. And never run this rig and
# run-all.sh at the same time: they share ports, and every lab script's
# lab_init kills any t- server it finds -- including this one.

source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

TOPOLOGY="T4 / F -- hub meta-leader rig (hand runs)"

SERVERS=(za-1 za-2 za-3 au-1 au-2 au-3 arb-1 arb-2 arb-3)

# Monitor port of a short server name: za-1 -> 8231, au-2 -> 8242, arb-3 -> 8543.
http_of() {
  local site="${1%-*}" n="${1##*-}"
  case "$site" in
    za)  echo "823$n" ;;
    au)  echo "824$n" ;;
    arb) echo "854$n" ;;
  esac
}

# Every port the rig listens on: client, monitor, route, gateway, per server.
ALL_PORTS=()
for i in 1 2 3; do
  ALL_PORTS+=(423$i 823$i 623$i 723$i  424$i 824$i 624$i 724$i  454$i 854$i 654$i 754$i)
done

# The nine configs are hand-written files in ../exercises/config/, not built by
# a script, so a person can read and change them. They match what
# 06-arbiter3.sh generates. `up` copies them into $RUN_DIR and starts each
# server from there: the command stays `nats-server -c t-<name>.conf`, so
# KILL_PATTERN still matches, and store_dir "./js/<name>" lands in lab/run/js/.
CONFIG_DIR="$LAB_DIR/../exercises/config"

copy_configs() {
  local s
  for s in "${SERVERS[@]}"; do
    [ -f "$CONFIG_DIR/t-$s.conf" ] || { echo "missing $CONFIG_DIR/t-$s.conf" >&2; exit 1; }
    cp "$CONFIG_DIR/t-$s.conf" "$RUN_DIR/"
  done
  cp "$CONFIG_DIR/accounts.conf" "$RUN_DIR/"
}

# A short, bounded probe. A FROZEN server still accepts the TCP connection and
# then never answers, so every read here needs --max-time or it hangs forever.
probe() { curl -fs --max-time 1 "http://127.0.0.1:$1/$2" 2>/dev/null; }

# --- up ---------------------------------------------------------------------

cmd_up() {
  need_tools

  # 1. Check first. Delete nothing until we know no rig is running.
  if pgrep -f "$KILL_PATTERN" >/dev/null 2>&1; then
    echo "refusing: t- servers are already running (pgrep -f \"$KILL_PATTERN\")." >&2
    echo "run ./lab/rig-t4.sh down first." >&2
    exit 1
  fi
  local p busy=()
  for p in "${ALL_PORTS[@]}"; do
    lsof -nP -iTCP:"$p" -sTCP:LISTEN >/dev/null 2>&1 && busy+=("$p")
  done
  if [ "${#busy[@]}" -gt 0 ]; then
    echo "refusing: these rig ports are already in use: ${busy[*]}" >&2
    exit 1
  fi

  # 2. Only now clear the last run's files. results.tsv and evidence/ stay.
  lab_reset_run_dir

  # 3. A failed or interrupted startup tears itself down.
  trap lab_down EXIT INT TERM

  copy_configs
  local s
  for s in "${SERVERS[@]}"; do start_server "$s"; done
  for s in "${SERVERS[@]}"; do wait_ready "$(http_of "$s")"; done
  wait_meta_leader 8541 60 || { echo "no meta leader within 30 s" >&2; exit 1; }
  sleep 5

  local size; size="$(meta_size 8541)"
  [ "$size" = "9" ] || { echo "meta group size is $size, expected 9" >&2; exit 1; }

  # 4. Success: drop the trap so the rig stays up after this script exits.
  trap - EXIT INT TERM
  echo "rig up: 9 servers, configs and logs in $RUN_DIR"
  echo
  cmd_status
}

# --- status (read-only) -----------------------------------------------------

cmd_status() {
  local s http state leader size any=0 lead_name="" lead_http=""
  printf '%-7s %-7s %-6s %-9s %s\n' SERVER MONITOR STATE META_SIZE META_LEADER
  for s in "${SERVERS[@]}"; do
    http="$(http_of "$s")"
    if probe "$http" healthz >/dev/null; then
      state=up; any=1
      local j; j="$(probe "$http" 'jsz?meta=1')" || j='{}'
      leader="$(jq -r '.meta_cluster.leader // "NONE"' <<<"$j" 2>/dev/null || echo NONE)"
      size="$(jq -r '.meta_cluster.cluster_size // 0' <<<"$j" 2>/dev/null || echo 0)"
      if [ -z "$lead_name" ] && [ "$leader" != NONE ]; then lead_name="$leader"; fi
    else
      state=dark; leader=-; size=-
    fi
    printf '%-7s %-7s %-6s %-9s %s\n' "$s" "$http" "$state" "$size" "$leader"
  done

  [ "$any" -eq 1 ] || { echo; echo "no server answers -- the rig is down or fully frozen"; return 0; }
  [ -n "$lead_name" ] || { echo; echo "no live server names a meta leader"; return 0; }

  # The leader is not in its own replica list, so only the leader's view covers
  # every other peer.
  lead_http="$(http_of "${lead_name#t-}")"
  echo
  echo "meta leader $lead_name -- its view of the other eight peers (monitor $lead_http):"
  if ! probe "$lead_http" 'jsz?meta=1' \
      | jq -r '.meta_cluster.replicas[]? | "  \(.name)  current=\(.current)  offline=\(.offline // false)  active=\(.active)"'; then
    echo "  (the leader's monitor did not answer -- it may be frozen; the name above may be stale)"
  fi
}

# --- restart one server -----------------------------------------------------

# Stop one server cleanly and start it again from the same config and store.
# Thaw first, so a frozen server can act on the TERM.
cmd_restart() {
  local s="${1:-}" pid i
  if [ -z "$s" ] || ! printf '%s\n' "${SERVERS[@]}" | grep -qx "$s"; then
    echo "usage: $0 restart <server>   (one of: ${SERVERS[*]})" >&2
    exit 2
  fi
  pid="$(cat "$RUN_DIR/pid/$s.pid" 2>/dev/null || true)"
  if [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; then
    echo "refusing: t-$s is not running (no live PID in $RUN_DIR/pid/$s.pid)" >&2
    exit 1
  fi
  kill -CONT "$pid" 2>/dev/null || true
  kill -TERM "$pid"
  for i in $(seq 1 30); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
  if kill -0 "$pid" 2>/dev/null; then
    echo "t-$s (PID $pid) did not stop within 30 s" >&2
    exit 1
  fi
  start_server "$s"
  wait_ready "$(http_of "$s")"
  echo "t-$s restarted: PID $pid -> $(cat "$RUN_DIR/pid/$s.pid")"
}

# --- freeze / thaw ----------------------------------------------------------

# Names to servers: a cluster (za, au, arb) means its three servers.
expand() {
  local a
  for a in "$@"; do
    case "$a" in
      za|au|arb) echo "$a-1 $a-2 $a-3" ;;
      *) printf '%s\n' "${SERVERS[@]}" | grep -qx "$a" || { echo "unknown server or cluster: $a" >&2; return 1; }
         echo "$a" ;;
    esac
  done
}

# kill -STOP: the region goes dark. A freeze must prove it froze, so every
# monitor is read until it stops answering (wait_dark).
cmd_freeze() {
  [ "$#" -gt 0 ] || { echo "usage: $0 freeze <cluster|server>..." >&2; exit 2; }
  local names s pid ports=()
  names="$(expand "$@")" || exit 2
  for s in $names; do
    pid="$(cat "$RUN_DIR/pid/$s.pid" 2>/dev/null || true)"
    [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null || { echo "refusing: t-$s is not running" >&2; exit 1; }
  done
  # shellcheck disable=SC2086
  freeze $names
  for s in $names; do ports+=("$(http_of "$s")"); done
  wait_dark "${ports[@]}" || exit 1
  echo "frozen:" $names "-- monitors ${ports[*]} do not answer"
}

# kill -CONT: the region comes back. Waits until each monitor answers again.
cmd_thaw() {
  [ "$#" -gt 0 ] || { echo "usage: $0 thaw <cluster|server>..." >&2; exit 2; }
  local names s
  names="$(expand "$@")" || exit 2
  # shellcheck disable=SC2086
  thaw $names
  for s in $names; do wait_ready "$(http_of "$s")" || exit 1; done
  echo "thawed:" $names "-- monitors answer"
}

# --- down -------------------------------------------------------------------

cmd_down() {
  if ! pgrep -f "$KILL_PATTERN" >/dev/null 2>&1; then
    echo "nothing running"
    return 0
  fi
  lab_down   # CONT before TERM, so a frozen server can act on the TERM
  if pgrep -f "$KILL_PATTERN" >/dev/null 2>&1; then
    echo "some t- servers are still running:" >&2
    pgrep -fl "$KILL_PATTERN" >&2 || true
    return 1
  fi
  echo "rig down"
}

# Sourced (by 10-hub-meta-leader.sh, for SERVERS and http_of): define only.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  case "${1:-}" in
    up)     cmd_up ;;
    status)  cmd_status ;;
    restart) cmd_restart "${2:-}" ;;
    freeze)  shift; cmd_freeze "$@" ;;
    thaw)    shift; cmd_thaw "$@" ;;
    down)    cmd_down ;;
    *)       echo "usage: $0 up | status | restart <server> | freeze <cluster|server>... | thaw <cluster|server>... | down" >&2; exit 2 ;;
  esac
fi
