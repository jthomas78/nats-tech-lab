---
name: multi-region-lives-in-demo-02
description: 2026-09-09 — all clustering, gateways, superclusters and JetStream domains were REMOVED from demo 01 and now live only in demos/02-multi-region/, which is two Compose projects lb-za-1 and lb-au-1 on three shared networks
metadata:
  type: project
---

**Decided and done 2026-09-09.** Demo 01 is **one region, one NATS server** (service name `nats`,
project `poc`). Demo 02 owns every multi-region mechanic.

**Removed from demo 01** (all of it was uncommitted, so this was a clean revert):
`deploy/up-mesh.sh`, `deploy/toolbox.sh`, `deploy/cell/compose.toolbox.yaml`,
`deploy/global/compose.hub.yaml`, `deploy/environments/local-mesh.env`,
`nats/nats-cluster.conf`, `nats/nats-hub.conf`, `nats/gateway-remotes-{za,au,hub}.conf`,
`toolbox/`, the 3-server cluster in `compose.infra.yaml`, `NATS_CLUSTER_NAME` /
`NATS_JS_DOMAIN` / `NATS_ROUTE_*` in both env files, and the `lb-gateway` network.
The `lb-za-1` -> `poc` rename was KEPT (see [[compose-split-aws-deployment-decision]]).

**Why:** demo 01 is the application POC and demo 02 is the plumbing. Mixing them meant demo 01's
JetStream meta group merged across three clusters and grew six ghost peers, and a NEW stream created
from the AU cell landed in the ZA cluster. A single-server demo 01 cannot have that bug at all.

**Demo 02 shape now — two projects, one per region.** A region is its own deployment, so
`docker compose -f compose.au.yaml down` takes Australia off the air and leaves ZA running. One
project could not do that.

| Project | Servers | File |
|---|---|---|
| `lb-za-1` | za-1..3 | `deploy/compose.za.yaml` |
| `lb-au-1` | au-1..3 | `deploy/compose.au.yaml` |

Three **external** networks, created by `deploy/up.sh`: `lb-za` and `lb-au` carry routes (6222),
`lb-wan` carries gateways (7222) and nothing else. Each server has a different name per network
(`za-1.rt` regional, `za-1.gw` on the wan) because a container on two networks resolves its plain
name to EITHER network and Docker does not promise which — measured, with plain names the routes
came up on the wan. See [[jetstream-domain-per-cluster-is-mandatory]] and
[[gateway-double-capture-and-option3]].

**There is no hub cluster yet.** Deliberate: the odometer example does not need one. `nats-hub.conf`
and `gateway-remotes-hub.conf` from demo 01 are gone from the repo; rewrite them if a hub is wanted.

**Do not add clustering back to demo 01.** CLAUDE.md and both READMEs say so.
