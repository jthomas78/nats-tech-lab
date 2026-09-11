# A gateway needs a JetStream domain per cluster, or streams collide silently

Raised in the Phase 64 design review, 2026-09-08. Recorded in
`demos/02-multi-region/docs/Multi-Region-Plan.md` as decision **D9**.

**The trap.** A NATS gateway link does not only forward subjects — it makes
the connected clusters one **supercluster**, and inside one account a
JetStream stream name is unique across the whole supercluster. Cells `za-1`
and `au-1` both create a stream named `SHIPPING`, both create KV buckets
`ships` / `container` / `meta`, and both do it against the same accounts. The
moment a gateway comes up, the second cell's `CreateOrUpdateStream` **finds
the first cell's stream and updates it** instead of creating a regional copy.

**There is no error.** `nats server report gateways` shows a healthy three-way
mesh. `nats stream info SHIPPING` shows a stream with three replicas. It looks
exactly like success. What is actually running is one global stream, not two
regional ones, which is the opposite of the intended topology.

`Replicas: 3` does not fix this and is not related to it.

**The fix.** Set `domain` inside each cluster conf's `jetstream {}` block:
`domain: za`, `domain: au`, `domain: hub`. **All three, including the hub** —
it is a domain per *cluster*, not per *region*. An unnamed hub sits in the
default domain and cannot be addressed by a cross-domain API prefix
(`$JS.za.API.>`), which is precisely the hub's job. Today
`demos/01-dictionary/nats/nats.conf` has a bare `jetstream {}` with no domain,
which is correct for one standalone server and wrong the instant a gateway
exists. Domains are also the prerequisite for a later mirror phase, because a
mirror sources from another domain.

**Consequences worth remembering.**
- Verification changes: `nats stream info SHIPPING` becomes ambiguous. Use
  `nats --domain za stream info SHIPPING`.
- Service Go code needs no change on the ordinary path — a client with no
  domain configured uses its own server's domain, and FR-6 keeps every service
  on its own regional cluster.
- Reading across domains is explicit and prefixed (`$JS.za.API.>`). Only the
  hub ever needs that.

**Second trap found in the same review:** one shared `nats-data` volume
(`deploy/cell/compose.infra.yaml`) cannot serve three servers. JetStream keeps a
per-server store there, and NATS documents that a resolver directory must not
be shared. So a cluster is **three named Compose services** sharing one conf
file, never one service with `replicas: 3`. See D10.

Related: [[compose_split_aws_deployment_decision]],
[[local_mesh_replication_is_the_goal]]
