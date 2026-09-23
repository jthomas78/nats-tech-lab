# T5 on AWS ECS — build instructions

Demo 03's **T5 / Figure D** shape, built on AWS ECS.

T5 is a 3-server **hub** plus one 3-server **leaf cluster per region**. Nine NATS
servers. Three separate JetStream systems, not one.

This runbook is the *how to build it* companion to
`nats-migration-strategy.html`. That document says which phase to do when. This
one says which AWS objects to make.

Source of truth for the behaviour: `demos/03-multi-cluster-and-accounts/`,
script `lab/04-hub-and-leaf.sh`, checks `D1`–`D15`.

---

## 0. The shape, and where it runs

| Place | Region (assumed) | Cluster name | Domain | Servers |
|---|---|---|---|---|
| Hub | `af-south-1` | `hub-cl` | `hub` | `hub-1` `hub-2` `hub-3` |
| South Africa leaf | `af-south-1` | `za-cl` | `za` | `za-1` `za-2` `za-3` |
| Australia leaf | `ap-southeast-2` | `au-cl` | `au` | `au-1` `au-2` `au-3` |

Change the regions if you need to. Nothing below depends on those two names.

Three rules that are not negotiable:

1. **Every `server_name` is globally unique and permanent.** Renaming a server
   in a live JetStream cluster breaks it. Pick the names before you build.
2. **Every `cluster.name` is different.** A leaf cluster must not share the
   hub's cluster name, or the hub rejects the link.
3. **A `jetstream.domain` may only change across a leaf link.** That is why
   this shape works and a gateway with domains does not (demo 03, figure B).

---

## 1. The one big AWS decision

NATS JetStream writes a file store. It needs a real block disk with predictable
latency. Network file systems are a poor fit and have caused real outages.

| Option | Verdict |
|---|---|
| **ECS on EC2**, one instance per NATS server, JetStream on a `gp3` EBS volume | **Use this.** |
| ECS on Fargate, JetStream on EFS | Do not use. EFS is NFS. |
| ECS on Fargate, JetStream on a task-attached EBS volume | Do not use. ECS makes a **new** volume per task and cannot attach an existing one, so the data does not survive a task restart. |

So: **EC2 launch type.** Nine EC2 instances, nine ECS services, one task each.

---

## 2. Per-region AWS objects

Build this once per region.

### 2.1 VPC and subnets

- One VPC per region. Private subnets in three Availability Zones.
- **Peer the AU VPC to the hub VPC** (inter-region VPC peering). The leaf link
  must cross it. Add routes both ways.
- ZA is in the hub's own VPC, so it needs no peering.

### 2.2 ECS cluster

One ECS cluster per region, EC2 capacity. Do **not** turn on service
auto scaling anywhere in this runbook.

### 2.3 EC2 instances — one per NATS server

Three instances per NATS cluster. Spread them across three AZs.

- AMI: ECS-optimized Amazon Linux 2023.
- Size: start at `m7i.large`. Raise it after you measure.
- Root volume: default.
- **Second volume: `gp3`, 100 GiB, 3000 IOPS.** This is the JetStream store.
- Set `deleteOnTermination: false` on that second volume.

Instance user data — this does three jobs: format and mount the disk, join the
ECS cluster, and label the instance with the NATS server name.

```bash
#!/bin/bash
set -euo pipefail
SERVER_NAME=hub-1            # change per instance
CLUSTER_NAME=nats-hub        # the ECS cluster name

# 1. the JetStream disk
DEV=/dev/nvme1n1
blkid "$DEV" || mkfs -t xfs "$DEV"
mkdir -p /var/lib/nats
echo "$DEV /var/lib/nats xfs defaults,nofail 0 2" >> /etc/fstab
mount -a
chown 1000:1000 /var/lib/nats

# 2 and 3. join the ECS cluster, and label this instance
cat >> /etc/ecs/ecs.config <<EOF
ECS_CLUSTER=$CLUSTER_NAME
ECS_INSTANCE_ATTRIBUTES={"nats.node":"$SERVER_NAME"}
ECS_ENABLE_TASK_ENI=true
EOF
```

`ECS_INSTANCE_ATTRIBUTES` is what pins each service to its own disk. Without it
ECS may start `hub-2` on the instance that holds `hub-1`'s data.

### 2.4 Service discovery for the route mesh

Servers inside one cluster find each other by DNS.

- Make an **AWS Cloud Map private DNS namespace** per region, for example
  `nats.internal`.
- Register one Cloud Map service per NATS server: `hub-1`, `hub-2`, `hub-3`.
- Each ECS service registers into its own Cloud Map service.

This gives you `hub-1.nats.internal`, and that name is stable.

### 2.5 Security groups

| From | To | Port | Why |
|---|---|---|---|
| NATS servers in a cluster | each other | 6222 | route mesh |
| Applications | NATS servers | 4222 | clients |
| Hub servers | each other | 6222 | hub route mesh |
| AU and ZA VPC CIDRs | hub leaf NLB | 7422–7424 | leaf links |
| Your monitoring | NATS servers | 8222 | `/healthz` and `/jsz` |

---

## 3. The hub leaf endpoint

Every leaf server must list **all three** hub leaf URLs. One URL makes one hub
node a single point of failure. So you need three stable addresses that point
at three specific hub servers.

Build an **internal Network Load Balancer** in the hub VPC:

| Listener (TCP) | Target group | Target |
|---|---|---|
| 7422 | `hub-1-leaf` | `hub-1` task IP, port 7422 |
| 7423 | `hub-2-leaf` | `hub-2` task IP, port 7422 |
| 7424 | `hub-3-leaf` | `hub-3` task IP, port 7422 |

- Target type **`ip`** (the tasks use `awsvpc` networking).
- Health check: **HTTP `/healthz` on port 8222**.
- Turn **cross-zone load balancing on**.
- Use an NLB, not an ALB. NLB is the load balancer AWS supports over VPC
  peering.

The NLB's DNS name resolves from the AU region and returns hub private IPs, so
the peered route carries the traffic. Put a friendly CNAME in front of it if
you like.

---

## 4. The container image

One image for all nine servers. The config comes from S3 at start, so you
change a URL without rebuilding.

```dockerfile
FROM nats:2.12-alpine
RUN apk add --no-cache aws-cli
COPY entrypoint.sh /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
```

```bash
#!/bin/sh
# entrypoint.sh
set -eu
aws s3 cp "$NATS_CONF_S3_URI" /etc/nats/nats.conf
aws s3 cp "$NATS_ACCOUNTS_S3_URI" /etc/nats/accounts.conf
exec nats-server -c /etc/nats/nats.conf
```

Give the ECS **task role** `s3:GetObject` on that one prefix, and nothing else.

---

## 5. The NATS configs

### 5.1 `accounts.conf` — the same file on all nine servers

```
accounts {
  $SYS  { users: [ { user: admin, password: $SYS_PASS } ] }
  LB    { jetstream: enabled, users: [ { user: lb, password: $LB_PASS } ] }
  LB_ZA { jetstream: enabled, users: [ { user: za, password: $ZA_PASS } ] }
  LB_AU { jetstream: enabled, users: [ { user: au, password: $AU_PASS } ] }
}
```

> **Warning.** Demo 03 used plain passwords on `127.0.0.1`. That is lab-only.
> For production, decide on operator mode and JWTs. Demo 03 measured **no**
> evidence for operator-mode auth across a leaf link. That is phase 3 of the
> migration strategy, and it is still unmeasured. Do not assume it.

### 5.2 `hub-1.conf` — hub server

```
server_name: hub-1
listen: 0.0.0.0:4222
http:   0.0.0.0:8222

jetstream {
  store_dir: "/var/lib/nats/js"
  domain: hub
  max_file_store: 90GB
}

cluster {
  name: hub-cl
  listen: 0.0.0.0:6222
  routes: [
    nats://hub-1.nats.internal:6222
    nats://hub-2.nats.internal:6222
    nats://hub-3.nats.internal:6222
  ]
}

leafnodes {
  listen: 0.0.0.0:7422
  tls {
    cert_file: "/etc/nats/tls/hub.crt"
    key_file:  "/etc/nats/tls/hub.key"
    ca_file:   "/etc/nats/tls/ca.crt"
  }
}

include "accounts.conf"
```

`hub-2.conf` and `hub-3.conf` are the same file with a new `server_name`.
A server ignores its own address in the `routes` list, so the list stays
identical.

### 5.3 `au-1.conf` — leaf server

```
server_name: au-1
listen: 0.0.0.0:4222
http:   0.0.0.0:8222

jetstream {
  store_dir: "/var/lib/nats/js"
  domain: au
  max_file_store: 90GB
}

cluster {
  name: au-cl
  listen: 0.0.0.0:6222
  routes: [
    nats://au-1.nats.internal:6222
    nats://au-2.nats.internal:6222
    nats://au-3.nats.internal:6222
  ]
}

leafnodes {
  remotes: [
    { urls: [ "tls://lb:$LB_PASS@hub-leaf.internal:7422"
              "tls://lb:$LB_PASS@hub-leaf.internal:7423"
              "tls://lb:$LB_PASS@hub-leaf.internal:7424" ]
      account: LB }

    { urls: [ "tls://au:$AU_PASS@hub-leaf.internal:7422"
              "tls://au:$AU_PASS@hub-leaf.internal:7423"
              "tls://au:$AU_PASS@hub-leaf.internal:7424" ]
      account: LB_AU }
  ]
}

include "accounts.conf"
```

Two things people get wrong here:

- **All three URLs, every time.** With three URLs an orphaned link moves to
  another hub node by itself. With one URL it does not.
- **One leaf remote carries one account.** If you want three accounts to cross
  the link, you write three `remotes` entries.

`za-1.conf` is the same file with `server_name: za-1`, `domain: za`,
`cluster.name: za-cl`, and the `za-*` route names.

---

## 6. The ECS task definition

One task definition per NATS server. Nine in total.

```json
{
  "family": "nats-hub-1",
  "requiresCompatibilities": ["EC2"],
  "networkMode": "awsvpc",
  "cpu": "1024",
  "memory": "3072",
  "taskRoleArn": "arn:aws:iam::<acct>:role/nats-task",
  "executionRoleArn": "arn:aws:iam::<acct>:role/nats-exec",
  "volumes": [
    { "name": "jsdata", "host": { "sourcePath": "/var/lib/nats" } }
  ],
  "containerDefinitions": [
    {
      "name": "nats",
      "image": "<acct>.dkr.ecr.af-south-1.amazonaws.com/nats:2.12-lab",
      "essential": true,
      "environment": [
        { "name": "NATS_CONF_S3_URI",
          "value": "s3://my-nats-config/hub-1.conf" },
        { "name": "NATS_ACCOUNTS_S3_URI",
          "value": "s3://my-nats-config/accounts.conf" }
      ],
      "mountPoints": [
        { "sourceVolume": "jsdata", "containerPath": "/var/lib/nats" }
      ],
      "portMappings": [
        { "containerPort": 4222 },
        { "containerPort": 6222 },
        { "containerPort": 7422 },
        { "containerPort": 8222 }
      ],
      "healthCheck": {
        "command": ["CMD-SHELL",
                    "wget -q -O- http://127.0.0.1:8222/healthz || exit 1"],
        "interval": 15, "timeout": 5, "retries": 5, "startPeriod": 60
      },
      "stopTimeout": 60
    }
  ]
}
```

Leave the `7422` port mapping off the six leaf servers.

---

## 7. The ECS service

One service per NATS server. Nine in total.

```json
{
  "serviceName": "nats-hub-1",
  "taskDefinition": "nats-hub-1",
  "desiredCount": 1,
  "launchType": "EC2",
  "placementConstraints": [
    { "type": "memberOf", "expression": "attribute:nats.node == hub-1" }
  ],
  "deploymentConfiguration": {
    "minimumHealthyPercent": 0,
    "maximumPercent": 100,
    "deploymentCircuitBreaker": { "enable": true, "rollback": true }
  },
  "serviceRegistries": [
    { "registryArn": "arn:aws:servicediscovery:...:service/srv-hub1" }
  ],
  "networkConfiguration": {
    "awsvpcConfiguration": {
      "subnets": ["subnet-hub-az-a"],
      "securityGroups": ["sg-nats"]
    }
  }
}
```

Three settings that matter, and why:

- **`minimumHealthyPercent: 0` and `maximumPercent: 100`.** A deploy must stop
  the old task **before** it starts the new one. Two tasks cannot share one
  disk.
- **`placementConstraints`.** This is what marries `hub-1` to `hub-1`'s disk.
- **`desiredCount: 1`.** Never more. Never auto scaling. A NATS server is not
  a stateless replica.

Only the three hub services get an entry in an NLB target group.

---

## 8. Build order, and the gate at each step

Do not build all nine at once. Build, then prove, then move on.

### Step 1 — the hub cluster

Start `hub-1`, `hub-2`, `hub-3`. Then:

```bash
curl -s "http://hub-1.nats.internal:8222/jsz?meta=1" | jq '.meta_cluster'
```

**Gate:** `cluster_size` is `3` and `leader` is a hub server name.

### Step 2 — the AU leaf cluster

Start `au-1`, `au-2`, `au-3`. Wait about 10 seconds; interest takes time to
spread across a leaf link. Then:

```bash
curl -s "http://au-1.nats.internal:8222/leafz" | jq '.leafnodes | length'
curl -s "http://au-1.nats.internal:8222/jsz?meta=1" | jq '.meta_cluster'
```

**Gate:** each AU server shows its leaf connections, and AU reports its **own**
meta leader — a different name from the hub's. That is demo 03 check `D1`:
three JetStream systems, not one.

### Step 3 — the ZA leaf cluster

Same as step 2, for `za-1` `za-2` `za-3`.

### Step 4 — prove the hub can die

This is the whole point of T5. Set all three hub services to
`desiredCount: 0`. Wait 30 seconds. Then, in **both** AU and ZA:

```bash
nats --server nats://au-1.nats.internal:4222 --user au --password $AU_PASS \
     stream add T_NEW --subjects "evt.t.v1" --storage file --replicas 3 --defaults
```

**Gate:** both regions still elect a leader, still accept publishes, and still
create a new 3-replica stream. These are demo 03 checks `D10`–`D14`.

Set the hub back to `desiredCount: 1` when you are done.

---

## 9. Copying data between regions

**Nothing replicates by itself across a leaf link.** Every cross-region copy is
a mirror or a source that a person writes down and maintains.

A cross-domain mirror **must** name the other domain's API:

```json
{
  "name": "M_AU_ODOMETER",
  "num_replicas": 3,
  "mirror": {
    "name": "ODOMETER",
    "external": { "api": "$JS.au.API" }
  }
}
```

> **The trap, measured as demo 03 checks `D7` and `D9`.** Leave out
> `external.api` and one of two things happens. Either the mirror finds
> nothing and sits at **zero messages forever, with no error and a healthy
> status**. Or it finds a local stream with the same name and silently copies
> the **wrong data**.
>
> So: **alert on mirror lag, not on mirror existence.** A mirror that exists
> proves nothing.

Also write down a subject ownership matrix before you publish anything. In T5
neither region can see the other's streams, so neither can refuse an overlap.
One publish really can be stored twice (check `D5`). The written rule is the
only thing that stops it.

---

## 10. Things that will bite you

| Thing | What happens | What to do |
|---|---|---|
| ECS replaces an EC2 instance | The JetStream disk is gone | `deleteOnTermination: false`, and re-attach the volume by hand |
| A rolling deploy starts two tasks | Two servers fight over one disk | `minimumHealthyPercent: 0` |
| `awsvpc` on EC2 runs out of ENIs | Tasks stay `PENDING` | `ECS_ENABLE_TASK_ENI=true`, or a larger instance type |
| Reading `/jsz?meta=1` once | The `leader` field stays **stale** for up to a minute after a failure | Poll until it settles. Never trust one read |
| A leaf link with one hub URL | One hub node becomes a single point of failure | List all three, always |
| Someone adds a gateway "for speed" | The three meta groups collapse into one, and a WAN cut freezes both sides | Leaf links only. Demo 03 figure B |

---

## 11. What this runbook does **not** answer

Carried forward from `nats-migration-strategy.html`. These are honest gaps, not
oversights.

- **The hub as a real JetStream store.** Demo 03's T5 hub only relayed traffic.
- **Mirror catch-up at production volume.** No lag or bandwidth number exists.
- **Operator-mode JWT auth across a leaf link.** Demo 03 used plain passwords.
- **More than two leaves.** Only two were ever measured.

---

*Sources for the AWS storage decision:*
[ECS EBS volumes](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/ebs-volumes.html) ·
[Running NATS on AWS EC2, EBS, Fargate, EFS (Synadia)](https://www.synadia.com/blog/running-nats-on-aws-ec2-ebs-fargate-efs) ·
[Fargate + EBS](https://aws.amazon.com/blogs/containers/unlocking-aws-fargate-feature-for-attaching-amazon-ebs-volumes-to-ecs-tasks/)
