#!/usr/bin/env bash
# clear-tempo.sh — delete every trace Tempo holds in one cell, and start
# Tempo again on an empty volume.
#
# Only Tempo's own volume (<project>_tempo-data) is removed. NATS, Postgres,
# Grafana and the Collector's queue are not touched. Spans sent while Tempo
# is down wait in the Collector's persistent queue (BR-068) and arrive when
# Tempo is up again. This cannot be undone.
#
# Usage:
#   demos/01-dictionary/otel/clear-tempo.sh [cell] [--yes]
#
#   cell     au-1 (default) or za-1; picks deploy/environments/local-<cell>.env
#   --yes    do it; without it the script only prints what it would do
#
# The compose project is lb-<cell>. Set PROJECT to use another name.
set -euo pipefail

CELL=au-1
YES=no
for a in "$@"; do
  case "$a" in
    --yes) YES=yes ;;
    -h | --help) sed -n '2,15p' "$0"; exit 0 ;;
    -*) echo "unknown option: $a" >&2; exit 2 ;;
    *) CELL=$a ;;
  esac
done

DEMO=$(cd "$(dirname "$0")/.." && pwd)
ENV_FILE=$DEMO/deploy/environments/local-$CELL.env
PROJECT=${PROJECT:-lb-$CELL}
VOLUME=${PROJECT}_tempo-data
CONTAINER=${PROJECT}-tempo-1
NETWORK=${PROJECT}_telemetry

[ -f "$ENV_FILE" ] || { echo "no env file for cell $CELL: $ENV_FILE" >&2; exit 2; }

compose() {
  docker compose -p "$PROJECT" --env-file "$ENV_FILE" \
    -f "$DEMO/deploy/cell/compose.yaml" \
    -f "$DEMO/deploy/global/compose.control.yaml" "$@"
}

if ! docker volume inspect "$VOLUME" >/dev/null 2>&1; then
  echo "volume $VOLUME does not exist; nothing to clear" >&2
  exit 1
fi
SIZE=$(docker run --rm -v "$VOLUME":/t:ro busybox:1.37.0 du -sh /t | awk '{print $1}')

echo "cell:      $CELL (project $PROJECT)"
echo "container: $CONTAINER"
echo "volume:    $VOLUME ($SIZE)"
if [ "$YES" != yes ]; then
  echo "dry run: nothing changed. Re-run with --yes to delete these traces."
  exit 0
fi

# --no-deps on every step: only Tempo stops and starts. The services keep
# running with whatever OTEL_CAPTURE_BODIES they were started with.
echo "== stop and remove $CONTAINER"
compose rm --stop --force tempo
echo "== remove $VOLUME"
docker volume rm "$VOLUME"
echo "== start Tempo on an empty volume"
compose up -d --no-deps tempo

# Tempo is not published on the host; ask it from inside its network.
echo "== wait for Tempo to be ready"
for _ in $(seq 1 60); do
  if docker run --rm --network "$NETWORK" busybox:1.37.0 wget -qO- http://tempo:3200/ready 2>/dev/null | grep -q ready; then
    echo "Tempo is ready. All earlier traces are gone."
    exit 0
  fi
  sleep 2
done
echo "Tempo did not report ready in 120 s; check: docker logs $CONTAINER" >&2
exit 1
