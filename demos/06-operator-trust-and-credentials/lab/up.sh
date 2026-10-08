#!/usr/bin/env bash
# Start the demo 06 server in the background with one config.
#   lab/up.sh ex01-nats-operator
# Log: .run/server.log (watch it with: tail -f .run/server.log)
set -euo pipefail
source "$(dirname "$0")/lib.sh"

name=${1:?usage: lab/up.sh <config name, e.g. ex01-nats-operator>}
conf="$D06_CONFIGS/$name.conf"
[[ -f "$conf" ]] || { echo "no such config: $conf" >&2; exit 1; }
[[ -f "$D06_RUN/trust.conf" ]] || { echo "no .run/trust.conf. Run lab/trust-conf.sh first." >&2; exit 1; }

if d06_port_busy; then
  echo "port 4922 is already in use. Run lab/down.sh, or stop the foreground server (Ctrl-C)." >&2
  exit 1
fi

mkdir -p "$D06_RESOLVER"
: > "$D06_LOG"
# The config's resolver dir is relative to the working directory.
cd "$D06_DIR"
nats-server -c "$conf" -l "$D06_LOG" -P "$D06_PID" &
disown

for _ in $(seq 1 50); do
  curl -sf "$D06_MONITOR/healthz" >/dev/null 2>&1 && { echo "up       $name (pid $(cat "$D06_PID"))"; exit 0; }
  sleep 0.1
done
echo "server did not become healthy. Last log lines:" >&2
tail -n 5 "$D06_LOG" >&2
exit 1
