#!/usr/bin/env bash
# STAGE 0 -- THE PROBLEM. One account across two regions is ONE JetStream.
#
# The setup is the shape the repo runs today:
#
#   ONE account, LINEBOOKER, valid in BOTH regions.
#   Ask for a stream called ODOMETER in ZA, placed on cluster za.
#   Ask for a stream called ODOMETER in AU, placed on cluster au.
#
# You do not get two streams. The second ask is REFUSED:
#
#   stream name already in use with a different configuration (10058)
#
# Why. A gateway joins the two clusters into one SUPERCLUSTER, and a
# supercluster is ONE JetStream namespace. Inside one account a stream name is
# unique across the whole thing. ZA took the name ODOMETER; AU cannot have it.
# And when AU asks about ODOMETER it is shown ZA's -- same cluster, same
# `created` timestamp.
#
# The JetStream `domain` does not save you, and it cannot. A domain names a
# JetStream system, and every server in a supercluster must carry the SAME
# name -- it may only change across a leaf-node link. We tried `za` and `au`
# here and it silently broke JetStream instead. See nats/nats.conf.
#
# So Australia is not a second region. It is a second door into South Africa's.
# Every AU service that "writes to its own stream" is writing to ZA's, and
# every AU consumer reads ZA's log. See ./03-odometer.sh for what that costs.
#
# CORRECTION 2026-09-11. This header used to say the refusal is "where the
# double capture comes from". There is no double capture here. One account
# holds ONE ODOMETER and ONE KV_vehicles, so a message cannot be stored twice
# and a total cannot be added twice. The cost is different and worse: AU owns
# nothing, pays WAN latency on every read, and loses the stream completely if
# cluster za dies. Double capture is real in a HUB-AND-LEAF topology, where
# each side is a separate JetStream system. Full write-up:
# ../diagrams/gateway-double-capture-options-2.html.
#
# Runs clean every time: it deletes its stream first and at the end.

source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

ZA="$(ctx linebooker za)"
AU="$(ctx linebooker au)"
SUBJECT="evt.odometer.vehicle.V1.travelled"

cleanup() {
  run_sh "nats $ZA stream rm ODOMETER -f >/dev/null 2>&1 || true
         nats $AU stream rm ODOMETER -f >/dev/null 2>&1 || true" >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup

banner "STAGE 0 -- THE PROBLEM"

cat <<'TXT'
  account   LINEBOOKER      one account, both regions
  ZA        ask for stream ODOMETER, filter evt.>, --cluster za
  AU        ask for stream ODOMETER, filter evt.>, --cluster au

  Watch the second ask.
TXT

run_sh "nats $ZA stream add ODOMETER --subjects 'evt.>' --replicas 1 --storage file --cluster za --defaults" >/dev/null 2>&1
printf '\n  ZA asked. Result: created.\n'

au_add="$(run_sh "nats $AU stream add ODOMETER --subjects 'evt.>' --replicas 1 --storage file --cluster au --defaults" 2>&1 | grep -i 'error' || true)"
if [[ -n "$au_add" ]]; then
  printf '  AU asked. Result: REFUSED.\n    %s\n' "$au_add"
else
  printf '  AU asked. Result: accepted -- unexpected, read the notes below.\n'
fi

za_where="$(where "$ZA" ODOMETER)"
au_where="$(where "$AU" ODOMETER)"

printf '\n  ZA context reports: %s\n' "$za_where"
printf '  AU context reports: %s\n' "$au_where"

if [[ "$za_where" == "$au_where" ]]; then
  printf '\n  SAME cluster, SAME created timestamp. There is only ONE stream.\n'
else
  printf '\n  Two different streams. That is NOT what this stage expects -- read on.\n'
fi

out="$(run_sh "
  nats $ZA stream purge ODOMETER -f >/dev/null
  echo BEFORE \$(nats $ZA stream info ODOMETER --json | jq .state.messages) \$(nats $AU stream info ODOMETER --json | jq .state.messages)
  nats $ZA pub '$SUBJECT' '{"km":12.5}' >/dev/null
  sleep 1
  echo AFTER \$(nats $ZA stream info ODOMETER --json | jq .state.messages) \$(nats $AU stream info ODOMETER --json | jq .state.messages)
" 2>&1 | grep -E '^(BEFORE|AFTER)')"

read -r _ before_za before_au <<<"$(echo "$out" | grep '^BEFORE')"
read -r _ after_za  after_au  <<<"$(echo "$out" | grep '^AFTER')"

echo
echo "  purged: ZA=$before_za AU=$before_au   (one purge emptied both -- same stream)"
echo "  published once in ZA on $SUBJECT"

if [[ "$za_where" == "$au_where" && "$after_za" -eq 1 && "$after_au" -eq 1 ]]; then
  scoreboard "BROKEN. AU has no stream of its own -- it is reading ZA's." "$after_za" "$after_au"
else
  scoreboard "unexpected for this stage -- see the notes above." "$after_za" "$after_au"
fi

cat <<'TXT'

WHAT THE TWO NUMBERS REALLY MEAN

  They are NOT two stored copies. They are ONE stream, read twice, because
  both contexts are looking at the same object.

  That is worse, not better. AU's writer writes into ZA's log. AU's projector
  reads ZA's log. Every one of those trips crosses the WAN, and nothing warns
  you. Nobody chose which region holds the data; the first one to ask for the
  name took it.

  If ZA goes off the air, AU loses the stream -- because AU never had one.
  If AU goes off the air, AU loses nothing, for the same reason.

  What does NOT happen here is a double capture. There is one stream and one
  KV bucket, so the 12.5 km trip is stored once and counted once. See
  ./03-odometer.sh.

WHAT DOES NOT FIX IT

  A JetStream domain. It names a JetStream system, and a supercluster must
  use one name everywhere. Per-region domains are not a boundary; they just
  stop the servers finding each other. Measured, then reversed -- the story
  is in nats/nats.conf.

  Placement (--cluster au). It decides WHERE a stream lives, and it works.
  What it cannot do is make a SECOND stream with a name this account has
  already used. That is the 10058 above.

  Two ways out, and only one of them is a boundary.
    ./01-option-1-subjects.sh   change the SUBJECT -- a convention
    ./03-odometer.sh            change the ACCOUNT -- a wall, in kilometres
TXT
