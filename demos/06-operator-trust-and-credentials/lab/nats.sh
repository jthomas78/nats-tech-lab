#!/usr/bin/env bash
# Run the host nats CLI against demo 06's isolated key store, with no context.
#   lab/nats.sh auth account ls
#   lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/order-svc.creds pub orders.created hi
set -euo pipefail
source "$(dirname "$0")/lib.sh"
d06_nats "$@"
