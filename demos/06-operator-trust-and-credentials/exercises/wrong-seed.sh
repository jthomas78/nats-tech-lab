#!/usr/bin/env bash
# NEGATIVE-TEST MACHINERY. Not part of the normal setup.
#
# Write a .creds file that holds the JWT of one user and the seed of another.
# The JWT is real; the seed that signs the server's nonce is not. Exercise 01
# uses it to show that a stolen JWT alone does not get in.
#
#   exercises/wrong-seed.sh <jwt-from.creds> <seed-from.creds> <out.creds>
set -euo pipefail

[[ $# -eq 3 ]] || { echo "usage: $0 <jwt-from.creds> <seed-from.creds> <out.creds>" >&2; exit 2; }

# In a .creds file the seed is the line after the BEGIN USER NKEY SEED line.
seed_of() { awk '/BEGIN USER NKEY SEED/ { getline; print; exit }' "$1"; }

good=$(seed_of "$1")
bad=$(seed_of "$2")
[[ -n "$good" && -n "$bad" ]] || { echo "no seed found in $1 or $2" >&2; exit 1; }

# Seeds are base32 (A-Z, 2-7), so they are safe in a sed pattern.
sed "s|^$good\$|$bad|" "$1" >"$3"
chmod 600 "$3"
echo "wrote $3 (JWT from $1, seed from $2)"
