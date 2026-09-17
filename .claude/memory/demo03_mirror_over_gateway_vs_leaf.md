---
name: demo03-mirror-over-gateway-vs-leaf
description: "[demo 03] Over a GATEWAY a mirror needs no `mirror.external.api` and just works. Over a LEAF link, omitting it copies nothing — or silently copies the wrong, local stream."
metadata:
  type: project
---

Measured 2026-09-17 in `demos/03-multi-cluster-and-accounts/lab/`. The same
question asked in two shapes, so the report states the trade twice.

**Over a gateway** (`01-gateway.sh`, checks `A15`–`A17`): a supercluster is ONE
JetStream namespace, so there is nothing for `mirror.external.api` to point at.
A plain `{"mirror": {"name": "ODOMETER"}}` with `placement.cluster = au` lands
in `au`, copies the source, and keeps up with new publishes.

**Over a leaf link** (`04-hub-and-leaf.sh`, checks `D7`–`D9`): each side is its
own JetStream domain, so the mirror needs
`mirror.external.api = "$JS.au.API"`.

- `D7` — without it: **0 messages copied**.
- `D7a` — and it does not tell you. No error, no warning, the stream reports
  healthy. It sits at zero forever.
- `D9`/`D9a` — worse, when a stream of the SAME NAME exists locally, a
  name-only mirror silently copies the LOCAL one. You get data, just not the
  data you asked for.

**The honest trade.** A gateway costs you a shared fate (one meta group, lose
the majority and every create freezes) and hands you the cross-region copy for
free. A hub-and-leaf keeps each region voting alone and makes you hand-write
every copy — `D15`.

**Still open:** neither shape measures mirror lag or bandwidth. Both only prove
that a copy happens.

Related: [[demo03-both-edges-of-a-failover-lie]].
