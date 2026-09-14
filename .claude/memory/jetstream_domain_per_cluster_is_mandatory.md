# RETRACTED — a JetStream domain per cluster does NOT split a gateway

> **Status: the fix in this note is wrong. Retracted 2026-09-14 by measurement.**
> The *trap* below is real and still holds. The *fix* below does not work over a
> gateway. Read [[demo02_jetstream_domains_do_not_split_a_supercluster]] first.
> The user's ruling, 2026-09-14: **"we need to use the same jetstream.domain"** —
> one domain across a supercluster, not one per cluster.

Originally raised in the Phase 64 design review, 2026-09-08, as decision **D9** in
`demos/02-multi-region/docs/Multi-Region-Plan.md`.

## The trap — still true, and later measured

A NATS gateway link does not only forward subjects — it makes the connected
clusters one **supercluster**, and inside one account a JetStream stream name is
unique across the whole supercluster (error `10058`). Two cells both creating
`SHIPPING` get **one** stream, not two regional copies. `CreateOrUpdateStream`
finds the first cell's stream and updates it.

**There is no error.** `nats server report gateways` shows a healthy mesh.
`nats stream info SHIPPING` shows one healthy stream. It looks like success.

`Replicas: 3` does not fix this and is not related to it. All of a stream's
replicas live inside ONE cluster.

## The fix — WRONG, do not do this

The note used to say: set `domain: za` / `domain: au` / `domain: hub` in each
cluster's `jetstream {}` block. **This does not work.** A gateway is not a leaf
link, so a domain name cannot split a supercluster.

Measured 2026-09-11 on four scratch servers (za x2, au x2, one `ACME` account,
`domain: za` / `domain: au`, joined by a gateway):

- `stream add SHIPPING` in both cells returned the **same** stream — one
  `Created` time, cluster `za`.
- `nats --js-domain au stream info` returned **za's** stream.
- The four servers formed **one** meta group of 4. `za` elected a leader;
  `au` never did (`leader: null`), and za listed au's peers as
  `Server name unknown ... offline: true`. AU stored nothing.

A domain boundary is only permitted across a **leafnode** link (hub + leaf
clusters). That is the one topology where per-cluster domains are correct.

**What actually gives two regional streams over a gateway:** a second
**account** per region (a second namespace), not a second domain name.

## Second trap — still true, unaffected

One shared `nats-data` volume (`deploy/cell/compose.infra.yaml`) cannot serve
three servers. JetStream keeps a per-server store there, and NATS documents that
a resolver directory must not be shared. So a cluster is **three named Compose
services** sharing one conf file, never one service with `replicas: 3`. See D10.

Related: [[demo02_jetstream_domains_do_not_split_a_supercluster]],
[[compose_split_aws_deployment_decision]], [[local_mesh_replication_is_the_goal]]
