# Dictionary as a Service — Plan

> **Status: DRAFT — pending approval.**
> On approval this becomes **Phase 11** of the main plan (sub-phases 11.1, 11.2, …), and the
> current Phases 11–15 renumber to 12–16. Until then the main plan carries a *proposed* reference
> to this document and no renumbering happens.
>
> Main plan: [Dictionary-POC-Plan.md](Dictionary-POC-Plan.md)

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

- **Identity** = `{type_key}.{code}` scoped by `{context}` (same tenant/region convention as the
  rest of the lab). **Locale is a read-time parameter**, never part of identity.
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
| **2. Change-event feed** | Publish `refdata.changed` events to a small JetStream stream, e.g. subjects `{region}.refdata.{tenant}.{type}.changed` on a `REFDATA` stream, LimitsPolicy with a **bounded MaxAge** (e.g. 24–48h) | **Yes — recommended.** Gives services that don't watch KV (or that batch) a notification channel, and gives late/restarting consumers a short replayable window of *what changed* (type + new set version — a pointer, not the payload). Bounded age is the explicit signal that this is a change-feed, **not** an event store: truth is always re-fetchable from the API/KV. |
| **3. Request-reply lookups** | NATS `micro` (services framework in nats.go): the dictionary answers `refdata.get.{type}.{code}` request-reply, with built-in discovery/stats/ping | **Optional spike.** For service-to-service consumers already on NATS, request-reply avoids HTTP entirely; REST+Swagger remains the admin/frontend/browser API. Worth a small demo endpoint in 11.3 to document the trade-off (one transport to operate vs HTTP ubiquity + Swagger tooling). |

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

- [ ] `refdata-service/` scaffold: `cmd/main.go`, hexagonal layout (`internal/domain`,
      `internal/postgres`, `internal/rest`), own Dockerfile, compose entry
- [ ] Postgres schema: `dictionary_types`, `dictionary_items`, `dictionary_localizations`,
      `dictionary_references` (+ migration, own schema `refdata`)
- [ ] Domain: item lifecycle (create / update / delete-when-unreferenced / deprecate), typed
      references, BR-D01/02/05/06
- [ ] REST: read API (`GET` set / item / version probe / locales) + admin CRUD
- [ ] Swagger: `swag` annotations on all endpoints, Swagger UI served at `/swagger/` (Phase 7 toolchain)
- [ ] Seed: ISO 4217, ISO 3166, Incoterms 2020, UNECE Rec 20 subset, UN hazard classes
- [ ] Ginkgo specs from the confirmed business rules; `ginkgo ./...` green
- [ ] `BUSINESS_RULES.md` + `ARCHITECTURE.md` sections for the new service

### Phase 11.2 — Localization + reference resolution

- [ ] Localization CRUD + fallback-chain resolution (BR-D03) server-side
- [ ] Locale management: add a locale to a context; per-type localization-completeness query
- [ ] Reference expansion on the read API (`?expand=defaultCurrency`)
- [ ] Bulk localized export per type (`GET …/{type}?locale=…` returns full localized set)
- [ ] Ginkgo specs: fallback chain, reference expansion, deprecated-item resolution

### Phase 11.3 — KV cache + versioned-read protocol + NATS comms

- [ ] Set-version bump in the same transaction as every mutation (BR-D04)
- [ ] Write-through projection to `refdata-{context}` (`{type}.{code}` + `{type}._meta`)
- [ ] Miss path: API back-fills KV
- [ ] `REFDATA` change-event stream (Q6 role 2): `{region}.refdata.{tenant}.{type}.changed`
      published after commit; LimitsPolicy + explicit bounded `MaxAge`; payload = pointer
      (type + new set version), never state
- [ ] Consumer protocol documented + demonstrated: existing shipping backend consumes one
      dictionary type (e.g. hazard classes or vehicle types) via KV with version-mismatch re-read
- [ ] KV watch → SSE for live invalidation
- [ ] (optional spike) NATS `micro` request-reply lookup endpoint (Q6 role 3), trade-off documented
- [ ] Ginkgo specs: version bump atomicity, mismatch → re-read, cold start, miss back-fill,
      change event published on mutation

### Phase 11.4 — UniFi-style frontend (extra)

- [ ] `frontend-dict/` scaffold sharing the Aura/UniFi theme preset
- [ ] View / add / delete entries — delete offered only while unreferenced, deprecate otherwise (BR-D02)
- [ ] Item editor (localization + references tabs)
- [ ] Locales panel: add a language, per-type completeness view
- [ ] (optional) AI-assisted translation: backend `translate` endpoint (Claude API, server-side
      key), draft → human review → save, `source: ai` flag (BR-D07)
- [ ] Cache status widget (Postgres version vs KV `_meta`, live via SSE)
- [ ] Compose entry + `npm run build` clean

### Phase 11.5 — (optional) Consolidation + build-vs-buy write-up

- [ ] Evaluate migrating the Phase 9.5 ports registry into the dictionary service (UN/LOCODE
      seed); decide and document — migrate or leave as-is with rationale
- [ ] Obsidian vault note (`obsidian/POC-Dictionaries/`): findings write-up — build-vs-buy
      conclusion (Q4), the versioned-cache protocol result (Q5), stakeholder summary

## Renumbering on approval

When this plan is approved, in one commit:

| Current | Becomes |
|---|---|
| *(new)* | **Phase 11 — Dictionary as a Service** (11.1–11.5 above) |
| Phase 11 — Write-Side Safety | Phase 12 |
| Phase 12 — Projection Hardening | Phase 13 |
| Phase 13 — Stream Split | Phase 14 |
| Phase 14 — Performance & Load Testing | Phase 15 |
| Phase 15 — NATS Accounts Spike | Phase 16 |

Cross-reference sweep required in the same commit (phase numbers are cited widely):

- [ ] Main plan internal references (Phase 9 "why this precedes Phase 11", Phase 10's
      Phase 11/13/14 mentions, Phase 13/14 mutual references)
- [ ] `demos/01-dictionary/PERFORMANCE.md` — deferred-scenario phase labels
- [ ] `ARCHITECTURE.md`, `BUSINESS_RULES.md`, code comments (`events.go`, `container.go`) that
      cite Phases 11–15
- [ ] `.claude/memory/` notes citing phase numbers
- [ ] `obsidian/POC-Dictionaries/` notes citing phase numbers

## Open questions to settle at approval

1. **Option A vs B** for Q1 — plan recommends B (separate service).
2. Which dictionary type the shipping backend should consume in 11.3 as the demonstration
   consumer (hazard classes? vehicle types?).
3. Whether 11.4 (frontend) is in the approved scope now or parked as "extra, when reached" —
   and within it, whether the AI-translation increment is in scope (needs a Claude API key in
   the service environment).
4. Confirm/amend BR-D01…BR-D07 — in particular BR-D02's resolution of "delete entries":
   hard-delete only while unreferenced, deprecate once referenced.
5. Q6 role 3 (NATS `micro` request-reply): include the optional spike in 11.3 or drop it.
