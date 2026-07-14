# Dictionary / Reference Data Service

A standalone Go service providing shared reference/master data — currencies,
countries, Incoterms, units of measure, hazard classes, and shipping-domain
enums like `ship-status` — with per-locale labels, typed cross-references,
and a versioned NATS-KV read cache in front of its own Postgres schema.

It is plain Postgres CRUD, not event-sourced: nothing here ever needs to
replay a lookup value's history, only its current value. NATS JetStream/KV
are used strictly for cache distribution and a bounded change-event feed —
never as this service's source of truth.

For the seeding process and the Postgres schema (including an ER diagram),
see [DICTIONARY.md](DICTIONARY.md). For the full design rationale (the Q5
versioned-read cache protocol, event-sourced vs. plain CRUD, KV bucket
layout), see [../ARCHITECTURE.md](../ARCHITECTURE.md) § "Reference Data
Service" and `../../../.claude/plans/Dictionary-Service-Plan.md`.

## Run it

**As part of the full demo stack** (from `demos/01-dictionary/`):

```bash
docker compose up --build
```

This starts `refdata-service` alongside Postgres, NATS, the shipping
backend, and all three frontends. See the top-level
[../README.md](../README.md) for the full stack's ports and the Dictionary
admin UI (`frontend-dict`, http://localhost:5175) that browses this
service's data.

**Standalone, outside Docker** (useful for hot-reload during development).
Requires Postgres and NATS already running — the easiest way is via compose:

```bash
cd demos/01-dictionary
docker compose up nats postgres
```

Then, in a separate terminal:

```bash
cd demos/01-dictionary/refdata-service
NATS_URL=nats://localhost:14222 \
DATABASE_URL="postgres://dict:dict@localhost:15432/dictionary?sslmode=disable" \
go run ./cmd
```

**Port collision note:** this service and the shipping backend both default
to `HTTP_ADDR=:8080`. If the backend is already running locally on `:8080`,
give refdata-service a different port:

```bash
cd demos/01-dictionary/refdata-service
NATS_URL=nats://localhost:14222 \
DATABASE_URL="postgres://dict:dict@localhost:15432/dictionary?sslmode=disable" \
HTTP_ADDR=:8081 \
go run ./cmd
```

If you run it on a non-default port, point the shipping backend's
`REFDATA_SERVICE_URL` at it (default fallback is `http://localhost:18081`)
so its `/api/refdata-demo/...` consumer demo route can reach it.

## Querying it

Once running (examples below assume `:8081` — adjust to whatever
`HTTP_ADDR` you used):

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
`refdata/internal/rest/handlers.go`. Swagger UI is served at
`/swagger/index.html` on the same port.

## Tests

```bash
go test ./...
```

Ginkgo spec suite lives in `refdata/*_test.go`. All business rules (BR-D01
… BR-D07, see [../BUSINESS_RULES.md](../BUSINESS_RULES.md)) must have a
passing spec before a change is considered done.
