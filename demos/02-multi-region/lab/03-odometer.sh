#!/usr/bin/env bash
# LAB 3 -- the odometer. Where does the truck's distance actually live?
#
# The question in plain words:
#   A truck drives 12.5 km, once, in South Africa. Two regions are joined by
#   a gateway. Afterwards, what does each region's odometer say, and WHERE is
#   the number stored?
#
# What you should see:
#   ONE account in both regions   -> there is only ONE odometer, and it sits
#                                    in whichever region built it first. The
#                                    other region reads it across the WAN.
#   ONE account PER region        -> each region owns its own odometer. The
#                                    number is right and it is local.
#
# CORRECTION 2026-09-11. An earlier version of this lab printed "25 km" here
# and called it a cross-region DOUBLE CAPTURE. That was wrong, twice over:
#
#   1. The 25 came from a bug in ../odometer/main.go. `defer sub.Unsubscribe()`
#      DELETES a durable pull consumer, so every `project --once` replayed the
#      stream from message 1 and added the same 12.5 km again. Measured: four
#      projector runs, ONE region, ONE account, ONE message in the stream ->
#      12.5, 25, 37.5, 50. No gateway was involved at all.
#   2. Even without the bug there is nothing to double-capture. Inside ONE
#      account a stream name is unique across the WHOLE supercluster, so the
#      second region's `stream add ODOMETER` is refused with 10058. One
#      account = one ODOMETER = one KV_vehicles. Two streams never existed.
#
# The conclusion did not change -- a region boundary must be an ACCOUNT
# boundary -- but the reason did. It is not "counted twice". It is "there is
# only one of everything, and you do not choose which region holds it".
#
# Why a number and not a message count. A stream count tells you how many
# messages were stored. It does not tell you whether the business is wrong.
#
# This is demo 02's only JetStream + CQRS example, on purpose. Demo 01
# already compared the read-model shapes; this demo is about where a message
# goes. Code and design: ../odometer/README.md.
#
# Needs Go on your Mac. The binary pins its dependencies in go.mod, so it is
# safe on a host. So is the nats CLI now -- see deploy/contexts.sh.

set -euo pipefail
cd "$(dirname "$0")/../odometer"

VEHICLE=V1
KM=12.5

od() { go run . "$@"; }
hr() { printf '\n%s\n' "----------------------------------------------------------"; }

# Read one vehicle's total out of a region, or 0 when the bucket is empty.
total_in() {
  local region="$1" account="$2"
  od show --region "$region" --account "$account" 2>/dev/null \
    | awk -v v="$VEHICLE" '$2==v { sub("totalKm=","",$3); print $3; found=1 }
                           END { if (!found) print 0 }'
}

# Which cluster physically holds the stream, as seen from one region.
home_of() {
  local region="$1" account="$2"
  od where --region "$region" --account "$account" 2>/dev/null || echo unknown
}

# One full run: wipe both regions, drive once in ZA, project both sides.
# $3 = "show" to let setup print its warnings.
drive_once() {
  local za_account="$1" au_account="$2" verbose="${3:-quiet}"
  for pair in "za:$za_account" "au:$au_account"; do
    od reset --region "${pair%%:*}" --account "${pair#*:}" >/dev/null 2>&1 || true
  done
  sleep 1
  for pair in "za:$za_account" "au:$au_account"; do
    if [[ "$verbose" == "show" ]]; then
      od setup --region "${pair%%:*}" --account "${pair#*:}" | sed 's/^/    /'
    else
      od setup --region "${pair%%:*}" --account "${pair#*:}" >/dev/null
    fi
  done
  od travelled --region za --account "$za_account" --vehicle "$VEHICLE" --km "$KM" >/dev/null
  sleep 2
  od project --region za --account "$za_account" --once >/dev/null
  od project --region au --account "$au_account" --once >/dev/null
}

cleanup() {
  for pair in "za:linebooker" "au:linebooker" "za:linebooker-za" "au:linebooker-au"; do
    od reset --region "${pair%%:*}" --account "${pair#*:}" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT

hr
cat <<'TXT'
LAB 3 -- THE ODOMETER

  event   evt.odometer.vehicle.V1.travelled   {"km": 12.5}
  stream  ODOMETER    LimitsPolicy, replicas 3
  read    KV bucket `vehicles`, key V1, value {"totalKm":..,"trips":..}

  KV only. There is no Postgres in this demo.
  We drive ONCE, 12.5 km, in South Africa. Twice: two different accounts.
TXT

# --- stage A -- one account everywhere -------------------------------------
hr
echo "STAGE A  ONE account, LINEBOOKER, valid in BOTH regions"
echo
echo "  Both regions ask for their own ODOMETER stream. Watch what AU is told."
echo

drive_once linebooker linebooker show
za="$(total_in za linebooker)"
au="$(total_in au linebooker)"
home="$(home_of au linebooker)"

printf '\n'
printf '    %-30s %s km\n' "odometer read from ZA:" "$za"
printf '    %-30s %s km\n' "odometer read from AU:" "$au"
printf '    %-30s %s km\n' "the truck actually drove:" "$KM"
printf '    %-30s %s\n'    "the stream really lives in:" "$home"
printf '\n'
if [[ "$za" == "$KM" && "$au" == "$KM" ]]; then
  echo "    VERDICT: the number is right, and that is the trap."
  echo "             Both lines above read the SAME bucket. There is only one."
  echo "             AU owns nothing. Every AU read crosses the WAN, and if"
  echo "             cluster $home goes down AU has no odometer at all."
else
  echo "    VERDICT: unexpected -- investigate before trusting this page."
fi

# --- stage B -- one account per region -------------------------------------
hr
echo "STAGE B  ONE account PER region, LINEBOOKER_ZA and LINEBOOKER_AU"
echo
echo "  Same subject. Same code. Same publish. Only the account differs."
echo

drive_once linebooker-za linebooker-au show
za="$(total_in za linebooker-za)"
au="$(total_in au linebooker-au)"
za_home="$(home_of za linebooker-za)"
au_home="$(home_of au linebooker-au)"

printf '\n'
printf '    %-30s %s km  (stream in %s)\n' "odometer in ZA:" "$za" "$za_home"
printf '    %-30s %s km  (stream in %s)\n' "odometer in AU:" "$au" "$au_home"
printf '    %-30s %s km\n' "the truck actually drove:" "$KM"
printf '\n'
if [[ "$za" == "$KM" && "$au" == "0" && "$za_home" == "za" && "$au_home" == "au" ]]; then
  echo "    VERDICT: CORRECT. Two odometers now exist, one per region."
  echo "             The trip is counted once, in the region that owns it,"
  echo "             and AU's own stream is real and local -- it is simply"
  echo "             empty, because this truck never drove in Australia."
else
  echo "    VERDICT: unexpected -- investigate before trusting this page."
fi

hr
cat <<'TXT'
WHAT THIS PROVES

  The account decided whether a second odometer could exist at all.
  Nothing else changed -- not the subject, not the stream name, not the
  projector, not the payload.

  Stage A: one account. `stream add ODOMETER` from the second region is
  refused with `stream name already in use (10058)`. That check is
  ACCOUNT-WIDE and PLACEMENT-BLIND, so `--cluster au` does not help. AU
  ends up reading ZA's stream over the gateway and does not know it.

  Stage B: one account per region. Both streams are created, both are local,
  and the trip is stored exactly once.

  A JetStream `domain` does not change Stage A, and it cannot. A gateway
  makes the two clusters ONE supercluster, and a supercluster is ONE
  JetStream system with ONE domain name. Both stages ran on the same domain,
  `lb`, because that is the only legal setting. Regions are kept apart by the
  ACCOUNT, and a stream is pinned to a region by PLACEMENT (--cluster za|au).

  So the region boundary must be an ACCOUNT boundary. That is option 3.

  WHAT THIS DOES NOT PROVE. It does not show a double capture. Over a
  gateway, one account cannot hold two overlapping streams, so a message
  cannot be stored twice. Double capture DOES happen in a hub-and-leaf
  topology, where each region is a separate JetStream system with its own
  domain and neither can see the other's subjects. See
  ../../03-multi-cluster-and-accounts/diagrams/meta-quorum-options.html.

  Related: ./01-the-wall.sh shows the account wall holding for plain
  messages. This shows what it is worth in money.
TXT
