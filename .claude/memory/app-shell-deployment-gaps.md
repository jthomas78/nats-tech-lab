---
name: app-shell-deployment-gaps
description: Green suites do not prove a Docker stack works — the three deployment gaps that keep recurring in this repo
metadata:
  type: feedback
---

Three failure shapes have now bitten twice each in this repo. All three pass every unit suite.

1. **A new shared Go module needs a COPY line in every consumer's Dockerfile.** `go.work` and the
   `replace` directives make it build locally and fail in Docker. Cost 2026-09-01: the official
   `docker compose up --build` for `refdata-service` failed outright.
2. **A grant change in `nats/bootstrap-operator.sh` is only live after `./bootstrap-operator.sh
   --force`** plus `docker compose down -v && up --build`. The script short-circuits on an existing
   `operator.jwt`, so checked-in `.creds`/JWT artifacts silently keep their old grants and the stack
   logs `Permissions Violation` while every spec stays green. Already written down as BR-AC34's
   general lesson; it recurred anyway on Phase 5's health grants.
3. **A grant that no live run ever exercised may simply be absent.** Regenerating credentials for
   reason 2 revealed that `rpc._platform.registry.entries.unregister.v1` had never been in the
   registry's `--allow-sub` — Phase 5b's withdrawal transport could not have worked in Docker,
   despite passing against an in-process broker.

**Why:** a unit spec asserts what the code does. All three of these are questions about what the
deployment carries, and nothing in the test pyramid asks them.

**How to apply:** when a phase adds a shared module, a NATS grant, or a new subject, run the live
stack before calling it done — and treat "all suites green" as saying nothing at all about these
three. See [[phase5_lifecycle_health_plan]].
