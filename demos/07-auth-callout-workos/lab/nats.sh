#!/usr/bin/env bash
# Run the host nats CLI with no context.
#   lab/nats.sh -s nats://127.0.0.1:4822 --creds .run/creds/sentinel.creds --token "$TOKEN" pub orders.created hi
set -euo pipefail
source "$(dirname "$0")/lib.sh"
d07_nats "$@"
