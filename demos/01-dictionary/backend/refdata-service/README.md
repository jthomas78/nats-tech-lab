# Dictionary / Reference Data Service

A standalone Go service providing shared reference/master data — currencies,
countries, Incoterms, units of measure, hazard classes, and shipping-domain
enums like `ship-status` — with per-locale labels, typed cross-references,
and a versioned NATS-KV read cache in front of its own Postgres instance
(`refdata-postgres` in `docker-compose.yml` — a separate database server from
the one `shipping-service` uses, not just a private schema on a shared one).
NATS is the only infrastructure this service shares with `shipping-service`.

It is plain Postgres CRUD, not event-sourced: nothing here ever needs to
replay a lookup value's history, only its current value. NATS JetStream/KV
are used strictly for cache distribution and a bounded change-event feed —
never as this service's source of truth.

For the seeding process, the Postgres schema (including an ER diagram), data
access paths, and cross-service consumption, see
[ARCHITECTURE-DICTIONARY.md](../../../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-DICTIONARY.md) (moved to the
obsidian vault). For the broader design rationale (the Q5 versioned-read cache
protocol, event-sourced vs. plain CRUD, KV bucket layout), see
[ARCHITECTURE.md](../../../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md)
§ "Reference Data Service" and `../../../../.claude/plans/Dictionary-Service-Plan.md`.

## Run it

**As part of the full demo stack** (from `demos/01-dictionary/`):

```bash
docker compose up --build
```

This starts `refdata-service` alongside Postgres, NATS, the shipping
backend, and all three frontends. See the top-level
[../../README.md](../../README.md) for the full stack's ports and the Dictionary
admin UI (`refdata`, http://localhost:7102) that browses this
service's data.

**Gotcha: stale containers don't pick up new seed data.** `Seed` (in
`refdata/seed.go`) runs on every `refdata-service` boot and is per-item
idempotent — it inserts new keys and leaves existing ones alone — but it
only runs when the process actually starts. A long-running container from
before a `seed.go` change (e.g. new `string` keys) keeps serving the old
catalog indefinitely; `docker compose up` with no flags won't rebuild an
image that already exists. After changing `seed.go`, rebuild and recreate
the container, then restart `shipping-service` too so its KV cache/`refdataconsumer`
picks up the new entries:

```bash
cd demos/01-dictionary
docker compose build refdata-service
docker compose up -d --force-recreate refdata-service
docker compose restart shipping-service
```

Symptom if you skip this: the shipping UI's language switch changes only a
couple of strings (whatever was seeded when the container last started)
and silently leaves the rest in English.

**Standalone, outside Docker** (useful for hot-reload during development).
Requires its own Postgres (`refdata-postgres`, port `5433` — not the
`postgres` container `shipping-service` uses) and NATS already running — the
easiest way is via compose:

```bash
cd demos/01-dictionary
docker compose up nats refdata-postgres
```

Then, in a separate terminal:

```bash
cd demos/01-dictionary/backend/refdata-service
NATS_URL=nats://localhost:4222 \
DATABASE_URL="postgres://refdata:refdata@localhost:5433/refdata?sslmode=disable" \
go run ./cmd
```

**Port collision note:** this service and the shipping backend both default
to `HTTP_ADDR=:8080`. If the backend is already running locally on `:8080`,
give refdata-service a different port:

```bash
cd demos/01-dictionary/backend/refdata-service
NATS_URL=nats://localhost:4222 \
DATABASE_URL="postgres://refdata:refdata@localhost:5433/refdata?sslmode=disable" \
HTTP_ADDR=:8081 \
go run ./cmd
```

If you run it on a non-default port, point the shipping backend's
`REFDATA_SERVICE_URL` at it (default fallback is `http://localhost:7201`)
so its `/api/refdata-demo/...` consumer demo route can reach it.

## Querying it

Three ways to look at the same data — Postgres (source of truth), REST (the
service's own read path, locale-aware), and NATS KV (the cache the REST
layer backfills and that consumers like the shipping backend's
`refdataconsumer` read directly).

### Docker compose SQL (once the stack is up)

```bash
cd demos/01-dictionary
docker compose exec refdata-postgres psql -U refdata -d refdata

# then, inside psql:
SELECT * FROM refdata.dictionary_types;
SELECT code, status, attrs FROM refdata.dictionary_items WHERE type_key = 'ship-status';
SELECT code, locale, label FROM refdata.dictionary_localizations WHERE type_key = 'ship-status' ORDER BY code, locale;
SELECT * FROM refdata.dictionary_locales;
SELECT * FROM refdata.dictionary_set_versions;
```

Or one-shot, without an interactive session:

```bash
docker compose exec refdata-postgres psql -U refdata -d refdata -c \
  "SELECT code, locale, label FROM refdata.dictionary_localizations WHERE type_key='ship-status' ORDER BY code, locale;"
```

### REST using curl (refdata-service)

Examples below assume `:8081` — adjust to whatever `HTTP_ADDR` you used, or
`:8080` if using compose (see [../README.md](../README.md) for the full
stack's port table):

```bash
# List registered types
curl -s http://localhost:8081/api/refdata/emea-acme/types | jq

# List items of a type
curl -s http://localhost:8081/api/refdata/emea-acme/ship-status | jq

# Get one item, optionally resolved to a locale
curl -s "http://localhost:8081/api/refdata/emea-acme/ship-status/docked?locale=es" | jq

# All localizations recorded for an item
curl -s http://localhost:8081/api/refdata/emea-acme/ship-status/docked/localizations | jq

# Locales known to this context
curl -s http://localhost:8081/api/refdata/emea-acme/locales | jq

# Localization completeness for a locale
curl -s "http://localhost:8081/api/refdata/emea-acme/ship-status/completeness?locale=es" | jq

# Postgres set version vs KV _meta version
curl -s http://localhost:8081/api/refdata/emea-acme/ship-status/cache-status | jq
```

Full route list is documented at the top of
`refdata/internal/rest/handlers.go`.

**Swagger UI** — browsable at `http://localhost:8081/swagger/index.html`
(same port as the REST API above; `:8080` if using compose). Raw OpenAPI
spec at `/swagger/doc.json`. Definitions are generated from Go annotations
(`@title`, route comments in `internal/rest/handlers.go`, etc., rooted at
`cmd/main.go`) into `docs/` — this happens automatically in the Docker
build (see `Dockerfile`); to regenerate locally after changing annotations:

```bash
go install github.com/swaggo/swag/cmd/swag@v1.16.6   # once
cd demos/01-dictionary/backend/refdata-service
swag init --generalInfo cmd/main.go --dir . --output docs/ --parseDependency=false --parseInternal
```

### NATS KV using NATS CLI

Bucket naming is `refdata-{context}` (prefix `refdata`, see
`internal/kvstore/kv.go`); keys are `{namespace}{type_key}.{code}` for an item
and `{namespace}{type_key}._meta` for the type's version/count stamp (see
`internal/kvcache/keys.go`). `{namespace}` is `enum.` for a `domain-enum`
category type and empty for every other category (BR-D31) — so
`enum.ship-status.docked`, but plain `currency.EUR`. Requires the
[`nats` CLI](https://github.com/nats-io/natscli) pointed at the compose
NATS port:

```bash
export NATS_URL=nats://localhost:4222

# List KV buckets
nats kv ls

# List keys in the context's bucket
nats kv ls refdata-emea-acme

# Get one item's cached entry (item + localizations + references + version)
# ship-status is a domain-enum type, hence the enum. namespace (BR-D31)
nats kv get refdata-emea-acme enum.ship-status.docked

# Get the type's _meta (current version, item count, last update)
nats kv get refdata-emea-acme enum.ship-status._meta

# Watch the bucket live — e.g. while re-seeding or editing via refdata
nats kv watch refdata-emea-acme

# Watch only the enums, using the key namespace as a subject filter
nats kv watch refdata-emea-acme "enum.>"
```

When running with NATS wired (compose, or `go run ./cmd` with `NATS_URL`
set), every write goes through the same `ChangeNotifier` path — so `Seed`'s
registrations at startup already populate the KV cache; you don't need to
`GET` an item first for it to show up in `nats kv get`. The miss → refetch →
backfill path (Q5) only kicks in later, if the bucket is wiped or a cache
entry goes stale relative to `dictionary_set_versions`.

See [ARCHITECTURE-DICTIONARY.md](../../../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-DICTIONARY.md) §
"Cross-Service Consumption" for how the shipping backend reads this
service's reference data (read-side only, and how the shared NATS instance
makes the KV cache path possible).

## Tests

```bash
go test ./...
```

Ginkgo spec suite lives in `refdata/*_test.go`. All business rules (BR-D01
… BR-D07, see [../../BUSINESS_RULES.md](../../BUSINESS_RULES.md)) must have a
passing spec before a change is considered done.
