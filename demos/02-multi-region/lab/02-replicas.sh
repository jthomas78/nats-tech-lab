#!/usr/bin/env bash
# LAB 2 -- what does Replicas 1 vs 3 cost when a node dies?
#
# The question in plain words:
#   A cluster has three servers. A JetStream stream keeps its messages on
#   Replicas servers out of those three. Default is 1. So what happens to
#   your data when the ONE server holding it goes away?
#
# What you should see:
#   R1 stream, stop its server  -> the stream stops answering. Data stuck.
#   R3 stream, stop its leader  -> a new leader in a second. Data still there.
#
# ONE STREAM AT A TIME, ON PURPOSE.
#   Demo 02 has exactly one stream name, ODOMETER, and never more than one
#   stream alive per region. So this lab does not build an R1 and an R3
#   stream side by side. It runs ODOMETER twice: pass 1 at R1, delete it,
#   pass 2 at R3. You read two blocks instead of one table. Same lesson.
#
# Why that matters: demo 01 runs its SHIPPING stream at Replicas 1. This
# script is what that looks like when the wrong container stops.
#
# The script puts everything back at the end.

set -euo pipefail
cd "$(dirname "$0")"

# The nats CLI runs on your machine. The lab2-linebooker-za context already
# lists ALL THREE host ports (4621, 4622, 4623), not one -- a client that
# knows only the server we are about to stop cannot tell "my stream is gone"
# from "my one address is gone", and this script measures the first.
CTX=(--context lab2-linebooker-za)

STREAM="ODOMETER"
SUBJECT="evt.odometer.vehicle.V1.travelled"

command -v nats >/dev/null 2>&1 || {
  echo "the nats CLI is not on your PATH -- brew install nats-io/nats-tools/nats" >&2
  exit 1
}
nats context info lab2-linebooker-za >/dev/null 2>&1 || {
  echo "no lab2-* contexts yet -- run ../deploy/contexts.sh" >&2
  exit 1
}
hr() { printf '\n%s\n' "--------------------------------------------------"; }

# Put everything back: the stopped server AND the stream.
#
# The servers come back first. A stream cannot be removed while the server
# holding it is down, and an R1 stream has only one -- so the order here is
# not a style choice. Give the cluster a moment to elect before deleting.
restore() {
  docker start lab2-za-1 lab2-za-2 lab2-za-3 >/dev/null 2>&1 || true
  sleep 3
  nats "${CTX[@]}" stream rm "$STREAM" --force >/dev/null 2>&1 || true
}
cleanup() {
  echo
  echo "==> putting the cluster back and removing $STREAM"
  restore
}
trap cleanup EXIT

leader_of() {
  nats "${CTX[@]}" stream info "$1" --json 2>/dev/null | jq -r '.cluster.leader'
}

# One pass: build ODOMETER at N replicas, stop the server holding it, report.
#   $1 replicas   $2 the sentence that says what to expect
run_pass() {
  local replicas="$1" expect="$2" leader victim

  # Start from nothing. cleanup() removes the stream at the end too, so this
  # only matters after a run that was killed part-way -- but a lab that
  # quietly reuses last run's stream adds a second message and stops meaning
  # what the text says.
  nats "${CTX[@]}" stream rm "$STREAM" --force >/dev/null 2>&1 || true

  hr
  echo "PASS: $STREAM at Replicas $replicas"
  echo "  $expect"
  echo

  nats "${CTX[@]}" stream add "$STREAM" \
      --subjects "$SUBJECT" --replicas "$replicas" \
      --storage file --retention limits --discard old \
      --max-msgs=-1 --max-bytes=-1 --max-age=1h --max-msg-size=-1 \
      --dupe-window=2m --no-allow-rollup --deny-delete --deny-purge \
      --defaults >/dev/null
  nats "${CTX[@]}" pub "$SUBJECT" '{"km":12.5}' >/dev/null 2>&1
  echo "  created. replicas=$replicas  subject=$SUBJECT  (1 message in)"

  leader="$(leader_of "$STREAM")"
  if [[ "$replicas" == "1" ]]; then
    echo "  it lives on: $leader   <- only this one server"
  else
    echo "  its leader is: $leader   <- plus two followers"
  fi

  victim="lab2-${leader}"
  echo
  echo "  ==> stopping $victim"
  docker stop "$victim" >/dev/null
  sleep 6

  printf '  AFTER THE FAILURE: '
  if nats "${CTX[@]}" stream info "$STREAM" --json >/tmp/lab2-r"$replicas".json 2>/tmp/lab2-r"$replicas".err; then
    echo "still answering. leader $(jq -r '.cluster.leader' /tmp/lab2-r"$replicas".json), $(jq -r '.state.messages' /tmp/lab2-r"$replicas".json) message(s) safe."
  else
    echo "GONE. Error was:"
    sed 's/^/      /' /tmp/lab2-r"$replicas".err
  fi

  # Bring the cluster back and clear the stream before the next pass, so only
  # ONE stream is ever alive in this region.
  echo
  echo "  ==> restoring the cluster"
  restore
}

run_pass 1 "expect: the stream stops answering."
run_pass 3 "expect: a new leader, and the message is still there."

hr
cat <<'TXT'
WHAT THIS PROVES

  Three servers is not the same as three copies. A stream keeps its data on
  the number of servers YOU ask for, and the default is one.

  R1 on a 3-node cluster buys you nothing when the wrong node stops. R3
  survives one loss and elects a new leader on its own.

  Cost of R3: every write goes to three servers, so it is slower and uses
  three times the disk. That is the trade -- make it on purpose, per stream.

  Demo 01 is one server, so R1 is all it can have. This choice only exists
  here, where there is a cluster.
TXT
