#!/usr/bin/env bash
# Stop the demo 05 server that lab/up.sh started. Touches nothing else.
# Keeps .run/secrets.env and .run/server.log. Add --clean to delete .run/.
set -euo pipefail
source "$(dirname "$0")/lib.sh"

if pid=$(d05_our_pid); then
  kill "$pid"
  for _ in $(seq 1 50); do
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.1
  done
  echo "stopped  pid $pid"
else
  echo "no demo 05 server from lab/up.sh is running"
fi
rm -f "$D05_PID"

if d05_port_busy; then
  echo "port 4522 is STILL in use — probably a foreground server. Stop it with Ctrl-C in its terminal." >&2
  exit 1
fi
echo "port     4522 is free"

if [[ "${1:-}" == "--clean" ]]; then
  rm -rf "$D05_RUN"
  echo "removed  $D05_RUN"
fi
