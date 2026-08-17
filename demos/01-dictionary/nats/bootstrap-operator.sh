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
#   nats/keys/{platform,acme,globex}-signing-key.nk
#                                 per-account signing key seeds (BR-AC19) —
#                                 adopted by accounts-service at startup so
#                                 these accounts keep a stable identity across
#                                 a `docker compose down -v`, instead of being
#                                 handed a fresh random key on each wiped boot.
#                                 Same secrets-manager caveat as above.
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

  # BR-AC19 — export this account's signing key seed alongside the operator's.
  # Without it accounts-service has no way to learn the key (nsc's keystore is
  # deleted below), so its startup ensureSigningKey minted a *fresh random one*
  # on every boot with an empty accounts Postgres and re-signed the account
  # claims with it — invalidating any .creds file signed by the previous key.
  # Exported under the lowercase tenant identity, matching creds/$user.creds.
  account_sk="$(nsc describe account "$account" --json | jq -r '.nats.signing_keys[0]')"
  account_sk_file="$(find "$NKEYS_PATH" -name "${account_sk}.nk")"
  if [[ -z "$account_sk_file" ]]; then
    echo "error: could not locate $account's signing key seed under $NKEYS_PATH" >&2
    exit 1
  fi
  cp "$account_sk_file" "$NATS_DIR/keys/$user-signing-key.nk"
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

  # Phase 28f — the reverse leg of the contract above: this tenant exports
  # its own obs.trace.> spans back to PLATFORM, so PLATFORM's cross-account
  # trace store (dictionary/composition.go's TRACES consumer) can subscribe
  # to them. No --allow-trace here — jwt.Export.Validate rejects that flag
  # on anything but a service export, and this is a stream export; the flag
  # that actually matters for this pipeline is on PLATFORM's stream
  # *import* of it below.
  nsc add export --account "$account" --subject "obs.trace.>" >/dev/null

  # BR-AC31 (Phase 30a) — a second reverse leg: this tenant exports its own
  # $SRV.> control subjects (nats.go/micro's PING/INFO/STATS discovery
  # protocol) back to PLATFORM, so observability-service's Services panel
  # can broadcast discovery into this account instead of needing its own
  # raw per-tenant connection. --response-type Stream (not the nsc default
  # Singleton) because $SRV.STATS is answered by every registered service
  # instance in the account, not just one — see BR-AC31's design note in
  # BUSINESS_RULES-ACCOUNTS.md for why Singleton would silently drop every
  # reply after the first.
  nsc add export --account "$account" --service --subject '$SRV.>' \
    --response-type Stream >/dev/null

  # BR-AC32 (Phase 30b, extended 30i) — a third reverse leg: seven narrow,
  # explicit $JS.API exports (never the $JS.API.> wildcard, which would
  # grant stream management — create/delete/purge — not just visibility),
  # so observability-service's JetStream/KV panels can introspect this
  # tenant's streams/consumers. Traced directly against the exact $JS.API
  # calls dictionary/internal/rest/{kv,replay}.go makes — see
  # Main-POC-Plan.md's Phase 30 Design section and BUSINESS_RULES-
  # ACCOUNTS.md's BR-AC32 for the full call-chain trace. Five of the seven
  # are plain single-reply Singleton (the nsc default, so no
  # --response-type flag); CONSUMER.MSG.NEXT.*.* alone needs
  # --response-type Stream, same reason $SRV.> above does — a batch pull
  # can yield multiple replies for one request. CONSUMER.CREATE.*.*.>
  # (Phase 30i live-verification fix) is a separate, necessary pattern
  # alongside CONSUMER.CREATE.*.* — nats.go's CreateOrUpdateConsumer
  # embeds a FilterSubject directly into the published $JS.API subject
  # rather than the request body whenever one is set
  # (apiConsumerCreateWithFilterSubjectT), and jetstream.KeyValue.WatchAll
  # (the KV Buckets panel's live-entries view) always sets one — caught
  # only once this ran against a real multi-account deployment, since unit
  # tests never exercise real NATS subject permissions.
  nsc add export --account "$account" --service --subject '$JS.API.STREAM.LIST' >/dev/null
  nsc add export --account "$account" --service --subject '$JS.API.STREAM.INFO.*' >/dev/null
  nsc add export --account "$account" --service --subject '$JS.API.CONSUMER.CREATE.*' >/dev/null
  nsc add export --account "$account" --service --subject '$JS.API.CONSUMER.CREATE.*.*' >/dev/null
  nsc add export --account "$account" --service --subject '$JS.API.CONSUMER.CREATE.*.*.>' >/dev/null
  nsc add export --account "$account" --service --subject '$JS.API.CONSUMER.MSG.NEXT.*.*' \
    --response-type Stream >/dev/null
  nsc add export --account "$account" --service --subject '$JS.API.CONSUMER.DELETE.*.*' >/dev/null
done

echo "==> PLATFORM imports each tenant's obs.trace.> (Phase 28f cross-account trace store)"
# This is the day-0 nsc equivalent of accounts-service's
# Provisioner.addPlatformTraceImport, which does the same thing at runtime
# for accounts minted after boot (see accounts/provisioner.go).
for account in ACME GLOBEX; do
  account_pub="$(nsc describe account "$account" --json | jq -r '.sub')"
  nsc add import --account PLATFORM --src-account "$account_pub" \
    --remote-subject "obs.trace.>" \
    --local-subject "obs.trace.>" \
    --allow-trace >/dev/null
done

echo "==> PLATFORM imports each tenant's \$SRV.> (BR-AC31 cross-account service discovery)"
# Day-0 nsc equivalent of accounts-service's Provisioner.addPlatformMonitorImport
# (accounts/provisioner.go), which does the same thing at runtime for accounts
# minted after boot. Every tenant exports the identical literal "$SRV.>"
# subject, so — unlike the obs.trace.> import above — this needs a
# tenant-scoped --local-subject remap: without it, GLOBEX's import would
# collide with ACME's on the same local subject.
for account in ACME GLOBEX; do
  account_pub="$(nsc describe account "$account" --json | jq -r '.sub')"
  account_name="$(echo "$account" | tr '[:upper:]' '[:lower:]')"
  nsc add import --account PLATFORM --service --src-account "$account_pub" \
    --remote-subject '$SRV.>' \
    --local-subject "monitor.${account_name}.srv.>" >/dev/null
done

echo "==> PLATFORM imports each tenant's \$JS.API introspection subjects (BR-AC32)"
# Day-0 nsc equivalent of accounts-service's Provisioner.addPlatformJSAPIImport
# (accounts/provisioner.go), which does the same thing at runtime for accounts
# minted after boot. Same tenant-scoped --local-subject remap rationale as
# the $SRV.> import above — every tenant exports the identical literal
# $JS.API.* subjects.
for account in ACME GLOBEX; do
  account_pub="$(nsc describe account "$account" --json | jq -r '.sub')"
  account_name="$(echo "$account" | tr '[:upper:]' '[:lower:]')"
  for suffix in STREAM.LIST STREAM.INFO.\* CONSUMER.CREATE.\* CONSUMER.CREATE.\*.\* CONSUMER.CREATE.\*.\*.\> CONSUMER.MSG.NEXT.\*.\* CONSUMER.DELETE.\*.\*; do
    nsc add import --account PLATFORM --service --src-account "$account_pub" \
      --remote-subject "\$JS.API.${suffix}" \
      --local-subject "monitor.${account_name}.js.${suffix}" >/dev/null
  done
done

echo "==> restricted PLATFORM shipping-admin creds"
nsc add user --account PLATFORM shipping-admin >/dev/null
# The ordered consumer for REFDATA requires only its create/next API
# subjects; reply inboxes are necessary for normal NATS request/reply. Do not
# grant $JS.API.> or access to tenant streams/KV. (RPCTRACE's matching grants
# were retired in Phase 28g along with the stream itself and
# eventhandler.RegisterRPCTraceNotify — see that file's retirement note.)
# notify._platform.> (Phase 23) is this user's own re-publish target: the
# eventhandler.RegisterRefdataNotify background bridge consumes REFDATA via
# the ordered-consumer API above and republishes onto
# notify._platform.refdata.> for the Admin UI's PLATFORM-account browser
# connection (auth/token.go's MintAdminToken) to subscribe to directly — a
# narrow publish grant, not the broad notify.> a tenant browser credential
# gets, since this user has no business publishing anywhere else.
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
  --allow-pub '$JS.API.CONSUMER.CREATE.REFDATA.>,$JS.API.CONSUMER.INFO.REFDATA.>,$JS.API.CONSUMER.DELETE.REFDATA.>,$JS.API.CONSUMER.MSG.NEXT.REFDATA.>,notify._platform.>,$SRV.>' \
  --allow-sub '$SRV.>,_INBOX.>,$JS.API.CONSUMER.MSG.NEXT.REFDATA.>' >/dev/null
nsc generate creds --account PLATFORM --name shipping-admin >"$NATS_DIR/creds/shipping-admin.creds"

echo "==> restricted PLATFORM observability creds (Phase 30c)"
# observability-service's one connection — narrowly scoped the same way
# shipping-admin above is, deliberately not platform.creds' unrestricted
# access (refdata-service/otlp-bridge's pattern): this phase's whole design
# rationale (Main-POC-Plan.md's Phase 30) is precise, enumerable grants
# instead of a broad credential, and it would be inconsistent to abandon
# that at the connection layer after BR-AC31/BR-AC32 went to the trouble of
# scoping the account-level exports/imports precisely.
#
# monitor.>/$SRV.> is everything this phase's own account-level imports
# resolve to (BR-AC31's per-tenant $SRV.> import, BR-AC32's six per-tenant
# $JS.API imports, all remapped under monitor.{tenant}.*) plus PLATFORM's
# own native $SRV.> (refdata-service registers there, same as
# shipping-admin's grant above needs for the same reason).
#
# Phase 30e — PLATFORM-native $JS.API access (this service's own
# REFDATA/TRACES stream introspection, not routed through any monitor.*
# remap) is the exact same seven-subject BR-AC32 list, applied directly to
# this account instead of imported from a tenant: same read-oriented
# rationale (STREAM.LIST/STREAM.INFO for listing, CONSUMER.CREATE/
# CONSUMER.MSG.NEXT/CONSUMER.DELETE for the replay panel's ephemeral
# consumer), same exclusion of $JS.API.> wildcard (no STREAM.CREATE/
# DELETE/PURGE/UPDATE). Deliberately not shipping-service's PlatformFullJS
# pattern (a second, broader-access connection) — one connection, narrowly
# scoped, consistent with this whole phase's design.
#
# Phase 30g — the trace store needs to CREATE/UPDATE two resources it owns
# outright: the TRACES stream and its KV_trace-request-reply backing stream
# (a KV bucket is a stream under the hood). This is a genuinely different
# risk than BR-AC31/BR-AC32's cross-account introspection grants above —
# those are read-only access into OTHER accounts' data; this is
# create/update access to two specifically-named resources in THIS
# account, the same shape shipping-admin's own REFDATA-scoped grants
# already use (resource-scoped by name, never a wildcard). Consumer
# create/pull for the durable trace-store-projector consumer needs no new
# grant — the existing CONSUMER.CREATE.*.*/CONSUMER.MSG.NEXT.*.* wildcards
# already cover any consumer name on any stream, durable or ephemeral (NATS
# wildcards don't distinguish the two). $KV.trace-request-reply.> is the KV
# bucket's own underlying publish subject (kv.Put is a plain JetStream
# publish, not a $JS.API call) and notify._platform.kv.trace-request-reply.>
# is this store's live-update publish, the same notify.* mechanism every
# other KV panel's writes already use.
#
# Deliberately not shipping-service's PlatformFullJS pattern (a second,
# unrestricted connection) even for this — one connection, every grant
# still enumerable and resource-scoped, consistent with this whole phase's
# design.
#
# Phase 30i live-verification fix — $JS.API.INFO was missing. nats.go's
# jetstream.CreateOrUpdateKeyValue (tracestore.Register's KV bucket
# provisioning) calls js.AccountInfo() first, which publishes to
# $JS.API.INFO before ever touching STREAM.CREATE — without this grant the
# very first startup call fails closed with a permissions violation and
# the KV bucket creation call above it times out waiting on a reply that
# was never going to arrive. Account-wide (not resource-scoped) since
# AccountInfo carries no resource name to scope against; it's a read of
# this account's own JetStream limits/usage, not a create/update on a
# named resource, so this doesn't reopen the enumerable-resource-grants
# rationale above.
#
# Phase 30i live-verification fix #2 — the durable trace-store-projector
# consumer is created WITH a FilterSubject (tracestore.go's
# CreateOrUpdateConsumer(ctx, "TRACES", ...FilterSubject: "obs.trace.>")),
# so nats.go publishes to apiConsumerCreateWithFilterSubjectT
# ("CONSUMER.CREATE.%s.%s.%s", the filter subject's own dots and trailing
# > folded straight into the published subject) rather than the plain
# two-wildcard apiConsumerCreateT the existing
# $JS.API.CONSUMER.CREATE.*.* grant above covers — so the literal wire
# subject is $JS.API.CONSUMER.CREATE.TRACES.trace-store-projector.obs.trace.>,
# which that grant's fixed two-wildcard shape cannot match no matter how
# many wildcards, since a filter subject's own token count varies by
# design. Superseded by fix #5 below, which generalizes this exact gap
# (BR-AC32's own CONSUMER.CREATE.*.*.> subject already covers this
# specific stream+consumer, so no separate resource-scoped grant remains
# here) — kept as a comment for the trail of why fix #5 exists, not as a
# separate active grant.
#
# Phase 30i live-verification fix #3 — appendSpan's read side (kv.Get) uses
# nats.go's JetStream direct-get optimization (apiDirectMsgGetLastBySubjectT,
# "$JS.API.DIRECT.GET.%s.%s"), not a plain consumer pull — and like the
# filter-subject case above, the second %s is the literal KV subject
# ($KV.trace-request-reply._platform.trace.<traceId>, itself multi-token),
# folded straight into the published subject rather than a single wildcard
# token. Scoped to the KV_trace-request-reply stream only.
#
# Phase 30i live-verification fix #4 — every delivered message's Ack is a
# publish to whatever reply subject the SERVER stamped on it
# ($JS.ACK.TRACES.trace-store-projector.<numDelivered>.<streamSeq>.
# <consumerSeq>.<timestamp>.<numPending>, server-generated per message, not
# something the client constructs from a fixed template) — without this
# grant every Ack silently fails permissions, so JetStream just keeps
# redelivering the same messages forever (never registers the Ack). Scoped
# to the TRACES stream + this consumer name only. (An earlier version of
# this comment blamed a co-occurring "no responders" on STREAM.LIST/
# KeyValueStores on client-side slow-consumer pressure from the resulting
# redelivery storm — that diagnosis was wrong. The actual cause, found
# afterward: AccountsClient.TenantNames compared account names
# case-sensitively against "PLATFORM", but accounts-service stores and
# returns them lowercase ("platform", "sys" — see fix in
# accounts_client.go's TenantNames), so introspectableAccounts built a
# bogus monitor.platform.js/monitor.sys.js-prefixed JetStream context with
# no matching import, and the very next request through it failed closed —
# aborting the whole handler on whichever of the two came first. Unrelated
# to Acks entirely.)
#
# Phase 30i live-verification fix #5 — the KV Buckets panel's live-entries
# view (kv.go's kvBucketEntriesOnce, via jetstream.KeyValue.WatchAll) always
# creates its ephemeral consumer WITH a FilterSubject (the bucket's own
# $KV.<bucket>.> subject) — the same apiConsumerCreateWithFilterSubjectT
# gap as fix #2, but for ANY bucket this account owns, not just TRACES/
# trace-store-projector, so this needs the general BR-AC32 pattern
# (mirrored into jsAPIExportSubjects/tenantExports above and
# accounts/provisioner.go's jsAPIExportSubjects for tenant accounts) rather
# than a one-off resource-scoped grant.
#
# Fix #6 (found after Phase 30i, live) — nats.go's KeyValue.WatchAll
# creates a push consumer with FlowControl enabled, so the client
# periodically PUBLISHES a flow-control ack to a server-generated
# $JS.FC.<stream>.<inbox> subject to keep delivery going (distinct from,
# and in addition to, the $JS.ACK grant in fix #4 above — FC acks aren't
# message Acks). Without this grant those acks fail closed; the visible
# symptom isn't an immediate error but a stall once the consumer's
# flow-control window fills, since the server simply stops pushing further
# updates rather than erroring the request. Scoped to the
# KV_trace-request-reply backing stream, mirroring fix #3/#4's scoping.
nsc add user --account PLATFORM observability >/dev/null
nsc edit user --account PLATFORM --name observability \
  --allow-pub 'monitor.>,$SRV.>,$JS.API.INFO,$JS.API.STREAM.LIST,$JS.API.STREAM.INFO.*,$JS.API.CONSUMER.CREATE.*,$JS.API.CONSUMER.CREATE.*.*,$JS.API.CONSUMER.CREATE.*.*.>,$JS.API.CONSUMER.MSG.NEXT.*.*,$JS.API.CONSUMER.DELETE.*.*,$JS.API.STREAM.CREATE.TRACES,$JS.API.STREAM.UPDATE.TRACES,$JS.API.STREAM.CREATE.KV_trace-request-reply,$JS.API.STREAM.UPDATE.KV_trace-request-reply,$JS.API.DIRECT.GET.KV_trace-request-reply.>,$JS.ACK.TRACES.trace-store-projector.>,$JS.FC.KV_trace-request-reply.>,$KV.trace-request-reply.>,notify._platform.kv.trace-request-reply.>' \
  --allow-sub 'monitor.>,$SRV.>,_INBOX.>' >/dev/null
nsc generate creds --account PLATFORM --name observability >"$NATS_DIR/creds/observability.creds"

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
