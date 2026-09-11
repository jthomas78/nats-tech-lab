#!/usr/bin/env bash
# LAB 1 -- does an account boundary hold across a gateway?
#
# The question in plain words:
#   Two regions are joined by a gateway. A gateway carries interest between
#   clusters. So if South Africa publishes on a subject, can Australia hear
#   it? And does it matter which ACCOUNT each side is in?
#
# What you should see:
#   SAME account, both regions  -> the message arrives. The gateway works.
#   DIFFERENT accounts          -> silence. The wall holds.
#
# Why that matters: the account is the ONLY hard wall in NATS. It is enforced
# by the server, not by a naming rule anyone can typo. One account per tenant
# means one tenant can never read another tenant's traffic, no matter what
# subject either of them uses.

set -euo pipefail
cd "$(dirname "$0")"

# The nats CLI runs on your machine. Contexts come from deploy/contexts.sh and
# carry the host ports (za 4621-4623, au 4721-4723) and the JetStream domain.
command -v nats >/dev/null 2>&1 || {
  echo "the nats CLI is not on your PATH -- brew install nats-io/nats-tools/nats" >&2
  exit 1
}
nats context info lab2-linebooker-za >/dev/null 2>&1 || {
  echo "no lab2-* contexts yet -- run ../deploy/contexts.sh" >&2
  exit 1
}

# Listen for one message, and give up after N seconds no matter what.
#
# `nats sub --timeout` is NOT a give-up timer -- it is how long the CLI waits
# for a reply from the server. A plain `nats sub` runs forever. macOS has no
# `timeout` command, so the give-up is a background sleep that kills the
# subscriber. `--count 1` still lets it exit early when a message arrives.
#
# The pid goes in a global, not a `$(...)` -- a command substitution runs in a
# subshell, and a job started there is not a child of THIS shell, so `wait`
# would refuse it.
SUB_PID=""
listen() {
  local ctx="$1" server="$2" subject="$3" out="$4"
  nats --context "$ctx" --server "$server" sub "$subject" --count 1 \
    >"$out" 2>&1 &
  SUB_PID=$!
  ( sleep 8; kill "$SUB_PID" 2>/dev/null ) >/dev/null 2>&1 &
}

hr() { printf '\n%s\n' "--------------------------------------------------"; }

hr
echo "TEST 1  same account, two regions -- the gateway should carry it"
echo
echo "  subscriber: linebooker-za in AUSTRALIA  (au-1)"
echo "  publisher:  linebooker-za in SOUTH AFRICA (za-1)"

# The subscriber runs in the background inside its own container and writes
# what it hears to a file we read afterwards. --count 1 makes it exit on its
# own, so nothing is left running.
listen lab2-linebooker-za nats://localhost:4721 lab.wall /tmp/lab2-same.txt
sleep 3

nats --context lab2-linebooker-za pub 'lab.wall' 'hello from ZA' >/dev/null

wait "$SUB_PID" || true
if grep -q 'hello from ZA' /tmp/lab2-same.txt; then
  echo "  RESULT: HEARD IT. The gateway carries interest between regions."
else
  echo "  RESULT: nothing arrived -- unexpected. Raw output:"
  sed 's/^/    /' /tmp/lab2-same.txt
fi

hr
echo "TEST 2  different accounts, two regions -- the wall should hold"
echo
echo "  subscriber: linebooker-au in AUSTRALIA  (au-1)"
echo "  publisher:  linebooker-za in SOUTH AFRICA (za-1)"
echo "  SAME SUBJECT as test 1. Only the account differs."

listen lab2-linebooker-au nats://localhost:4721 lab.wall /tmp/lab2-cross.txt
sleep 3

nats --context lab2-linebooker-za pub 'lab.wall' 'hello from ZA' >/dev/null

wait "$SUB_PID" || true
if grep -q 'hello from ZA' /tmp/lab2-cross.txt; then
  echo "  RESULT: HEARD IT -- THE WALL LEAKED. That is a bug, investigate."
else
  echo "  RESULT: silence. The account wall held across the gateway."
fi

hr
cat <<'TXT'
WHAT THIS PROVES

  The subject was identical in both tests. The region was identical. The
  only thing that changed was the account -- and that alone decided whether
  the message crossed.

  So tenant isolation does NOT depend on getting subject names right. It is
  a property of the account, checked by the server on every message.

  This is why the repo settled on one account per tenant (option 3). See
  .claude/memory/gateway_double_capture_and_option3.md.
TXT
