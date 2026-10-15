#!/usr/bin/env bash
# Build and start the Go auth callout service in the background.
#   lab/callout.sh
# Needs .env.workos (D07_WORKOS_CLIENT_ID, D07_WORKOS_ORG_ID) and the chain from exercise 01.
# Log: .run/callout.log (watch it with: tail -f .run/callout.log)
set -euo pipefail
source "$(dirname "$0")/lib.sh"

env_file="$D07_DIR/.env.workos"
[[ -f "$env_file" ]] || { echo "no $env_file. Write D07_WORKOS_CLIENT_ID and D07_WORKOS_ORG_ID into it first (WORKOS-SETUP.md, step 8)." >&2; exit 1; }
if pid=$(d07_our_pid "$D07_CALLOUT_PID"); then
  echo "callout already running (pid $pid). Run lab/down.sh first." >&2
  exit 1
fi

set -a
source "$env_file"
set +a

# The account public key, and the seed file of its first signing key.
account() { d07_nsc describe account --name "$1" --field sub | tr -d '"'; }
seed_file() {
  local k
  k=$(d07_nsc describe account --name "$1" --field nats.signing_keys | jq -r '.[0]')
  echo "$D07_NSC_HOME/keys/${k:0:1}/${k:1:2}/$k.nk"
}

export D07_URL
export D07_CALLOUT_CREDS="$D07_NSC_HOME/creds/D07/AUTH/callout-svc.creds"
export D07_AUTH_ACCOUNT=$(account AUTH)
export D07_AUTH_SIGNING_SEED=$(seed_file AUTH)
export D07_ORDERS_ACCOUNT=$(account ORDERS)
export D07_ORDERS_SIGNING_SEED=$(seed_file ORDERS)

# Built into .run/ so the PID's command line names this folder (down.sh checks).
go -C "$D07_DIR/callout" build -o "$D07_RUN/callout" .
: > "$D07_CALLOUT_LOG"
"$D07_RUN/callout" >>"$D07_CALLOUT_LOG" 2>&1 &
echo $! > "$D07_CALLOUT_PID"
disown

for _ in $(seq 1 50); do
  grep -q '^.* ready ' "$D07_CALLOUT_LOG" && { echo "callout  ready (pid $(cat "$D07_CALLOUT_PID"))"; exit 0; }
  kill -0 "$(cat "$D07_CALLOUT_PID")" 2>/dev/null || break
  sleep 0.1
done
echo "callout did not become ready. Last log lines:" >&2
tail -n 5 "$D07_CALLOUT_LOG" >&2
exit 1
