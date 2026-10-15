#!/usr/bin/env bash
# Stop the demo 07 server and callout service that this folder started.
# Touches nothing else: each PID is checked against this folder first.
# Keeps .run/. Add --clean to delete .run/ (that deletes the operator too).
set -euo pipefail
source "$(dirname "$0")/lib.sh"

for f in "$D07_CALLOUT_PID" "$D07_PID"; do
  if pid=$(d07_our_pid "$f"); then
    kill "$pid"
    for _ in $(seq 1 50); do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.1
    done
    echo "stopped  pid $pid ($(basename "$f" .pid))"
  else
    echo "not running from this folder: $(basename "$f" .pid)"
  fi
  rm -f "$f"
done

if d07_port_busy; then
  echo "port 4822 is STILL in use — probably a foreground server. Stop it with Ctrl-C in its terminal." >&2
  exit 1
fi
echo "port     4822 is free"

if [[ "${1:-}" == "--clean" ]]; then
  rm -rf "$D07_RUN"
  echo "removed  $D07_RUN"
fi
