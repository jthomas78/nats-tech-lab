#!/usr/bin/env bash
# Bring demo 02 up with one command.
#
# Demo 02 is TWO Compose projects, one per region, plus three shared Docker
# networks. That is three long commands in the right order, so this script
# does it for you.
#
#   lb-za-1   za-1 za-2 za-3   compose.za.yaml
#   lb-au-1   au-1 au-2 au-3   compose.au.yaml
#
#   lb-za  lb-au   the two regional networks -- routes, 6222
#   lb-wan         the only road between them -- gateways, 7222
#
# The networks are `external: true` because two projects share them. This
# script creates them when absent. A plain `down` leaves them alone, so it
# never strands the other project; `down -v` removes them.
#
# Usage:
#   ./up.sh              mint the trust chain if needed, start six servers,
#                        then register the lab2-* nats contexts
#   ./up.sh down         stop, keep the data
#   ./up.sh down -v      stop, drop every volume, remove the networks
#   ./up.sh reseed       wipe the trust chain AND the data, then start clean
#
# Why bootstrap runs first: compose mounts nats/operator.jwt as a FILE. When
# that file is missing Docker helpfully creates a DIRECTORY with that name,
# and the server then fails with a confusing parse error. So the trust chain
# has to exist before the first `up`.

set -euo pipefail
cd "$(dirname "$0")"

NETWORKS=(lb-za lb-au lb-wan)
ZA=(docker compose -f compose.za.yaml)
AU=(docker compose -f compose.au.yaml)

networks_up() {
  for n in "${NETWORKS[@]}"; do
    docker network inspect "$n" >/dev/null 2>&1 && continue
    echo "==> creating network $n"
    docker network create "$n" >/dev/null
  done
}

networks_down() {
  for n in "${NETWORKS[@]}"; do
    docker network rm "$n" >/dev/null 2>&1 || true
  done
}

# Minting needs nsc on your machine. There is no container for it any more.
bootstrap() {
  command -v nsc >/dev/null 2>&1 || {
    echo "nsc is not on your PATH -- brew install nats-io/nats-tools/nsc" >&2
    exit 1
  }
  echo "==> minting the trust chain with your local nsc ($(nsc --version 2>&1 | tail -1))"
  sh ../nats/bootstrap.sh "$@"
}

action="${1:-up}"
shift || true

case "$action" in
  up)
    networks_up
    [[ -f ../nats/operator.jwt ]] || bootstrap
    "${ZA[@]}" up -d "$@"
    "${AU[@]}" up -d "$@"
    ./contexts.sh
    cat <<'TXT'

==> six servers up in two projects:
      lb-za-1   za-1..3 (cluster za)
      lb-au-1   au-1..3 (cluster au)
    joined by one gateway across lb-wan.

Look around -- your own nats CLI, one lab2-* context per credential:

  nats --context lab2-sys server list
  nats --context lab2-linebooker-za stream ls

Then run the lab scripts:

  ../lab/01-the-wall.sh      does an account boundary hold across a gateway?
  ../lab/02-replicas.sh      what does Replicas 1 vs 3 cost when a node dies?
  ../lab/03-odometer.sh      is the double capture real? (needs Go)
TXT
    ;;
  down)
    "${AU[@]}" down "$@" || true
    "${ZA[@]}" down "$@" || true
    # Only a `down -v` takes the shared networks away.
    for arg in "$@"; do
      # -v wipes the volumes and, on the next `up`, re-mints the creds. The
      # lab2-* contexts would then point at files that no longer exist, so
      # they go too.
      [[ "$arg" == "-v" || "$arg" == "--volumes" ]] && { networks_down; ./contexts.sh remove || true; }
    done
    ;;
  reseed)
    "${AU[@]}" down -v || true
    "${ZA[@]}" down -v || true
    rm -f ../nats/operator.jwt ../nats/resolver-preload.generated.conf
    rm -rf ../nats/creds ../nats/resolver
    networks_up
    bootstrap --force
    "${ZA[@]}" up -d "$@"
    "${AU[@]}" up -d "$@"
    ./contexts.sh
    ;;
  *)
    "${ZA[@]}" "$action" "$@"
    "${AU[@]}" "$action" "$@"
    ;;
esac
