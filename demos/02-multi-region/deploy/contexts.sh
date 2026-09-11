#!/usr/bin/env bash
# Register one `nats` context per credential, on YOUR machine.
#
# Demo 02 uses the nats CLI installed on the host (Homebrew), not a CLI inside
# a container. So the contexts have to live in your own store,
# ~/.config/nats/context/, and they must use the PUBLISHED host ports --
# `za-1` is a Docker name and your Mac cannot resolve it.
#
#   za-1 4621   za-2 4622   za-3 4623
#   au-1 4721   au-2 4722   au-3 4723
#
# Every name is prefixed `lab2-`. That is not decoration. Demo 01 already owns
# contexts called `sys` and `platform` in the same store, pointing at
# localhost:4222 with demo 01's trust chain. Unprefixed names here would
# overwrite them and break that demo with no warning.
#
# Safe to re-run. `nats context add` overwrites a context of the same name,
# and only lab2-* names are touched.
#
# Usage:
#   ./contexts.sh            add or refresh every lab2-* context
#   ./contexts.sh remove     delete every lab2-* context

set -euo pipefail
cd "$(dirname "$0")"

CREDS="$(cd ../nats && pwd)/creds"

command -v nats >/dev/null 2>&1 || {
  echo "the nats CLI is not on your PATH -- brew install nats-io/nats-tools/nats" >&2
  exit 1
}

if [[ "${1:-add}" == "remove" ]]; then
  for name in $(nats context ls --names 2>/dev/null | grep '^lab2-' || true); do
    nats context rm "$name" --force >/dev/null 2>&1 || true
    echo "removed $name"
  done
  exit 0
fi

[[ -d "$CREDS" ]] || {
  echo "no creds yet -- run ./up.sh first" >&2
  exit 1
}

# Every context gets all three servers of its region. A client that knows only
# one address cannot tell "my stream is gone" from "my one address is gone".
ZA_URLS="nats://localhost:4621,nats://localhost:4622,nats://localhost:4623"
AU_URLS="nats://localhost:4721,nats://localhost:4722,nats://localhost:4723"

# ONE JetStream domain for the whole supercluster. A domain names a JetStream
# system, and the docs require the same name on every server of a cluster AND a
# supercluster -- it may only change across a leaf-node link. Per-region domains
# were tried and silently broke JetStream; see the comment in nats/nats.conf.
# Regions are separated by PLACEMENT (--cluster za|au) and by ACCOUNTS.
JS_DOMAIN="lb"

add() {  # $1 context name  $2 creds file  $3 urls  $4 jetstream domain ("" for none)
  nats context add "$1" --server "$3" --creds "$2" \
    ${4:+--js-domain "$4"} >/dev/null
  echo "added $1"
}

for creds in "$CREDS"/*.creds; do
  [[ -f "$creds" ]] || continue
  base="$(basename "$creds" .creds)"
  case "$base" in
    # The system account watches servers. It crosses the gateway on its own,
    # so one context is enough, and it holds no streams -- no domain.
    sys)           add "lab2-sys"           "$creds" "$ZA_URLS" "" ;;
    *-au)          add "lab2-$base"         "$creds" "$AU_URLS" "$JS_DOMAIN" ;;
    *-za)          add "lab2-$base"         "$creds" "$ZA_URLS" "$JS_DOMAIN" ;;
    # An account that spans BOTH regions needs one context per side. It is
    # named `-shared-` so it can never be confused with the split accounts
    # above -- `lab2-linebooker-shared-za` and `lab2-linebooker-za` are two
    # different accounts, and that difference IS the demo.
    linebooker|platform)
      add "lab2-$base-shared-za" "$creds" "$ZA_URLS" "$JS_DOMAIN"
      add "lab2-$base-shared-au" "$creds" "$AU_URLS" "$JS_DOMAIN"
      ;;
    *)             add "lab2-$base"         "$creds" "$ZA_URLS" "$JS_DOMAIN" ;;
  esac
done

echo
echo "try it:"
echo "  nats --context lab2-sys server list"
echo "  nats --context lab2-linebooker-za stream ls"
