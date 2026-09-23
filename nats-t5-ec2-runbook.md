# T5 on AWS EC2 — build instructions

Demo 03's **T5 / Figure D** shape, built on plain EC2 instances.

T5 is a 3-server **hub** plus one 3-server **leaf cluster per region**. Nine NATS
servers. Three separate JetStream systems, not one.

No containers. One `nats-server` process per EC2 instance, run by `systemd`.
This is the closest thing to demo 03, which ran nine bare processes on one Mac.

- Companion: `nats-t5-ecs-runbook.md` — the same shape on ECS.
- Phase order and gates: `nats-migration-strategy.html`.
- Evidence: `demos/03-multi-cluster-and-accounts/lab/04-hub-and-leaf.sh`,
  checks `D1`–`D15`.

---

## 0. The shape, and the names

| Place | Region (assumed) | Cluster name | Domain | Servers |
|---|---|---|---|---|
| Hub | `af-south-1` | `hub-cl` | `hub` | `hub-1` `hub-2` `hub-3` |
| South Africa leaf | `af-south-1` | `za-cl` | `za` | `za-1` `za-2` `za-3` |
| Australia leaf | `ap-southeast-2` | `au-cl` | `au` | `au-1` `au-2` `au-3` |

Three rules that are not negotiable:

1. **Every `server_name` is globally unique and permanent.** Renaming a server
   in a live JetStream cluster breaks it. Pick the nine names before you build.
2. **Every `cluster.name` is different.** A leaf cluster must not share the
   hub's cluster name, or the hub rejects the link.
3. **A `jetstream.domain` may only change across a leaf link.** That is why
   this shape works and a gateway with domains does not (demo 03, figure B).

---

## 1. Ports

Nine boxes, so every server can use the NATS defaults.

| Port | What |
|---|---|
| 4222 | clients |
| 6222 | routes, inside one cluster |
| 7422 | leaf links — **hub only** |
| 8222 | monitor, `/healthz` and `/jsz` |

---

## 2. Network

### 2.1 VPCs

- One VPC per region. Private subnets in three Availability Zones.
- **Peer the AU VPC to the hub VPC** (inter-region VPC peering). Add routes
  both ways. ZA is in the hub's own VPC, so ZA needs no peering.
- On the peering connection, **turn on DNS resolution both ways**. Without it,
  AU cannot resolve the hub names.

### 2.2 Stable addresses — do this before you launch anything

A NATS server's address must not change when the box is rebuilt. So give each
server a fixed address up front.

**Make one Elastic Network Interface (ENI) per server**, with a fixed private
IP, in the subnet that server will live in. Attach it at launch as `eth0`.

| Server | AZ | ENI private IP (example) |
|---|---|---|
| hub-1 | af-south-1a | 10.0.1.11 |
| hub-2 | af-south-1b | 10.0.2.11 |
| hub-3 | af-south-1c | 10.0.3.11 |
| za-1 | af-south-1a | 10.0.1.21 |
| za-2 | af-south-1b | 10.0.2.21 |
| za-3 | af-south-1c | 10.0.3.21 |
| au-1 | ap-southeast-2a | 10.1.1.11 |
| au-2 | ap-southeast-2b | 10.1.2.11 |
| au-3 | ap-southeast-2c | 10.1.3.11 |

### 2.3 Private DNS

Make a **Route 53 private hosted zone**, `nats.internal`. Add one A record per
server, pointing at the ENI IP above:

```
hub-1.nats.internal  A  10.0.1.11
hub-2.nats.internal  A  10.0.2.11
hub-3.nats.internal  A  10.0.3.11
za-1.nats.internal   A  10.0.1.21
...
au-3.nats.internal   A  10.1.3.11
```

Associate the zone with **both** VPCs. Cross-region association takes two
steps:

```bash
aws route53 create-vpc-association-authorization \
  --hosted-zone-id Z123EXAMPLE \
  --vpc VPCRegion=ap-southeast-2,VPCId=vpc-au

aws route53 associate-vpc-with-hosted-zone \
  --region ap-southeast-2 \
  --hosted-zone-id Z123EXAMPLE \
  --vpc VPCRegion=ap-southeast-2,VPCId=vpc-au
```

Now every config file names a server, never an IP. That is the point.

**No load balancer is needed.** On ECS you need one, because task IPs move.
Here the ENI holds the IP still, so a leaf server can name the three hub
servers directly.

### 2.4 Security groups

| From | To | Port |
|---|---|---|
| NATS servers in one cluster | each other | 6222 |
| Applications | NATS servers | 4222 |
| AU VPC CIDR and ZA subnets | **hub servers only** | 7422 |
| Monitoring | NATS servers | 8222 |

Do **not** open 7422 on the six leaf servers. They dial out. They never listen.

---

## 3. The EC2 instances

Nine instances. One per NATS server.

- AMI: Amazon Linux 2023.
- Size: start at `m7i.large`. Raise it after you measure.
- Network interface: the ENI from section 2.2. Do not let EC2 make a new one.
- Root volume: `gp3`, 30 GiB.
- **Second volume: `gp3`, 100 GiB, 3000 IOPS.** This is the JetStream store.
  Set `deleteOnTermination: false`.
- IAM instance profile: SSM Session Manager, plus read access to wherever you
  keep the config and the TLS keys.

### Why a separate volume

JetStream writes a file store. It wants a real block disk with predictable
latency. Keeping it off the root volume means you can rebuild the box and keep
the data.

Do **not** put the store on EFS. EFS is NFS, and NFS under a JetStream file
store has caused real outages.

Local NVMe instance store is faster, but it is wiped when the instance stops.
With `--replicas 3` that is survivable, but it is a deliberate risk. Start with
`gp3`.

### Auto recovery instead of an Auto Scaling group

Do **not** put these in an Auto Scaling group. A NATS server is not a
stateless replica; the group would replace it and lose its name, IP and disk.

Use **EC2 auto recovery** instead. It restarts the same instance on new
hardware, and keeps the instance ID, the ENI and the EBS volume.

```bash
aws cloudwatch put-metric-alarm \
  --alarm-name nats-hub-1-recover \
  --namespace AWS/EC2 --metric-name StatusCheckFailed_System \
  --dimensions Name=InstanceId,Value=i-0hub1 \
  --statistic Maximum --period 60 --evaluation-periods 2 --threshold 1 \
  --comparison-operator GreaterThanOrEqualToThreshold \
  --alarm-actions arn:aws:automate:af-south-1:ec2:recover
```

---

## 4. Install NATS on one instance

This is the user data. Change `SERVER_NAME` on each box.

```bash
#!/bin/bash
set -euo pipefail
SERVER_NAME=hub-1
NATS_VERSION=2.12.1

# ---- 1. the JetStream disk ------------------------------------------------
DEV=/dev/nvme1n1
blkid "$DEV" || mkfs -t xfs "$DEV"
mkdir -p /var/lib/nats
grep -q /var/lib/nats /etc/fstab || \
  echo "$DEV /var/lib/nats xfs defaults,nofail 0 2" >> /etc/fstab
mount -a

# ---- 2. the binary --------------------------------------------------------
cd /tmp
curl -sSfL -o nats.tar.gz \
  "https://github.com/nats-io/nats-server/releases/download/v${NATS_VERSION}/nats-server-v${NATS_VERSION}-linux-amd64.tar.gz"
tar xzf nats.tar.gz
install -m 0755 nats-server-v${NATS_VERSION}-linux-amd64/nats-server /usr/local/bin/nats-server

# ---- 3. the user and the folders -----------------------------------------
id nats >/dev/null 2>&1 || useradd --system --no-create-home --shell /sbin/nologin nats
mkdir -p /etc/nats/tls
chown -R nats:nats /var/lib/nats /etc/nats

# ---- 4. the config --------------------------------------------------------
aws ssm get-parameter --with-decryption \
    --name "/nats/${SERVER_NAME}/nats.conf" \
    --query Parameter.Value --output text > /etc/nats/nats.conf
aws ssm get-parameter --with-decryption \
    --name "/nats/accounts.conf" \
    --query Parameter.Value --output text > /etc/nats/accounts.conf
chown nats:nats /etc/nats/*.conf
chmod 0640 /etc/nats/*.conf

# ---- 5. the service -------------------------------------------------------
cat > /etc/systemd/system/nats.service <<'EOF'
[Unit]
Description=NATS Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=nats
Group=nats
ExecStart=/usr/local/bin/nats-server -c /etc/nats/nats.conf
ExecReload=/bin/kill -HUP $MAINPID
Restart=always
RestartSec=5
LimitNOFILE=800000
TimeoutStopSec=90

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now nats
```

`ExecReload` with `HUP` re-reads the config with **no restart**. Use it when you
add a leaf URL.

---

## 5. The config files

### 5.1 `/etc/nats/accounts.conf` — identical on all nine servers

```
accounts {
  $SYS  { users: [ { user: admin, password: "<from SSM>" } ] }
  LB    { jetstream: enabled, users: [ { user: lb, password: "<from SSM>" } ] }
  LB_ZA { jetstream: enabled, users: [ { user: za, password: "<from SSM>" } ] }
  LB_AU { jetstream: enabled, users: [ { user: au, password: "<from SSM>" } ] }
}
```

> **Warning.** Demo 03 used plain passwords on `127.0.0.1`. That is lab-only.
> For production, decide on operator mode and JWTs. Demo 03 measured **no**
> evidence for operator-mode auth across a leaf link. That is phase 3 of the
> migration strategy, and it is still unmeasured. Do not assume it.

### 5.2 `/etc/nats/nats.conf` on `hub-1`

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
### 5.3 `/etc/nats/nats.conf` on `hub-2`

```
server_name: hub-2
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
### 5.4 `/etc/nats/nats.conf` on `hub-3`

```
server_name: hub-3
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
### 5.5 `/etc/nats/nats.conf` on `za-1`

```
server_name: za-1
listen: 0.0.0.0:4222
http:   0.0.0.0:8222

jetstream {
  store_dir: "/var/lib/nats/js"
  domain: za
  max_file_store: 90GB
}

cluster {
  name: za-cl
  listen: 0.0.0.0:6222
  routes: [
    nats://za-1.nats.internal:6222
    nats://za-2.nats.internal:6222
    nats://za-3.nats.internal:6222
  ]
}

leafnodes {
  remotes: [
    { urls: [ "tls://lb:<pass>@hub-1.nats.internal:7422"
              "tls://lb:<pass>@hub-2.nats.internal:7422"
              "tls://lb:<pass>@hub-3.nats.internal:7422" ]
      account: LB }

    { urls: [ "tls://za:<pass>@hub-1.nats.internal:7422"
              "tls://za:<pass>@hub-2.nats.internal:7422"
              "tls://za:<pass>@hub-3.nats.internal:7422" ]
      account: LB_ZA }
  ]
}

include "accounts.conf"
```
### 5.6 `/etc/nats/nats.conf` on `za-2`

```
server_name: za-2
listen: 0.0.0.0:4222
http:   0.0.0.0:8222

jetstream {
  store_dir: "/var/lib/nats/js"
  domain: za
  max_file_store: 90GB
}

cluster {
  name: za-cl
  listen: 0.0.0.0:6222
  routes: [
    nats://za-1.nats.internal:6222
    nats://za-2.nats.internal:6222
    nats://za-3.nats.internal:6222
  ]
}

leafnodes {
  remotes: [
    { urls: [ "tls://lb:<pass>@hub-1.nats.internal:7422"
              "tls://lb:<pass>@hub-2.nats.internal:7422"
              "tls://lb:<pass>@hub-3.nats.internal:7422" ]
      account: LB }

    { urls: [ "tls://za:<pass>@hub-1.nats.internal:7422"
              "tls://za:<pass>@hub-2.nats.internal:7422"
              "tls://za:<pass>@hub-3.nats.internal:7422" ]
      account: LB_ZA }
  ]
}

include "accounts.conf"
```
### 5.7 `/etc/nats/nats.conf` on `za-3`

```
server_name: za-3
listen: 0.0.0.0:4222
http:   0.0.0.0:8222

jetstream {
  store_dir: "/var/lib/nats/js"
  domain: za
  max_file_store: 90GB
}

cluster {
  name: za-cl
  listen: 0.0.0.0:6222
  routes: [
    nats://za-1.nats.internal:6222
    nats://za-2.nats.internal:6222
    nats://za-3.nats.internal:6222
  ]
}

leafnodes {
  remotes: [
    { urls: [ "tls://lb:<pass>@hub-1.nats.internal:7422"
              "tls://lb:<pass>@hub-2.nats.internal:7422"
              "tls://lb:<pass>@hub-3.nats.internal:7422" ]
      account: LB }

    { urls: [ "tls://za:<pass>@hub-1.nats.internal:7422"
              "tls://za:<pass>@hub-2.nats.internal:7422"
              "tls://za:<pass>@hub-3.nats.internal:7422" ]
      account: LB_ZA }
  ]
}

include "accounts.conf"
```
### 5.8 `/etc/nats/nats.conf` on `au-1`

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
    { urls: [ "tls://lb:<pass>@hub-1.nats.internal:7422"
              "tls://lb:<pass>@hub-2.nats.internal:7422"
              "tls://lb:<pass>@hub-3.nats.internal:7422" ]
      account: LB }

    { urls: [ "tls://au:<pass>@hub-1.nats.internal:7422"
              "tls://au:<pass>@hub-2.nats.internal:7422"
              "tls://au:<pass>@hub-3.nats.internal:7422" ]
      account: LB_AU }
  ]
}

include "accounts.conf"
```
### 5.9 `/etc/nats/nats.conf` on `au-2`

```
server_name: au-2
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
    { urls: [ "tls://lb:<pass>@hub-1.nats.internal:7422"
              "tls://lb:<pass>@hub-2.nats.internal:7422"
              "tls://lb:<pass>@hub-3.nats.internal:7422" ]
      account: LB }

    { urls: [ "tls://au:<pass>@hub-1.nats.internal:7422"
              "tls://au:<pass>@hub-2.nats.internal:7422"
              "tls://au:<pass>@hub-3.nats.internal:7422" ]
      account: LB_AU }
  ]
}

include "accounts.conf"
```
### 5.10 `/etc/nats/nats.conf` on `au-3`

```
server_name: au-3
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
    { urls: [ "tls://lb:<pass>@hub-1.nats.internal:7422"
              "tls://lb:<pass>@hub-2.nats.internal:7422"
              "tls://lb:<pass>@hub-3.nats.internal:7422" ]
      account: LB }

    { urls: [ "tls://au:<pass>@hub-1.nats.internal:7422"
              "tls://au:<pass>@hub-2.nats.internal:7422"
              "tls://au:<pass>@hub-3.nats.internal:7422" ]
      account: LB_AU }
  ]
}

include "accounts.conf"
```
### 5.11 What actually differs, across all nine

Because the configs name **DNS hosts**, not ports, there is very little
variation. Inside one cluster, **only `server_name` changes**.

| | hub-1/2/3 | za-1/2/3 | au-1/2/3 |
|---|---|---|---|
| `server_name` | `hub-1` `hub-2` `hub-3` | `za-1` `za-2` `za-3` | `au-1` `au-2` `au-3` |
| `domain` | `hub` | `za` | `au` |
| `cluster.name` | `hub-cl` | `za-cl` | `au-cl` |
| `routes` | the three `hub-*` names | the three `za-*` names | the three `au-*` names |
| `leafnodes` | **`listen`** on 7422 | **`remotes`**, no listen | **`remotes`**, no listen |
| second remote account | — | `LB_ZA` | `LB_AU` |

A server ignores its own address in `routes`, so every server in a cluster
carries the identical three-name list.

Two things people get wrong here:

- **All three hub URLs, every time.** With three URLs, an orphaned link moves
  to another hub node by itself. With one URL, one hub node becomes a single
  point of failure.
- **One leaf remote carries one account.** Demo 03 only ever crossed one
  account (`LB`). The second `remotes` entry above is **not** measured by
  demo 03.

---

## 6. Build order, and the gate at each step

Do not build all nine at once. Build, then prove, then move on.

### Step 1 — the hub cluster

Start `hub-1`, `hub-2`, `hub-3`.

```bash
curl -s "http://hub-1.nats.internal:8222/jsz?meta=1" | jq '.meta_cluster'
```

**Gate:** `cluster_size` is `3`, and `leader` is a hub server name.

### Step 2 — the AU leaf cluster

Start `au-1`, `au-2`, `au-3`. **Wait about 10 seconds.** Interest takes that
long to spread across a leaf link. Demo 03 once read `0 received` after 2
seconds and nearly wrote it up as a failure.

```bash
curl -s "http://au-1.nats.internal:8222/leafz"     | jq '.leafnodes | length'
curl -s "http://au-1.nats.internal:8222/jsz?meta=1" | jq '.meta_cluster'
```

**Gate:** each AU server shows its leaf connections, and AU reports its **own**
meta leader — a different name from the hub's. That is demo 03 check `D1`:
three JetStream systems, not one.

### Step 3 — the ZA leaf cluster

Same as step 2, for `za-1` `za-2` `za-3`.

### Step 4 — prove the hub can die

This is the whole point of T5.

```bash
# on each hub box
sudo systemctl stop nats
```

Wait 30 seconds. Then, in **both** AU and ZA:

```bash
nats --server nats://au-1.nats.internal:4222 --user au --password <pass> \
     stream add T_NEW --subjects "evt.t.v1" --storage file --replicas 3 --defaults
```

**Gate:** both regions still elect a leader, still accept publishes, and still
create a new 3-replica stream. Those are demo 03 checks `D10`–`D14`.

Start the hub again when you are done.

---

## 7. Copying data between regions

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
> `external.api` and one of two things happens. Either the mirror finds nothing
> and sits at **zero messages forever, with no error and a healthy status**. Or
> it finds a local stream with the same name and silently copies the **wrong
> data**.
>
> So: **alert on mirror lag, not on mirror existence.** A mirror that exists
> proves nothing.

Write a subject ownership matrix before you publish anything. In T5, neither
region can see the other's streams, so neither can refuse an overlap. One
publish really can be stored twice (demo 03 check `D5`). The written rule is
the only thing that stops it.

---

## 8. Things that will bite you

| Thing | What happens | What to do |
|---|---|---|
| An Auto Scaling group replaces a box | Name, IP and disk all change; the cluster breaks | Plain instances plus EC2 auto recovery |
| `deleteOnTermination` left on | The JetStream disk is gone when you terminate | Set it to `false` on the data volume |
| A leaf server listed with one hub URL | One hub node is a single point of failure | List all three, always |
| Reading `/jsz?meta=1` once | The `leader` field stays **stale** for up to a minute after a failure | Poll until it settles. Never trust one read |
| DNS not shared over the peering | AU cannot resolve `hub-1.nats.internal` | Turn on peering DNS resolution both ways |
| Reusing a `server_name` on a rebuilt box | JetStream state gets confused | Never reuse. Never rename. Pick once |
| Someone adds a gateway "for speed" | The three meta groups collapse into one, and a WAN cut freezes both sides | Leaf links only. Demo 03 figure B |
| A full disk | JetStream stops accepting writes | Alarm on `/var/lib/nats` at 70% |

---

## 9. What to watch

| Source | What | Alarm when |
|---|---|---|
| `/healthz` on 8222 | is the server up | not 200 for 60s |
| `/jsz?meta=1` | `.meta_cluster.leader` | empty for 60s |
| `/leafz` | leaf link count | drops below expected |
| stream info on a mirror | `state.messages` is moving | lag grows, or stays at 0 |
| disk | `/var/lib/nats` used | above 70% |

---

## 10. What this runbook does **not** answer

Carried forward from `nats-migration-strategy.html`. These are honest gaps, not
oversights.

- **The hub as a real JetStream store.** Demo 03's T5 hub only relayed traffic.
- **Mirror catch-up at production volume.** No lag or bandwidth number exists.
- **Operator-mode JWT auth across a leaf link.** Demo 03 used plain passwords.
- **More than two leaves.** Only two were ever measured.
- **A second account across the leaf link.** Demo 03 crossed only `LB`.

---

## 11. EC2 or ECS?

| | EC2 (this file) | ECS (`nats-t5-ecs-runbook.md`) |
|---|---|---|
| Stable disk | EBS on the box | EBS on the box, plus a pinning rule |
| Stable address | ENI with a fixed IP | needs a Network Load Balancer |
| Launch type | not applicable | must be EC2; Fargate cannot hold the disk |
| Moving parts | fewer | more |
| Closest to demo 03 | **yes** | no |

**Use EC2.** ECS adds a scheduler you must then fight to keep still, and you
still end up on EC2 boxes with EBS volumes underneath.

---

*Sources for the storage decision:*
[Running NATS on AWS EC2, EBS, Fargate, EFS (Synadia)](https://www.synadia.com/blog/running-nats-on-aws-ec2-ebs-fargate-efs) ·
[ECS EBS volumes](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/ebs-volumes.html)
