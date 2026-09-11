# Demo 02: one supercluster takes ONE JetStream domain

**[demo 02]** Measured, fixed and re-measured 2026-09-09 on NATS 2.14.

## The rule

A JetStream `domain` names a JetStream **system**. Every server in a cluster
**and in a supercluster** must carry the **same** domain name. The name may
only change across a **leaf-node** link. A gateway is not a leaf link.

Source: the "JetStream in Leaf Nodes" page, *Configuration* section. The
sentence is gone from the current English page at
https://docs.nats.io/running-a-nats-service/configuration/leafnodes/jetstream_leafnodes
— confirmed verbatim on the older mirror at
https://docs.natsclub.cn/cn/yun-xing-yi-ge-nats-fu-wu/configuration/leafnodes/jetstream_leafnodes

## What we had wrong, and how it looked

`domain: za` and `domain: au` were set per region. It broke JetStream silently:

- Setting a domain **suppresses JetStream traffic on the system account**, so
  the clusters never learned each other's server names.
- za's meta leader listed the three au servers as
  `Server name unknown … offline: true`.
- All three au servers: `/jsz?meta=1` → `leader: null`, `cluster_size: 6`.
- `stream add --cluster au` → `no suitable peers for placement (10005)`.
- No error at startup. Only `JetStream cluster new remote metadata leader`.

## The fix

`NATS_JS_DOMAIN: lb` in **both** `deploy/compose.za.yaml` and
`compose.au.yaml`; `JS_DOMAIN="lb"` in `deploy/contexts.sh`; `jsDomain = "lb"`
in `odometer/main.go`. After it: all six peers `offline=false current=true`,
and `--cluster au` places correctly.

**Do not reintroduce a per-region domain.** The reasoning is in the comment
block in `demos/02-multi-region/nats/nats.conf`.

## What separates regions instead

1. **Placement** — `--cluster za|au` on `stream add`, or Go
   `Placement: &nats.Placement{Cluster: region}`. The odometer sets it on the
   stream and the KV bucket.
2. **Accounts** — the only real wall. `lab/03-odometer.sh` prices it.

## The consequence that surprised us

Inside **one** account a stream name is unique across the whole supercluster.
ZA takes `SHIPPING`; AU's request is refused with `stream name already in use
with a different configuration (10058)`, and `stream info` from AU returns
**ZA's** stream (same cluster, same `created`). So one account across two
regions is not two regions — AU is a second door into ZA's JetStream.

`lab/00-the-problem.sh` and `lab/01-option-1-subjects.sh` were rewritten around
this and both now pass. Option 1 costs a region token **twice**: in the subject
and in the stream name (`SHIPPING_ZA` / `SHIPPING_AU`).

See also [[demo02_host_cli_and_lab2_contexts]], [[gateway_double_capture_and_option3]].
