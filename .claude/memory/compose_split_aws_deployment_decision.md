---
name: compose-split-aws-deployment-decision
description: Decision 2026-09-07 — split docker-compose into 3 files x 3 projects (control plane + 2 regions) and prove multi-region on the laptop BEFORE any AWS work; records the AWS work list and the four dev/prod differences that are allowed
metadata:
  type: project
---

**Decided 2026-09-07. Nothing implemented yet — no compose change, no ADR, no IaC.** The target shape is
`demos/01-dictionary/diagrams/multi-cluster-and-region/multi-region-control-plane-topology-3.html`
(revision 3: global band = platform control services + trust material + management backbone; below it, N regional cells).

**The split — files and projects are two different axes.**

- Files = *when you ship it*: `compose.infra.yml` (nats, postgres — has data), `compose.control.yml` (the provisioner — exactly one exists), `compose.runtime.yml` (accounts, refdata, business services — no data). A fourth `compose.yml` is `include:`-only, for one-command local runs.
- Projects = *where it runs*: `-p lb-cp` (global control plane, once), `-p lb-eu`, `-p lb-us`. **EU and US run the same two files; only the env file differs.** That is the whole trick.

**Chosen sequence: prove it locally first, then AWS.** Rejected alternative was going straight at one AWS region, which means fighting NATS storage, secrets and IaC simultaneously with no proof the split is correct. Local first is ~one session and forces the migration-job and variable-port work that AWS needs anyway. **Write the ADR during the split, not after.**

**Hard blocker on a second local region:** `demos/01-dictionary/docker-compose.yml` hard-codes every host port (`4222`, `8222`, `9222`, `5432`, `72xx`, `71xx`). Two regions cannot both bind `4222`. Every host port becomes `${VAR:-default}` with `.env.eu` / `.env.us` supplying the offsets — see [[frontend_port_structure]] and [[project-ports-tenant-scoping]] for the existing port allocation rules.

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
