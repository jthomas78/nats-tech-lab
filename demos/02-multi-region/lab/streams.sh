#!/usr/bin/env bash
# Show every stream in every demo 02 account, and which cluster it sits on.
#
# There is no single "list all streams" command in NATS. A stream lives in an
# ACCOUNT, so you must ask each account. And `stream ls` does not tell you the
# region -- both clusters are one supercluster, so one account sees all six
# servers. The cluster name comes from `stream info`.
#
#   ./streams.sh          one look, then exit
#   ./streams.sh watch    redraw every second (Ctrl-C to stop)
#
# Run `./streams.sh watch` in a second terminal, then run a lab in the first,
# to see the streams appear and disappear.

set -uo pipefail

CTXS=(
  lab2-linebooker-za
  lab2-linebooker-au
  lab2-linebooker-shared-za
  lab2-linebooker-shared-au
  lab2-platform-shared-za
  lab2-platform-shared-au
)

pass() {
  local found=0 c s loc
  printf '  %-28s %-14s %s\n' ACCOUNT-CONTEXT STREAM WHERE
  printf '  %-28s %-14s %s\n' ---------------------------- -------------- -----
  for c in "${CTXS[@]}"; do
    for s in $(nats --context "$c" stream ls -n 2>/dev/null); do
      loc="$(nats --context "$c" stream info "$s" --json 2>/dev/null \
        | jq -r '"cluster=" + (.cluster.name // "?") + " R" + (.config.num_replicas|tostring) + " msgs=" + (.state.messages|tostring)')"
      printf '  %-28s %-14s %s\n' "$c" "$s" "$loc"
      found=1
    done
  done
  [[ $found -eq 0 ]] && printf '  (no streams -- the labs delete theirs when they finish)\n'
  return 0
}

if [[ "${1:-}" == "watch" ]]; then
  while true; do
    clear
    printf 'demo 02 streams   %s\n\n' "$(date +%H:%M:%S)"
    pass
    sleep 1
  done
else
  pass
fi
