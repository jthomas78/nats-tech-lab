# CQRS Shapes

<EyebrowLabel text="Architecture" />

Three shapes were built side-by-side over the same Ship/Container domain
before settling on one:

- **KV as the read model** — every query served straight from NATS KV.
- **Postgres projection + KV write-through cache** — the shape running
  today: Postgres is the canonical CQRS projection, KV is an eager cache
  in front of it.
- **Event-sourced reconstruction** — read models rebuilt by replaying
  JetStream on demand.

<VerdictBadge status="completed" /> The comparison is decided; the other
two shapes were retired once the write-through-cache shape won.

*Content in progress — full comparison detail to follow.*
