#!/usr/bin/env bash
#
# Start demo 04, all of it, from a cold machine.
#
# Why this file exists (app-shell task 16k). The lab shell shows an operator a
# ONE-LINE recovery command, taken from `frontend/public/demo.json`. That line
# used to be `docker compose ... up -d`, which starts the NATS server and
# nothing else — so a reader who followed it got a demo whose readiness check
# still could not be answered, because nothing was listening on 20402 to
# answer it. The check reported `unknown`, which is honest and useless: the
# command it had just printed was not the command that fixes it.
#
# Four processes, not one:
#
#   1. NATS          docker, port 4422 (and the browser WebSocket on 20403)
#   2. cqrs serve    the command API on 127.0.0.1:20402, and /readyz with it
#   3. cqrs snapshotter   folds the log into the write-side KV bucket
#   4. cqrs projector     folds the log into the read-side KV bucket
#
# The stream and both KV buckets are created by the binary itself: every
# subcommand runs `ensureStream` and `ensureKV` before it dispatches (see
# cqrs/main.go). So there is no separate init step — starting `cqrs serve` IS
# the init step. Without the two projectors the buckets stay empty and the KV
# panels on the page have nothing to fold, which is why they are started here
# and not left to the reader.
#
# The three Go processes run in the background with their logs in
# `deploy/.run/`. `deploy/stop.sh` stops them and the container together.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
demo="$(dirname "$here")"
run="$here/.run"
mkdir -p "$run"

echo "==> NATS"
docker compose -f "$here/compose.yaml" up -d

# The compose healthcheck is the authority on "the server is up". Polling the
# monitor port ourselves would be a second opinion that can disagree with it.
echo -n "    waiting for lab4-nats to be healthy"
for _ in $(seq 1 60); do
  if [ "$(docker inspect -f '{{.State.Health.Status}}' lab4-nats 2>/dev/null || true)" = "healthy" ]; then
    echo " — healthy"
    break
  fi
  echo -n "."
  sleep 1
done

echo "==> building the CLI"
( cd "$demo/cqrs" && go build -o cqrs . )

# Started only if it is not already running, so this script converges rather
# than stacking a second copy of each projector on every run.
start() {
  local name="$1"; shift
  if pgrep -f "cqrs $name" >/dev/null 2>&1; then
    echo "    $name is already running"
    return
  fi
  echo "    starting $name (log: deploy/.run/$name.log)"
  # `</dev/null` matters: without it the child keeps the caller's stdin, and a
  # wrapper that pipes this script (`start.sh | tail`) waits for the child to
  # close it — so the script looks hung when it has in fact finished.
  ( cd "$demo/cqrs" && nohup ./cqrs "$name" </dev/null >"$run/$name.log" 2>&1 & echo $! >"$run/$name.pid" )
}

echo "==> command API and projectors"
start serve
start snapshotter
start projector

# The same question the lab shell asks. It is asked here too so the script
# does not report success before the demo can answer for itself.
echo -n "==> waiting for /readyz"
for _ in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:20402/readyz >/dev/null 2>&1; then
    echo " — ready"
    echo
    echo "Demo 04 is up. Standalone UI: http://localhost:20401 (npm run dev in frontend/)"
    echo "Embedded in the lab shell: http://localhost:7110/demo-04"
    exit 0
  fi
  echo -n "."
  sleep 1
done

echo " — NOT ready"
echo "The command API did not report ready. Look in deploy/.run/serve.log." >&2
exit 1
