---
name: compose-split-aws-deployment-decision
description: Decisions 2026-09-07/08 — split docker-compose into 3 files x 3 projects (control plane + 2 regions), prove multi-region on the laptop BEFORE any AWS work, and use Compose locally + Helm/K8s in production (two definitions, drift accepted and managed); records the AWS work list and the four dev/prod differences that are allowed
metadata:
  type: project
---

**Cell split DONE 2026-09-08 ([[ADR-055]] = `ADR/lab/ADR-055-lab-platform-compose-split-cell-and-global.md`).**
`demos/01-dictionary/deploy/cell/` now holds `compose.yaml` (include-only) + `compose.infra.yaml` /
`compose.control.yaml` / `compose.runtime.yaml` / `compose.dedicated.yaml`, plus
`deploy/environments/local-za-1.env` and `local-au-1.env`. All 25 host ports are `${VAR:-default}`; every
`container_name:` and `name: poc` is gone. **Proved:** the resolved `docker compose config` is byte-identical
to the old flat file under `local-za-1.env`, and the two cells publish 25 ports each with zero overlap.
au-1 offsets are **+50 inside CLAUDE.md's two fixed bands** (za-1 low half, au-1 high half) and **+100 for the
conventional ports** — a flat +100 was wrong, it walks 7100 into the 7200 backend band.
Three constraints found during the work, all recorded in ADR-055: the plugin fixtures cannot be separated
(YAML anchor `&plugin_dependencies` does not cross files, and `service:mfe-plugin-host` is a same-project build
context); paths are now relative to `deploy/cell/` (`../../../..` for the repo root, `../../nats/...` for mounts);
`nats-logs` is declared in two tier files identically because `nats` writes it and `observability-service` reads it.
**Both cells RAN side by side 2026-09-08 and it works:** 17 containers each, 25 published ports each with
zero overlap, two distinct NATS `server_id`s, 28 JetStream streams per cell with independent counts, and a
marker row written into za-1's Postgres was absent from au-1's.

**CORRECTED 2026-09-08 — the "shared trust material" defect recorded here earlier was WRONG.** The shared NATS
trust tree is **correct, not a defect**: one operator plus one set of account JWTs across many regional servers is
how NATS multi-region works, and it is ADR-054 rule 5 (operator minted once, Linebooker-held). Disproved by four
checks: both cells' `/data/jwt` hold four **byte-identical** JWTs (4526/7785/1560/4502); no cross-cell connections
(`172.19.x` vs `172.21.x`); identical 8-subject `PUBSUB` shape; each account JWT imports the two tenants and never
itself. Do not re-propose a per-cell trust tree — it would also force a per-cell `nats.conf`, because
`nats/bootstrap-operator.sh` **rewrites `nats.conf` in place** (an awk splice replaces the `system_account:` /
`resolver_preload:` tail).

**The two real causes of the anomalies.** (a) `PUBSUB` 2272 vs 1136 is a **container start-order race**:
`RegisterRefdataNotify` in `backend/shipping-service/dictionary/internal/eventhandler/platform_notify.go` bridges
`evt.*.refdata.>` to `notify._platform.refdata.>` with `DeliverPolicy: jetstream.DeliverNewPolicy`; za-1 seeded
before the bridge attached (no republishes), au-1's bridge attached first (every seed republished). Pre-existing,
nothing to do with the compose split. (b) au-1's empty `KV_mfe-registry` is a **hardcoded port in a data file**:
`demos/01-dictionary/registry.json` names `http://localhost:7112/remoteEntry.js` literally, au-1's
`REGISTRY_ALLOWED_ORIGINS` is 7161–7165, so the preload is withheld. **A port can hide in a data file, not just in
compose** — that one IS a gap in the split.


**Done 2026-09-08 — the global band exists.** `deploy/global/compose.control.yaml` holds `accounts-service`;
`deploy/cell/compose.yaml`'s `include:` no longer lists it. `deploy/global/` sits at the **same depth** as
`deploy/cell/`, so no relative path changed. Rule learned: **a `depends_on` cannot name a service in another
project** — Compose fails with `invalid compose project`, and `required: false` does NOT relax it (tested,
Compose v5.4.0; it only relaxes a service that exists but has not started). So the control file names no cell
service, the five cell services that named `accounts-service` no longer do, and `restart: on-failure` replaces the
ordering both ways. **One hub per trust domain. The mesh has one. A sovereign cell is its own** — Botswana runs the
*same* file in its own project (`-f compose.yaml -f ../global/compose.control.yaml`), because `L2-020` gives it no
gateway path, so per `nats-network-topology-2.html` it is a second trust domain, not a third cluster. The two
drawings never disagreed: one draws the mesh, the other draws what a sovereign cell needs.

**Fixed 2026-09-08 — BR-AS74.** `mfe-registry-service` now expands `${VAR}`/`${VAR:-default}` in the mounted
preload file before parsing (`registry/internal/preload/expand.go`; unset-with-no-default fails boot; bare `$name`
and unclosed `${` left alone). `registry.json`'s origin is `http://localhost:${PLUGIN_CATALOG_PORT:-7112}` and
`compose.runtime.yaml` passes the var through. au-1 logs `seeded=1 withheld=0` where it withheld before.

**Fixed 2026-09-08 — BR-AC46, creds read-only.** The old hazard (`nats/creds` mounted read-write into
`accounts-service`) is gone. `NATS_CREDS_DIR` is now a **PATH-style list**, `/etc/nats/creds:/var/lib/nats/creds`,
first directory wins — seed first so BR-AC19's stable platform/acme/globex identity can never be shadowed. That
shape is why it was cheap: `Discover(credsDir string)` keeps its signature, so shipping/refdata/pricing/
organizations inherited the list with **zero call-site changes**. Write side is its own var,
`NATS_CREDS_WRITE_DIR`, one directory, never the seed; `Handlers.CredsDir` renamed `CredsWriteDir` so the trap
cannot be re-set. Writable half is a per-project named volume `nats-creds-minted` (rw in the writer, `:ro` in all
four scanners), so two cells do not share minted tenants.

**The gotcha worth remembering: a correct compose file can still fail on volume ownership.** A named volume
mounted at a path the image does not contain is created `root:root 0755`; `accounts-service` runs as `app`
(uid 1000) and the first mint died with `permission denied` while `docker compose config` looked perfect. Docker
seeds a volume from the image's directory **including ownership**, so the fix is `mkdir` + `chown app:app` before
the `USER app` line in the Dockerfile — Compose cannot set a volume's owner. Found by minting a tenant, not by
reading resolved config. Also: **BR-D41 was already taken** (refdata admin isolation) — check the highest existing
ID per prefix before naming a rule.

**Still to do:** make
`RegisterRefdataNotify` start-order independent; retire the old flat `demos/01-dictionary/docker-compose.yml`;
write the hub NATS `gateway {}` config (`nats/nats.conf` has no `cluster {}`/`gateway {}`/`leafnodes {}` today, so
every hub line is additive).

**Decided 2026-09-07.** The target shape is
`demos/01-dictionary/diagrams/multi-cluster-and-region/multi-region-control-plane-topology-3.html`
(revision 3: global band = platform control services + trust material + management backbone; below it, N regional cells).

**The split — files and projects are two different axes.**

- Files = *when you ship it*: `deploy/cell/compose.infra.yaml` (nats, postgres — has data), `deploy/global/compose.control.yaml` (the provisioner — exactly one exists), `deploy/cell/compose.runtime.yaml` (accounts, refdata, business services — no data). A fourth `deploy/cell/compose.yaml` is `include:`-only, for one-command local runs.
- Projects = *where it runs*: `-p lb-global` (global control plane, once), `-p lb-za-1`, `-p lb-au-1`. **ZA and AU run the same two files; only the env file differs.** That is the whole trick. Regions are **South Africa (`za`, AWS `af-south-1`) and Australia (`au`, AWS `ap-southeast-2`)** per [[v3-tenancy-axes-decision]] — an earlier `eu`/`us` placeholder naming was wrong and was corrected on 2026-09-07.

**Chosen sequence: prove it locally first, then AWS.** Rejected alternative was going straight at one AWS region, which means fighting NATS storage, secrets and IaC simultaneously with no proof the split is correct. Local first is ~one session and forces the migration-job and variable-port work that AWS needs anyway. **Write the ADR during the split, not after.**

**Hard blocker on a second local region:** `demos/01-dictionary/docker-compose.yml` hard-codes every host port (`4222`, `8222`, `9222`, `5432`, `72xx`, `71xx`). Two regions cannot both bind `4222`. Every host port becomes `${VAR:-default}` with `deploy/environments/local-za-1.env` / `local-au-1.env` supplying the offsets — see [[frontend_port_structure]] and [[project-ports-tenant-scoping]] for the existing port allocation rules.

**Decided 2026-09-08 — Option A: Compose locally, Helm/Kubernetes in production.** Rejected alternative was one definition
for both (Compose in production via ECS, or Kind/k3d locally so the laptop also runs Helm). Chosen because the local
Compose file already works and the local split is ~one session, so this is the fastest route to a running two-region
proof; production on AWS then means EKS, which item 1 of the AWS work list below already forces (there is no managed
NATS, and JetStream needs a StatefulSet's stable disk and stable node identity).

**The accepted cost, stated plainly: two definitions of the same system, and they will drift.** How the drift is held down:

- **The service list has one source of truth.** A service exists in `deploy/` Compose *and* in the Helm chart, or it does
  not exist. Adding a service to one without the other is the drift that matters; anything else is cosmetic.
- **One image, built once.** No `if production` in Go. Config comes only from env vars and secrets. A service is addressed
  by name, never by host port. (Already recorded below — this decision is what makes it load-bearing rather than tidy.)
- **Only the four differences below are allowed to differ.** Any fifth difference is a bug in the split, not a config choice.
- **The topology drawing is the shared spec, not either file.** Both editions must render
  `multi-region-control-plane-topology-3.html` and `nats-network-topology-2.html`; when they disagree, the drawing wins.
- **Local stays honest by always running two cells.** One cell hides every cross-region assumption; see the sequence note above.

**Not decided yet:** whether the Helm chart is hand-written or generated, and whether Argo CD or plain `helm upgrade`
applies it. Revision 3's figure-4 table already maps each Compose file to its Helm/Argo counterpart
(`compose.infra.yaml` → OpenTofu + the official NATS Helm chart as a StatefulSet; `compose.control.yaml` → one Helm
release in the management cluster; `compose.runtime.yaml` → one Helm release per cell; `za-1.env`/`au-1.env` →
`values-za-1.yaml`/`values-au-1.yaml`; `-p lb-za-1` → the cell namespace and its Argo `Application`) — treat that table as
the starting point, not as a made decision.

**AWS work list, biggest first** (assessed 2026-09-07, feedback only):

1. **NATS storage + node identity decides everything else.** JetStream needs disk that survives a restart and 3 fixed route addresses. Fargate gives neither well. EKS StatefulSet vs EC2 vs Synadia Cloud — decide first. There is no managed NATS on AWS.
2. **The global control plane does not exist as code.** Revision 3 draws a global `refdata master` pushing to regional copies over `evt._platform.refdata.*`; today `refdata-service` is one service. New mechanism, not a deploy setting. Related: [[linebooker_refdata_layering_model]], [[refdata_v3_ladder_placement]].
3. **Trust material.** `demos/01-dictionary/nats/bootstrap-operator.sh` mints operator + accounts + `.creds` and is lab-only. Production mints once, offline, operator NKey never enters AWS (ADR-054 rule 5, Linebooker-held). Mostly process.
4. **Secrets.** `.creds` are files mounted from `./nats/creds`; AWS needs Secrets Manager / SSM injected at task start. Never in an image, never in git.
5. **JetStream replicas are R1 today** (revision 3 says so in amber). Three AZs need R3 on every stream and KV bucket.
6. **Migrations run in-process at service start** (`backend/*/internal/postgres/migrate.go`). Safe at one task, races at three. Move to a one-shot job.
7. Ingress: ALB per region, ACM, Route 53 latency routing. Frontends are nginx containers today; S3 + CloudFront is the better production answer.
8. Temporal is a container + its own Postgres; production means Temporal Cloud or a real cluster, and it needs its own ADR under ADR-054 rule 1.
9. Jaeger is a lab tool, not a production trace backend.
10. **No Terraform/CDK exists at all.**

**Four dev/prod differences are accepted as config, not as drift:** NATS 1 node vs 3 across AZs; streams R1 vs R3; secrets from file vs Secrets Manager; Postgres container vs RDS. **Everything else must be identical** — one image, no `if production` in Go, config only from env vars and secrets, services addressed by name and never by host port.

**Rough size:** local split ~one session; first AWS region weeks not days (item 1 is most of it); second region smaller if the first is clean; global control plane is the biggest, because items 2 and 3 are new work rather than new config. See also [[app-shell-deployment-gaps]] — green suites prove nothing about Dockerfile COPYs, NATS grants or creds regeneration.
