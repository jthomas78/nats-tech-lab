# Memory Index

One-line hooks. Open a file only when its hook looks relevant to the task.

## Conventions / gotchas (evergreen)
- [project_plan_location](project_plan_location.md) — plans live in `.claude/plans/`, not repo root
- [dev_machine_toolchain](dev_machine_toolchain.md) — Linux box has no Docker; Mac does — check before assuming
- [br_classification_heuristic](br_classification_heuristic.md) — check `commands/*.go` for precedent before asking BR vs input-validation
- [design_discussion_vs_implementation_signal](design_discussion_vs_implementation_signal.md) — user iterates/reverts ideas before "let's plan" — don't implement early
- [demo_context_isolation](demo_context_isolation.md) — every demo is an isolated task/AI context; current CQRS diagram discussion belongs to demo 03
- [demo_playbook_session_2026-09-17](demo_playbook_session_2026-09-17.md) — 2026-09-17: `demo-playbook.html` is the ONE demo lifecycle (stages 01-04, Learn is `04`); demo 03 retro-fit still open; `development-playbook.*` still untracked
- [verify_before_resuming_offloaded_work](verify_before_resuming_offloaded_work.md) — check git log before trusting a resumed summary
- [ui_bug_triage_trust_framing](ui_bug_triage_trust_framing.md) — user says "the UI" is broken → check frontend first
- [admin_ui_design_viewport](admin_ui_design_viewport.md) — UIs target 1920x1080; verify at that width
- [swag_regen_diff_noise](swag_regen_diff_noise.md) — `swag init` rewrites all `$ref` repo-wide; hand-patch instead
- [codex_must_run_gpt_5_6_terra_high](codex_must_run_gpt_5_6_terra_high.md) — Codex is the DEFAULT worker, pinned to `gpt-5.6-terra` + effort `high`; Claude is the fallback, and the user naming Claude is the override

## Frontend gotchas
- [phase5_lifecycle_health_plan](phase5_lifecycle_health_plan.md) — Phase 5 lifecycle/withdrawal/health: COMPLETE and live-verified 2026-09-01
- [phase8c_manifest_drift](phase8c_manifest_drift.md) — checker done; preload-only, mapped service origins, memory-only observations, independent Manifest column
- [phase8_federated_catalog](phase8_federated_catalog.md) — 8f/d/e done; five plugin origins, frozen activation API, native fallback, remote CSS/theme singleton
- [phase4_shell_nats_transport](phase4_shell_nats_transport.md) — shell registry NATS-only, first-paint/reconnect rules; shipping notifycoverage baseline caveat
- [stale_select_value_bug_pattern](stale_select_value_bug_pattern.md) — PrimeVue `Select` v-model doesn't auto-clear on option-list change
- [primevue_radiobutton_group_for_shared_state](primevue_radiobutton_group_for_shared_state.md) — standalone RadioButtons in v-for need grouping
- [locale_switch_race_condition](locale_switch_race_condition.md) — overlapping fetches resolve out of order; fix with a request-token guard
- [vue_todisplaystring_array_gotcha](vue_todisplaystring_array_gotcha.md) — `{{ }}` JSON-stringifies arrays; join NATS header values first
- [frontend_port_structure](frontend_port_structure.md) — `frontend/seafreight-app/`; Fleet/Port split, refdata l10n, Vitest gotchas
- [admin_stat_card_one_ratio_rule](admin_stat_card_one_ratio_rule.md) — one `value / max` + bar per card, one 20px value size per row
- [mockup_fidelity_functional_capability](mockup_fidelity_functional_capability.md) — design-gate mockups must show real create/edit affordances vs the running app

## Architecture / NATS
- [shipping_domain_overview](shipping_domain_overview.md) — Ship/Container on SHIPPING stream; `{context}`=business-unit; both UUID-keyed
- [container_status_model](container_status_model.md) — only `in-terminal`/`on-ship`; derive UI splits from `destPort` client-side
- [event_sourcing_source_of_truth_patterns](event_sourcing_source_of_truth_patterns.md) — Postgres+outbox vs JetStream-as-truth; POC uses B
- [nats_volume_legacy_messages](nats_volume_legacy_messages.md) — stale-subject Nak loop after a rename; fix `docker compose down -v`
- [nats_account_is_the_only_authn](nats_account_is_the_only_authn.md) — nothing verifies a JWT; new HTTP ingress needs a capability ticket minted over NATS
- [nats_sys_claims_subjects](nats_sys_claims_subjects.md) — `$SYS.REQ.CLAIMS` is core request-reply (not JetStream) for JWT resolver mgmt in operator mode
- [nats_scoped_signing_keys](nats_scoped_signing_keys.md) — server enforces the key's permission template, discards user JWT's own
- [nats_tower_operator_mode_tradeoff](nats_tower_operator_mode_tradeoff.md) — server is operator mode; Tower→sys.creds via its UI not done
- [connz_limit_is_page_size_not_capacity](connz_limit_is_page_size_not_capacity.md) — `/connz` limit 1024 is page size; ceiling is `/varz` max_connections
- [refdata_database_per_service](refdata_database_per_service.md) — ONE Postgres instance for all services (ADR-052); db+role per service, port 5432 only
- [refdata_cross_tenant_stream_import](refdata_cross_tenant_stream_import.md) — open bug: tenants import `evt.*.refdata.*.changed` unbounded, see each other's metadata
- [v3_tenancy_axes_decision](v3_tenancy_axes_decision.md) — tenant = marketplace-operating business, not region; 5 axes; narrowed 2026-09-08: today's tenants are one-per-region, but that is not an invariant
- [phase16_tenancy_taxonomy](phase16_tenancy_taxonomy.md) — 13-point record; 16a–16f DONE; gap: refdata reads don't track own tenant
- [tenant_service_separation_decision](tenant_service_separation_decision.md) — accounts-service is its own service/DB; Admin UI merges both
- [project-ports-tenant-scoping](project-ports-tenant-scoping.md) — pending: ports/refdata should scope to tenant not BU; hack uses `_default_bu`
- [odometer_is_demo_02s_only_cqrs_example](odometer_is_demo_02s_only_cqrs_example.md) — **[demo 02]** 2026-09-09, CORRECTED 2026-09-11: demo 02's ONE CQRS example; KV-only, no `{context}` in the subject (on purpose); a shared account gives BOTH regions the same 12.5 km out of ONE bucket in cluster `za` (not 25/50 — that was a durable-consumer replay bug); split accounts give 12.5 / 0, each local
- [multi_region_lives_in_demo_02](multi_region_lives_in_demo_02.md) — **[demo 02]** 2026-09-09: clustering/gateways/domains REMOVED from demo 01 (one server, project `poc`); demo 02 = 2 projects lb-za-1 + lb-au-1 on lb-za/lb-au/lb-wan; no hub yet
- [compose_split_aws_deployment_decision](compose_split_aws_deployment_decision.md) — ZA cell project is `-p poc` (renamed 2026-09-09, NOT lb-za-1); cell split DONE 2026-09-08 (ADR-055), global band not written: deploy/cell tier files x per-cell env files, prove multi-region locally before AWS, Compose local + Helm/EKS production; AWS work list
- [local_mesh_replication_is_the_goal](local_mesh_replication_is_the_goal.md) — **[demo 02]** GOAL: run the full hub+za+au mesh locally, not in cloud; nats.conf has no cluster/gateway block yet

## Demo 03 — multi-cluster and accounts (stage 03 complete 2026-09-17)
- [demo03_state_and_handover](demo03_state_and_handover.md) — **[demo 03] START HERE** — seven shapes measured, 73 checks green, stage 04 pattern cards NOT started; reports are generated, `figures.html` is the one hand-drawn file, the `t-` prefix must not be tidied away
- [demo03_both_edges_of_a_failover_lie](demo03_both_edges_of_a_failover_lie.md) — **[demo 03]** `/jsz` names a dead leader for up to 54s after, and a new leader seconds BEFORE it accepts a change; retry and measure, never `sleep`
- [demo03_mirror_over_gateway_vs_leaf](demo03_mirror_over_gateway_vs_leaf.md) — **[demo 03]** over a gateway a mirror needs no `external.api`; over a leaf link, omitting it copies nothing, or silently copies the wrong local stream
- [demo03_export_import_is_the_safe_sharing](demo03_export_import_is_the_safe_sharing.md) — **[demo 03]** account export/import is one-way, renamed and subject-only; the opposite of T5's silent double capture
- [demo03_three_node_arbiter_buys_one_node_of_slack](demo03_three_node_arbiter_buys_one_node_of_slack.md) — **[demo 03]** T4 (9 peers, majority 5) survives a region plus one node; T3 (7 peers, majority 4) has no slack

## Reference material
- [aws_console_as_shell_app](aws_console_as_shell_app.md) — AWS Console as app-shell mental model; documented MFE discovery pattern + where our contribution points go further

## Linebooker / V3 domain modelling
- [proposed_linebooker_v3_architecture_levels](proposed_linebooker_v3_architecture_levels.md) — current L0-L2 inventory and proposed topology context; canonical hierarchy/catalogue lives in the architecture authority file
- [refdata_v3_ladder_placement](refdata_v3_ladder_placement.md) — refdata's agreed V3 home: data jobs + theme storage in L3-07, shell theme consumption in L3-03, whole story as an L4
- [linebooker_platform_vs_tenant_service_split](linebooker_platform_vs_tenant_service_split.md) — Refdata+Accounts/Auth platform; Marketplace/Payments tenant-scoped
- [linebooker_platform_marketplace_tenant_diagram](linebooker_platform_marketplace_tenant_diagram.md) — Marketplace under PLATFORM; Trips per-tenant; 2 UIs per tenant
- [linebooker_refdata_layering_model](linebooker_refdata_layering_model.md) — platform/tenant/org 3-layer; flags snapshot-onto-history gap
- [linebooker_v2_refdata_candidates](linebooker_v2_refdata_candidates.md) — enum+table duplicates tier 1; versioning+l10n net-new
- [linebooker_business_type_vs_entity_type](linebooker_business_type_vs_entity_type.md) — "Company" is legal structure; roles Customer/Transporter/Operator/Integrator
- [linebooker_shipper_vs_customer_naming](linebooker_shipper_vs_customer_naming.md) — "Shipper" is the V3 term, pairs with "Transporter"
- [linebooker_trading_partners_term_and_fleet_cardinality](linebooker_trading_partners_term_and_fleet_cardinality.md) — "Trading partners"; Transporter→truck one-to-many via FleetAssetEntity
- [linebooker_trading_partner_phase_v1_scope](linebooker_trading_partner_phase_v1_scope.md) — Phase 26 IMPLEMENTED e2e (organizations-service + Admin UI); BR-TP01-14
- [linebooker_registration_ui_placement](linebooker_registration_ui_placement.md) — Registration → Admin UI "Trading partners", not RefData UI
- [linebooker_bid_tender_allocation_rules](linebooker_bid_tender_allocation_rules.md) — Bid/Tender unconnected tracks; lowest-bid wins at expiry
- [linebooker_transport_execution_phase_naming](linebooker_transport_execution_phase_naming.md) — 4 stages: dispatch→collection→in-transit→delivery
- [linebooker_payments_settlement_phase](linebooker_payments_settlement_phase.md) — PaymentEntity, InvoiceSplitType, EarlySettlementRequest (factoring)
- [linebooker_truck_types_open_work](linebooker_truck_types_open_work.md) — truck/vehicle types are only a one-off preview seeder (114 rows, 4-deep hierarchy), not wired into refdata `Seed()`; open work flagged 2026-09-08

## Phase history (completed — consult for background only)
- [phase8_registry_preload_announce](phase8_registry_preload_announce.md) — preload/announce wiring done; fail-closed publisher; staged catalog; legacy lifecycle edge
- [phase17_request_reply_panel](phase17_request_reply_panel.md) — DONE; admin frontend has no Vitest infra
- [phase18_requestor_responder_headers](phase18_requestor_responder_headers.md) — DONE; fixed micro.Config.Name vs nats.Name mismatch
- [phase21_account_exports_imports](phase21_account_exports_imports.md) — DONE 2026-08-03; PLATFORM/tenant two-account partitioning via NATS exports/imports
- [admin_ui_realtime_transport_options](admin_ui_realtime_transport_options.md) — Admin uses one PLATFORM WebSocket; Phase 23 tenant conn retired
- [accounts_service_plan](accounts_service_plan.md) — Phase 14 dynamic provisioning; open gap: unrestricted service creds
- [phase25i_diesel_overlay](phase25i_diesel_overlay.md) — DONE; fixed BR-P24 zero-baseline + DatePicker UTC shift; 25j not started
- [phase28_trace_detail_request_response_split](phase28_trace_detail_request_response_split.md) — DONE through 28q; KV bucket `trace-request-reply`; waterfall walks parentSpanId tree
- [rest_nats_transport_consolidation](rest_nats_transport_consolidation.md) — Phases 31-34: business comms NATS-only, REST for admin/health; `.v1` stays
- [phase31_shape_b_consolidation](phase31_shape_b_consolidation.md) — DONE 2026-08-17; Shapes A/C retired; `queries.Ships`/`ships` bucket/`ship-projector`
- [phase32_refdata_platform_credential](phase32_refdata_platform_credential.md) — frontend/refdata cross-tenant; own MintRefdataAdminToken + MountPlatformAPI
- [phase33_refdata_admin_rest_exemption](phase33_refdata_admin_rest_exemption.md) — `/api/refdata/admin/*` stays REST; accounts-service calls it server-to-server
- [phase34_boundary_enforcement](phase34_boundary_enforcement.md) — DONE 2026-08-17; mux allowlist (BR-040) + traceSpan.Requester (BR-041) + 2-axis filter
- [phase35_shared_go_package_extraction](phase35_shared_go_package_extraction.md) — DONE 2026-08-18; shared/natstenants, natstrace, browserrpc; go.work
- [tenants_manager_triplication](tenants_manager_triplication.md) — RESOLVED Phase 35; historical only
- [phase36_tech_lab_operator_rebrand](phase36_tech_lab_operator_rebrand.md) — 36.1+36.2 DONE 2026-08-19; refdata → "Tech Lab Operator" + Trading Partners migrated
- [phase63_nats_hop_tracing_renumbered](phase63_nats_hop_tracing_renumbered.md) — NATS 2.11 Server-Hop Tracing is Phase 63, DEFERRED
- [phase38b_transporter_vetting](phase38b_transporter_vetting.md) — Temporal two-branch saga, attempt-keyed dedup, fleet gate; BR-TP21-28
- [phase38di_transporter_ui](phase38di_transporter_ui.md) — panel + drill-in tabs; branch on error-envelope flags not prose; dev gaps
- [phase38_document_object_store](phase38_document_object_store.md) — OBJ bucket = stream sharing tenant 1 GiB; blob-before-record, write-once; nginx 1 MiB default
- [phase38e_organizations_rename](phase38e_organizations_rename.md) — `trading-partner-service`→`organizations-service`; "trading partner" stays as vocab, BR-TP* keep numbers
- [accounts_overview_pulse_design](accounts_overview_pulse_design.md) — DONE Phase 45; ring buffer + duration selector (BR-043) + gated search (BR-044)
- [app-shell-deployment-gaps](app-shell-deployment-gaps.md) — green suites prove nothing about Dockerfile COPYs, NATS grants, or creds regeneration
- [jetstream_domain_per_cluster_is_mandatory](jetstream_domain_per_cluster_is_mandatory.md) — **[demo 02] RETRACTED fix** — a gateway makes one supercluster and `10058` is real, but a per-cluster `domain` does NOT split it; use ONE domain, and a second account for a second namespace
- [hub_means_one_nats_cluster_not_the_control_plane](hub_means_one_nats_cluster_not_the_control_plane.md) — **[demo 02]** `hub` is used two ways in this repo; in the plan and drawing it is one transport tile, not the control-plane band
- [gateway_double_capture_and_option3](gateway_double_capture_and_option3.md) — **[demo 02]** RESOLVED, and 2026-09-11 the "double capture" was DISPROVED: over a gateway one account holds ONE stream (10058), so nothing is stored twice; option 3 = one account per tenant = what we already do; options 1 and 2 dropped
- [cross_region_load_handoff](cross_region_load_handoff.md) — **[demo 02]** a load crossing regions is two loads + one handoff (integration, not replication); origin owns journey completion via a per-dropoff POD checklist
- [Demo 02 uses the host nats/nsc CLI](demo02_host_cli_and_lab2_contexts.md) — **[demo 02]** no toolbox container; every context name starts `lab2-`
- [One supercluster takes ONE JetStream domain](demo02_jetstream_domains_do_not_split_a_supercluster.md) — **[demo 02]** per-region domains silently broke JetStream; now `lb` everywhere, regions split by placement + accounts
- [A WAN cut freezes JetStream management only](demo02_wan_cut_freezes_jetstream_management_only.md) — **[demo 02]** lost meta majority = no stream create/delete; core NATS and existing streams keep running. Never fake a cut with `docker network disconnect`
