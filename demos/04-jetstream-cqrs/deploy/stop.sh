#!/usr/bin/env bash
#
# Stop everything start.sh started: the three Go processes first, then the
# container. The processes are stopped by the PID files start.sh wrote, so a
# `cqrs` you started yourself in another terminal is left alone.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
run="$here/.run"

for name in projector snapshotter serve; do
  pid_file="$run/$name.pid"
  [ -f "$pid_file" ] || continue
  pid="$(cat "$pid_file")"
  if kill -0 "$pid" 2>/dev/null; then
    echo "    stopping $name (pid $pid)"
    kill "$pid" 2>/dev/null || true
  fi
  rm -f "$pid_file"
done

echo "==> NATS"
docker compose -f "$here/compose.yaml" down
