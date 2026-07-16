# Memory Index

- [Project plan location](project_plan_location.md) — Plan .md files live in `.claude/plans/` inside the repo, not at the root
- [Dev machine toolchain](dev_machine_toolchain.md) — two machines: Linux (~/.local, no Docker) and Mac (Homebrew/Volta, Docker installed)
- [Shipping domain overview](shipping_domain_overview.md) — Ship + Container aggregates on `SHIPPING` stream, BR-001..BR-016, plus the hydrate/read-modify-write/422 architecture decisions
- [NATS volume legacy messages](nats_volume_legacy_messages.md) — stale-subject messages in nats-data volume cause projector Nak loop; fix: `docker compose down -v`
- [Container status model](container_status_model.md) — only `in-terminal`/`on-ship` exist; no "delivered" status — derive UI splits from `destPort` client-side
- [frontend-port structure](frontend_port_structure.md) — activity-bar Fleet/Port view split, refdata-driven i18n (BR-D16), Vitest harness gotchas, store conventions
- [Stale Select value bug pattern](stale_select_value_bug_pattern.md) — PrimeVue Select v-model doesn't auto-clear when options change; must `watch` and reset explicitly
- [swag regen diff noise](swag_regen_diff_noise.md) — `swag init` rewrites all $ref names repo-wide; hand-patch doc strings instead of regenerating
- [BR classification heuristic](br_classification_heuristic.md) — check `commands/*.go` for precedent before asking whether a check is a formal BR or input validation
- [Event sourcing source-of-truth patterns](event_sourcing_source_of_truth_patterns.md) — Pattern A (Postgres+outbox) vs Pattern B (JetStream-as-truth); B answers the POC question
- [UI bug triage: trust framing](ui_bug_triage_trust_framing.md) — when user says "the UI" is wrong, investigate frontend first, don't default to backend audits
- [PrimeVue RadioButtonGroup for shared state](primevue_radiobutton_group_for_shared_state.md) — standalone RadioButtons in a v-for each keep local state; wrap in RadioButtonGroup
- [Locale switch race condition](locale_switch_race_condition.md) — overlapping locale-switch fetches can resolve out of order; fixed with a request-token guard, not a BR-D rule
