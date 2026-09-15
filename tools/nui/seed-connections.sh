#!/usr/bin/env bash
# Seed NUI's connection list for every demo in this lab.
#
# NUI has no config-as-code, but it does have a REST API, so this script is
# the config. Run it once after `up -d`; the nui-db volume keeps the result.
# It is idempotent: a connection whose name already exists is deleted and
# re-created, so re-running it after an edit here is the way to apply it.
#
#   ./tools/nui/seed-connections.sh
#
# Every URL is host.docker.internal, not localhost: NUI is a container and
# dials back out to the host, where each demo publishes its NATS port.
#
# Auth mode "auth_creds_file" takes a path INSIDE the NUI container. The
# compose file mounts each operator-mode demo's creds directory read-only
# under /creds/<demo>.
set -euo pipefail

NUI="${NUI_URL:-http://localhost:31311}"

# name | hosts (comma-separated) | creds path in container ("" = no auth)
CONNECTIONS=(
  "demo01-za-1|nats://host.docker.internal:4222|/creds/demo01/platform.creds"
  "demo02-za|nats://host.docker.internal:4621,nats://host.docker.internal:4622,nats://host.docker.internal:4623|/creds/demo02/platform.creds"
  "demo02-au|nats://host.docker.internal:4721,nats://host.docker.internal:4722,nats://host.docker.internal:4723|/creds/demo02/platform.creds"
  "demo04-odometer|nats://host.docker.internal:4422|"
)

existing=$(curl -fsS "$NUI/api/connection")

for row in "${CONNECTIONS[@]}"; do
  IFS='|' read -r name hosts creds <<<"$row"

  # Delete any connection already holding this name, so the script can run twice.
  for id in $(printf '%s' "$existing" | NAME="$name" python3 -c '
import json, os, sys
want = os.environ["NAME"]
print(" ".join(c["id"] for c in json.load(sys.stdin) if c["name"] == want))
'); do
    curl -fsS -X DELETE "$NUI/api/connection/$id" >/dev/null
  done

  body=$(NAME="$name" HOSTS="$hosts" CREDS="$creds" python3 -c '
import json, os
auth = []
if os.environ["CREDS"]:
    auth = [{"active": True, "mode": "auth_creds_file", "creds": os.environ["CREDS"]}]
print(json.dumps({
    "name": os.environ["NAME"],
    "hosts": os.environ["HOSTS"].split(","),
    "auth": auth,
    "subscriptions": [],
}))
')

  curl -fsS -X POST "$NUI/api/connection" \
    -H 'Content-Type: application/json' -d "$body" >/dev/null
  echo "seeded $name"
done

echo
echo "Open $NUI"
