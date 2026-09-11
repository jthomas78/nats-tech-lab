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
# ADR-055 retired the one flat demos/01-dictionary/docker-compose.yml on
# 2026-09-08. A plugin now touches TWO compose files, because the split put the
# two halves of a plugin in different bands: the plugin service and its release
# volume are fixtures, so they live in the cell's dedicated band, while the
# origin allowlist and the fetch map belong to mfe-registry-service, which is a
# real cell service in the runtime band. Missing either half is silent -- the
# plugin comes up and every entry pointing at it is refused (BR-AS20).
DEPLOY="$REPO_ROOT/demos/01-dictionary/deploy"
DEDICATED="$DEPLOY/cell/compose.dedicated.yaml"
RUNTIME="$DEPLOY/cell/compose.runtime.yaml"
BOOTSTRAP="$REPO_ROOT/demos/01-dictionary/nats/bootstrap-operator.sh"
README="$REPO_ROOT/demos/01-dictionary/README.md"
COMPOSE_TEMPLATE="$REPO_ROOT/scripts/templates/plugin-compose.yml.tpl"
# Every published host port is a ${VAR:-default} fed by one env file per cell
# (ADR-055), so a new plugin needs a variable in every cell, not just a number
# in one file. za-1 holds the base ports; au-1 is offset by +50.
ENV_FILES=("$DEPLOY/environments/local-za-1.env" "$DEPLOY/environments/local-au-1.env")
ENV_OFFSETS=(0 50)

for required in "$TEMPLATE" "$DEDICATED" "$RUNTIME" "$BOOTSTRAP" "$README" "$COMPOSE_TEMPLATE" "${ENV_FILES[@]}"; do
  if [[ ! -e "$required" ]]; then
    echo "error: required scaffold source is missing: $required" >&2
    exit 1
  fi
done
if [[ -e "$TARGET" ]]; then
  echo "error: plugin already exists: $PLUGIN_ID" >&2
  exit 1
fi
for index in "${!ENV_FILES[@]}"; do
  cell_port=$(( PLUGIN_PORT + ${ENV_OFFSETS[$index]} ))
  if grep -q "=$cell_port\$" "${ENV_FILES[$index]}"; then
    echo "error: port already allocated: $cell_port in ${ENV_FILES[$index]}" >&2
    exit 1
  fi
done
if grep -q "localhost:$PLUGIN_PORT" "$DEDICATED" "$RUNTIME"; then
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

# ADR-055: two compose files, and the host port is a variable, not a number.
# The variable name is derived from the plugin id so nothing has to be chosen
# by hand and nothing can disagree between the two cells.
deploy = root / "demos" / "01-dictionary" / "deploy"
port_var = "PLUGIN_" + plugin_id.replace("-", "_").upper() + "_PORT"
origin = "http://localhost:${" + port_var + ":-" + port + "}"

# --- the cell's dedicated band: the plugin service and its release volume.
dedicated_path = deploy / "cell" / "compose.dedicated.yaml"
dedicated = dedicated_path.read_text()
template = (root / "scripts" / "templates" / "plugin-compose.yml.tpl").read_text()
stanza = (
    template.replace("__PLUGIN_PORT_VAR__", port_var)
    .replace("__PLUGIN_ID__", plugin_id)
    .replace("__PLUGIN_PORT__", port)
)
marker = "  # Announcer-only: this fixture has no code and no web server, by design\n"
if marker not in dedicated:
    raise SystemExit("error: Compose plugin insertion marker is missing")
dedicated = dedicated.replace(marker, stanza + "\n" + marker, 1)
volume_marker = "  example-plugin-incompatible-release:\n"
if volume_marker not in dedicated:
    raise SystemExit("error: Compose release-volume insertion marker is missing")
dedicated = dedicated.replace(volume_marker, volume_marker + "  " + plugin_id + "-release:\n", 1)
dedicated_path.write_text(dedicated)

# --- the cell's runtime band: mfe-registry-service's allowlist and fetch map.
# Both are required. The allowlist decides whether an entry pointing at this
# origin is accepted at all (BR-AS20); the fetch map translates that browser
# origin into the container address the manifest drift check dials (BR-AS45).
# Its twin REGISTRY_HEALTH_ORIGINS was retired in Phase 15c -- a new plugin
# needs no health wiring, because it reports itself on a subject derived from
# its own id.
runtime_path = deploy / "cell" / "compose.runtime.yaml"
runtime = runtime_path.read_text()
allow_marker = 'REGISTRY_ALLOWED_ORIGINS: "'
if allow_marker not in runtime:
    raise SystemExit("error: REGISTRY_ALLOWED_ORIGINS is missing")
runtime = runtime.replace(allow_marker, allow_marker + origin + ",", 1)
start = runtime.index("REGISTRY_FETCH_ORIGINS: >-")
end = runtime.index("}", start)
runtime = runtime[:end] + ',\n         "' + origin + '":"http://' + plugin_id + '-frontend:8080"' + runtime[end:]
runtime_path.write_text(runtime)

# --- one port variable per cell. za-1 holds the base port, au-1 is +50.
for env_name, offset in (("local-za-1.env", 0), ("local-au-1.env", 50)):
    env_path = deploy / "environments" / env_name
    env = env_path.read_text()
    env_marker = "PLUGIN_INCOMPATIBLE_PORT=%d\n" % (7115 + offset)
    if env_marker not in env:
        raise SystemExit("error: port-variable insertion marker is missing in " + env_name)
    env_path.write_text(env.replace(env_marker, env_marker + "%s=%d\n" % (port_var, int(port) + offset), 1))

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
echo "the plugin fixtures are not part of a cell -- start them with an extra -f:"
echo "  cd demos/01-dictionary/deploy/cell"
echo "  docker compose -p poc --env-file ../environments/local-za-1.env \\"
echo "    -f compose.yaml -f compose.dedicated.yaml -f ../global/compose.control.yaml up -d --build"
