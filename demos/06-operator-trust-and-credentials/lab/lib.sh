# Shared paths and helpers for demo 06. Source it; do not run it.

D06_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
D06_RUN="$D06_DIR/.run"
D06_CONFIGS="$D06_DIR/exercises/config"
D06_CREDS="$D06_RUN/creds"
D06_RESOLVER="$D06_RUN/resolver"
D06_PID="$D06_RUN/nats-server.pid"
D06_LOG="$D06_RUN/server.log"

D06_URL="nats://127.0.0.1:4922"
D06_MONITOR="http://127.0.0.1:8922"

# `nats auth` keeps keys and JWTs under $XDG_DATA_HOME/nats/nsc and its
# settings under $XDG_CONFIG_HOME/nats/nsc. Pointing both into .run/ keeps
# this demo's operator out of the author's real store (~/.local/share/nats),
# which other demos' nsc work uses.
export XDG_DATA_HOME="$D06_RUN/xdg-data"
export XDG_CONFIG_HOME="$D06_RUN/xdg-config"

# Every nats call goes through this. --no-context stops the CLI adding the
# credentials of whatever context another demo left selected.
d06_nats() {
  nats --no-context "$@"
}

d06_port_busy() {
  lsof -nP -iTCP:4922 -sTCP:LISTEN >/dev/null 2>&1
}

# Print one claim from a JWT. The argument is a .jwt file or a .creds file.
#   d06_claim .run/creds/order-svc.creds sub      -> the user's public key
#   d06_claim .run/creds/order-svc.creds exp      -> expiry, unix seconds
d06_claim() {
  python3 - "$1" "$2" <<'PY'
import base64, json, re, sys
text = open(sys.argv[1]).read()
m = re.search(r"-----BEGIN NATS USER JWT-----\n(.*?)\n------END", text, re.S)
jwt = (m.group(1) if m else text).strip()
p = jwt.split(".")[1]
claims = json.loads(base64.urlsafe_b64decode(p + "=" * (-len(p) % 4)))
v = claims.get(sys.argv[2], "")
print(json.dumps(v) if isinstance(v, (dict, list)) else v)
PY
}

# The PID in the PID file, but only if that process is a nats-server started
# from THIS folder's exercises/ configs. Anything else is somebody else's process.
d06_our_pid() {
  [[ -f "$D06_PID" ]] || return 1
  local pid
  pid=$(cat "$D06_PID")
  ps -p "$pid" -o command= 2>/dev/null | grep -q "nats-server -c $D06_CONFIGS/" || return 1
  echo "$pid"
}
