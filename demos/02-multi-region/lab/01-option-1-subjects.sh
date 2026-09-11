#!/usr/bin/env bash
# OPTION 1 -- CHANGE THE SUBJECT. Put the region in the subject name.
#
# Same account. Same gateway. Nothing about the trust chain moves.
# The idea is to change only the filter on each stream:
#
#   ZA stream   filter evt.za.>
#   AU stream   filter evt.au.>     <- no longer overlaps
#
# Stage 0 showed why that alone is not enough. One account across a
# supercluster is ONE JetStream namespace, so the two streams cannot both be
# called ODOMETER. Option 1 therefore costs a region token TWICE: once in the
# subject, and once in the stream NAME.
#
#   ZA stream ODOMETER_ZA   filter evt.za.>   --cluster za
#   AU stream ODOMETER_AU   filter evt.au.>   --cluster au
#
# With that, it works. Publish evt.za.… in South Africa. The message still
# CROSSES the gateway -- nothing stops it -- it simply matches no filter on
# the far side, and is dropped there.
#
# The cost is not technical, it is rules we already spent:
#
#   - This repo decided that region is NOT a subject token
#     (ARCHITECTURE-COMMUNICATIONS section 2). Tenancy is the account
#     boundary; region is a deployment. Neither belongs in a subject.
#   - Streams are named for the domain, not the deployment. ODOMETER_ZA
#     breaks that too.
#   - It touches every publisher, every consumer, and every stream filter.
#   - It still leaves TWO write sides, with no story for merging them.
#
# In other words: it gives AU a real local stream, and fixes nothing else.
#
# CORRECTION 2026-09-11. This line used to read "it stops the double capture".
# There is no double capture to stop. With one account there is only ONE
# ODOMETER, so nothing is ever stored twice. What option 1 really buys is a
# SECOND stream -- AU stops reading ZA's log over the WAN and gets its own.
# It buys that with a region token in the subject AND in the stream name.
# See ../diagrams/gateway-double-capture-options-2.html.

source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

ZA="$(ctx linebooker za)"
AU="$(ctx linebooker au)"

cleanup() {
  run_sh "nats $ZA stream rm ODOMETER_ZA -f >/dev/null 2>&1 || true
         nats $AU stream rm ODOMETER_AU -f >/dev/null 2>&1 || true" >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup

banner "OPTION 1 -- A REGION TOKEN IN THE SUBJECT"

cat <<'TXT'
  account   LINEBOOKER      unchanged -- still one account, both regions
  gateway                   unchanged
  ZA stream ODOMETER_ZA     filter evt.za.>   --cluster za
  AU stream ODOMETER_AU     filter evt.au.>   --cluster au

  Two names, because one account cannot hold the same name twice.

  We will publish exactly one message, in ZA, on evt.za.…
TXT

out="$(run_sh "
  nats $ZA stream add ODOMETER_ZA --subjects 'evt.za.>' --replicas 1 --storage file --cluster za --defaults >/dev/null
  nats $AU stream add ODOMETER_AU --subjects 'evt.au.>' --replicas 1 --storage file --cluster au --defaults >/dev/null
  nats $ZA stream purge ODOMETER_ZA -f >/dev/null
  nats $AU stream purge ODOMETER_AU -f >/dev/null
  nats $ZA pub 'evt.za.odometer.vehicle.V1.travelled' '{"km":12.5}' >/dev/null
  sleep 1
  echo AFTER \$(nats $ZA stream info ODOMETER_ZA --json | jq .state.messages) \$(nats $AU stream info ODOMETER_AU --json | jq .state.messages)
" 2>&1 | grep -E '^AFTER')"

read -r _ after_za after_au <<<"$out"

printf '\n  ZA stream lives at: %s\n' "$(where "$ZA" ODOMETER_ZA)"
printf '  AU stream lives at: %s\n' "$(where "$AU" ODOMETER_AU)"
printf '\n  Two clusters, two created times. These really are two streams.\n'

if [[ "$after_au" -eq 0 && "$after_za" -eq 1 ]]; then
  scoreboard "FIXED -- but by spending two design rules. AU matched nothing." "$after_za" "$after_au"
else
  scoreboard "unexpected for this stage." "$after_za" "$after_au"
fi

cat <<'TXT'

WHAT ACTUALLY HAPPENED

  The message DID cross the gateway. It was not blocked. There is no wall
  here at all -- AU's stream simply had no filter that matched, so AU threw
  it away.

  That distinction matters. Option 1 is a naming convention, not a boundary.
  A typo in one filter, or one publisher using the wrong prefix, and ZA's
  event lands in AU's stream as well -- stored twice, in two streams, with no
  error anywhere. The server is not checking. You are.

  Note what DID hold the two streams apart physically: --cluster za and
  --cluster au. That is placement, and it is the right tool for "where does
  this live". It is not a security or correctness boundary either.

  Compare with option 3, where the server refuses.
TXT
