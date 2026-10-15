#!/usr/bin/env bash
# Start the demo 07 server in the background with one config.
#   lab/up.sh ex01-nats-callout
# Log: .run/server.log (watch it with: tail -f .run/server.log)
set -euo pipefail
source "$(dirname "$0")/lib.sh"

name=${1:?usage: lab/up.sh <config name, e.g. ex01-nats-callout>}
conf="$D07_CONFIGS/$name.conf"
[[ -f "$conf" ]] || { echo "no such config: $conf" >&2; exit 1; }
[[ -f "$D07_RUN/trust.conf" ]] || { echo "no .run/trust.conf. Build the chain first (exercise 01, Initial setup)." >&2; exit 1; }

if d07_port_busy; then
  echo "port 4822 is already in use. Run lab/down.sh, or stop the foreground server (Ctrl-C)." >&2
  exit 1
fi

mkdir -p "$D07_RESOLVER"
: > "$D07_LOG"
# trust.conf's resolver dir './jwt' is relative to the working directory.
cd "$D07_RUN"
nats-server -c "$conf" -l "$D07_LOG" -P "$D07_PID" &
disown

for _ in $(seq 1 50); do
  curl -sf "$D07_MONITOR/healthz" >/dev/null 2>&1 && { echo "up       $name (pid $(cat "$D07_PID"))"; exit 0; }
  sleep 0.1
done
echo "server did not become healthy. Last log lines:" >&2
tail -n 5 "$D07_LOG" >&2
exit 1
