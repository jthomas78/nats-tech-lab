Findings write-up for Phase 11 — Dictionary as a Service, closing out the investigation started in [[Problem Statement - Dictionary]]. Implementation detail lives in the repo (`ARCHITECTURE.md` § "Reference Data Service", `BUSINESS_RULES.md` § "Reference Data Service"); this note is the stakeholder-facing *why* and *what we learned*.

## Summary

The dictionary/reference-data question from the original problem statement is answered by a working, tested implementation: a **separate service** (`refdata-service/`), plain Postgres CRUD (not event-sourced), with localization, typed cross-references, and a versioned NATS-KV cache protocol — plus a UniFi-style admin frontend and a live cross-service consumer demo against the existing shipping backend. All four sub-phases (11.1–11.4) shipped; 11.5 (this note + the ports-migration question below) closes the phase.

## Build vs buy (Q4)

**Conclusion: build, don't buy — for this platform, at this stage.**

Commercial RDM/MDM products (Informatica Reference 360, TIBCO EBX, Semarchy xDM, Profisee, Stibo STEP) exist and are mature, but they solve a **governance problem** — stewardship workflow, multi-system approval chains, crosswalk management across many *external* consuming systems — that this platform doesn't have yet. What they don't provide at all is this investigation's actual question: a **NATS-KV distribution/cache-coherence layer** for reference data inside an event-driven architecture. Buying one of these products now would mean paying enterprise licensing for governance ceremony while still having to build the KV distribution layer ourselves.

The lighter-weight alternative worth remembering if a future team wants an admin-UI shortcut: **Directus**, a database-first headless CMS that can point at existing Postgres lookup tables and auto-generate REST/GraphQL + admin UI + translations. Not a substitute for the KV protocol, but a credible shortcut if `frontend-dict/`-style hand-built admin UIs stop being worth maintaining.

**Re-evaluate this decision only if** multi-system stewardship workflows become real — i.e., if reference data starts being edited/approved by parties outside this platform's own admin UI, with audit and rollback requirements this POC deliberately didn't build (per the original problem statement's working assumption).

## Versioned-cache protocol result (Q5)

**Conclusion: yes, and it was the most valuable part of the exercise — confirmed, not just assumed.**

The build validated the whole hypothesis end-to-end, including catching a real bug during verification: a nil-interface (`domain.ChangeNotifier`) gotcha where a typed-nil `*kvcache.Projector` passed into a plain interface field would have silently *not* been nil, causing a panic when NATS was unavailable in tests — fixed by explicitly zero-valuing the interface variable rather than relying on the pointer being nil. That kind of thing is exactly what a POC is for.

What the protocol demonstrates, concretely:

- **Postgres stays authoritative.** Every mutation (item create/deprecate/delete, localization upsert, reference create) atomically bumps a per-`{context, type}` set version, then rebuilds the affected item's KV cache entry and the type's `_meta` entry, then publishes a *pointer* change-event (never state) on a bounded `REFDATA` stream.
- **The cross-service consumer demo is real, not simulated.** The shipping backend's `internal/refdataconsumer` package reads the `refdata-{context}` KV bucket directly over the shared NATS connection — zero dependency on refdata-service's Go code, only on a bucket-naming and JSON-shape convention, exactly as two independent platform services would coordinate for real. Verified live via Docker: a hazard-class lookup returns `source: "kv-cache"` on a hit, and version-mismatch detection was proven with a dedicated test that manually desynchronizes an entry's stamped version from `_meta` to force the "updatable read on version mismatch" path.
- **The cache status widget makes the protocol visible**, not just correct — `frontend-dict/`'s widget shows Postgres's version side by side with the KV cache's version and an "in sync"/"stale" tag, live via the same KV-watch → SSE pattern the rest of the lab already uses.

**Honest caveat kept from the original design write-up:** there's a small window between "Postgres commit" and "KV cache entry updated" where a reader could see stale state — the same eventual-consistency window Shape B already documented for the shipping domain. The version protocol is what makes that window *detectable* by a consumer instead of silently wrong; it doesn't eliminate the window itself.

## Ports registry — not migrated (decision + rationale)

The Phase 9.5 ports registry (a plain Postgres table in the shipping backend, used for BR-017/BR-018 existence checks on every ship arrival and container registration) was evaluated for migration into the new dictionary service and the answer is **no, not now** — a deliberate decision, not an oversight.

**Why not:** ports are checked synchronously on the shipping backend's hot write path, inside the same request that hydrates, validates, and publishes a domain event. Moving that check to refdata-service would turn a fast in-process Postgres query into a cross-service call on every single command — adding latency and a new failure mode (ship arrivals blocked if refdata-service is down) to a part of the system that Phases 8–14 have deliberately kept fast and self-contained. That's a real architectural cost, not a style preference.

**If this is ever revisited:** the right pattern is already built and proven — consume ports the same way the hazard-class demo consumes reference data, via the KV-cache-plus-REST-fallback pattern (`internal/refdataconsumer`), not a synchronous call per command. That would keep the hot path fast (KV read) while still centralizing port master data, and would be a natural follow-up once/if UN/LOCODE-standard port codes become a real requirement (the current registry is free-text names, not LOCODE-validated).

## Stakeholder summary

- The core architectural question — "does reference data belong as a service with its own KV-distributed cache, and is that worth building over an off-the-shelf product" — has a confirmed **yes** and **build**, with working code and live-verified tests to back both answers, not just a design document.
- Scope explicitly **not** built in this pass: AI-assisted translation drafting (BR-D07's review-gate rule is confirmed and ready, but the endpoint/UI is parked) and the NATS `micro` request-reply spike (Q6 role 3) — both flagged for a future pass if they become real requirements, not silently dropped.
- The ports registry stays where it is, on purpose, for a documented latency/availability reason — this is the kind of decision a future team revisiting this code needs to see reasoned out, not rediscover by tripping over it.
