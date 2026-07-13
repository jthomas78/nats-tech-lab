# Memory Index

- [Project plan location](project_plan_location.md) — Plan .md files live in `.claude/plans/` inside the repo, not at the root
- [Dev machine toolchain](dev_machine_toolchain.md) — two machines: Linux (~/.local, no Docker) and Mac (Homebrew/Volta, Docker installed)
- [Shipping domain overview](shipping_domain_overview.md) — current state (Phase 8): Ship + Container aggregates on `SHIPPING` stream, BR-001..BR-016, plus the hydrate/read-modify-write/422 architecture decisions
- [NATS volume legacy messages](nats_volume_legacy_messages.md) — stale-subject messages in nats-data volume cause projector Nak loop; fix: `docker compose down -v`
- [Container status model](container_status_model.md) — only `in-terminal`/`on-ship` exist; no "delivered" status — derive UI splits from `destPort` client-side
- [frontend-port structure](frontend_port_structure.md) — "Ship Management" UI: fleet-wide FleetPanel + port-scoped group; operations localized per panel
- [Stale Select value bug pattern](stale_select_value_bug_pattern.md) — PrimeVue Select v-model doesn't auto-clear when options change; must `watch` and reset explicitly
- [swag regen diff noise](swag_regen_diff_noise.md) — `swag init` rewrites all $ref names repo-wide; hand-patch doc strings instead of regenerating
- [BR classification heuristic](br_classification_heuristic.md) — check `commands/*.go` for precedent before asking whether a check is a formal BR or input validation
- [Event sourcing source-of-truth patterns](event_sourcing_source_of_truth_patterns.md) — Pattern A (Postgres+outbox) vs Pattern B (JetStream-as-truth); B answers the POC question; pending Obsidian note append
