#!/usr/bin/env bash
# Stop the demo 06 server that lab/up.sh started. Touches nothing else.
# Keeps the key store, creds, resolver and log. Add --clean to delete .run/
# (that deletes the operator too: the next run builds a new chain).
set -euo pipefail
source "$(dirname "$0")/lib.sh"

if pid=$(d06_our_pid); then
  kill "$pid"
  for _ in $(seq 1 50); do
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.1
  done
  echo "stopped  pid $pid"
else
  echo "no demo 06 server from lab/up.sh is running"
fi
rm -f "$D06_PID"

if d06_port_busy; then
  echo "port 4922 is STILL in use — probably a foreground server. Stop it with Ctrl-C in its terminal." >&2
  exit 1
fi
echo "port     4922 is free"

if [[ "${1:-}" == "--clean" ]]; then
  rm -rf "$D06_RUN"
  echo "removed  $D06_RUN"
fi
