#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: scripts/new-plugin.sh <lowercase-kebab-id> <7100-7199-port>" >&2
  exit 2
fi

PLUGIN_ID="$1"
PLUGIN_PORT="$2"
if [[ ! "$PLUGIN_ID" =~ ^[a-z0-9]+(-[a-z0-9]+)*$ ]]; then
  echo "error: plugin id must be lowercase-kebab" >&2
  exit 2
fi
if [[ ! "$PLUGIN_PORT" =~ ^71[0-9][0-9]$ ]]; then
  echo "error: plugin port must be in 7100-7199" >&2
  exit 2
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${PLUGIN_SCAFFOLD_ROOT:-$(cd "$SCRIPT_DIR/.." && pwd)}"
TEMPLATE="$REPO_ROOT/lab-shell/plugins/example-plugin"
TARGET="$REPO_ROOT/lab-shell/plugins/$PLUGIN_ID"
COMPOSE="$REPO_ROOT/demos/01-dictionary/docker-compose.yml"
BOOTSTRAP="$REPO_ROOT/demos/01-dictionary/nats/bootstrap-operator.sh"
README="$REPO_ROOT/demos/01-dictionary/README.md"
COMPOSE_TEMPLATE="$REPO_ROOT/scripts/templates/plugin-compose.yml.tpl"

for required in "$TEMPLATE" "$COMPOSE" "$BOOTSTRAP" "$README" "$COMPOSE_TEMPLATE"; do
  if [[ ! -e "$required" ]]; then
    echo "error: required scaffold source is missing: $required" >&2
    exit 1
  fi
done
if [[ -e "$TARGET" ]]; then
  echo "error: plugin already exists: $PLUGIN_ID" >&2
  exit 1
fi
if grep -q "localhost:$PLUGIN_PORT" "$COMPOSE"; then
  echo "error: port already allocated: $PLUGIN_PORT" >&2
  exit 1
fi

cp -R "$TEMPLATE" "$TARGET"
find "$TARGET" -type d \( -name node_modules -o -name dist \) -prune -exec rm -rf {} +

PLUGIN_SCAFFOLD_ID="$PLUGIN_ID" PLUGIN_SCAFFOLD_PORT="$PLUGIN_PORT" PLUGIN_SCAFFOLD_ROOT="$REPO_ROOT" python3 - <<'PY'
import json
import os
from pathlib import Path

root = Path(os.environ["PLUGIN_SCAFFOLD_ROOT"])
plugin_id = os.environ["PLUGIN_SCAFFOLD_ID"]
port = os.environ["PLUGIN_SCAFFOLD_PORT"]
target = root / "lab-shell" / "plugins" / plugin_id
title = " ".join(word.capitalize() for word in plugin_id.split("-"))

for path in target.rglob("*"):
    if not path.is_file():
        continue
    text = path.read_text()
    text = text.replace("example_plugin", plugin_id.replace("-", "_"))
    text = text.replace("example-plugin", plugin_id)
    text = text.replace("Example Plugin", title)
    text = text.replace("/example", "/" + plugin_id)
    text = text.replace("7111", port)
    path.write_text(text)

manifest_path = target / "public" / "manifest.json"
manifest = json.loads(manifest_path.read_text())
manifest["id"] = plugin_id
manifest["name"] = title
manifest["description"] = f"Scaffolded MFE plugin {plugin_id}."
manifest["routePrefix"] = plugin_id
manifest["remote"]["url"] = "/remoteEntry.js"
manifest_path.write_text(json.dumps(manifest, indent=2) + "\n")

compose_path = root / "demos" / "01-dictionary" / "docker-compose.yml"
compose = compose_path.read_text()
template = (root / "scripts" / "templates" / "plugin-compose.yml.tpl").read_text()
stanza = template.replace("__PLUGIN_ID__", plugin_id).replace("__PLUGIN_PORT__", port)
marker = "  # Announcer-only: this fixture has no code and no web server, by design\n"
if marker not in compose:
    raise SystemExit("error: Compose plugin insertion marker is missing")
compose = compose.replace(marker, stanza + "\n" + marker, 1)
compose = compose.replace(
    'REGISTRY_ALLOWED_ORIGINS: "',
    'REGISTRY_ALLOWED_ORIGINS: "http://localhost:' + port + ',',
    1,
)
for mapping_name in ("REGISTRY_FETCH_ORIGINS", "REGISTRY_HEALTH_ORIGINS"):
    start = compose.index(mapping_name + ": >-")
    end = compose.index("}", start)
    compose = compose[:end] + ',\n         "http://localhost:' + port + '":"http://' + plugin_id + '-frontend:8080"' + compose[end:]
target_marker = '"example-plugin-incompatible":[]}'
compose = compose.replace(target_marker, '"example-plugin-incompatible":[],\n         "' + plugin_id + '":[]}', 1)
volume_marker = "  example-plugin-incompatible-release:\n"
compose = compose.replace(volume_marker, volume_marker + "  " + plugin_id + "-release:\n", 1)
compose_path.write_text(compose)

bootstrap_path = root / "demos" / "01-dictionary" / "nats" / "bootstrap-operator.sh"
bootstrap = bootstrap_path.read_text()
bootstrap_marker = "  # new-plugin.sh inserts plugin ids immediately above this marker.\n"
if bootstrap_marker not in bootstrap:
    raise SystemExit("error: bootstrap plugin insertion marker is missing")
bootstrap_path.write_text(bootstrap.replace(bootstrap_marker, "  " + plugin_id + "\n" + bootstrap_marker, 1))

readme_path = root / "demos" / "01-dictionary" / "README.md"
readme = readme_path.read_text()
row_marker = "| Incompatible plugin fixture | http://localhost:7115 |\n"
if row_marker not in readme:
    raise SystemExit("error: README port-table insertion marker is missing")
readme_path.write_text(readme.replace(row_marker, row_marker + f"| {title} plugin | http://localhost:{port} |\n", 1))
PY

echo "created $PLUGIN_ID on http://localhost:$PLUGIN_PORT"
echo "run demos/01-dictionary/nats/bootstrap-operator.sh --force before starting the stack"
