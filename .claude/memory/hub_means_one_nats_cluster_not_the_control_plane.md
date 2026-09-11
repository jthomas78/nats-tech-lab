# `hub` means two different things in this repo — know which one you are reading

Pinned 2026-09-08 after the word cost two round trips in one session. The
authoritative definition now lives in `demos/02-multi-region/docs/Multi-Region-Plan.md`
under **FR-5**.

**The drawing nests three levels.**
`demos/02-multi-region/diagrams/multi-cluster-and-region/multi-region-control-plane-topology-3.html`

| Level | Label in the drawing | Contents |
| --- | --- | --- |
| 1 | `GLOBAL CONTROL PLANE` | the whole top band — 10 tiles in 3 groups |
| 2 | `MANAGEMENT BACKBONE` | 2 tiles: a NATS cluster, and one Postgres for placement + entitlement rows |
| 3 | `nats cluster · 3 nodes` | **this is `hub`** — the drawing labels it `3 nodes · the gateway hub` |

**In `Multi-Region-Plan.md` and in the drawing, `hub` is level 3.** It is
transport only. It is *not* the global control plane and *not* the management
backbone — the backbone's other tile is a Postgres that Phase 64 does not
build.

**In `demos/01-dictionary/deploy/global/compose.control.yaml`, `hub` is level
1.** Its header opens "THE CONTROL PLANE. One hub per trust domain." That file
predates the drawing and the sentence is still true of the deployment unit it
describes — it is not a bug to go and fix in isolation.

**Why it matters, not just vocabulary.** Phase 64 stands up a `hub` in the
level-3 sense with **no control-plane service attached**. The result is a
working topology and a non-working control plane. Each cell still runs its own
`accounts-service`, so there are two minters and the one place a single global
minter would live has none. That is D5's recorded contradiction, and reading it
is impossible until the word is pinned.

The two meanings get reconciled in the control-plane phase (§ 5 question 1),
which is when the band's services and this cluster first sit together and
`compose.control.yaml`'s header gets reworded.

Related: [[jetstream_domain_per_cluster_is_mandatory]],
[[compose_split_aws_deployment_decision]]
