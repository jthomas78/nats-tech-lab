#!/usr/bin/env bash
# Log in to WorkOS with the CLI Auth device flow and keep the access token.
#   lab/workos-login.sh dispatcher
# <name> is only the label of the saved file. It does not pick the user: you
# pick the user by who you log in as in the browser.
# Opens nothing by itself: it prints a URL and a code; you log in in a browser.
# Gives up when the device code expires (expires_in from WorkOS).
# Saves the access token to .run/workos/<name>.token (mode 600) and prints its
# claims. It never prints the token. The refresh token is not kept.
# The claims are decoded, NOT verified: this script does not check the
# signature. The Go callout service does that.
set -euo pipefail
source "$(dirname "$0")/lib.sh"

name=${1:?usage: lab/workos-login.sh <name, e.g. dispatcher>}
env_file="$D07_DIR/.env.workos"
[[ -f "$env_file" ]] || { echo "no $env_file. Write D07_WORKOS_CLIENT_ID and D07_WORKOS_ORG_ID into it first (WORKOS-SETUP.md, step 8)." >&2; exit 1; }
source "$env_file"
: "${D07_WORKOS_CLIENT_ID:?D07_WORKOS_CLIENT_ID is not set in .env.workos}"

api=https://api.workos.com/user_management
# Every HTTP call is bounded, so a stalled network never hangs the script.
http() { curl -sS --connect-timeout 5 --max-time 15 "$@"; }

dev=$(http -X POST "$api/authorize/device" -d client_id="$D07_WORKOS_CLIENT_ID")
device_code=$(jq -r '.device_code // empty' <<<"$dev")
[[ -n "$device_code" ]] || { echo "device authorization failed: $(jq -c . <<<"$dev")" >&2; exit 1; }
interval=$(jq -r '.interval' <<<"$dev")
expires_in=$(jq -r '.expires_in' <<<"$dev")
deadline=$(( $(date +%s) + expires_in ))

echo "Open this URL and log in as the $name test user:"
echo "  $(jq -r '.verification_uri_complete' <<<"$dev")"
echo "Code: $(jq -r '.user_code' <<<"$dev")  (valid $expires_in s)"

while :; do
  sleep "$interval"
  if (( $(date +%s) >= deadline )); then
    echo "login timed out: the code expired after $expires_in s. Run the script again." >&2
    exit 1
  fi
  resp=$(http -X POST "$api/authenticate" \
    -d grant_type=urn:ietf:params:oauth:grant-type:device_code \
    -d device_code="$device_code" \
    -d client_id="$D07_WORKOS_CLIENT_ID")
  token=$(jq -r '.access_token // empty' <<<"$resp")
  [[ -n "$token" ]] && break
  code=$(jq -r '.error // .code // "unknown"' <<<"$resp")
  case "$code" in
    authorization_pending) ;;
    slow_down) interval=$((interval + 5)) ;;
    *) echo "login failed: $code $(jq -r '.error_description // .message // empty' <<<"$resp")" >&2; exit 1 ;;
  esac
done

mkdir -p "$D07_RUN/workos"
out="$D07_RUN/workos/$name.token"
( umask 077; printf '%s' "$token" > "$out" )
echo "saved    $out (the token itself is not shown)"
echo "claims (decoded only; the signature is NOT checked here):"
claims=$(jq -R 'split(".")[1] | gsub("-";"+") | gsub("_";"/") | @base64d | fromjson' "$out")
jq '{iss, sub, org_id, role, roles, sid,
     iat: (.iat | todate), exp: (.exp | todate), lifetime_s: (.exp - .iat)}' <<<"$claims"

org=$(jq -r '.org_id // empty' <<<"$claims")
if [[ -z "${D07_WORKOS_ORG_ID:-}" ]]; then
  echo "org_id   not compared: D07_WORKOS_ORG_ID is not set in .env.workos" >&2
elif [[ "$org" == "$D07_WORKOS_ORG_ID" ]]; then
  echo "org_id   matches D07_WORKOS_ORG_ID"
else
  echo "org_id   does NOT match D07_WORKOS_ORG_ID. The callout service will refuse this token." >&2
fi
