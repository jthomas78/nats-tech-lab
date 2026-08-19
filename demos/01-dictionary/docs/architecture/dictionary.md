# Dictionary (Reference Data)

<EyebrowLabel text="Architecture" />

`refdata-service` owns dictionary/reference data — types, items,
localizations — as its own Go service with its own Postgres schema, REST
API, and NATS `rpc.*` surface. It publishes change events on its own
`REFDATA` stream, independent of shipping-service's `SHIPPING` stream.

<VerdictBadge status="completed" /> Running in code today (Phase 11
onward).

*Content in progress — seeding, schema/ER detail, and cross-service
consumption paths to follow.*
