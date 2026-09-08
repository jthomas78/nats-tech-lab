---
name: compose-split-aws-deployment-decision
description: Decisions 2026-09-07/08 — split docker-compose into 3 files x 3 projects (control plane + 2 regions), prove multi-region on the laptop BEFORE any AWS work, and use Compose locally + Helm/K8s in production (two definitions, drift accepted and managed); records the AWS work list and the four dev/prod differences that are allowed
metadata:
  type: project
---

**Decided 2026-09-07. Nothing implemented yet — no compose change, no ADR, no IaC.** The target shape is
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
