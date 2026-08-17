---
name: phase31-shape-b-consolidation
description: Shapes A/C retired (implemented 2026-08-17) — Shape A owned two live production browser paths that had to migrate to Shape B first, or deletion would have degraded silently
metadata:
  type: project
---

**Implemented 2026-08-17.** All 8 sub-phases (31.1–31.8) landed: Shape A's notify/list responsibilities migrated into Shape B first (31.1), Shape A deleted (31.2), Shape C deleted (31.3), identifiers renamed to neutral terms — `RegisterShips`, `queries.Ships`, `ships` KV bucket, `ship-projector` durable (31.4), frontend collapsed to a single-shape panel (31.5), business rules confirmed already accurate (31.6, they were pre-written to the target state), docs/diagrams updated including this file (31.7), findings note pending (31.8). `ginkgo ./...` and both frontend test suites green throughout. See `obsidian/POC-Dictionaries/` for the "why Shape B won" findings note once 31.8 lands, and `BUSINESS_RULES-SHIPPING.md` BR-038 for the new ship-list read-path rule.

Phase 31 (Main-POC-Plan.md) retired Shape A (KV-as-read-model) and Shape C (event-sourced reconstruction), keeping only Shape B (Postgres projection + KV write-through cache) — the POC's founding three-way comparison is answered, B won.

**The trap, confirmed by an Explore agent 2026-08-17 — this is not a pure deletion.** Shape A owns two things Sea Freight Flow actually depends on in production, not just the demo comparison panel:
1. `publishNotify`/`publishRawNotify` for `entity="ship"` fires **only** from `RegisterShapeA` (`eventhandler/handler.go`) — `RegisterShapeB` explicitly does not publish. This is the sole source of live fleet updates on the frontend.
2. `queries.ShapeA.ListShips` (reads the `dict-a` KV bucket) backs `api.*.shipping.ship.list.v1`, Sea Freight Flow's bootstrap/reconnect query.

Deleting Shape A before migrating these produces no error — ship rows just go stale, and the fleet panel is empty on connect. Sub-phase 31.1 (migrate both into the Shape B projector) must land and go green *before* 31.2 deletes Shape A's code.

**`container` and `meta` KV buckets are NOT Shape-A-only** despite looking adjacent (their handlers cross-reference `RegisterShapeA`'s nil-safe-`nc` convention in doc comments only) — `RegisterContainers` upserts Postgres first then KV, structurally the Shape B pattern; `ARCHITECTURE.md` classifies both with no shape letter. They survive Phase 31 untouched.

**Confirmed decisions (2026-08-17, via AskUserQuestion):**
- Identifiers renamed to neutral terms: `RegisterShapeB`→`RegisterShips`, `queries.ShapeB`→`queries.Ships`, KV bucket `dict-b`→`ships`. The bucket rename is a data migration (KV bucket names are immutable) — accepted as a `docker compose down -v` reset, not a dual-read path, since KV buckets are rebuildable projections by definition. The REST route `/api/shape-b/ships/...` is deliberately NOT renamed in Phase 31 — Phase 33 reclassifies/deletes it anyway, renaming twice is churn.
- Admin UI keeps a single-shape panel (delete `ShapeCPanel.vue`, `ShapePanel.vue` drops its shape prop, nav badge 3→1) rather than deleting the view or folding into Overview.
- `ARCHITECTURE.md`'s "frozen once assigned" variant ids (`Proj.KV`, `Read.FR.AGG`, `Read.KV`, the A/B/C alias map) are deleted outright, not tombstoned — the "we evaluated three and chose one" record belongs in the `obsidian/POC-Dictionaries/` narrative vault instead.
- Phases 100/103/104 (which reason from A/B/C tradeoffs — 104/snapshotting exists because Shape C degrades with stream depth) are flagged with a note in Phase 31, not re-scoped — that's explicit follow-up work.

**Business rules touched:** BR-024 rewritten (its "Shape B does not also publish" clause inverted once Shape A's projector is the only one — restated as a direct one-publisher-per-event invariant), BR-020/BR-019 amended (stale `dict-a-{context}` bucket naming, three-way→two-way mechanism choice), new BR-038 (ship list reads Postgres, KV is per-entity cache only, never enumerated for a list).

**Resolved 2026-08-17 — `system-architecture.png` deleted, not regenerated.** Its generator turned out to be recoverable after all (`shared/unifi-theme/render-diagram.sh` + a source SVG recoverable from git history at commit `4ae9f4f`), but that source SVG was itself already stale before Phase 31 — a later commit (`575f1b7`) deliberately deleted it as a "stale architecture SVG export" showing retired SSE, and left the two PNG copies (`demos/01-dictionary/` + `obsidian/POC-Dictionaries/Architecture/`) behind as orphans. Neither PNG was embedded/linked from any live doc (grepped for markdown image refs — only this memory file and the plan mentioned the filename). Given it needed two rounds of staleness fixed (pre-Phase-23 SSE *and* Phase 31 shapes) and nothing displays it today, both orphaned PNGs were deleted rather than regenerated — completing the cleanup commit `575f1b7` started. If a system-architecture diagram is wanted again, redraw it fresh against the current architecture rather than resurrecting the recovered SVG.

`obsidian/POC-Dictionaries/4. Findings - Distributed Tracing (Phase 28).md`'s central thesis is "why the trace store is Shape A" — needs an authorial rewrite pass, not find-and-replace. Not yet actioned.
