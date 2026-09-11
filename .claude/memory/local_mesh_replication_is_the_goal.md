---
name: local_mesh_replication_is_the_goal
description: stated 2026-09-08 — the goal is to replicate the full NATS mesh topology (hub + za + au clusters, gateways, R3) on the local machine; cloud deployment is NOT the target, which contradicts the "AWS solves it" escape hatch written into the compose headers
metadata:
  type: project
---

**UPDATED 2026-09-09: the goal stands, but the mesh moved.** It is built in
`demos/02-multi-region/` as two Compose projects (`lb-za-1`, `lb-au-1`) on three shared
Docker networks (`lb-za`, `lb-au`, `lb-wan`). There is no hub cluster. Demo 01 is one
region on one NATS server and must stay that way. See [[multi-region-lives-in-demo-02]].
The network namespacing was solved with `external: true` networks, so the local mesh does
work locally — no cloud needed, as asked.

Stated by the user on 2026-09-08, while comparing the topology diagram against
the running stacks: **"The main goal was to replicate the structure locally
without need to deploy it to the cloud."**

**Why this matters:** the compose band split (ADR-055) was written with cloud as
the escape hatch. `deploy/global/compose.control.yaml`'s header argues that a
separate `lb-global` project cannot reach a cell's `nats` because Compose
namespaces networks per project, then says *"In AWS that problem does not exist
... so the honest local shape today is the sovereign one"*. `deploy/cell/compose.yaml`
carries the same reasoning. **That deferral is not available** if local
replication is the goal — the local mesh has to actually work, so the network
namespacing has to be solved rather than waited out.

**How to apply:** when a topology question comes up, do not answer "that only
works properly in AWS / EKS". Find the local shape. Judge a design by whether it
runs on one Docker engine.

**Gap as of 2026-09-08** (see [[compose_split_aws_deployment_decision]] for the
band split, which IS done):

- `demos/01-dictionary/nats/nats.conf` has **no `cluster {}` block and no
  `gateway {}` block**. Only client 4222, monitor 8222, websocket 9222.
- `server_name` is hardcoded `"nats-dev-1"`, and the file is a single read-only
  mount shared by every cell. A cluster needs a unique name per server, so this
  needs templating or one file per server.
- Compose runs **one** `nats` service per cell. The diagram wants three per
  cell plus a three-node hub cluster — 9 servers.
- So R3 is impossible today (one server holds one replica) and the two cells are
  fully isolated Docker projects with no gateway between them.

**Recommended local shape (not yet agreed):** one Compose project holding the hub
and both cells, so all 9 servers share one Docker network. No external networks,
no host-port mapping for routes/gateways, and it sidesteps gateway port 7222
colliding with the repo's reserved 7200-7299 backend host band. The alternative
— three projects wired over hand-created external networks — matches the
diagram's shape more literally but must be created before any `up`.

**Genuinely not reproducible locally, and fine to accept:** real za-au latency
(the diagram's 250-300 ms round trip; fakeable with `tc netem`, needs
NET_ADMIN), real region/AZ failure, and real cost or throughput figures. The
clusters, gateways, hub and R3 replication are all honest locally.

**One real snag:** account JWTs do not propagate across a gateway. Servers within
one cluster share them; separate clusters do not. Each cluster needs its own
resolver directory copy — the same "one writer per trust domain" point the
control-plane header already makes.
