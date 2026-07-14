#!/usr/bin/env bash
#
# Render a UniFi-theme architecture diagram SVG to PNG.
#
# Single-source icons: the diagram SVG references glyphs by id
# (<use href="#ico-nats" .../>) and contains the marker <!--UNIFI-ICONS-->
# inside its <defs>. This script inlines the <symbol> library from icons.svg
# at that marker, then rasterizes with headless Chrome at 2x. (Headless Chrome
# blocks cross-file <use> in standalone SVGs, so the symbols must be inlined —
# this build does that for you, keeping icons.svg the one place they're defined.)
#
# Usage: render-diagram.sh <src.svg> <out.png> [width] [height]
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

SRC="${1:?src svg required}"
OUT="${2:?out png required}"
W="${3:-1400}"
H="${4:-1020}"

TMP="$(mktemp -t unifi-diagram).svg"
trap 'rm -f "$TMP"' EXIT

python3 - "$HERE/icons.svg" "$SRC" "$TMP" <<'PY'
import re, sys
icons_path, src_path, out_path = sys.argv[1:4]
lib = re.sub(r"<!--.*?-->", "", open(icons_path).read(), flags=re.S)  # drop comments
symbols = re.findall(r"<symbol\b.*?</symbol>", lib, re.S)
defs = "\n".join(symbols)
svg = open(src_path).read()
if "<!--UNIFI-ICONS-->" not in svg:
    sys.exit(f"error: {src_path} has no <!--UNIFI-ICONS--> marker in its <defs>")
open(out_path, "w").write(svg.replace("<!--UNIFI-ICONS-->", defs))
PY

"$CHROME" --headless --disable-gpu --hide-scrollbars \
  --force-device-scale-factor=2 --default-background-color=00000000 \
  --window-size="${W},${H}" --screenshot="$OUT" "$TMP" 2>/dev/null

echo "rendered $OUT (${W}x${H} @2x)"
