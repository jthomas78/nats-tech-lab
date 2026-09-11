#!/usr/bin/env bash
# LAB 3 -- the odometer. Is the double capture real? Read the number.
#
# The question in plain words:
#   A truck drives 12.5 km, once. Two regions are joined by a gateway. What
#   does the odometer say afterwards?
#
# What you should see:
#   ONE account in both regions   -> the odometer says 25 km. Wrong.
#   ONE account PER region        -> the odometer says 12.5 km. Right.
#
# Why a number and not a message count. A stream count tells you how many
# messages were stored. It does not tell you whether the business is wrong.
# 25 km for a 12.5 km trip is not a rounding question. It is proof.
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

# Same three lines every stage. Only the account changes.
#   $1 label   $2 za total   $3 au total   $4 fleet total
scoreboard() {
  printf '\n'
  printf '    %-26s %s km\n' "odometer in ZA:"  "$2"
  printf '    %-26s %s km\n' "odometer in AU:"  "$3"
  printf '    %-26s %s km\n' "the fleet believes:" "$4"
  printf '    %-26s %s km\n' "the truck actually drove:" "$KM"
  printf '\n'
  printf '    VERDICT: %s\n' "$1"
}

# Read one vehicle's total out of a region, or 0 when the bucket is empty.
total_in() {
  local region="$1" account="$2"
  od show --region "$region" --account "$account" 2>/dev/null \
    | awk -v v="$VEHICLE" '$2==v { sub("totalKm=","",$3); print $3; found=1 }
                           END { if (!found) print 0 }'
}

# One full run: wipe both regions, drive once in ZA, project both sides.
drive_once() {
  local za_account="$1" au_account="$2"
  for pair in "za:$za_account" "au:$au_account"; do
    od reset --region "${pair%%:*}" --account "${pair#*:}" >/dev/null
    od setup --region "${pair%%:*}" --account "${pair#*:}" >/dev/null
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
echo "  Each region runs its own projector. Both are valid listeners for the"
echo "  same subject in the same account, so the gateway serves them both."

drive_once linebooker linebooker
za="$(total_in za linebooker)"
au="$(total_in au linebooker)"
fleet="$(awk -v a="$za" -v b="$au" 'BEGIN { print a + b }')"

if [[ "$fleet" != "$KM" ]]; then
  scoreboard "BROKEN. One trip, counted more than once." "$za" "$au" "$fleet"
else
  scoreboard "the total was right -- unexpected for this stage." "$za" "$au" "$fleet"
fi

# --- stage B -- one account per region -------------------------------------
hr
echo "STAGE B  ONE account PER region, LINEBOOKER_ZA and LINEBOOKER_AU"
echo
echo "  Same subject. Same code. Same publish. Only the account differs."

drive_once linebooker-za linebooker-au
za="$(total_in za linebooker-za)"
au="$(total_in au linebooker-au)"
fleet="$(awk -v a="$za" -v b="$au" 'BEGIN { print a + b }')"

if [[ "$fleet" == "$KM" ]]; then
  scoreboard "CORRECT. One trip, counted once, in the region that owns it." "$za" "$au" "$fleet"
else
  scoreboard "the total is still wrong -- investigate." "$za" "$au" "$fleet"
fi

hr
cat <<'TXT'
WHAT THIS PROVES

  The account decided the number. Nothing else changed -- not the subject,
  not the stream, not the projector, not the payload.

  A JetStream `domain` does not save stage A, and it cannot. A gateway makes
  the two clusters ONE supercluster, and a supercluster is ONE JetStream
  system with ONE domain name. Both stages ran on the same domain, `lb`,
  because that is the only legal setting. Regions are kept apart by the
  ACCOUNT, and a stream is pinned to a region by PLACEMENT (--cluster za|au).

  So the region boundary must be an ACCOUNT boundary. That is option 3, and
  this is the number that pays for it.

  Related: ./01-the-wall.sh shows the account wall holding for plain
  messages. This shows what it is worth in money.
TXT
