# NUI — one web GUI for every demo's NATS server

[NUI](https://github.com/nats-nui/nui) is a browser UI for NATS: streams,
consumers, KV buckets, object stores, and a live message view.

It is a **lab tool, not a demo**. It joins no demo network and changes no
demo's Compose file. Every demo already publishes its NATS client port on the
host, so NUI dials back out to `host.docker.internal`. Demos 01–04 stay sealed.

## Run it

```bash
docker compose -p lab-tools -f tools/nui/compose.yaml up -d
```

Then seed the connections (once):

```bash
./tools/nui/seed-connections.sh
```

Open <http://localhost:31311>.

Stop it:

```bash
docker compose -p lab-tools -f tools/nui/compose.yaml down
```

`down` keeps the `nui-db` volume, so the connections survive. Add `-v` to start
over, then re-run the seed script.

## The connections

NUI has no config file, but it has a REST API, so `seed-connections.sh` is the
config. Edit the `CONNECTIONS` array there and re-run it — the script deletes a
connection of the same name before re-creating it, so it is safe to run twice.

| Connection | Server URLs | Credential |
|---|---|---|
| `demo01-za-1` | `4222` | `/creds/demo01/platform.creds` |
| `demo02-za` | `4621`, `4622`, `4623` | `/creds/demo02/platform.creds` |
| `demo02-au` | `4721`, `4722`, `4723` | `/creds/demo02/platform.creds` |
| `demo04-odometer` | `4422` | none |

Notes on the choices:

- **`platform.creds`, not `sys.creds`.** The system account sees servers and
  connections but **not** another account's streams or KV, so `sys.creds` shows
  an empty stream list. Swap in a tenant's file (`acme.creds`, `globex.creds`)
  to see that tenant's data instead.
- **Demo 02 is six servers in two clusters**, so it is two connections, three
  URLs each. One connection cannot span a gateway.
- **Demo 04 needs no credential.** It is a plain server with no operator mode.
- Auth mode is `auth_creds_file`, and NUI reads that **path inside its own
  container**. `compose.yaml` mounts each operator-mode demo's creds directory
  read-only under `/creds/<demo>`. A host path will not work.

## Demo 03 does not work here, and cannot

Demo 03 runs its six servers **on the host**, and every `.conf` binds to
`127.0.0.1` (for example `demos/03-multi-cluster-and-accounts/za-1.conf`).
A container's traffic arrives over the Docker bridge, not over loopback, so a
loopback-only listener refuses it.

Use the `nats` CLI for demo 03. Do not "fix" this by rebinding those servers to
`0.0.0.0` — the loopback bind is what keeps that demo off the network.

## Port

`31311` is NUI's own default. It sits outside this repo's `7100–7199` frontend
band on purpose: that band is fully allocated between demo 01's `za-1`
(7100–7149) and `au-1` (7150–7199) cells, and NUI is not a demo frontend.
