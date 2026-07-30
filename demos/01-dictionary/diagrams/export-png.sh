#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEMO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$DEMO_ROOT/../.." && pwd)"
# The workbook and its exported images live in the obsidian vault — see
# obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-COMMUNICATIONS.md.
VAULT_DIR="$REPO_ROOT/obsidian/V3-Platform/Architecture/Dictionary-POC"
DRAWIO_BIN="${DRAWIO_BIN:-/Applications/draw.io.app/Contents/MacOS/draw.io}"

if [[ ! -x "$DRAWIO_BIN" ]]; then
  printf 'Draw.io executable not found: %s\n' "$DRAWIO_BIN" >&2
  printf 'Set DRAWIO_BIN to the installed Draw.io Desktop executable.\n' >&2
  exit 1
fi

node "$SCRIPT_DIR/sync-unifi-assets.mjs"

source="$VAULT_DIR/architecture-dictionary.drawio"

pages=(
  "shipping-ui-dictionary-map:1"
  "localized-rendering-lifecycle:2"
  "shipping-ui-dictionary-sequence:3"
  "docker-compose-network:4"
  "rpc-proposed-dual-transport:5"
  "jwt-minting-sequence:6"
)

for page in "${pages[@]}"; do
  name="${page%%:*}"
  page_index="${page##*:}"
  "$DRAWIO_BIN" \
    --export \
    --format png \
    --page-index "$page_index" \
    --output "$VAULT_DIR/images/$name.png" \
    "$source"
done
