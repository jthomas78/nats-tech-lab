#!/usr/bin/env bash
# Start the demo 05 server in the background with one config.
#   lab/up.sh ex01-a-open
#   lab/up.sh ex01-b-users
# Log: .run/server.log (watch it with: tail -f .run/server.log)
set -euo pipefail
source "$(dirname "$0")/lib.sh"

name=${1:?usage: lab/up.sh <config name, e.g. ex01-b-users>}
conf="$D05_CONFIGS/$name.conf"
[[ -f "$conf" ]] || { echo "no such config: $conf" >&2; exit 1; }

if d05_port_busy; then
  echo "port 4522 is already in use. Run lab/down.sh, or stop the foreground server (Ctrl-C)." >&2
  exit 1
fi

mkdir -p "$D05_RUN"
"$D05_DIR/lab/secrets.sh" >/dev/null
source "$D05_SECRETS"

: > "$D05_LOG"
nats-server -c "$conf" -l "$D05_LOG" -P "$D05_PID" &
disown

for _ in $(seq 1 50); do
  curl -sf "$D05_MONITOR/healthz" >/dev/null 2>&1 && { echo "up       $name (pid $(cat "$D05_PID"))"; exit 0; }
  sleep 0.1
done
echo "server did not become healthy. Last log lines:" >&2
tail -n 5 "$D05_LOG" >&2
exit 1
