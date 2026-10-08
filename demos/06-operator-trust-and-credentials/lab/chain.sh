#!/usr/bin/env bash
# Build exercise 01's trust chain from nothing: operator D06, account ORDERS,
# user order-svc, SYSTEM user admin, their .creds, and .run/trust.conf.
#   lab/chain.sh
# The scripted form of EXERCISE-01 steps 1-4. Refuses to run over an existing
# operator: run `lab/down.sh --clean` first (that deletes every key in .run/).
set -euo pipefail
source "$(dirname "$0")/lib.sh"

store="$XDG_DATA_HOME/nats/nsc/stores/D06"
if [[ -d "$store" ]]; then
  echo "operator D06 already exists in .run/. Run lab/down.sh --clean first." >&2
  exit 1
fi

mkdir -p "$D06_CREDS"
q() { d06_nats "$@" >/dev/null; }
q auth operator add D06
q auth account add ORDERS --defaults
q auth user add order-svc ORDERS --defaults --credential "$D06_CREDS/order-svc.creds"
q auth user add admin SYSTEM --defaults --credential "$D06_CREDS/sys.creds"
"$D06_DIR/lab/trust-conf.sh" >/dev/null
echo "built    operator D06, account ORDERS, users order-svc + admin (SYSTEM)"
