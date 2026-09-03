# Memory Index

One-line hooks. Open a file only when its hook looks relevant to the task.

## Conventions / gotchas (evergreen)
- [project_plan_location](project_plan_location.md) — plans live in `.claude/plans/`, not repo root
- [dev_machine_toolchain](dev_machine_toolchain.md) — Linux box has no Docker; Mac does — check before assuming
- [br_classification_heuristic](br_classification_heuristic.md) — check `commands/*.go` for precedent before asking BR vs input-validation
- [design_discussion_vs_implementation_signal](design_discussion_vs_implementation_signal.md) — user iterates/reverts ideas before "let's plan" — don't implement early
- [verify_before_resuming_offloaded_work](verify_before_resuming_offloaded_work.md) — check git log before trusting a resumed summary
- [ui_bug_triage_trust_framing](ui_bug_triage_trust_framing.md) — user says "the UI" is broken → check frontend first
- [admin_ui_design_viewport](admin_ui_design_viewport.md) — UIs target 1920x1080; verify at that width
- [swag_regen_diff_noise](swag_regen_diff_noise.md) — `swag init` rewrites all `$ref` repo-wide; hand-patch instead

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
- [refdata_database_per_service](refdata_database_per_service.md) — refdata-service on its own Postgres (port 5433)
- [refdata_cross_tenant_stream_import](refdata_cross_tenant_stream_import.md) — open bug: tenants import `evt.*.refdata.*.changed` unbounded, see each other's metadata
- [v3_tenancy_axes_decision](v3_tenancy_axes_decision.md) — tenant = marketplace-operating business, not region; 5 axes
- [phase16_tenancy_taxonomy](phase16_tenancy_taxonomy.md) — 13-point record; 16a–16f DONE; gap: refdata reads don't track own tenant
- [tenant_service_separation_decision](tenant_service_separation_decision.md) — accounts-service is its own service/DB; Admin UI merges both
- [project-ports-tenant-scoping](project-ports-tenant-scoping.md) — pending: ports/refdata should scope to tenant not BU; hack uses `_default_bu`

## Reference material
- [aws_console_as_shell_app](aws_console_as_shell_app.md) — AWS Console as app-shell mental model; documented MFE discovery pattern + where our contribution points go further

## Linebooker / V3 domain modelling
- [proposed_linebooker_v3_architecture_levels](proposed_linebooker_v3_architecture_levels.md) — reference discussion: proposed L1-L4 diagram hierarchy, participant/external-system taxonomy, and retirement of old V2/V3 diagram labels; not implementation authorization
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
