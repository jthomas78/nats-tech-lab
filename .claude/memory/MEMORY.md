# Memory Index

- [Project plan location](project_plan_location.md) — Plans live in `.claude/plans/`, not repo root
- [Dev machine toolchain](dev_machine_toolchain.md) — Linux box has no Docker; Mac (Homebrew/Volta) does — check before assuming
- [Shipping domain overview](shipping_domain_overview.md) — Ship/Container aggregates on `SHIPPING` stream; hydrate/read-modify-write/422 conventions
- [NATS volume legacy messages](nats_volume_legacy_messages.md) — stale-subject Nak loop after a domain rename; fix: `docker compose down -v`
- [Container status model](container_status_model.md) — only `in-terminal`/`on-ship` exist; derive UI splits from `destPort` client-side
- [frontend-port structure](frontend_port_structure.md) — now at `frontend/seafreight-app/`; Fleet/Port view split, refdata i18n, Vitest gotchas
- [Stale Select value bug pattern](stale_select_value_bug_pattern.md) — PrimeVue `Select` v-model doesn't auto-clear on option-list change
- [swag regen diff noise](swag_regen_diff_noise.md) — `swag init` rewrites all `$ref` names repo-wide; hand-patch docs instead
- [BR classification heuristic](br_classification_heuristic.md) — check `commands/*.go` for precedent before asking BR vs input-validation
- [Event sourcing source-of-truth patterns](event_sourcing_source_of_truth_patterns.md) — Postgres+outbox vs JetStream-as-truth; this POC uses B
- [UI bug triage: trust framing](ui_bug_triage_trust_framing.md) — user names "the UI" as broken → investigate frontend first
- [PrimeVue RadioButtonGroup for shared state](primevue_radiobutton_group_for_shared_state.md) — standalone RadioButtons in v-for need grouping
- [Locale switch race condition](locale_switch_race_condition.md) — overlapping fetches resolve out of order; fixed with a request-token guard
- [Verify before resuming offloaded work](verify_before_resuming_offloaded_work.md) — check git log before trusting a resumed summary's "still open" claims
