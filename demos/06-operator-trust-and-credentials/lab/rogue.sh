#!/usr/bin/env bash
# Like lab/nats.sh, but against a second, throwaway key store for operator
# ROGUE (.run/rogue/). Exercise 01 uses it for one refusal test only.
#   lab/rogue.sh auth operator add ROGUE
set -euo pipefail
source "$(dirname "$0")/lib.sh"
export XDG_DATA_HOME="$D06_RUN/rogue/xdg-data"
export XDG_CONFIG_HOME="$D06_RUN/rogue/xdg-config"
d06_nats "$@"
