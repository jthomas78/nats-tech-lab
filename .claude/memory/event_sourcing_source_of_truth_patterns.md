---
name: event-sourcing-source-of-truth-patterns
description: Two source-of-truth patterns (Postgres+outbox vs JetStream-as-truth) and why this POC picked B; Obsidian Patterns-folder notes/PDFs exist to document both
metadata:
  type: project
---

# Event sourcing source-of-truth patterns (concluded in a claude.ai chat, 2026-07-12)

## The two patterns

**Pattern A — Postgres as source of truth:** events table with `UNIQUE (aggregate_id, version)` for optimistic concurrency; outbox row written in the same transaction; relay publishes to JetStream; projections consume from the stream. NATS is transport-only. Queryable, transactionally-consistent truth natively.

**Pattern B — JetStream as source of truth:** append with `Nats-Expected-Last-Subject-Sequence` for concurrency; projections are durable consumers; a raw-event projection into Postgres provides queryability/audit as a derived, eventually-consistent, read-only copy.

**The asymmetry (core insight):** the outbox (A) and the raw-event projection (B) are the *same bridge built from opposite banks* — each pattern bolts on exactly the component the other gets natively. A gets SQL queryability for free and buys distribution via outbox; B gets distribution for free and buys queryability via projection. The real difference is *where transactionally-consistent, ad-hoc-queryable history lives*: the source itself (A) vs a lagging derivative (B).

**Decision rule:** A optimizes for transactional consistency and native SQL queryability; B optimizes for streaming-native distribution and is the only pattern that actually tests JetStream as an event store. For this repo's POC goal ("exercise NATS as a fundamental choice"), **B is the pattern that answers the question** — but building a thin slice of both behind the same event envelope is the cleanest way to feel the trade.

Pattern B footguns to keep load-bearing in the demo: stream retention must never discard (LimitsPolicy with no limits that erode truth), no cross-aggregate transactional write (→ sagas), per-subject ordering only, KV/projection idempotency (version-guard KV writes against stale redelivery).

## Obsidian vault artifacts (all done, 2026-07-12/13)

The `Distributed Systems/Patterns/` folder in the user's Obsidian vault now holds, all orbiting the pre-existing `Event Sourcing.md` deep-dive: a `###` section appended to `Event Sourcing.md` itself; a POC-tied `— Cheatsheet.md` (7 decisions incl. this A/B choice, Mermaid trees, POC-map table); a theory-only, zero-repo-references `— Decision Patterns.md` (same 7 decisions, generic examples); and 4 dark pattern-card-style PDFs (2 POC, 2 theory — booklet + poster each), built as self-contained HTML rendered via Chrome headless print-to-pdf. Vault convention: `###`/`####` headings, `[[wikilinks]]`, no callouts, ASCII ` ```text ` diagrams (no Mermaid pre-existing, though the new cheatsheet introduced it deliberately). Match these conventions if editing that folder again — see the vault directly for current contents rather than re-deriving from this note.

Related: [[shipping-domain-overview]]
