---
name: phase31-shape-b-consolidation
description: Retiring Shapes A/C — Shape A owns two live production browser paths that must migrate to Shape B first, or deletion degrades silently
metadata:
  type: project
---

Phase 31 (Main-POC-Plan.md) retires Shape A (KV-as-read-model) and Shape C (event-sourced reconstruction), keeping only Shape B (Postgres projection + KV write-through cache) — the POC's founding three-way comparison is answered, B won.

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

Also surfaced, not yet actioned: `demos/01-dictionary/system-architecture.png` has no generator/source found in-repo and is already stale (shows retired SSE) — locate its source before Phase 31's diagram regen step, or decide to drop it. `obsidian/POC-Dictionaries/4. Findings - Distributed Tracing (Phase 28).md`'s central thesis is "why the trace store is Shape A" — needs an authorial rewrite pass, not find-and-replace.
