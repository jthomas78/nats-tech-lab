#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEMO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DRAWIO_BIN="${DRAWIO_BIN:-/Applications/draw.io.app/Contents/MacOS/draw.io}"

if [[ ! -x "$DRAWIO_BIN" ]]; then
  printf 'Draw.io executable not found: %s\n' "$DRAWIO_BIN" >&2
  printf 'Set DRAWIO_BIN to the installed Draw.io Desktop executable.\n' >&2
  exit 1
fi

node "$SCRIPT_DIR/sync-unifi-assets.mjs"

source="$SCRIPT_DIR/architecture-dictionary.drawio"

pages=(
  "shipping-ui-dictionary-map:1"
  "localized-rendering-lifecycle:2"
  "shipping-ui-dictionary-sequence:3"
  "docker-compose-network:4"
)

for page in "${pages[@]}"; do
  name="${page%%:*}"
  page_index="${page##*:}"
  "$DRAWIO_BIN" \
    --export \
    --format png \
    --page-index "$page_index" \
    --output "$DEMO_ROOT/backend/refdata-service/$name.png" \
    "$source"
done
