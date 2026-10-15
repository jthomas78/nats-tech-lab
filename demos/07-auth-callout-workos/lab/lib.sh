# Shared paths and helpers for demo 07. Source it; do not run it.

D07_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
D07_RUN="$D07_DIR/.run"
D07_CONFIGS="$D07_DIR/exercises/config"
D07_NSC_HOME="$D07_RUN/nsc"
D07_CREDS="$D07_RUN/creds"
D07_RESOLVER="$D07_RUN/jwt"   # trust.conf says dir './jwt'; up.sh runs from .run/
D07_PID="$D07_RUN/nats-server.pid"
D07_LOG="$D07_RUN/server.log"
D07_CALLOUT_PID="$D07_RUN/callout.pid"
D07_CALLOUT_LOG="$D07_RUN/callout.log"

D07_URL="nats://127.0.0.1:4822"
D07_MONITOR="http://127.0.0.1:8822"

# nsc keeps its config, its JWT store and its keys (seeds) in three places,
# by default under the author's home. -H points all three into .run/nsc, so
# this demo's operator never lands in ~/.config/nats, ~/.local/share/nats or
# ~/.nkeys, which other work uses.
d07_nsc() {
  nsc -H "$D07_NSC_HOME" "$@"
}

# Every nats call goes through this. --no-context stops the CLI adding the
# credentials of whatever context another demo left selected.
d07_nats() {
  nats --no-context "$@"
}

d07_port_busy() {
  lsof -nP -iTCP:4822 -sTCP:LISTEN >/dev/null 2>&1
}

# The PID in a PID file, but only if that process's command line names THIS
# folder. Anything else is somebody else's process: never touch it.
d07_our_pid() {
  local file=$1 pid
  [[ -f "$file" ]] || return 1
  pid=$(cat "$file")
  ps -p "$pid" -o command= 2>/dev/null | grep -q "$D07_DIR" || return 1
  echo "$pid"
}
