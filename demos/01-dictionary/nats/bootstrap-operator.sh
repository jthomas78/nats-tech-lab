#!/usr/bin/env bash
# Generates the operator-mode JWT/NKey artifacts for demo 01's NATS server
# (Phase 14a — .claude/plans/Main-POC-Plan.md). Run on the host (not in
# Docker) whenever nats/nats.conf needs to move to operator mode from
# scratch, or after --force to regenerate everything.
#
# Requires the `nsc` CLI (https://github.com/nats-io/nsc) — not vendored,
# expected on $PATH.
#
# Idempotent: exits early if nats/operator.jwt already exists, unless --force
# is passed. --force wipes every generated artifact and starts over — this
# invalidates every previously-issued account/user JWT and .creds file, so
# any running docker-compose stack needs `docker compose down -v` and
# `up --build` afterward (same operational note as Phase 13a's nats.conf
# changes; see .claude/memory/nats_volume_legacy_messages.md).
#
# Outputs (checked into the repo — spike-only, never production artifacts,
# same plaintext-fixture precedent as Phase 13's accounts{} block):
#   nats/operator.jwt            operator JWT (public — servers load this directly)
#   nats/resolver/*.jwt          one JWT per account, for resolver_preload
#   nats/creds/*.creds           one .creds file per service/account user
#   nats/keys/operator-signing-key.nk
#                                 operator signing key seed — the one artifact
#                                 a real deployment would keep in a secrets
#                                 manager, not a repo; needed by
#                                 accounts-service (Phase 14b) to mint new
#                                 account JWTs at runtime.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
NATS_DIR="$(pwd)"

FORCE=0
if [[ "${1:-}" == "--force" ]]; then
  FORCE=1
fi

if [[ -f "$NATS_DIR/operator.jwt" && "$FORCE" -eq 0 ]]; then
  echo "operator.jwt already exists — skipping (pass --force to regenerate)"
  exit 0
fi

if ! command -v nsc >/dev/null 2>&1; then
  echo "error: nsc CLI not found on PATH — https://github.com/nats-io/nsc" >&2
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "error: jq not found on PATH" >&2
  exit 1
fi

rm -rf "$NATS_DIR/.nsc-store" "$NATS_DIR/resolver" "$NATS_DIR/creds" "$NATS_DIR/keys" "$NATS_DIR/operator.jwt"
mkdir -p "$NATS_DIR/resolver" "$NATS_DIR/creds" "$NATS_DIR/keys"

# Isolate this script's nsc store from the operator's real ~/.local/share
# store — every artifact this repo needs is exported below and the scratch
# store is discarded at the end, so nothing here depends on nsc state
# surviving between runs.
export NKEYS_PATH="$NATS_DIR/.nsc-store/keys"
export NSC_HOME="$NATS_DIR/.nsc-store/store"
mkdir -p "$NKEYS_PATH" "$NSC_HOME"
nsc env -s "$NSC_HOME" >/dev/null

echo "==> operator + SYS account"
nsc add operator lab-operator --sys >/dev/null
nsc edit operator --sk generate >/dev/null # operator signing key: accounts-service (14b) mints new accounts with this, never the root operator key

declare -A JS_LIMITS=(
  [PLATFORM]="1G 5G 20 100"
  [ACME]="256M 1G 10 20"
  [GLOBEX]="256M 1G 10 20"
)

for account in PLATFORM ACME GLOBEX; do
  read -r mem file streams consumers <<<"${JS_LIMITS[$account]}"
  echo "==> account $account (mem=$mem file=$file streams=$streams consumers=$consumers)"
  nsc add account "$account" >/dev/null
  nsc edit account "$account" --sk generate >/dev/null # account signing key: 14b mints per-account users with this
  nsc edit account "$account" \
    --js-mem-storage "$mem" \
    --js-disk-storage "$file" \
    --js-streams "$streams" \
    --js-consumer "$consumers" >/dev/null
  user="$(echo "$account" | tr '[:upper:]' '[:lower:]')"
  nsc add user --account "$account" "$user" >/dev/null
  nsc generate creds --account "$account" --name "$user" >"$NATS_DIR/creds/$user.creds"
done

platform_pub="$(nsc describe account PLATFORM --json | jq -r '.sub')"

echo "==> PLATFORM exports (Phase 21 account imports)"
for op in item.get type.list item.get-versioned locales.list; do
  nsc add export --account PLATFORM --service \
    --subject "rpc.*.refdata.${op}.v1" >/dev/null
done
nsc add export --account PLATFORM --service \
  --subject "rpc._platform.refdata.context.list.v1" >/dev/null
nsc add export --account PLATFORM --subject "notify.accounts.account.*" >/dev/null
nsc add export --account PLATFORM --subject "evt.*.refdata.*.changed" >/dev/null

for account in ACME GLOBEX; do
  echo "==> $account imports from PLATFORM"
  account_name="$(echo "$account" | tr '[:upper:]' '[:lower:]')"
  for op in item.get type.list item.get-versioned locales.list; do
    nsc add import --account "$account" --service --src-account "$platform_pub" \
      --remote-subject "rpc.${account_name}.refdata.${op}.v1" \
      --local-subject "refdata.${op}.v1" >/dev/null
  done
  nsc add import --account "$account" --service --src-account "$platform_pub" \
    --remote-subject "rpc._platform.refdata.context.list.v1" \
    --local-subject "rpc._platform.refdata.context.list.v1" >/dev/null
  nsc add import --account "$account" --src-account "$platform_pub" \
    --remote-subject "notify.accounts.account.*" \
    --local-subject "notify.accounts.account.*" >/dev/null
  nsc add import --account "$account" --src-account "$platform_pub" \
    --remote-subject "evt.*.refdata.*.changed" \
    --local-subject "evt.*.refdata.*.changed" >/dev/null
done

echo "==> restricted PLATFORM shipping-admin creds"
nsc add user --account PLATFORM shipping-admin >/dev/null
# Ordered consumers for REFDATA/RPCTRACE require only their create/next API
# subjects; reply inboxes are necessary for normal NATS request/reply. Do not
# grant $JS.API.> or access to tenant streams/KV.
# notify._platform.> (Phase 23) is this user's own re-publish target: the
# eventhandler.RegisterRefdataNotify/RegisterRPCTraceNotify background
# bridges consume REFDATA/RPCTRACE via the ordered-consumer API above and
# republish onto notify._platform.refdata.>/notify._platform.rpctrace.entry
# for the Admin UI's PLATFORM-account browser connection (auth/token.go's
# MintAdminToken) to subscribe to directly — a narrow publish grant, not the
# broad notify.> a tenant browser credential gets, since this user has no
# business publishing anywhere else.
# $SRV.> was already allow-sub'd (to receive discovery replies) but never
# allow-pub'd, so the Services panel's $SRV.STATS broadcast (nats_ops.go's
# listNatsServices, over this same shipping-admin PLATFORM connection) was
# being silently dropped server-side — "Publish Violation" in the NATS
# server log — and refdata-service (which registers on PLATFORM) never
# appeared in the panel, only shipping-service (found via the tenant
# connection instead). Added to allow-pub, matching the existing allow-sub
# breadth, so the service-discovery broadcast/reply round-trip actually
# completes.
nsc edit user --account PLATFORM --name shipping-admin \
  --allow-pub '$JS.API.CONSUMER.CREATE.REFDATA.>,$JS.API.CONSUMER.CREATE.RPCTRACE.>,$JS.API.CONSUMER.INFO.REFDATA.>,$JS.API.CONSUMER.INFO.RPCTRACE.>,$JS.API.CONSUMER.DELETE.REFDATA.>,$JS.API.CONSUMER.DELETE.RPCTRACE.>,$JS.API.CONSUMER.MSG.NEXT.REFDATA.>,$JS.API.CONSUMER.MSG.NEXT.RPCTRACE.>,notify._platform.>,$SRV.>' \
  --allow-sub '$SRV.>,_INBOX.>,$JS.API.CONSUMER.MSG.NEXT.REFDATA.>,$JS.API.CONSUMER.MSG.NEXT.RPCTRACE.>' >/dev/null
nsc generate creds --account PLATFORM --name shipping-admin >"$NATS_DIR/creds/shipping-admin.creds"

echo "==> SYS account user creds (accounts-service, Phase 14b)"
nsc generate creds --account SYS --name sys >"$NATS_DIR/creds/sys.creds"

echo "==> exporting operator + account JWTs"
nsc describe operator --raw >"$NATS_DIR/operator.jwt"
for account in SYS PLATFORM ACME GLOBEX; do
  nsc describe account "$account" --raw >"$NATS_DIR/resolver/$account.jwt"
done

echo "==> exporting operator signing key seed"
operator_sk="$(nsc describe operator --json | jq -r '.nats.signing_keys[0]')"
operator_sk_file="$(find "$NKEYS_PATH" -name "${operator_sk}.nk")"
if [[ -z "$operator_sk_file" ]]; then
  echo "error: could not locate the operator signing key seed under $NKEYS_PATH" >&2
  exit 1
fi
cp "$operator_sk_file" "$NATS_DIR/keys/operator-signing-key.nk"

echo "==> writing nats.conf's resolver_preload / system_account snippet"
{
  echo "# Generated by bootstrap-operator.sh — public keys for nats.conf. Not"
  echo "# consumed directly; bootstrap-operator.sh also refreshes nats.conf's"
  echo "# generated system_account/resolver_preload tail."
  echo "system_account: $(nsc describe operator --json | jq -r '.nats.system_account')"
  for account in SYS PLATFORM ACME GLOBEX; do
    pubkey="$(nsc describe account "$account" --json | jq -r '.sub')"
    echo "resolver_preload.$account: $pubkey"
  done
} >"$NATS_DIR/.generated-keys.txt"

# Keep the generated JWT blobs separate while producing a self-consistent
# nats.conf tail from this newly minted trust chain.
{
  echo "# Generated by bootstrap-operator.sh; do not edit."
  echo "system_account: $(nsc describe operator --json | jq -r '.nats.system_account')"
  echo "resolver_preload: {"
  for account in SYS PLATFORM ACME GLOBEX; do
    pubkey="$(nsc describe account "$account" --json | jq -r '.sub')"
    jwt="$(tr -d '\n' <"$NATS_DIR/resolver/$account.jwt")"
    echo "  $pubkey: \"$jwt\""
  done
  echo "}"
} >"$NATS_DIR/resolver-preload.generated.conf"

# Replace just the generated system-account/preload tail in nats.conf. The
# readable operator-mode commentary and resolver settings above it stay
# hand-maintained; the cryptographic values must always match this run.
config_tmp="$NATS_DIR/nats.conf.tmp"
awk '
  /^system_account:/ { next }
  /^resolver_preload:/ { exit }
  { print }
' "$NATS_DIR/nats.conf" >"$config_tmp"
cat "$NATS_DIR/resolver-preload.generated.conf" >>"$config_tmp"
mv "$config_tmp" "$NATS_DIR/nats.conf"

rm -rf "$NATS_DIR/.nsc-store"

echo "==> done. Public keys written to nats/.generated-keys.txt for reference."
cat "$NATS_DIR/.generated-keys.txt"
