# Shared bits for the option labs. Sourced, never run on its own.
#
# Every stage in this lab asks the SAME question:
#
#   I published ONE message in South Africa. How many are in ZA? How many in AU?
#
# So every stage prints the same two numbers. Only the wiring behind them
# changes. That is the whole argument, and it is why these helpers exist --
# each script should read as setup, one publish, two numbers.

set -euo pipefail

# The nats CLI runs on YOUR machine, not in a container. So every address is
# a published host port, and `za-1` is never used -- your Mac cannot resolve a
# Docker name.
#
#   za-1 4621  za-2 4622  za-3 4623      au-1 4721  au-2 4722  au-3 4723
#
# Contexts are registered by deploy/contexts.sh and all start `lab2-`, so they
# cannot collide with demo 01's `sys` and `platform` in the same store.

DEPLOY="$(cd "$(dirname "${BASH_SOURCE[0]}")/../deploy" && pwd)"

command -v nats >/dev/null 2>&1 || {
  echo "the nats CLI is not on your PATH -- brew install nats-io/nats-tools/nats" >&2
  exit 1
}
nats context info lab2-linebooker-za >/dev/null 2>&1 || {
  echo "no lab2-* contexts yet -- run $DEPLOY/contexts.sh" >&2
  exit 1
}

# Run one shell line. It used to go into a container; it does not any more.
# Stages still batch their calls into one line, because the text reads as
# "setup, one publish, two numbers" and that grouping is the point.
run_sh() { bash -c "$1"; }

# Build the CLI arguments for one side. $1 = account label, $2 = za|au.
#
# A label that already names a region (linebooker-za) is a SPLIT account --
# one per region. A label without one (linebooker) is the SHARED account that
# spans both, and it needs a `-shared-<region>` context per side. Those are
# two different accounts and the difference is the whole demo.
#
# The context carries the servers and the one shared JetStream domain (`lb`).
# The domain does NOT keep the regions apart. Both clusters are one
# supercluster, so they are one JetStream namespace, and a domain names a
# JetStream system -- it must be the SAME on every server of a supercluster.
# What keeps regions apart is the ACCOUNT, and inside one account, PLACEMENT
# (`--cluster za` / `--cluster au`). See the comment in nats/nats.conf.
ctx() {
  local name="$1" region="$2"
  case "$name" in
    *-za|*-au) printf -- '--context lab2-%s' "$name" ;;
    *)         printf -- '--context lab2-%s-shared-%s' "$name" "$region" ;;
  esac
}

# Where did a stream really land, and when was it really made? Two streams with
# one name in one account are impossible here, and this is how a stage proves
# it: identical cluster AND identical `created` means it is ONE stream seen
# twice, not two.
#   $1 context args (from ctx)   $2 stream name
where() {
  # shellcheck disable=SC2086
  nats $1 stream info "$2" --json 2>/dev/null \
    | jq -r '"cluster=" + .cluster.name + " created=" + .created' \
    || echo "cluster=? created=?"
}

hr() { printf '\n%s\n' "----------------------------------------------------------"; }

banner() {
  hr
  printf '%s\n' "$1"
  hr
}

# The scoreboard every stage prints.
#   $1 label   $2 za count   $3 au count
scoreboard() {
  printf '\n'
  printf '    %-22s %s\n' "messages in ZA:" "$2"
  printf '    %-22s %s\n' "messages in AU:" "$3"
  printf '    %-22s %s\n' "publishes sent:" "1"
  printf '\n'
  printf '    VERDICT: %s\n' "$1"
}
