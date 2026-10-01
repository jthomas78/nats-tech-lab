# Shared paths and helpers for demo 05. Source it; do not run it.

D05_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
D05_RUN="$D05_DIR/.run"
D05_CONFIGS="$D05_DIR/exercises"
D05_PID="$D05_RUN/nats-server.pid"
D05_LOG="$D05_RUN/server.log"
D05_SECRETS="$D05_RUN/secrets.env"

D05_URL="nats://127.0.0.1:4522"
D05_MONITOR="http://127.0.0.1:8522"

# Every nats call goes through this. --no-context stops the CLI adding the
# credentials of whatever context another demo left selected.
d05_nats() {
  nats --no-context -s "$D05_URL" "$@"
}

d05_port_busy() {
  lsof -nP -iTCP:4522 -sTCP:LISTEN >/dev/null 2>&1
}

# The PID in the PID file, but only if that process is a nats-server started
# from THIS folder's exercises/ configs. Anything else is somebody else's process.
d05_our_pid() {
  [[ -f "$D05_PID" ]] || return 1
  local pid
  pid=$(cat "$D05_PID")
  ps -p "$pid" -o command= 2>/dev/null | grep -q "nats-server -c $D05_CONFIGS/" || return 1
  echo "$pid"
}
