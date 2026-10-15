#!/usr/bin/env bash
# Run the host nsc against demo 07's own store in .run/nsc.
#   lab/nsc.sh list accounts
#   lab/nsc.sh describe account AUTH
set -euo pipefail
source "$(dirname "$0")/lib.sh"
d07_nsc "$@"
