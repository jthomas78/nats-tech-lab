# Refdata Service — Versioning, Tenancy & Template Inheritance

**Status:** PROPOSED — awaiting approval
**Date:** 2026-07-22
**Depends on:** Phase 11 (Dictionary as a Service) — delivered

---

## 1. Problem Statement

The refdata service currently serves reference data scoped by a single hardcoded context
(`emea-acme`). For a multi-tenant logistics platform it needs to:

1. **Multi-tenancy** — serve core/common app data to all tenant apps from a central service.
2. **Corpus-level versioning** — snapshot the entire refdata corpus (all types, items,
   localizations, references) as an immutable version, not per-type.
3. **Draft/publish lifecycle** — edits accumulate in a draft; publishing promotes the draft
   atomically to become the new default; first-class rollback with audit trail.
4. **Template inheritance** — define reusable base templates; tenants inherit from a template
   (or from another tenant) with the ability to override values and add new entries; multi-level
   hierarchy where changes propagate through the entire chain unless an intermediate override
   breaks propagation.
5. **Version pinning** — consuming services pin to a specific published corpus version; old and
   new versions coexist indefinitely.
6. **Hybrid KV materialization** — eagerly materialize on publish; TTL governs KV lifespan;
   expired entries are lazily re-materialized on demand (rewrite-on-read to reset TTL).

---

## 2. Context Hierarchy (Template Inheritance)

### 2.1 Data Model

A **context** represents a node in the inheritance tree. The root is a base template with no
parent; tenants are leaf (or intermediate) nodes.

```
refdata.contexts
  context       TEXT PK        -- e.g. "global", "emea", "emea-acme"
  parent        TEXT FK → contexts(context)  -- NULL for root
  name          TEXT NOT NULL
  description   TEXT NOT NULL DEFAULT ''
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
```

Example hierarchy:

```
global                      (base template — standards, shared enums)
├── emea                    (regional override — EU-specific regulatory data)
│   ├── emea-acme           (tenant — Acme's overrides + additions)
│   └── emea-globex         (tenant — Globex's overrides + additions)
└── apac                    (regional override)
    └── apac-orient         (tenant)
```

### 2.2 Inheritance Resolution

An item's effective value for a context is resolved by walking up the ancestor chain (child →
parent → grandparent → root) and returning the **first match**:

```
resolve(context, type_key, code):
  for ctx in [context, parent(context), grandparent(context), ...root]:
    if item exists at (ctx, type_key, code):
      return item                -- override or original
  return nil                     -- not in corpus
```

- **Override** = an item at `(child_context, type_key, code)` that also exists at an ancestor.
  The child's value wins; propagation from above is broken at this level and below.
- **Addition** = an item at `(child_context, type_key, code)` with no ancestor match. Visible
  only at this level and below.
- **Inherited** = no item at this context; resolved from an ancestor. Propagation continues: if
  the ancestor's value changes (new corpus version), the child sees it — unless an intermediate
  node overrides it first.
- **No deletion of inherited entries** — a child context cannot remove an item that exists in a
  parent. It can only override the value or leave it inherited.

### 2.3 Propagation Rules

When a parent context publishes a new corpus version:

1. **New items in parent** — automatically visible to all descendants that don't override them.
   No action required at the child level.
2. **Changed items in parent** — descendants that inherit (no override) see the new value.
   Descendants that override are unaffected (propagation broken).
3. **The propagation-break is per-item, not per-type** — a child can override `currency.EUR`
   while still inheriting `currency.USD` from the same parent type.

### 2.4 Materialization of Flattened Views

At **publish time**, the corpus for a context is flattened: the inheritance chain is walked for
every `(type_key, code)` pair across all ancestor levels, producing the complete effective set.
This flattened snapshot is what gets stored as the published corpus version and materialized
into KV.

This means:
- Reads are always against the flattened snapshot — no runtime chain-walking.
- Publishing a parent requires **re-flattening and re-publishing** all descendant contexts
  that haven't overridden the changed items. This is an explicit downstream step, not
  automatic — an operator publishes the parent, then decides when each child should pick up
  the changes (via a new child draft that inherits the updated parent version).

---

## 3. Corpus Versioning & Lifecycle

### 3.1 Data Model

```
refdata.corpus_versions
  context       TEXT NOT NULL FK → contexts(context)
  version       INTEGER NOT NULL          -- monotonically increasing per context
  status        TEXT NOT NULL             -- 'draft', 'published', 'rolled-back'
  parent_version INTEGER                  -- NULL for first version; otherwise the version
                                          -- this draft was branched from
  base_context_version INTEGER            -- version of the parent context's corpus that
                                          -- this version inherits from (NULL if root)
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
  published_at  TIMESTAMPTZ               -- set on publish
  rolled_back_at TIMESTAMPTZ              -- set on rollback
  rolled_back_by INTEGER                  -- version that replaced this one on rollback
  notes         TEXT NOT NULL DEFAULT ''
  PK (context, version)
```

```
refdata.corpus_items
  context       TEXT NOT NULL
  version       INTEGER NOT NULL
  type_key      TEXT NOT NULL
  code          TEXT NOT NULL
  status        TEXT NOT NULL DEFAULT 'active'
  attrs         JSONB NOT NULL DEFAULT '{}'
  source_context TEXT NOT NULL             -- which context this item originates from
                                           -- (the context itself for overrides/additions,
                                           -- an ancestor for inherited items)
  is_override   BOOLEAN NOT NULL DEFAULT false
  PK (context, version, type_key, code)
  FK (context, version) → corpus_versions
```

```
refdata.corpus_localizations
  context       TEXT NOT NULL
  version       INTEGER NOT NULL
  type_key      TEXT NOT NULL
  code          TEXT NOT NULL
  locale        TEXT NOT NULL
  label         TEXT NOT NULL
  description   TEXT NOT NULL DEFAULT ''
  source        TEXT NOT NULL DEFAULT 'manual'
  source_context TEXT NOT NULL
  PK (context, version, type_key, code, locale)
  FK (context, version, type_key, code) → corpus_items
```

```
refdata.corpus_references
  context       TEXT NOT NULL
  version       INTEGER NOT NULL
  from_type_key TEXT NOT NULL
  from_code     TEXT NOT NULL
  relation      TEXT NOT NULL
  to_type_key   TEXT NOT NULL
  to_code       TEXT NOT NULL
  source_context TEXT NOT NULL
  PK (context, version, from_type_key, from_code, relation)
  FK (context, version) → corpus_versions
```

### 3.2 Lifecycle

```
                    ┌──────────┐
         create     │          │   publish    ┌───────────┐
    ───────────────►│  DRAFT   ├─────────────►│ PUBLISHED │
                    │          │              └─────┬─────┘
                    └──────────┘                    │
                         ▲                    rollback
                         │                         │
                         │              ┌──────────▼──────────┐
                         │              │   ROLLED-BACK       │
                         │              │  (audit entry only)  │
                         │              └─────────────────────┘
                         │
                    new draft from
                    published version
```

**Create draft:**
- A new draft is branched from the current published version (or empty for the first version).
- The draft copies the flattened corpus of the parent version as its starting point.
- Edits (add items, override values, add localizations) modify the draft's `corpus_items` /
  `corpus_localizations` / `corpus_references`.

**Publish:**
- The draft is promoted to `published`. The `published_at` timestamp is set.
- The previous published version (if any) remains in `published` status — versions coexist.
- Eager KV materialization fires (see §5).
- A change-event is published on the `REFDATA` stream so consumers know a new version exists.

**Rollback:**
- An operator can rollback to any previously-published version.
- This creates a **new** corpus version (not a status flip) with the rolled-back-to version's
  data, so the version sequence only moves forward.
- The rolled-back-from version's status is set to `rolled-back` with audit fields.
- The new version is immediately published (eager KV materialization fires).

### 3.3 Relationship to Existing Tables

The existing `dictionary_items`, `dictionary_localizations`, etc. tables become the **working
tables** — the mutable editing surface. The `corpus_*` tables are the **immutable snapshots**
created at publish time. The working tables continue to serve the current draft; publishing
copies (flattens) their state into a new `corpus_*` row set.

The existing per-type `dictionary_set_versions` is subsumed by the corpus-level version. It
may be retained as an internal optimisation (tracking which types changed since the last
corpus version) but is no longer the consumer-facing version number.

---

## 4. Version Pinning

### 4.1 Consumer Model

A consuming service (e.g. the shipping backend) declares which corpus version it wants:

```
GET /api/refdata/v/{version}/{context}/{type}/{code}
```

Or via the KV bucket naming convention (see §5). The "latest published" shorthand resolves to
the highest-version row with `status = 'published'` for the context.

### 4.2 Pin Registry (Future — Phase N+1)

For automated compatibility checks (deferred per the user's decision), a registry tracks
which services are pinned to which versions:

```
refdata.consumer_pins
  service_id    TEXT NOT NULL PK
  context       TEXT NOT NULL
  pinned_version INTEGER NOT NULL
  pinned_at     TIMESTAMPTZ NOT NULL DEFAULT now()
```

This enables: "which services still need version 7?" before considering eviction or
deprecation of old versions. Deferred — not in scope for initial implementation.

---

## 5. Hybrid KV Materialization

### 5.1 Bucket Strategy

Each materialized version gets its own KV bucket:

```
refdata-{context}-v{version}
```

Example: `refdata-emea-acme-v12`

A metadata key in a version-independent bucket tracks the current published version:

```
Bucket: refdata-meta-{context}
Key:    current-version
Value:  {"version": 12, "publishedAt": "2026-07-22T10:00:00Z"}
```

### 5.2 Eager Materialization (on publish)

When a corpus version is published:

1. Create KV bucket `refdata-{context}-v{N}` (if it doesn't exist).
2. For every item in `corpus_items` where `(context, version) = (ctx, N)`:
   - Build the `kvcache.Entry` (item + localizations + references + version stamp).
   - Write to `{type_key}.{code}` in the versioned bucket.
   - Write `{type_key}._meta` with the type-level metadata.
3. Update `refdata-meta-{context}` → `current-version` to point to N.
4. Publish a change event on the `REFDATA` stream.

The actively published version's KV entries have **no TTL** (or a very long TTL) — they stay
warm indefinitely.

### 5.3 TTL & Lazy Re-Materialization (for old versions)

When a new version M is published (M > N):

1. The **previous** version N's KV bucket entries get a TTL applied (e.g. 24 hours).
   - Implementation: NATS KV doesn't support per-key TTL natively. Options:
     - **Bucket-level TTL** — set the bucket's `TTL` config to 24h. All entries expire together.
       Simple but coarse. *(Recommended for POC.)*
     - **Application-level sweep** — a periodic goroutine deletes entries from old-version
       buckets that haven't been accessed in N hours. More control, more code.

2. If a consumer pinned to version N requests data after the TTL has expired:
   - KV miss → refdata service reads from `corpus_items` in Postgres → writes back to the
     versioned KV bucket → resets the TTL (rewrite-on-read).
   - The rewrite is a full `kv.Put()` of the entry, which resets the bucket-level TTL for
     that key (NATS KV TTL is per-revision, so a new put = new TTL countdown).

3. If no consumer requests the expired version, the KV bucket's entries stay expired (or the
   bucket itself can be deleted by a cleanup sweep if no pins remain — future, after the pin
   registry exists).

### 5.4 Read Path (Consumer Side)

```
consumer requests version V of (context, type_key, code):

1. Check KV bucket refdata-{context}-v{V}, key {type_key}.{code}
   → HIT: return entry, done
   → MISS (expired or never materialized):

2. Call refdata-service REST:
   GET /api/refdata/v/{V}/{context}/{type}/{code}
   → Service reads from corpus_items (Postgres)
   → Service writes entry back to KV bucket (rewrite-on-read, resets TTL)
   → Returns entry to consumer
```

### 5.5 Backward Compatibility

The existing `refdata-{context}` bucket (no version suffix) continues to serve the "latest
published" alias — it is eagerly updated on every publish, same as today. Consumers that
don't need version pinning keep working unchanged.

---

## 6. Audit Trail

### 6.1 Corpus Version History

`corpus_versions` is itself the audit trail — every version (draft, published, rolled-back) is
a permanent row with timestamps and the `notes` field. The `rolled_back_by` field links a
rolled-back version to its replacement.

### 6.2 Change Log (Key-Level)

For the "list of changed keys" between versions (the user's current requirement):

```
diff(context, from_version, to_version):
  SELECT type_key, code, 'added'    WHERE in to_version but not from_version
  UNION
  SELECT type_key, code, 'removed'  WHERE in from_version but not to_version
  UNION
  SELECT type_key, code, 'changed'  WHERE in both but attrs/localizations differ
```

This is a query against the `corpus_items` / `corpus_localizations` snapshots — no separate
change-log table needed since both versions' full data is retained.

---

## 7. API Surface (New/Changed Endpoints)

### Context Management
```
POST   /api/refdata/admin/contexts              -- register a context (with optional parent)
GET    /api/refdata/admin/contexts              -- list all contexts + hierarchy
GET    /api/refdata/admin/contexts/{context}    -- get context details + ancestors
```

### Corpus Versioning
```
POST   /api/refdata/admin/corpus/{context}/draft           -- create new draft
GET    /api/refdata/admin/corpus/{context}/draft            -- get current draft contents
PUT    /api/refdata/admin/corpus/{context}/draft/items      -- edit draft items
POST   /api/refdata/admin/corpus/{context}/publish          -- publish the draft
POST   /api/refdata/admin/corpus/{context}/rollback/{version}  -- rollback to a version
GET    /api/refdata/admin/corpus/{context}/versions         -- list all versions + status
GET    /api/refdata/admin/corpus/{context}/versions/{v}     -- get version details
GET    /api/refdata/admin/corpus/{context}/diff/{v1}/{v2}   -- diff two versions
```

### Versioned Read (Consumer-Facing)
```
GET    /api/refdata/v/{version}/{context}/{type}/{code}    -- read item at version
GET    /api/refdata/v/{version}/{context}/{type}           -- list type at version
GET    /api/refdata/v/latest/{context}/{type}/{code}       -- alias for current published
```

### Existing Endpoints (Unchanged)
The current unversioned endpoints (`/api/refdata/{context}/{type}/{code}`) continue to work,
reading from the working tables (current draft or latest published, depending on context).

---

## 8. Schema Migration Strategy

The new tables (`contexts`, `corpus_versions`, `corpus_items`, `corpus_localizations`,
`corpus_references`) are **additive** — no existing tables are dropped or altered (except
possibly soft-deprecating `dictionary_set_versions` in favor of corpus-level versioning).

The existing working tables (`dictionary_items`, etc.) remain as the mutable editing surface.
Publishing copies their state into the immutable `corpus_*` tables.

Migration steps (in `migrate.go`):
1. Create `refdata.contexts` table.
2. Create `refdata.corpus_versions` table.
3. Create `refdata.corpus_items`, `corpus_localizations`, `corpus_references` tables.
4. Insert a default `contexts` row for the existing `emea-acme` context (no parent — root).
5. Optionally: snapshot current working-table state as corpus version 1 (published) so
   existing data is immediately available through the versioned API.

---

## 9. Implementation Phases

> **This is Phase 12 in the main plan** ([Dictionary-POC-Plan.md](Dictionary-POC-Plan.md)).
> Sub-phases 12.1–12.7 below. Previous Phase 12 (Ship Container Capacity Limit) has been
> renumbered to Phase 13; all subsequent phases bumped by one.

### Phase 12.1 — Context Hierarchy

Postgres `contexts` table, REST endpoints for context management, domain model for hierarchy
traversal (ancestors, descendants). No versioning yet — establishes the multi-tenant
scaffolding.

**Checklist:**
- [ ] `domain/context.go` — `Context` entity, `ContextRepository` port (`Register`, `Get`,
      `List`, `Ancestors`, `Descendants`)
- [ ] `postgres/migrate.go` — `refdata.contexts` table
- [ ] `postgres/context_repository.go` — implementation
- [ ] `application/commands/context.go` — `ContextHandler` (register, list, get with ancestors)
- [ ] `rest/handlers.go` — context admin endpoints
- [ ] Seed: register `emea-acme` as root context in `seed.go`
- [ ] Ginkgo specs for hierarchy traversal (ancestor chain, multiple levels)
- [ ] `go build ./...` + `ginkgo ./...` green

### Phase 12.2 — Corpus Versioning & Draft/Publish Lifecycle

Corpus-level versioning tables, draft creation, item editing within a draft, publish
operation, version listing. No inheritance resolution yet — single-context only.

**Checklist:**
- [ ] `domain/corpus.go` — `CorpusVersion` entity, `CorpusRepository` port, status enum,
      lifecycle rules (only one draft per context, publish transitions, etc.)
- [ ] `postgres/migrate.go` — `corpus_versions`, `corpus_items`, `corpus_localizations`,
      `corpus_references` tables
- [ ] `postgres/corpus_repository.go` — implementation (create draft by copying from last
      published, publish = status flip + timestamp, version diff query)
- [ ] `application/commands/corpus.go` — `CorpusHandler` (create draft, edit draft items,
      publish, list versions, diff)
- [ ] `rest/handlers.go` — corpus admin endpoints
- [ ] Business rules: BR-V01 (one draft per context), BR-V02 (only drafts can be published),
      BR-V03 (publish is atomic — all or nothing)
- [ ] Ginkgo specs: draft lifecycle, publish, diff between versions
- [ ] `BUSINESS_RULES.md` updated
- [ ] `go build ./...` + `ginkgo ./...` green

### Phase 12.3 — Rollback with Audit

First-class rollback: rolling back creates a new version (forward-only version numbers) with
the rolled-back-to version's data, marks the rolled-back-from version, publishes the new
version.

**Checklist:**
- [ ] `domain/corpus.go` — rollback rules (target must be a published version, creates new
      version, audit fields)
- [ ] `postgres/corpus_repository.go` — rollback implementation (copy corpus data, status
      updates, audit fields)
- [ ] `application/commands/corpus.go` — `Rollback` handler
- [ ] `rest/handlers.go` — rollback endpoint
- [ ] Business rules: BR-V04 (rollback target must be published), BR-V05 (rollback creates a
      new version, not a status revert)
- [ ] Ginkgo specs: rollback lifecycle, audit trail, version sequence
- [ ] `go build ./...` + `ginkgo ./...` green

### Phase 12.4 — Template Inheritance Resolution

Inheritance resolution during draft creation and publish: walking the ancestor chain,
merging items, tracking `source_context` and `is_override`, propagation rules.

**Checklist:**
- [ ] `domain/inheritance.go` — `FlattenCorpus(context, ancestors, working_items)` — the
      resolution algorithm; returns the merged item set with source attribution
- [ ] `postgres/corpus_repository.go` — `CreateDraft` now resolves inheritance from parent
      context's latest published version
- [ ] Override detection: items in child matching `(type_key, code)` in parent = override;
      no-match = addition
- [ ] Propagation test: change in grandparent propagates to grandchild unless intermediate
      parent overrides
- [ ] Addition test: child can add items not in parent; these don't propagate upward
- [ ] No-delete test: child cannot remove an inherited item
- [ ] Ginkgo specs: multi-level hierarchy (3+ levels), override breaks propagation,
      parent publish → child sees new items
- [ ] `go build ./...` + `ginkgo ./...` green

### Phase 12.5 — Hybrid KV Materialization & Version Pinning

Versioned KV buckets, eager materialization on publish, TTL on old versions, lazy
re-materialization on demand, versioned read endpoints.

**Checklist:**
- [ ] `kvcache/versioned.go` — versioned bucket management (`refdata-{context}-v{N}`),
      materialize corpus into bucket, TTL application on superseded versions
- [ ] `application/commands/corpus.go` — publish triggers eager KV materialization
- [ ] Lazy re-materialization: on KV miss, read from `corpus_items` Postgres, write to KV
      (rewrite-on-read)
- [ ] `rest/handlers.go` — versioned read endpoints (`/api/refdata/v/{version}/...`)
- [ ] `refdata-meta-{context}` bucket with `current-version` key
- [ ] Backward compat: unversioned `refdata-{context}` bucket still updated on publish
- [ ] TTL strategy: bucket-level TTL on old version buckets (configurable, default 24h)
- [ ] Ginkgo specs: eager materialization, TTL expiry + lazy re-materialization, version
      pinning reads correct data
- [ ] `go build ./...` + `ginkgo ./...` green

### Phase 12.6 — Frontend (Versioning Admin UI)

Admin UI for context management, corpus versioning, publish/rollback, version diff viewer.
Likely in `frontend-dict` as new panels/tabs.

**Checklist:**
- [ ] Context hierarchy viewer/editor
- [ ] Corpus version list (status badges, timestamps)
- [ ] Draft editor (item overrides, additions)
- [ ] Publish confirmation dialog
- [ ] Rollback confirmation dialog with version selector
- [ ] Version diff viewer (changed keys list)
- [ ] `npm run build` green

### Phase 12.7 — Consumer Integration & Documentation

Update the shipping backend's `refdataconsumer` to support version pinning. Update all
architecture docs.

**Checklist:**
- [ ] `refdataconsumer` — version-aware `Lookup` (reads from versioned KV bucket, falls back
      to versioned REST endpoint)
- [ ] Configuration: pinned version as an env var / startup config
- [ ] `ARCHITECTURE-DICTIONARY.md` — updated data model, data access paths, cross-service
      consumption
- [ ] `BUSINESS_RULES.md` — all new BR-V* rules
- [ ] `go build ./...` + `ginkgo ./...` green in both services

---

## 10. Business Rules (New)

| Rule | Description |
|---|---|
| BR-V01 | At most one draft per context at a time |
| BR-V02 | Only a draft can be published; publishing a non-draft is rejected |
| BR-V03 | Publish is atomic — the entire corpus snapshot is written or none of it |
| BR-V04 | Rollback target must be a previously-published version |
| BR-V05 | Rollback creates a new forward version (version numbers never go backward) |
| BR-V06 | A child context cannot delete an inherited item (override or leave as-is) |
| BR-V07 | An override in a child context breaks propagation for that item to all descendants |
| BR-V08 | A parent context publishing a new version does not automatically publish descendants |

---

## 11. Open Questions — RESOLVED (2026-07-22)

All five decided; implementation may proceed against these answers.

1. **Draft editing model** — **Working tables + copy-on-publish.** Edits go against the
   existing `dictionary_items`/`dictionary_localizations`/etc. working tables (unchanged
   editing UX, unchanged existing endpoints); publish flattens/copies their state into a new
   immutable `corpus_*` row set (§3.3 already describes this model — confirmed, not changed).

2. **Automated downstream re-publish** — **Fully manual, no auto-draft.** Per BR-V08, a
   parent publish never auto-publishes or auto-drafts descendants. An operator manually opens
   or creates a child draft when they want to pull in parent changes. No conflict-resolution
   design needed for this phase since there's no automatic merge/overwrite path.

3. **Localization inheritance** — **Yes, localizations flow with the inherited item.** The
   flattened snapshot at publish time includes localizations for inherited items. A child can
   override a single locale for an inherited item without overriding the item itself —
   consistent with BR-V07 (override breaks propagation per-item, not per-locale).

4. **KV bucket cleanup** — **Deferred to Phase 12.7+ / the pin registry.** No pin registry
   exists in this phase, so there's no reliable way to know "no consumer is pinned" yet. Old
   versioned KV buckets are left to accumulate for the POC; revisit once the pin registry
   (§4.2) is built.

5. **Corpus size at scale** — **Full snapshots, not delta-based storage.** The corpus is
   reference data, not transactional data — row count per version is bounded by dictionary
   size, not by traffic volume, so multiplicative growth across versions is cheap in practice.
   Delta-based storage is not needed for this phase.
