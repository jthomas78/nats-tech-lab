# Dictionary as a Service — Plan

> **Status: APPROVED (2026-07-13; re-approved for 11.12 on 2026-07-24; 11.13 dropped the same
> day — see its entry).** This is **Phase 11** of the main plan (sub-phases 11.1–11.12
> delivered; see 11.12's entry for what shipped); the former Phases 11–15 have renumbered to
> 13–17 (Phase 12 was inserted for refdata versioning/tenancy — see the "Renumbering" section
> below).
>
> Main plan: [Main-POC-Plan.md](Main-POC-Plan.md)
>
> **Decisions made at original approval (2026-07-13):**
> 1. **Q1 — Option B (separate service).** `refdata-service/` is its own Go service/container,
>    own Postgres schema, own KV bucket — not a module in the existing monolith.
> 2. **11.3 demo consumer — hazard classes.** The shipping backend consumes the `hazard-class`
>    dictionary type via KV with version-mismatch re-read, as the concrete cross-service proof.
> 3. **Scope — 11.1–11.4 approved then.** The AI-assisted translation increment inside 11.4 and
>    the Q6-role-3 NATS `micro` request-reply spike in 11.3 were **not** in scope for that pass —
>    deferred/parked. 11.5 (consolidation write-up) was optional/deferrable.
> 4. **BR-D01–D07 confirmed as drafted**, unchanged from the table below.
>
> **Re-opened 2026-07-24 — approved, not yet implemented:**
> 5. **Phase 11.12 — AI-assisted translation**, un-parking item 3 above — **approved and
>    implemented 2026-07-24**. See the sub-phase entry for design/checklist.
> 6. **Phase 11.13 — Countries (ISO 3166) as a second demo consumer — dropped**, not approved.
>
> **Q1 revisited 2026-07-27 — approved and implemented:** tightened from schema-per-service to
> **database-per-service**. `refdata-service` now runs against its own Postgres instance
> (`refdata-postgres` in `docker-compose.yml`, port `5433`, own `refdata` role/database) rather
> than a private schema on `shipping-service`'s `postgres` instance — NATS is now the only
> infrastructure the two services share.
>    Investigation found the generic `{type}` REST endpoints already serve `country` with zero
>    new code (`country` is already seeded and localized); the only real options were a thin
>    tests-only deliverable or a much larger real-domain-field change. User chose to drop it
>    entirely rather than build either. See the sub-phase entry for the full rationale.
> 7. **Q6 role 3 (NATS `micro` request-reply)** remains parked *for this plan* — it's superseded
>    by [Main-POC-Plan.md § Phase 12.10](Main-POC-Plan.md), which covers the same
>    NATS `micro` request-reply pattern generically. **12.10 is also approved (2026-07-24)**;
>    see that phase's own entry for implementation status.
> 8. **Both 11.12 and 12.10 followed the repo's AI Agent Workflow**: business rules confirmed
>    with the user first (BR-D07/BR-D24 for 11.12; BR-D25/BR-D26 for 12.10, in
>    `BUSINESS_RULES-REFDATA.md`), then Ginkgo specs written before code, red → green → refactor.

## Definition (project goal)

> **Dictionary — Shared reference/master data.** Central repository for lookup values used
> throughout the platform: vehicle types, order statuses, currencies, units of measure, trailer
> types, Incoterms, hazard classes, countries, etc.

This is added as a first-class goal of the lab: alongside the event-sourcing shapes (A/B/C), the lab now also evaluates **reference data as a dedicated service** — how the rest of a V3 platform consumes shared lookup values with low latency, localization, and cache coherence.

## Relationship to the existing POC

Demo 01 started life as a "dictionary" POC and evolved into the shipping domain. The **Phase 9.5
ports registry** was the first true reference-data table and set the precedent this plan follows:

- Reference data is **plain Postgres CRUD**, not event-sourced — nothing ever replays a lookup
  value (see `ARCHITECTURE.md` § "Event Sourcing vs Plain CRUD").
- Reads that must be fast/shared go through **NATS KV** (Shape B write-through), with Postgres as
  the source of truth.

The dictionary service generalizes the ports registry into a proper service: many dictionary
types, localization, cross-references, and a versioned cache protocol. Migrating the ports
registry *into* the dictionary service is a candidate follow-up (see sub-phase 11.5) but is
explicitly **not** required — the current implementation is untouched.

---

## Q1 — Can this be added as a separate service without disrupting the current implementation?

**Yes.** The dictionary is an independent bounded context with zero dependency on the Ship /
Container aggregates, the `SHIPPING` stream, or the existing KV buckets. Two options:

| Option | What it is | Trade-off |
|---|---|---|
| **A. New module in the existing monolith** | A `refdata/` composition module beside `dictionary/`, wired via the existing `internal/monolith` Module interface | Zero infra change, but doesn't prove the *service* boundary — everything shares one process and one deploy |
| **B. Separate Go service + container (recommended)** | New `demos/01-dictionary/refdata-service/` with its own `cmd/main.go`, own Postgres schema, own KV bucket, added to the same `docker-compose.yml` | Proves the actual question — how *other* services consume shared reference data over the wire / via KV — at the cost of a second Go build |

**Recommendation: Option B.** "Dictionary as a service" is only demonstrated if it *is* a service:
its consumers (the existing backend, the frontends, hypothetically every V3 service) reach it via
its REST API and its KV cache, never via shared Go packages or shared tables.

Non-disruption guarantees:

- **No changes** to the `SHIPPING` stream, subjects, or existing consumers.
- **New KV bucket namespace**: `refdata-{context}` — does not collide with `dict-a-*`, `dict-b-*`,
  `container-*`, `meta-*`.
- **Own Postgres schema** (`refdata`) in the same Postgres instance — no shared tables.
- **Additive compose change only**: one new service entry (plus optional frontend); existing
  services' definitions untouched.
- Existing tests remain green with the new service absent — the shipping backend has no
  compile-time or runtime dependency on it.

## Q2 — Localization and referenced data

Both are core to the data model, not bolt-ons:

### Data model (Postgres, source of truth)

```
dictionary_types           type_key, name, description                  -- currency, country, incoterm, uom, hazard-class, trailer-type, vehicle-type, order-status, …
dictionary_items           type_key, code, context, status(active|deprecated), attrs JSONB, version
dictionary_localizations   item → locale → label, description           -- e.g. (currency EUR, de-DE) → "Euro"
dictionary_references      from_item → relation → to_item               -- typed, e.g. country ZA →defaultCurrency→ currency ZAR
```

- **Identity** = `{type_key}.{code}` scoped by `{context}` (the same company / business-unit
  convention as the rest of the lab — not tenant, not region; see `Main-POC-Plan.md` § Phase 16).
  **Locale is a read-time parameter**, never part of identity.
- **Locale fallback chain** on resolution: `de-DE → de → default locale → code itself`. The read
  API resolves the chain server-side so consumers never implement fallback logic.
- **References are typed and validated**: a relation declares its target type
  (`defaultCurrency → currency`); creating a reference to a missing or deprecated item is
  rejected. This is what distinguishes a dictionary service from a bag of enums — country pulls
  its currency, hazard class pulls its UN class, a UoM pulls its base unit.
- **Deprecate, never delete**: items that are referenced (by other items, or plausibly by
  transactional data elsewhere in the platform) are status-flipped, not removed. Historic data
  stays resolvable.
- **Seed from standards**: ISO 4217 (currencies), ISO 3166 (countries), Incoterms 2020, UNECE
  Rec 20 (units of measure), UN hazard classes. UN/LOCODE noted as the eventual seed for the
  ports registry if it migrates in.

### Read API sketch

```
GET /api/refdata/{context}/{type}?locale=de-DE          # full localized set + set version
GET /api/refdata/{context}/{type}/{code}?locale=de-DE   # single item, references expandable
GET /api/refdata/{context}/{type}/version               # cheap version probe (Q5 protocol)
GET /api/refdata/{context}/locales                      # locales known to this context
POST/PUT/DELETE under /api/refdata/admin/…              # CRUD, localization, locale management, references
POST /api/refdata/admin/{type}/{code}/translate         # AI-drafted translations (Q3), review-before-save
```

**Swagger/OpenAPI is a requirement**: the service exposes its full API via `swag` annotations and
serves Swagger UI at `/swagger/` — same toolchain and pattern as the existing backend (Phase 7
precedent, including the hand-patch-don't-regenerate caveat for doc strings).

## Q3 — UniFi-style VueJS frontend (extra)

A third frontend, `frontend-dict/`, reusing the established stack: Vue 3 + PrimeVue v4 Aura
preset + the shared UniFi token overrides (main plan § "UI Styling — UniFi Aesthetic", shared
theme file).

**Required capabilities:**

- **View** existing entries — per-type grid, filter, status chips.
- **Add** new entries — item create with attributes + initial localizations.
- **Delete** entries — hard-delete while unreferenced; once referenced, the UI offers
  **deprecate** instead (see BR-D02) with an explanation of why.
- **Language management** — add a new locale to a context; per-locale completeness view
  ("de-DE: 412/460 items localized") so translation gaps are visible per type.
- **AI-assisted translation (optional)** — for a selected item (or a whole type × locale gap),
  request AI-drafted translations. The backend (not the browser) calls the model — Claude API,
  key server-side — via `POST /api/refdata/admin/{type}/{code}/translate`, returning **drafts
  that a human reviews and saves**; nothing AI-generated lands in Postgres without an explicit
  save. Drafts are flagged (`source: ai`) so reviewed vs machine-drafted labels are
  distinguishable.

Screens:

1. **Type navigator** (left rail) — dictionary types with item counts.
2. **Item grid** — `<DataTable size="small">`, UniFi-density; filter, status chips
   (active/deprecated), code + default label columns; add / delete / deprecate actions.
3. **Item editor** — attributes, localization tab (one row per locale, fallback preview,
   "draft with AI" action per missing locale), references tab (typed relation picker).
4. **Locales panel** — add a language, per-type completeness bars, bulk "draft missing
   translations with AI" entry point.
5. **Cache status widget** — live view of the Q5 protocol: Postgres set version vs KV `_meta`
   version per type, with a KV-watch-driven "in sync / stale" indicator. This makes the
   version-mismatch story *visible*, in the same spirit as the existing shape panels.

Marked **(extra)** — the service and its API/cache are complete and testable without it
(sub-phase 11.4 is skippable/deferrable; AI translation is a further optional increment inside
it, since it needs a model API key in the environment).

## Q4 — Off-the-shelf (enterprise-level) solutions

**Yes, they exist — but they solve a governance problem this platform doesn't have yet.**
(Web-verified July 2026.)

### Enterprise commercial RDM/MDM

| Product | Dedicated RDM? | Deployment | Localized labels | Typed references |
|---|---|---|---|---|
| **Informatica Reference 360** | Yes — the only purpose-built SaaS RDM product (code lists, hierarchies) | SaaS only; 5–6 figures/yr | Via attributes/crosswalks | Yes — **crosswalks** (value mappings between code lists) are first-class |
| **TIBCO EBX** | Yes — RDM is its historic core strength | Self-hosted Java or EBX Cloud | Strong built-in i18n | Yes — FK relationships, hierarchies, inheritance |
| **Semarchy xDM** | Reference data as one MDM domain | Self-hosted (on your Postgres) or cloud; ~$360/user/yr reported | Modeled, not built-in | Yes — model-driven |
| **Profisee** | Reference data as one MDM domain | SaaS / Azure / self-hosted; 2025 Gartner MQ Leader | Modeled, not built-in | Yes — domain (lookup) attributes |
| **Stibo Systems STEP** | Yes — Reference Data platform domain | SaaS | Yes — strong (PIM lineage) | Yes |
| **SAP MDG** | Custom objects; SAP-estate oriented | S/4HANA embedded or hub | Yes (language text tables) | Yes |
| **Oracle EDM Cloud** | Hierarchy/CoA-centric, wrong shape for operational lookups | SaaS | Limited | Mapping-centric |

### Lighter / open source

- **AtroCore** — open-source data-management platform marketed for reference data/MDM; the most
  credible purpose-built OSS option, but a small ecosystem.
- **Directus** — database-first headless CMS: point it at existing Postgres lookup tables and it
  auto-generates REST/GraphQL + admin UI + content translations and relations. Not RDM (no
  crosswalk/versioning governance), but a pragmatic 80% admin-UI shortcut.
- **Honest note:** there is no widely-adopted, purpose-built open-source RDM service — teams use
  MDM-lite tools, a headless CMS, or build a small dictionary service (i.e., exactly this POC).

### Seed datasets

ISO 4217 (SIX XML), ISO 3166 (freely mirrored), UN/LOCODE (free UNECE CSV), UNECE Rec 20 UoM
(free), UN hazard classes (UNECE Model Regulations), Incoterms 2020 (the 11 codes are freely
enumerable; full rule text is ICC-copyrighted). **No product ships these pre-loaded** — loading is
an import exercise everywhere (~a day from the public GitHub datasets).

### Build-vs-buy conclusion

Commercial RDM is justified when **stewardship workflow, approval, audit, and crosswalk management
across many consuming systems** are the problem. For a greenfield platform where the dictionary is
a service inside your own event-driven architecture (Postgres + NATS KV), buying Informatica/EBX
buys governance ceremony at enterprise cost — and none of them provide the NATS-KV distribution/
cache-coherence layer that is this lab's actual question (Q5). **Build the thin service now,
seeded from the free ISO/UNECE datasets; re-evaluate Reference 360 / EBX only if multi-system
stewardship workflows become real. Directus is a credible interim admin-UI shortcut if 11.4 is
skipped.**

## Q5 — Do I need KV as a cache, with updatable reads on version mismatch?

**Yes — and it's the most valuable part of the exercise.** This is Shape B generalized from
"cache for one demo's read model" to "platform-wide reference data distribution", and it's the
piece an off-the-shelf RDM product would *not* give you.

Why a cache at all: dictionary lookups sit on every hot path of every service (validate an order
status, resolve a currency label, check a hazard class). Hitting the dictionary's REST API per
lookup couples every service's latency *and availability* to it. A local/NATS-KV cache decouples
that — reference data is the textbook read-heavy, write-rarely workload.

### Versioned-read protocol (proposed)

- Postgres is the source of truth. **Every mutation to a type's items/localizations/references
  atomically bumps that type's `version`** (monotonic, per `{type, context}`) in the same
  transaction.
- Write-through to KV bucket `refdata-{context}`:
  - `{type}.{code}` → item JSON (attrs + localizations + references), stamped with the item's
    version.
  - `{type}._meta` → `{version, itemCount, updatedAt}` for the whole set.
- **Consumers hold the set version they last loaded.** On read:
  1. Read the KV key (fast path).
  2. If the entry's stamped set-version ≠ the consumer's held version → re-read `{type}._meta`;
     if genuinely newer, **re-pull the set (or delta) from the service API and update the held
     version** — the "updatable read on version mismatch".
- **KV watch = push invalidation** (same watch → SSE plumbing as the existing demo);
  **version check = pull correctness** — it covers cold starts, missed watch events, and bucket
  rebuilds. Push for freshness, version for truth.
- Cache miss (key absent) falls through to the service API, which back-fills KV — identical to
  Shape B's miss path.

### Alternatives considered

| Alternative | Why not (alone) |
|---|---|
| No cache — REST per lookup | Hot-path latency + availability coupling of every service to the dictionary |
| In-process cache + TTL | Unbounded staleness window within the TTL; no cross-instance coherence; version mismatch undetectable |
| KV per-key revision (CAS) as the version | Revisions are per-key and reset on bucket rebuild; the app-level set version is transport-agnostic and survives re-projection — KV revisions stay what they are today: the projector's guard, not the consumer protocol |

One honest caveat for the write-up: with LimitsPolicy KV and write-through from the same
transaction boundary as Postgres, there is a small window where Postgres has committed and KV
hasn't been updated yet (same eventual-consistency window Shape B already documents). The version
protocol is what makes that window *detectable* by consumers instead of silent.

## Q6 — Should / can NATS (streams) form part of the comms?

**Can: yes, in three distinct roles. Should: two of them yes, one optional.** The guardrail: NATS
carries *distribution and notification*; **Postgres stays the source of truth and JetStream never
becomes the dictionary's event store** — that would contradict the Event Sourcing vs Plain CRUD
heuristic this service exists to demonstrate (nothing replays a lookup value to reconstruct it).

| Role | Mechanism | Verdict |
|---|---|---|
| **1. Cache distribution** | NATS KV (`refdata-{context}`) + KV watch — the Q5 protocol | **Yes — core of the design.** Note KV *is* a JetStream stream under the hood, so NATS streaming is already in the comms path. |
| **2. Change-event feed** | Publish `refdata.changed` events to a small JetStream stream, e.g. subjects `evt.{context}.refdata.{type}.changed` (as implemented; the original draft here read `{region}.refdata.{tenant}.{type}.changed` — superseded, neither region nor tenant belongs in a subject, see `Main-POC-Plan.md` § Phase 16) on a `REFDATA` stream, LimitsPolicy with a **bounded MaxAge** (e.g. 24–48h) | **Yes — recommended.** Gives services that don't watch KV (or that batch) a notification channel, and gives late/restarting consumers a short replayable window of *what changed* (type + new set version — a pointer, not the payload). Bounded age is the explicit signal that this is a change-feed, **not** an event store: truth is always re-fetchable from the API/KV. |
| **3. Request-reply lookups** | NATS `micro` (services framework in nats.go): the dictionary answers `refdata.get.{type}.{code}` request-reply, with built-in discovery/stats/ping | **Optional spike — dropped from 11.3's scope at the 2026-07-13 approval, superseded by [Main-POC-Plan.md § Phase 12.10](Main-POC-Plan.md), APPROVED 2026-07-24 (not yet implemented).** Phase 12.10 is the actual vehicle for this: a general `rpc.*` dual-transport pattern (not dictionary-specific) whose first concrete case is exactly this — shipping-service calling refdata-service's item lookup over NATS `micro` instead of REST. Not re-scoped here to avoid building it twice — see 12.10 for the live design/checklist. |

What NATS should **not** do here:

- **Not the write path** — admin CRUD goes REST → Postgres transaction (version bump included).
  Publishing the change event happens *after* commit (transactional-outbox-style ordering noted in
  the write-up; for the POC, publish-after-commit with the version protocol as the safety net is
  acceptable and documented).
- **Not the source of truth** — the `REFDATA` stream carries pointers ("hazard-class set is now
  v42"), consumers fetch state from KV/API. No consumer ever folds `refdata.changed` events into
  state.

---

## Business rules (proposed — confirm at approval)

| ID | Rule |
|---|---|
| BR-D01 | Item codes are unique per `{type, context}` |
| BR-D02 | An **unreferenced** item may be hard-deleted; an item that is referenced (by a `dictionary_references` row, or in use by a consuming service) cannot be deleted — only deprecated |
| BR-D07 | AI-drafted translations are never persisted without explicit human save; persisted localizations record their `source` (manual \| ai) |
| BR-D03 | Localized resolution follows the fallback chain `requested locale → language → default locale → code`; resolution never fails outright for an existing item |
| BR-D04 | Every mutation to a type's items, localizations, or references bumps that type's set version atomically with the write |
| BR-D05 | A reference must target an **active** item of the relation's declared target type |
| BR-D06 | Deprecated items still resolve on read (historic data must remain renderable); they are excluded from "assignable values" listings by default |

Per the repo quality rules: each confirmed rule maps to a Ginkgo `Context` block, written before
implementation, and lands in `BUSINESS_RULES.md` in the same commit.

## Sub-phases

### Phase 11.1 — Core service (Postgres CRUD + read API)

> **Complete (2026-07-14).**

- [x] `refdata-service/` scaffold: own `go.mod`, `cmd/main.go` (connects Postgres, runs
      migration + seed, starts the HTTP server), hexagonal layout under `refdata/internal/`
      (`domain`, `application/commands`, `postgres`, `rest`), own Dockerfile, compose entry
      (shares the `postgres` service, own `DATABASE_URL`, port `18081`)
- [x] Postgres schema: `dictionary_types`, `dictionary_items`, `dictionary_localizations`,
      `dictionary_references` (+ migration, own schema `refdata`) — `refdata/internal/postgres/migrate.go`
- [x] Domain: item lifecycle (create / delete-when-unreferenced / deprecate), typed
      references, BR-D01/02/05/06 — `refdata/internal/domain/dictionary.go` +
      `refdata/internal/application/commands/{item,reference,type}.go`
- [x] REST: read API (list types, list/get items, BR-D06 assignable-vs-all) + admin CRUD
      (register type/item, deprecate, delete, create reference) — `refdata/internal/rest/handlers.go`.
      Version-probe and locale-scoped reads are **not yet meaningful** (no set-version or locale
      data exists until 11.2/11.3) — added when those land, not stubbed here.
- [x] Swagger: `swag` annotations on all endpoints, generated `docs/`, Swagger UI served at
      `/swagger/` (same toolchain as Phase 7, `swag init` run fresh — no prior docs to diff against)
- [x] Seed: representative subsets of ISO 4217 (35 currencies), ISO 3166 (52 countries), the
      full Incoterms 2020 (11 terms), a UNECE Rec 20 subset (12 units), and all 9 UN hazard
      classes — `refdata/seed.go`, idempotent (`RegisterItem` + `ErrDuplicateItemCode` ignored),
      run from `Startup()` on every boot
- [x] Ginkgo specs from the confirmed business rules; `ginkgo ./...` green (12/12 —
      `refdata/item_test.go`, `refdata/reference_test.go`, in-memory fakes, no real Postgres in
      the suite, same convention as the shipping backend's `fakePortRepo`)
- [x] `BUSINESS_RULES.md` + `ARCHITECTURE.md` sections for the new service

### Phase 11.2 — Localization + reference resolution

> **Complete (2026-07-14).**

- [x] Localization CRUD + fallback-chain resolution (BR-D03) server-side —
      `domain.ResolveLabel()` + `commands.LocalizationHandler`
- [x] Locale management: add a locale to a context; per-type localization-completeness query —
      `LocalizationHandler.AddLocale/ListLocales/Completeness`, `postgres.LocaleRepository`
- [x] Reference expansion on the read API (`?expand=defaultCurrency`) — `ReferenceHandler.Expand()`,
      `GET /api/refdata/{context}/{type}/{code}?expand=...`
- [x] Bulk localized export per type (`GET …/{type}?locale=…` returns full localized set) —
      `listItems` in `refdata/internal/rest/handlers.go`
- [x] Ginkgo specs: fallback chain, reference expansion, deprecated-item resolution, locale
      management, completeness (`refdata/localization_test.go`, 8 specs; suite total 20/20 green)

### Phase 11.3 — KV cache + versioned-read protocol + NATS comms

> **Complete (2026-07-14).** One deliberate atomicity simplification vs. the original design
> sketch: the version bump (`VersionRepository.Bump`, a single atomic Postgres `UPSERT ...
> RETURNING`) is a separate statement from the item/reference/localization write, not wrapped in
> one shared multi-statement transaction — sequenced at the application-handler level immediately
> after the write succeeds, rather than at the SQL level. Documented as a known trade-off (same
> spirit as Q6's publish-after-commit note), not silently glossed over.

- [x] Set-version bump per mutation (BR-D04) — `postgres.VersionRepository.Bump()`, atomic at the
      statement level
- [x] Write-through projection to `refdata-{context}` (`{type}.{code}` + `{type}._meta`) —
      `kvcache.Projector.NotifyItemChanged()`, carrying the item + its localizations + outbound
      references, not just the raw row
- [x] Miss path: API back-fills KV — `kvcache.Projector.Backfill()`, called from the REST `getItem`
      handler as a best-effort side effect
- [x] `REFDATA` change-event stream (Q6 role 2): `evt.{context}.refdata.{type}.changed` (as implemented; the original draft here read `{region}.refdata.{tenant}.{type}.changed` — superseded, neither region nor tenant belongs in a subject, see `Main-POC-Plan.md` § Phase 16)
      published after commit; LimitsPolicy + explicit bounded `MaxAge` (48h); payload = pointer
      (type + new set version), never state
- [x] Consumer protocol documented + demonstrated: `backend/internal/refdataconsumer` consumes the
      **hazard-class** dictionary type via KV with version-mismatch re-read, demoed at
      `GET /api/refdata-demo/{context}/{type}/{code}` on the shipping backend
- [x] KV watch → SSE for live invalidation — `GET /api/refdata-watch/{context}` on refdata-service
- [x] Ginkgo specs: version bump atomicity (20 concurrent mutations, no lost bumps), cache/`_meta`
      rebuild, change event published on mutation, cold start, miss back-fill
      (`refdata/kvcache_test.go`, 7 specs; suite total 27/27 green); version-mismatch → re-read
      covered by `backend/internal/refdataconsumer/consumer_test.go` (4 specs, plain `go test`)
- [~] Q6-role-3 (NATS `micro` request-reply lookup) — parked per the approval decision, not built

> Q6-role-3 (NATS `micro` request-reply lookup endpoint) is **parked, not in this pass's scope** —
> revisit later if a service-to-service NATS-only consumer becomes real.

### Phase 11.4 — UniFi-style frontend (extra)

> **Complete (2026-07-14).** Verified live in-browser against the full Docker stack, not just
> `npm run build` — see ARCHITECTURE.md's "Dictionary frontend" subsection for the full component
> map and what was exercised.

- [x] `frontend-dict/` scaffold sharing the Aura/UniFi theme preset (dev port 5175)
- [x] View / add / delete entries — delete offered but a 409 (BR-D02, referenced) is caught and the
      toast points at Deprecate instead
- [x] Item editor (localization + references tabs) — required two new REST list endpoints
      (`.../{code}/localizations`, `.../{code}/references`) not built in earlier sub-phases
- [x] Locales panel: add a language, per-type completeness view
- [x] Cache status widget (Postgres version vs KV `_meta`, live via SSE) — required a new
      `GET .../{type}/cache-status` REST endpoint
- [x] Compose entry (`5175:80`, depends on `refdata-service`) + `npm run build` + lint clean (0 errors)

> AI-assisted translation (backend `translate` endpoint, Claude API, draft → human review → save,
> BR-D07's `source: ai` flag) is **parked, not in this pass's scope** — the BR-D07 rule itself
> stays confirmed for whenever the feature lands; only the UI/endpoint work is deferred.

### Phase 11.5 — (optional) Consolidation + build-vs-buy write-up

> **Complete (2026-07-14).**

- [x] Evaluate migrating the Phase 9.5 ports registry into the dictionary service (UN/LOCODE
      seed); decide and document — **decision: leave as-is, not migrated.** Ports are checked
      synchronously on the shipping backend's hot write path (BR-017/018, every ship arrival and
      container registration); moving that check to refdata-service would turn a fast in-process
      Postgres query into a cross-service call with a new failure mode, contradicting the
      fast-and-self-contained write path Phases 8–15 protect. If revisited, the KV-cache-plus-
      REST-fallback pattern already built for the hazard-class demo (`internal/refdataconsumer`)
      is the right consumption model, not a synchronous per-command call. Full rationale in the
      Obsidian note below.
- [x] Obsidian vault note (`obsidian/POC-Dictionaries/Findings - Dictionary Service (Phase 11).md`):
      findings write-up — build-vs-buy conclusion (Q4: build, not buy — commercial RDM solves a
      governance problem this platform doesn't have; Directus flagged as an admin-UI shortcut if
      ever needed), the versioned-cache protocol result (Q5: confirmed via a real cross-service
      demo, including a nil-interface bug caught during verification), ports decision, and a
      stakeholder summary

### Phase 11.6 — Shipping UI consumes localized ship-status from refdata

> **Complete (2026-07-14).** The shipping backend now resolves `ship-status` labels
> from the `refdata-emea-acme` KV cache (KV-first, REST fallback — BR-D08) and both
> shipping frontends render them in a user-selected locale, updating live on refdata
> change. Refdata context is fixed at `emea-acme`, independent of the fleet selector.

- [x] Extend `internal/refdataconsumer`: decode the cached `localizations` map, add a
      `locale` param to `Lookup`, resolve labels KV-first via the BR-D03 fallback chain
      (reimplemented locally — no refdata-service import), add `ResolveType` (enumerates
      the bucket via `kvstore.Keys`) and a `Locales` REST passthrough
- [x] Backend REST: `GET /api/refdata/types/{type}?locale=` and `GET /api/refdata/locales`
      (fixed `refdataContext = "emea-acme"`); `GET /api/refdata-watch` SSE reusing the
      existing `watchBuckets` engine (refactored to take an explicit context); `getRefdataDemo`
      now forwards `?locale=`
- [x] Shared `useRefdataLabels` composable (`demos/01-dictionary/shared/refdata/`, `@refdata`
      vite alias in both apps): fetch label map + locales, live-refresh via SSE, `statusLabel()`
      with built-in English fallback
- [x] Admin UI (`frontend/`): locale `<Select>` in topbar; `ShapePanel`/`ShapeCPanel` resolve
      labels from refdata, keeping the local `STATUS_SEVERITY` colour map
- [x] Port UI (`frontend-port/`): locale `<Select>`; `FleetPanel`/`ShipsAtPortPanel` adopt the
      `ship.status` field for the label, keeping `currentPort`-derived fallback + severity
- [x] **BR-D08** added to `BUSINESS_RULES.md`; consumer specs cover KV-hit resolution,
      language/default/code fallback, `?locale=` forwarding on miss, and `ResolveType`

### Phase 11.7 — UI copy via refdata + dictionary type categories (refdata-as-TMS)

> **Category field, `l10n` type, and the Localization view are done (2026-07-14).**
> **A UI-restructure pass (design review 2026-07-15) shipped as Phase 11.9.**
> Extends Phase 11.6's pattern from *domain reference data* (ship statuses) to *UI
> chrome* — the Port UI's status-filter "All" option and both apps' "Language" label are
> now sourced from refdata via vue-i18n, with a bundled fallback catalog. Per-category
> governance hints and the remaining UI-layout polish items (cache strip, ItemGrid header
> split, propagation pulse) are deferred — not required for this pass.

**Architecture** — reuse the Phase 11.6 consumer pipeline end to end; the frontend never
talks to refdata-service directly:

```
refdata-service (SoT) → KV cache → backend consumer → /api/refdata/types/l10n?locale=xx → vue-i18n
```

- [x] Register an `l10n` dictionary type, namespaced separately from domain types so UI
      strings never leak into business queries. Codes are message keys (e.g. `filter.all`),
      labels are the copy; seeded `en`/`es` (`filter.all`, `nav.language` — BR-D10 covers the
      typed-reference/deprecation exemption).
- [x] No new backend endpoint — an `l10n` fetch reuses the generic
      `/api/refdata/types/{type}?locale=` route + `ResolveType` and the `/api/refdata-watch`
      SSE refresh built in Phase 11.6 (verified: works unchanged for `l10n`).
- [x] Frontend l10n layer (vue-i18n) whose catalog loader (`shared/refdata/useL10nCopy.js`)
      fetches `l10n`, folds the `{code,label}[]` list into a message object, and calls
      `setLocaleMessage(locale, msgs)`. The shared `selectedLocale` (from `useRefdataLabels`)
      drives both refdata data labels and UI copy from one switcher.
- [x] Live refresh: on the `/api/refdata-watch` change signal, re-fetch and re-set the
      catalog — translators' edits appear without a frontend build.
- [x] Locale-list ownership: both domain labels and UI copy already source their locale list
      from `/api/refdata/locales` (Phase 11.6) — satisfied by construction, no extra work.

**Rationale — one translation workflow for everything.** Translators edit domain labels
*and* UI chrome in the same place, live, without a frontend build. This is what commercial
TMS tools (Phrase, Lokalise, Crowdin) centralize; if the org wants that workflow,
refdata-as-TMS is coherent. The trade-off: UI copy becomes runtime data — no code review,
no compile-time key checking — so this is a deliberate platform choice, not a default.

- [x] **Bundled fallback catalog (required).** Shipped as `shared/refdata/l10nFallback.en.js`,
      an override layer under the live refdata catalog. **Visible indicator done:** a warning
      `Tag` in both apps' topbars distinguishes total fallback (refdata unreachable) from
      partial fallback (a key resolved to its own code — BR-D11).

**Type categories / namespaces (folded in — the `l10n` namespacing above is the first
concrete use).** As the registry grows (currency, country, incoterm, uom, hazard-class,
ship-status, `l10n`) the types fall into categories with genuinely *different
governance*, not just different names. Formalize a `category` so the Dictionary UI can
group them and category-specific rules are explicit.

**Preliminary namespace list (initial — provisional, expected to grow):**

- `standards` — cross-cutting, standards-based reference data (currency, country, incoterm, uom, hazard-class)
- `domain-enum` — mirrors of a backend domain enum (ship-status)
- `l10n` — UI chrome / application copy (the `l10n` type above)
- `config` — *(future)* platform / tenant configuration values

Names are provisional and the set is intentionally small. The functional line is `l10n`
vs the rest — it is not reference data and must stay out of business queries; the
`standards`-vs-`domain-enum` split is more informational but still worth surfacing:

| Category | Examples | Source of truth | Who edits | Codes at runtime |
|---|---|---|---|---|
| `standards` | currency, country, incoterm, uom, hazard-class | external standards (ISO, Incoterms, UNECE, UN) | data stewards | rarely change; adds safe |
| `domain-enum` | ship-status | the backend domain (`ShipStatus` consts) | devs own codes; stewards translate | adding/removing a code is meaningless unless the domain emits it |
| `l10n` | l10n (Phase 11.7) | the frontend | translators | keys owned by devs; only labels translatable |

- [x] Add a single `category` string to `DictionaryType` (controlled small vocabulary:
      `standards` / `domain-enum` / `l10n`, `config` later) — one category per type, not
      tags. Backend: `dictionary_types.category` column (`migrate.go`), domain struct
      (`domain.ValidateCategory`, BR-D09), seed assigns each existing type its category, REST
      `typesResponse`/`typeRequest` carry it. Covered by `refdata/type_test.go`.
- [x] Group the TypeNavigator by category in `frontend-dict` — non-selectable eyebrow headers
      (Reference Data / Domain Enums / UI Copy) so `l10n` visibly sits apart from domain
      reference data; every type stays one click away.
- [ ] Surface a per-category governance hint in the UI — e.g. `domain-enum` types show a
      "codes owned by the backend — translate only" note, discouraging stewards from adding
      codes nothing emits. **Deferred** — not required for this pass.

**Dictionary UI layout (folded in — design review 2026-07-14).** The current
`frontend-dict` layout puts a per-type item table, context-wide locale admin, and an
infrastructure diagnostic at the same visual level. The categories above make the mismatch
structural: the UI serves three audiences (data stewards, translators, demo/ops) and the
layout should encode that split. Target shape:

```
┌ Topbar: Dictionary · Context · [watching] · ☾ ────────────────┐
├──────────────┬────────────────────────────────────────────────┤
│ ▸ REFERENCE  │  country  [standards] 52 items  Locale▾ ⊘ +Add │
│ ▸ UI COPY    │  ┌ item table (unchanged) ────────────────────┐│
│ ──────────── │  └─────────────────────────────────────────────┘│
│ ⚑ Localization│  pg v12 · kv v12 · ● in sync      (cache strip)│
└──────────────┴────────────────────────────────────────────────┘
   Localization view = types × locales completeness matrix
   + locale registration + TMS export/import (this phase)
```

- [x] **Grouped TypeNavigator** (extends the category-grouping checkbox above): category
      headers are non-selectable eyebrows, not drill-down folders — every type stays one
      click away. The sidebar reads as the governance model made visible.
      **Superseded by Phase 11.8**, which turns the eyebrows into an expand/collapse tree.
- [x] **"Localization" becomes a view/mode, not a lower panel.** Split `LocalesPanel`'s two
      jobs: locale *registration* (rare, context-level admin, kept as-is) and *completeness*
      (a translator's dashboard). Promoted to a sidebar entry (`⚑ Localization`) /
      `LocalizationView.vue` where completeness renders as a **types × locales matrix** (one
      glance, no per-locale Select-and-wait). `LocalesPanel.vue` removed, superseded by this.
- [x] **One locale context** — resolved by the matrix design rather than by merging selectors:
      the completeness matrix shows every registered locale as a column at once, so the old
      per-locale completeness Select no longer exists to be disconnected from the ItemGrid's.
- [ ] **Cache Status becomes a slim strip**, not a quarter-page panel: one line under the
      item grid (`pg v7 · kv v7 · 5 items · ● in sync`), expandable on click. Permanently
      visible (that's the demo's point) without competing with the data. **Deferred.**
- [ ] **ItemGrid header split** before it overflows: type identity (name, category chip,
      count, cache dot) on one line; view controls (locale, deprecated toggle, Add)
      right-aligned — leaves room for this phase's fallback indicator. **Deferred.**
- [ ] *(polish, optional)* **Propagation pulse**: on an SSE change event, briefly flash the
      cache strip and the affected row — the Q5 write→version-bump→re-hydrate→in-sync loop
      made visible. Cheap; the SSE plumbing already exists. **Deferred.**

**Cautions:** keep it light — a `category` field + grouped nav is right; a full relational
category entity with its own CRUD is premature and the fuzzy categories will tempt
proliferation, so resist until a real need appears. `category` is orthogonal to `context`
(the company / business-unit scope — not tenant, not region; see
`Main-POC-Plan.md` § Phase 16) — don't conflate them.

### Phase 11.8 — Dictionary UI follow-ups (locale default, tree nav, un-deprecate)

> **Complete (2026-07-14).** Three small follow-ups to Phase 11.7's `frontend-dict` work,
> raised after that phase shipped.

- [x] **Locale selector defaults to `en`** (BR-D13) instead of an empty selection, which
      resolved items by raw code. `dictionary.js`'s `selectedLocale` initial state changed
      from `''` to `'en'`, matching the precedent already set in
      `shared/refdata/useRefdataLabels.js` for the other two frontends.
- [x] **TypeNavigator becomes a two-level expand/collapse tree** (Category → Type). The
      Phase 11.7 eyebrow headers (non-interactive) are now clickable — they toggle their
      type list open/closed, chevron-indicated, starting expanded so nothing is hidden on
      first load. Type selection behavior is unchanged.
- [x] **Reactivate a deprecated item** (BR-D12) — deprecation (BR-D02/BR-D06) was previously
      one-way. Added `ItemHandler.ReactivateItem` (mirrors `DeprecateItem`: existence check,
      status flip, BR-D04 version bump/cache rebuild), a Postgres `Reactivate` repo method, and
      `POST /api/refdata/admin/items/{type}/{context}/{code}/reactivate`. `ItemGrid.vue` gets a
      symmetric "Reactivate" row action, enabled only when an item is deprecated.

### Phase 11.9 — Dictionary UI restructure (master-detail + governance-split sidebar)

> **Complete (2026-07-15, approved same day).** Second layout pass on `frontend-dict`,
> agreed in review. Scope note: **display labels and layout only** — the BR-D09 category
> vocabulary (`standards` / `domain-enum` / `l10n` / `config`) is unchanged in the
> domain, DB, and API, so no business-rule or backend changes are involved.

Target shape:

```
┌ Topbar: Dictionary · Context · [watching] · ☾ ──────────────────────────┐
├───────────────┬──────────────────────────────────────────────────────────┤
│ REFERENCE DATA│  country  [standards] 52 items   Locale▾ ⊘ +Add          │
│   country     │  ┌ item list ──────────┐ ┌ item detail ────────────────┐ │
│   currency    │  │ ZA  South Africa    │ │ ZA — South Africa           │ │
│   …           │  │ ZW  Zimbabwe        │ │ [Attrs][Localizations][Refs]│ │
│ ───────────── │  │ … (filterable,      │ │                             │ │
│ DOMAIN        │  │    compact)         │ │                             │ │
│  ▸ Enums      │  └─────────────────────┘ └─────────────────────────────┘ │
│  ▸ UI Strings │                                                          │
│  ▸ Configura… │                                                          │
│ ───────────── │                                                          │
│ ⚑ Localization│                                                          │
└───────────────┴──────────────────────────────────────────────────────────┘
```

- [x] **Category display renames** (labels centralized in `src/categories.js`, consumed
      by `TypeNavigator.vue` and the main-panel category chip — category *keys* unchanged):
      `Domain Enums` → **Enums**, `UI Copy` → **UI Strings**, `Config` → **Configuration**;
      `Reference Data` stays.
- [x] **Sidebar encodes the governance split** — Reference Data vs Domain — as *visual*
      grouping, not a deeper tree: Reference Data group first, then a divider with a
      `DOMAIN` super-eyebrow above the Enums / UI Strings / Configuration groups. The
      Phase 11.8 expand/collapse behavior stays on the category eyebrows (Category → Type);
      the Domain super-eyebrow is a non-interactive label — no third interaction level.
      Revisit collapsible nesting only if the type count grows past ~15.
- [x] **Master-detail everywhere** — the main panel becomes `[item list | item detail]`
      for *all* categories (one spatial model; no per-category layout forks). New
      `ItemDetailPanel.vue` (driven by `store.selectedCode`, kept valid by
      `refreshItems`) takes over `ItemEditorDialog`'s tabs plus a new **Attrs** tab and
      the per-item lifecycle actions (deprecate / reactivate / delete), killing the
      modal churn when walking item-by-item. Only **density** varies by category:
      a filter input appears for `standards` types (or any list past ~15 items);
      enums/UI strings/configuration show their full small sets.
- [x] `ItemEditorDialog.vue` retired — the detail panel covers edit; create stayed a
      dialog (the small "Register item" form), as anticipated.
- [x] The **ItemGrid header split** deferred in Phase 11.7 is subsumed: type identity
      (name, category chip, item count) sits left; view controls (locale, deprecated
      toggle, Add) sit right.
- [x] *(follow-up, same day)* **Locales panel: Default column + register dialog.** The
      Localization view's Locales table gains a Default column with radio (exactly-one)
      semantics — picking another locale *moves* the default via the existing
      `POST /admin/locales` upsert, which clears the old default atomically; the inline
      add-row became a `+ Add` → "Register locale" dialog. Read-side support added:
      `GET /{context}/locales` now returns `defaultLocale` (additive), with the
      single-default invariant promoted to **BR-D14** (spec in `localization_test.go`).

### Phase 11.10 — Localize the shipping UI (static strings + enums, en/es)

> **Approved (2026-07-15) — Option D (all refdata, generated fallback).** Extends Phase
> 11.7's refdata-as-TMS pattern from its two proof-of-concept keys (`filter.all`,
> `nav.language`) to the *whole* Port UI (`frontend-port`). Implemented (2026-07-15),
> pending independent Spanish-copy review.

**Scope (from the 2026-07-15 inventory).** `frontend-port` only. `frontend` (the
architecture/demo UI) is out of scope; `frontend-dict` is a separate admin app with its
own locale mechanism and no vue-i18n wiring — not in scope here.

- **~90 hardcoded user-facing literals across 4 files** — `App.vue` (~18),
  `FleetPanel.vue` (~20), `ShipsAtPortPanel.vue` (~22), `TerminalPanel.vue` (~26):
  headings, buttons, column headers, placeholders, toasts, empty states, aria-labels,
  validation hints. Only 2 (`nav.language`, `filter.all`) go through `t()` today.
- **Enums are largely already done.** `ship-status` — the only *coded* enum rendered in
  the Port UI — resolves through refdata end-to-end (Phase 11.6). No other coded enum is
  displayed: container-status is derived into Outbound/Arrived buckets client-side and
  isn't even seeded; port names are free-form values, not coded reference data.
- **No number/date/currency formatting** exists in `frontend-port` — nothing to localize
  there (the only `toLocaleString`/date calls live in `frontend/`, out of scope).

**Decision — where does UI copy live? Option D chosen (2026-07-15).** Today (Phase 11.7)
every UI string routed through l10n is an `l10n` category refdata item (en+es rows) *and* a
bundled `l10nFallback.en.js` entry — BR-D11 requires the bundled `en` so chrome still
renders when refdata is unreachable. The options considered (kept below for the record):

| Option | l10n home | Per-string cost | Trade-off |
|---|---|---|---|
| **A — all refdata** | every string an `l10n` item | ~3 artifacts (en+es seed rows + bundled `en` fallback) | Maximal TMS thesis. But BR-D11 still forces a bundled `en` for each string, so `en` is maintained *twice* (seed + fallback); weakest safety for strings coupled to code — a validation hint can silently drift from the regex it describes. |
| **B — all bundled** | vue-i18n catalog (en+es), dev-owned | 2 entries, one place | Simplest; code-reviewed; keys greppable. Abandons refdata-as-TMS for the Port UI (refdata keeps only domain *data* labels like ship-status) — contradicts Phase 11.7's direction. |
| **C — split by editorial ownership** | domain-facing copy → refdata; implementation-coupled chrome → bundled | mixed | Demonstrates the *judgment* of where dictionary-as-a-service earns its keep; avoids double-maintaining `en` for chrome nobody edits live. Requires the boundary be encoded as a namespace convention (proposed BR-D17) so it isn't ad hoc. |
| **D — all refdata, *generated* fallback** *(chosen)* | every string an `l10n` item; the bundled `en` is a build-time *snapshot of the seed*, not hand-written | 1 authored artifact (seed en+es) + generator + CI drift check | "Option A done right." Single source of truth (refdata), but the bundle is *compiled* from the seed like a lockfile — so no double-`en` maintenance and no drift; keeps first-paint correctness and offline degrade (BR-D11 stays). Cost is one-time tooling, not per-string effort. Pairs with a `<UiString code>` seam for call-site provenance. |

Fault line for Option C, had it been chosen — **would a translator/steward ever edit this
live, decoupled from a code change?** *Yes* → refdata (panel titles, section headers,
status/filter labels, empty-state prose, domain buttons like Arrive/Depart/Unload, success
toasts like "Ship arrived"). *No* → bundled (validation hints that mirror a regex/format,
placeholders embedding format examples like `TCKU1234567`, aria-labels, error/diagnostic
toasts like "Depart failed"). Kept here only as the discarded alternative's reasoning.

**Option D — chosen (2026-07-15).** Keep *refdata as the sole authored source* (like A),
but stop *hand-writing* the bundled fallback — **generate** it from `l10nSeed` at build
time and commit it (lockfile model): a generator emits `shared/refdata/l10nFallback.en.js`
with a `GENERATED — do not edit` banner, a `prebuild` hook keeps it fresh, and a
CI drift-check fails if regenerating yields a diff (this is what keeps the seed authoritative
rather than silently forking). The generated catalog drops into the existing override-base
slot (`useL10nCopy.js:45`) unchanged — live refdata overlays it once the fetch lands — so
there is *no runtime change*, and the "frozen at seed values" concern is invisible because
it is only the cold-paint base. This gives A's single-source purity **and** clean admin-UI
provenance (every string is an `l10n` item, so all are visible/editable in the Dictionary
UI) **without** A's double-maintenance. A no-bundle variant (copy as a hard runtime
dependency, retiring BR-D11) was considered and rejected — it surrenders first-paint
correctness for nothing the generated bundle doesn't already give. Because D routes every
string through refdata (no split), **BR-D17's namespace-boundary convention does not
apply** — that was only needed for Option C's dev-owned/refdata split.

**Track 1 — static UI strings (the bulk of the work).**

- [x] Design a key namespace (e.g. `port.*`, `fleet.*`, `terminal.*`, `status.*`,
      `form.*`, `a11y.*`, `toast.*`) — under Option D this is purely organizational
      (every key lives in refdata regardless of prefix), not a routing boundary.
- [x] Extract each literal → `t('key')` in templates and the composition `t` in
      `<script setup>` for toasts/JS across the 4 files.
- [ ] Supply `en` + `es` for every key as an `l10n` seed row (`l10nSeed` in
      `seed.go`) — the sole authored source under Option D; the bundled fallback is
      generated from it (see Build tooling below), never hand-written. **`es`
      is a deliverable, not auto-fill** — seeded; pending human/translator review before
      being accepted as final.
- [x] **Interpolation** — `Ships at Port — {port}`, `Terminal Yard — {port}` use named
      interpolation, not string concatenation.
- [x] **Pluralization** — `container(s)` (two files) uses vue-i18n plural rules with real
      `es` plural forms, not an English `(s)` suffix.

**Track 2 — enums (small; mostly already done).**

- [x] The derived display labels `at sea` (`FleetPanel.vue`) and the `Outbound` / `Arrived`
      bucket headers (`TerminalPanel.vue`) are **UI copy, not refdata enums** — route them
      through Track 1.
- [ ] *(Only if a second coded enum is ever surfaced in the Port UI)* generalize
      `shared/refdata/useRefdataLabels.js` from its single hardwired type (`ship-status`)
      to multi-type. **Not required by any string in today's Port UI — list-only here so
      the constraint is on record.** `hazard-class`/`incoterm` are seeded (en+es) but
      unconsumed; wiring them is its own future phase, not this one.

**Consolidation / cleanup.**

- [x] Reconcile the duplicated English ship-status fallbacks — call sites now rely on the
      single `SHIP_STATUS_FALLBACK` map in `useRefdataLabels.js`, which remains intentional
      offline resilience for the domain enum.

**Build tooling (required by Option D).**

- [x] Node generator emits the bundled catalog from `refdata-service`'s `l10nSeed`.
      Output is committed with a `GENERATED — do not edit` banner (lockfile model) and drops
      into the existing `useL10nCopy.js:45` override base, so no runtime change.
- [x] `npm run gen:i18n` wired as a `prebuild` hook so `npm run build` can't ship a stale
      bundle.
- [x] **CI drift check** — regenerate in CI and fail on any diff. The load-bearing piece:
      it is what makes the seed authoritative and prevents a silent two-source fork.
- [x] Decide bundle breadth — default-locale (`en`) only, enough for cold-paint (live locale
      overlays once fetched), vs all locales for full offline rendering at a larger JS size.
- [ ] *(optional, complements D)* a `<UiString code="…">` component/directive as the
      refdata-bound call-site seam: makes provenance explicit (every `<UiString>` *is* a
      refdata string — answers "which values are refdata-bound?"), centralizes the
      pending/missing-key state, and gives a free inspect hook. Complements `t()` rather than
      replacing it (attributes / aria-labels / placeholders still need functional `t()`), and
      does **not** remove cold-paint — hence still generate the bundle.

**Track 3 — frontend test harness (closes the BR-D16 UI-rendering gap).**

> **Added 2026-07-15.** Gap found during review: Phase 11.10's localization is verified
> today only by (a) `scripts/check-i18n.mjs` — a *static* scan that greps `.vue` files for
> bare literals and runs the generator drift-check, and (b) `refdata/seed_test.go`'s BR-D16
> spec, which proves the seed *data* is complete (en+es non-empty) but never mounts the UI.
> Nothing renders `App.vue`, switches locale, and asserts the visible strings actually
> change. Per CLAUDE.md ("every business rule must have a test"), BR-D16's *consumption*
> side is uncovered — all locale-switch verification this session was manual via the browser.
> This track adds an automated rendered-output harness.

> **Also in scope for this track (undocumented until now):** landing the harness required
> mounting `App.vue` after the Fleet/Port split into an activity-bar layout — `NavSidebar.vue`
> + `icons/IconFleet.vue` + `icons/IconPort.vue`, `App.vue`'s `activeView` ref replacing the
> old always-both-sections layout, and `app.subtitle` splitting into `app.subtitleFleet` /
> `app.subtitlePort` (one per view). This UI restructuring wasn't itemized under Track 1 —
> recorded here since the Track 3 no-view-overlap spec exists specifically to guard it.

> **Post-review hardening (2026-07-16):** a completeness review of this track found the
> harness passing but fragile in three ways, now fixed:
> - `App.spec.js`'s `seedCatalogs()` resolved `seed.go` via `resolve(process.cwd(), ...)`,
>   so the suite only passed when `npm test` ran with `frontend-port` as cwd. It now derives
>   the path from `import.meta.url` (matching `scripts/gen-i18n.mjs`'s approach), and passes
>   regardless of invocation directory. Note it deliberately avoids the literal
>   `new URL('...', import.meta.url)` pattern — Vite's import analysis special-cases exactly
>   that syntax for asset resolution and rewrites it to a dev-server `/@fs/...` URL, which
>   breaks a plain `readFileSync`; `dirname(fileURLToPath(import.meta.url))` sidesteps it.
> - The seed-parsing regex/boundary logic was duplicated between `gen-i18n.mjs` and
>   `App.spec.js`. Extracted into `scripts/parseL10nSeed.mjs`, imported by both, so a future
>   seed-format change only needs fixing in one place.
> - The pluralization spec asserted `.manifest-count` nodes by DOM position
>   (`[0,1,2]` order), which happened to match ship-insertion order but wasn't guaranteed by
>   the DataTable. Rewrote as `manifestCountsByShipId()`, keying each assertion to its row's
>   own shipID instead of render order.
> - Added assertions for the `app.subtitlePort` branch (previously only `subtitleFleet` was
>   exercised) and broadened the "no bare en literal survives locale switch" check to also
>   cover the language-selector label and connection-status tag, not just the title/nav.

- [x] **Install the harness** in `demos/01-dictionary/frontend-port`: `vitest`,
      `@vue/test-utils`, and a DOM env (`happy-dom` preferred over `jsdom` — lighter, enough
      for text assertions). Add `"test": "vitest run"` (and optionally `"test:watch":
      "vitest"`) to `package.json` scripts. Configure `test.environment = 'happy-dom'` in
      `vite.config.js` (Vitest reads the existing Vite config, so the `@refdata`/`@unifi-theme`
      aliases and the Vue plugin come for free — do not duplicate them).
- [x] **Mount with a real i18n instance.** Build the test i18n from the *generated*
      `shared/refdata/l10nFallback.en.js` for `en`, plus an `es` catalog. Prefer deriving
      both locales from `refdata-service`'s `l10nSeed` so the test can't silently drift from
      the authored source (mirror how `scripts/gen-i18n.mjs` parses `seed.go`); at minimum,
      seed the handful of keys the assertions below touch. Stub the network layer
      (`useL10nCopy`/`useRefdataLabels` refdata fetch) so tests are offline and deterministic —
      the point is to prove the *l10n consumption path* renders, not to hit refdata.
- [x] **BR-D16 rendered-output specs** (the load-bearing tests):
      - Locale switch changes visible chrome: mount at `en`, assert the topbar title, nav
        labels ("Fleet Management" / "Port Management"), and view subtitle render their `en`
        strings; set `i18n.global.locale` to `es`, `await nextTick()`, assert the *same* nodes
        now show the `es` strings ("Gestión de flota" / "Gestión portuaria" / etc.). This is
        the test that would have caught the stale-container symptom (only 2 strings switching).
      - Interpolation localizes: a string using named interpolation (e.g. `Ships at Port —
        {port}` / `Terminal Yard — {port}`) renders the port value inside the localized frame
        in both locales.
      - Pluralization localizes: the `container(s)` plural renders correct `en` and real `es`
        plural forms at counts 0/1/2 — not an English `(s)` suffix.
      - No bare literals leak: assert no assertion-targeted node still shows the `en` string
        after switching to `es` (guards against a `t()` call that was missed).
- [x] **No-view-overlap spec** (guards the Track-1 `v-if`/`v-else` in `App.vue`): with
      `activeView = 'fleet'` the Fleet panel renders and the Port section does not; toggling to
      `'port'` inverts it — the two views are never in the DOM simultaneously.
- [x] **Wire into CI** alongside `check:i18n` (the static scan and the rendered harness are
      complementary, not redundant — keep both). `npm run test` must pass green as the task's
      final step, same gate CLAUDE.md imposes on the Go suite.

**Quality (per CLAUDE.md — BR + test + docs in the same task).**

- [x] **BR-D16:** all user-facing Port-UI copy resolves through the l10n layer —
      no bare user-facing literals in `frontend-port` templates. *Static* side covered by
      `scripts/check-i18n.mjs`; *rendered* side (locale switch actually changes visible
      strings) covered by the Track 3 harness above — **the Vitest specs are the BR-D16
      test of record for the UI-consumption path.**
- [ ] ~~BR-D17~~ — dropped. It only made sense under Option C's dev-owned/refdata split;
      Option D routes every string through refdata, so there is no boundary to encode.
- [x] Update `BUSINESS_RULES.md` and this checklist together. (Next free number is
      **BR-D16** — BR-D15 is already claimed by the implicit-`en`-default work in
      `localization.go`/`localization_test.go`.)

**Cautions.** Keep the enum track from ballooning — no coded enum beyond `ship-status` is
shown today, so resist generalizing `useRefdataLabels` speculatively. The heavy part is
breadth (string extraction + translation data), not architecture; the pipeline already
exists. Do not start extraction before the Decision is made — re-homing strings after the
fact is the expensive mistake this gate exists to prevent.

### Phase 11.11 — Enum value localization UX (IMPLEMENTED 2026-07-17)

> Mirrors the detailed entry in [Main-POC-Plan.md](Main-POC-Plan.md) § Phase 11.11 —
> that file is the source of truth for the full ASCII mockups; this is the tracking copy.

**Goal.** Enum values (e.g. `Ship Status` → `at-anchor` = "At Anchor") can be viewed but not
localized in `frontend-dict`, and reference-data items only get a one-locale-at-a-time editor.
Make an enum value first-class, localizable data (stable key · default label · translations ·
description · status) and make bulk translation across locales fast. Primarily a `frontend-dict`
change — **frontend-only**, no new backend (see below).

**Backend is already in place — no new endpoint or business rule.** The item-update this UX needs
is **BR-D18**: `PATCH /api/refdata/admin/items/{type}/{context}/{code}/attrs`
(`commands.ItemHandler.UpdateItemAttrs()`), a full attrs-map replace. Editing the default label =
read attrs, set `attrs.name`/`attrs.description`, PATCH the whole map. Duplicate = `registerItem`
with copied attrs. The only missing piece is a `frontend-dict` `updateItem` wrapper for that PATCH.

**Design (three-level layout).**

- **Left — enum types:** existing type list.
- **Middle — enum values as a compact, sortable/searchable table** (replaces the text list):
  `Key` (monospace, fixed width, truncate + full-key tooltip, not inline-editable) · `Default label`
  (flexible) · `Status` (compact badge). Per-row overflow menu: Edit · Deactivate/Reactivate ·
  Duplicate · Delete.
- **Right — detail**, retabbed from `Attrs | Localizations | References` to first-class
  **`General | Translations | Usage`**:
  - **General** — Key (read-only) · Default label (editable) · Status · Description (editable).
  - **Translations** — a table (Locale · Display name · Translation · Status =
    Default/Complete/Missing) with search, a **Missing only** toggle, and **+ Add locale**; inline
    edit. Display names via `Intl.DisplayNames`; "Missing" rows are a client-side join of
    `store.locales` against existing localizations.
  - **Usage** — where the value is referenced (existing `listItemReferences`).
- **Bulk translation matrix** — a per-type **`Values | Translation Matrix`** toggle; matrix lays
  values (rows) × locales (columns) with editable cells (each an existing `setLocalization` upsert).
  Distinct from Phase 11.7's types×locales *completeness* matrix (that shows ratios; this edits
  individual values within one type).

**Checklist.**

- [x] `api.js` + store: `updateItem` wrapping `PATCH …/items/{type}/{context}/{code}/attrs`
      (BR-D18); duplicate via `registerItem`
- [x] Middle panel → sortable/searchable `DataTable` (Key / Default label / Status), truncate +
      tooltip, per-row overflow menu (Edit · Deactivate/Reactivate · Duplicate · Delete)
- [x] Detail panel retabbed to `General | Translations | Usage` (`ItemDetailPanel.vue`; shared with
      the Reference Data screen, so both benefit)
- [x] Translations tab: locale table with Default/Complete/Missing status, `Intl.DisplayNames`
      names, **Missing only** filter, **+ Add locale**, inline edit
- [x] Bulk **Translation Matrix** view + `Values | Translation Matrix` toggle
- [x] ~~All UI strings via `l10n` (BR-D16)~~ — dropped, N/A: `frontend-dict` has no
      `vue-i18n`/`l10n` wiring (that's `frontend-port`-only per Phase 11.10's explicit scope);
      new strings follow the existing plain-hardcoded-English convention already used throughout
      `frontend-dict`.
- [x] No new BR needed (reuses BR-D18). `frontend-dict` build green (`vite build`, `eslint`) +
      new Vitest suite (`itemFields.spec.js`, `localization.spec.js`, 24 tests) for the pure
      table/translation logic.
- [x] New files: `src/itemFields.js`, `src/localization.js` (+ specs),
      `src/components/TranslationMatrix.vue`
- [ ] **Manual browser click-through still owed.** This was implemented and verified from a
      background job with no display/browser access — every mutating endpoint (`updateItem` PATCH,
      `setLocalization` upsert, `registerItem` for Add value/Duplicate, deprecate/reactivate/delete)
      was exercised directly against the live `refdata-service` with disposable scratch data
      (created, checked, deleted), confirming wire-format correctness, but no one has visually
      confirmed the rendered UI (three-panel layout, `SelectButton` toggle, table styling,
      responsiveness). Do this next time the app is opened in a real browser.

### Phase 11.12 (APPROVED 2026-07-24 — IMPLEMENTED 2026-07-24) — AI-assisted translation

> **Un-parks the increment deferred at the original 2026-07-13 approval** (see "Open questions —
> resolved at approval" item 3 below). Raised again and approved 2026-07-24; implemented the same
> day. Bulk drafting is strictly sequential per the user's 2026-07-24 confirmation (BR-D24); the
> Claude API client sits behind the `domain.TranslationDrafter` port so Ginkgo specs use a fake
> rather than calling the real model API.
> Builds on Phase 11.11's translation UX (`frontend/refdata`'s Translations tab / Translation
> Matrix) and BR-D07 (already confirmed at the original approval, implemented here).

#### Goal

For a selected item/locale gap (or a whole type × locale gap), let a steward request an
AI-drafted translation instead of typing it by hand. The model call is **backend-mediated only**
— the browser never holds or sends a model API key — and nothing AI-generated is persisted
without an explicit human save.

#### Design

- **Endpoint**: `POST /api/refdata/admin/{type}/{code}/translate` (already sketched in the Q2 API
  list above) — body: target locale(s); calls the model (Claude API, key held server-side via env
  var, never returned to the client) with the item's default-locale label/description as context;
  returns draft label/description per requested locale. Drafts are **not** written to Postgres by
  this call.
- **Save is a separate, explicit step**: the existing `setLocalization` upsert (Phase 11.2/11.11)
  persists a draft only once a human accepts it, with `source: ai` set on that row (BR-D07);
  a human-edited-then-saved draft, or a manually-typed translation, is `source: manual`.
- **Frontend**: Phase 11.11's Translations tab gains a "Draft with AI" action per missing-locale
  row (single item) and the Translation Matrix gains a bulk "Draft missing (AI)" entry point per
  Q3's original sketch; drafts render inline as editable, unsaved suggestions (visually distinct
  from saved rows) until the steward saves or discards each one.
- **Failure/cost guardrails**: model call failures surface inline (no silent empty drafts); no
  auto-retry loop against the model API from bulk actions — a bulk "draft missing" issues one call
  per item/locale sequentially or with bounded concurrency, not unbounded fan-out.

#### Checklist

- [x] Confirm/finalize business rules for this sub-phase — BR-D07 formalized as-drafted; new
      BR-D24 added for the bulk sequential-only guard (user confirmed "sequential, one call at a
      time" over bounded concurrency, and a mocked `TranslationDrafter` for tests over hitting the
      real Anthropic API in Ginkgo specs).
- [x] `refdata-service`: `translate` handler + backend-side Claude API client (key via env var,
      `ANTHROPIC_API_KEY`, never logged or returned to the caller) —
      `refdata/internal/anthropic/client.go`, wired through `commands.TranslationHandler`
      (`refdata/internal/application/commands/translation.go`) behind the `domain.TranslationDrafter`
      port; `Handlers.Translations` stays `nil` (REST returns 501) when the env var is unset.
- [x] `dictionary_localizations`: the `source` column already existed from BR-D07's original
      scope; `domain.ValidateSource` now enforces `manual|ai` and `SetLocalization` derives it
      from the caller's input (defaulting to `manual`) instead of hardcoding `"manual"`.
- [x] Swagger annotation for the new endpoint (Phase 11.1 toolchain) — `POST
      /api/refdata/admin/{type}/{code}/translate`, regenerated via `swag init` (clean additive
      diff, no `$ref`-renaming noise this time).
- [x] `frontend/refdata`: "Draft with AI" (single, `ItemDetailPanel.vue`'s Translations tab) +
      "Draft missing (AI)" (bulk, per-locale-column button in `TranslationMatrix.vue`) actions;
      draft-vs-saved visual distinction (an "AI draft, unsaved" `Tag` / drafted-cell styling with
      accept/discard icons); bulk drafting awaits each row sequentially, never `Promise.all`.
- [x] Ginkgo specs: draft never persists without an explicit save call; saved draft records
      `source: ai`; manually-typed/edited-then-saved translation records `source: manual` — see
      `refdata/translation_test.go` ("Dictionary Translation Domain Rules").
- [x] `BUSINESS_RULES-REFDATA.md` updated with BR-D07's and BR-D24's implementation status.
- [x] `go build ./...` + `ginkgo ./...` green (92/92 specs); `frontend/refdata` `npm run build` +
      `npm test` (vitest) clean — no lint script exists in this app to run. Manually verified in
      the browser against the rebuilt `refdata-service`/`refdata` containers: with
      `ANTHROPIC_API_KEY` unset, both the single-item and bulk "Draft with AI" actions correctly
      surface a 501 "not configured" error end-to-end (frontend → REST → handler), and the bulk
      action's network log confirmed 5 sequential per-row `POST .../translate` calls, never
      concurrent.

### ~~Phase 11.13 — Second demo consumer: Countries (ISO 3166)~~ — DROPPED (2026-07-24)

> **Raised 2026-07-24, dropped the same day before implementation started.** Investigation
> found `GET /api/refdata-demo/{context}/{type}/{code}` and `GET /api/refdata/types/{type}`
> (`dictionary/internal/rest/handlers.go`) already take `{type}` as a fully generic path
> parameter — nothing hardcodes `hazard-class`. Since `country` is already seeded (Phase 11.1)
> and localized en/es (Phase 11.2), hitting either endpoint with `type=country` **already works
> today with zero new code.** That collapsed this phase's real options to: (B) add only
> consumer/Ginkgo specs proving it by test — a thin deliverable, mostly documenting something
> already true — or (A) add a genuine new domain field (e.g. a container's destination country)
> to make the lookup load-bearing, a much larger change than "a second demo consumer" implied.
> Presented both; **user chose to drop the phase entirely** rather than build either. No further
> work planned here; `country` remains available generically via the existing endpoints if a
> real need for it as a domain field arises later.

---

## Renumbering (done at approval)

| Was | Now |
|---|---|
| *(new)* | **Phase 11 — Dictionary as a Service** (11.1–11.5 above) |
| Phase 11 — Write-Side Safety | Phase 13 (was 12) |
| Phase 12 — Projection Hardening | Phase 14 (was 13) |
| Phase 13 — Stream Split | Phase 15 (was 14) |
| Phase 14 — Performance & Load Testing | Phase 16 (was 15) |
| Phase 15 — NATS Accounts Spike | Phase 17 (was 16) |

Cross-reference sweep (same commit):

- [x] Main plan internal references (Phase 9 "why this precedes Phase 12"→14, Phase 10's
      Phase 11/14/15 mentions→11/15/16, Phase 13–17 mutual references→14–18)
- [x] `demos/01-dictionary/PERFORMANCE.md` (and the `obsidian/POC-Dictionaries/` copy) — deferred-scenario phase labels
- [x] `ARCHITECTURE.md`, `BUSINESS_RULES.md`, code comments (`events.go`, `container.go`) that
      cite Phases 12–17
- [x] `.claude/memory/` notes citing phase numbers
- [x] `obsidian/POC-Dictionaries/` notes citing phase numbers

## Repo Restructuring — `backend/` and `frontend/` parent folders (completed 2026-07-22)

`demos/01-dictionary/` had five sibling directories mixing Go services and Vue frontends at
the same level (`backend`, `refdata-service`, `frontend`, `frontend-dict`, `frontend-port`).
Restructured into two parent folders, renaming three directories to clearer business names:

```
demos/01-dictionary/
  backend/
    shipping-service/   (was: backend)
    refdata-service/    (unchanged name, moved down one level)
  frontend/
    admin/               (was: frontend)
    refdata/             (was: frontend-dict)
    seafreight-app/      (was: frontend-port)
```

**Module isolation confirmed intact** (the driving concern for this move): `backend`'s and
`refdata-service`'s `go.mod`s have zero cross-imports, no `go.work` file, no `replace`
directives — each is a fully independent module communicating only over HTTP at runtime
(`REFDATA_SERVICE_URL`). Nesting is filesystem organization only; both modules still build
and test independently post-move.

**Decisions made with the user before executing:**
- Go module paths **updated to match** new physical location (mechanical rewrite, ~62/40
  import lines across the two modules — safe given zero cross-module coupling).
- Docker Compose service names, container hostnames, and `package.json` name fields
  **renamed to match** (`shipping-service`, `admin`, `refdata`, `seafreight-app`).
  `refdata-service`'s own service name/hostname is unchanged.
- Historical entries in this file, `Main-POC-Plan.md`, and `.ai-archive/*.md` **left
  untouched** — phase-by-phase build logs describing what existed at the time, not live docs.

**Checklist.**

- [x] `git mv` all five directories into the new nested layout (temp-rename step for
      `backend`→`backend/shipping-service` and `frontend`→`frontend/admin` to avoid nesting
      a directory into itself); git tracked all ~180 touched files as renames, history intact
- [x] Rewrote both `go.mod` module lines and every internal import to the new module paths
- [x] `docker-compose.yml` — renamed service keys, updated all `build`/`dockerfile` paths and
      `depends_on` references
- [x] nginx `proxy_pass` targets updated (`admin`/`seafreight-app` → `shipping-service`;
      `refdata`'s target unchanged)
- [x] `package.json` name fields renamed to match (`admin`, `refdata`, `seafreight-app`)
- [x] Frontend relative-path fixes — `vite.config.js` aliases (`@unifi-theme`, `@refdata`,
      `server.fs.allow`) in all three apps, `seafreight-app`'s `gen-i18n.mjs` /
      `parseL10nSeed.mjs` / `App.spec.js` (these cross the backend/frontend split, so both
      the extra frontend nesting level and refdata-service's own new nesting level apply)
- [x] All three Dockerfiles updated (`shipping-service`'s and `refdata-service`'s own
      Dockerfiles needed zero edits — confirmed path-agnostic)
- [x] CI workflow renamed `frontend-port.yml` → `seafreight-app.yml`, paths/working-directory
      updated
- [x] `.claude/launch.json` — all 4 configs' `cwd` and identifiers updated
- [x] Docs updated: `CLAUDE.md`, root `README.md`/`AGENTS.md` (path references only — pre-existing
      unrelated staleness in those two left alone), `demos/01-dictionary/README.md`,
      `ARCHITECTURE.md`, `BUSINESS_RULES.md`, `backend/refdata-service/README.md` +
      `ARCHITECTURE-DICTIONARY.md` (relative-link depth fixed too), two Draw.io skill files
- [x] `.claude/memory/frontend_port_structure.md` updated in place (filename kept, content
      repointed + a path-note added) — Obsidian vault needed no changes (its only path
      reference is to `docker-compose.yml` itself, which didn't move)
- [x] Verified: `go build`/`go vet`/`ginkgo` green in both modules (57 + 52 specs), `npm run
      build` green in all three frontends, `seafreight-app`'s full harness green
      (`check:i18n`, `test` 15/15, `build`), `docker compose config` valid, full `docker
      compose up --build` — all 5 services started and frontends reached their renamed
      backend hostnames through nginx, then torn down clean

## Open questions — resolved at approval (2026-07-13)

1. **Option A vs B** for Q1 — **B (separate service)**, as recommended.
2. Demonstration consumer in 11.3 — **hazard classes**.
3. **11.1–11.4 approved now.** AI-translation increment inside 11.4 — **parked**.
4. **BR-D01…BR-D07 confirmed as drafted**, including BR-D02's delete-vs-deprecate resolution.
5. Q6 role 3 (NATS `micro` request-reply) — **dropped from this pass's scope**, parked.

## Open questions — resolved at second approval (2026-07-24)

1. **AI-translation increment** (parked at item 3 above) — **un-parked, approved, and implemented
   as Phase 11.12** (2026-07-24).
2. **Second demo consumer type** — **dropped.** Investigation showed `country` already works
   through the existing generic `{type}` endpoints with zero new code; neither remaining option
   (tests-only, or a real new domain field) matched what "a second demo consumer phase" was
   meant to deliver, so it was dropped rather than built as either.
3. **Q6 role 3 revisited** — stays parked for this plan; superseded by
   [Main-POC-Plan.md § Phase 12.10](Main-POC-Plan.md), **approved and implemented
   2026-07-24** (Dual-Transport RPC + Admin UI Observability) — see that phase's own tasks list.
4. **Phase 18 → 12.11 renumber** — considered and **declined**; Phase 18 (NATS Accounts Tenancy
   Spike) stays where it is. 11.12/Phase 13 (Capacity Limit) also confirmed to stay at their
   current numbering — none are versioning/tenancy/RPC work, so folding them under Phase 12
   would be filing by proximity, not by topic. (11.13 was dropped entirely — see its own entry.)
