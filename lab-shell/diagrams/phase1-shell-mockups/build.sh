#!/bin/sh
# Assembles the .dc.html artboards from parts/head.html + parts/body-<Name>.html + parts/tail.html.
set -e
cd "$(dirname "$0")"
for body in parts/body-*.html; do
  name=$(basename "$body" .html); name=${name#body-}
  cat parts/head.html "$body" parts/tail.html > "$name.dc.html"
  echo "built $name.dc.html"
done
