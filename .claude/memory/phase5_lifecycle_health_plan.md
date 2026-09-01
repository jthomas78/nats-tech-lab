# Phase 5 lifecycle and health — planning approved 2026-08-31

User completed 14 questions (all A except Q8 B), then explicitly approved updating the plan,
business rules and required test cases **without implementation**. Phase 5 remains unimplemented
except backend lifecycle storage inherited from Phase 8. Do not mistake documentation approval for
permission to begin code or executable specs.

Source of truth: `.claude/plans/Application-Shell-Microfrontend-Plan.md` Phase 5 (59–65 refined,
Q1–Q14 recorded) and `demos/01-dictionary/BUSINESS_RULES-APP-SHELL.md` BR-AS52–65 + planned matrix.
Old draft BR-AS30–34 labels were superseded; canonical Phase 4 BR-AS30/31 remain unchanged.

Decisions: explicit static/dynamic; backfill legacy static; class edits after reload. Static disable
offers reload; dynamic operator disable AND signed publisher unregister withdraw live. Unregister preserves
approval separately from operator enablement; return never overrides operator disable/trust. Retain
modules, restore unchanged cached contributions without reactivation; changed definitions reload.
Occupied URL tombstones in place. Slot-owner withdrawal suspends placements, not other plugins.
Security revocation always overrides, and outages/degraded reads never ordinary-withdraw.

Health: frontend/backend separate, both classes, registry frontend `/healthz` via allowlisted mapped
origins (explicit BR-AS45 extension), backend bounded NATS readiness, deployment plugin→service map;
missing means not configured, empty means frontend-only. Every 5s, timeout 2s, fail after 2, recover
after 1; stale after 15s without fresh observation with last-check time. Decoration only; separate
health snapshots/hints, never catalogue revision/curation/drift. No full scope disposal.

Architecture has an explicitly planned section; narrative decisions note is not as-built findings.
5a–5d and implementation evidence in 5e are unchecked. No Phase 5 executable tests added/run by this
planning task. Future implementation must derive executable specs before code and run real suites.
