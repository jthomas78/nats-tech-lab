---
name: demo02-wan-cut-freezes-jetstream-management-only
description: "[demo 02] Losing JetStream meta-group majority stops stream management only; core NATS and existing streams keep working. Never fake a WAN cut with `docker network disconnect`."
metadata:
  type: project
---

Measured 2026-09-09 in `demos/02-multi-region/`.

A gateway makes all six servers ONE JetStream meta group (Raft). Majority of 6
is 4. Cut the WAN 3/3 and neither side elects a meta leader (`leader=NONE`).

**What that actually costs — measured on ZA with AU stopped:**

| Operation | Works? | Measured |
|---|---|---|
| Core NATS pub/sub | yes | message delivered |
| Read an existing stream | yes | `msgs=10` |
| Write to an existing stream | yes | 10 -> 15, acked |
| Create / delete / edit a stream | **no** | `JetStream system temporarily unavailable (10008)` |

So a WAN cut is a **change freeze**, not an outage. Existing streams keep
serving because all of a stream's replicas live inside one cluster.

**Cases where both sides do NOT freeze:** uneven regions (3+2 = 5 peers,
majority 3, so the 3-side keeps its leader); a third hub cluster; leaf nodes
with their own JetStream domain (a genuinely separate JetStream system, which
is what domains are for -- see [[demo02-jetstream-domains-do-not-split-a-supercluster]]).

**TRAP -- do not repeat this mistake.** `docker network disconnect lb-wan
<container>` on a RUNNING container also drops that container's published host
ports. My first run of this test reported "both regions died completely"; that
was a Docker artifact. The giveaway: core NATS pub/sub also "failed", which a
meta-group split cannot cause. To lose majority honestly, stop one region's
containers: `docker stop lab2-au-1 lab2-au-2 lab2-au-3`.

Written up in `demos/02-multi-region/diagrams/gateway-vs-wan-cut.html`.
