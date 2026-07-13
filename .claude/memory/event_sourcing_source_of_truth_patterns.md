---
name: event-sourcing-source-of-truth-patterns
description: Two source-of-truth patterns (Postgres+outbox vs JetStream-as-truth) concluded in a claude.ai chat, plus pending task to append a section to the user's Obsidian Event Sourcing note
metadata:
  type: project
---

# Event sourcing source-of-truth patterns (carried over from claude.ai chat, 2026-07-12)

The user had an extended architecture discussion in the Claude web app and transferred the context here. Key conclusions:

## The two patterns

**Pattern A — Postgres as source of truth:** events table with `UNIQUE (aggregate_id, version)` for optimistic concurrency; outbox row written in the same transaction; relay publishes to JetStream; projections consume from the stream. NATS is transport-only. Queryable, transactionally-consistent truth natively.

**Pattern B — JetStream as source of truth:** append with `Nats-Expected-Last-Subject-Sequence` for concurrency; projections are durable consumers; a raw-event projection into Postgres provides queryability/audit as a derived, eventually-consistent, read-only copy.

**The asymmetry (core insight):** the outbox (A) and the raw-event projection (B) are the *same bridge built from opposite banks* — each pattern bolts on exactly the component the other gets natively. A gets SQL queryability for free and buys distribution via outbox; B gets distribution for free and buys queryability via projection. The real difference is *where transactionally-consistent, ad-hoc-queryable history lives*: the source itself (A) vs a lagging derivative (B).

**Decision rule:** A optimizes for transactional consistency and native SQL queryability; B optimizes for streaming-native distribution and is the only pattern that actually tests JetStream as an event store. For this repo's POC goal ("exercise NATS as a fundamental choice"), **B is the pattern that answers the question** — but building a thin slice of both behind the same event envelope is the cleanest way to feel the trade.

Pattern B footguns to keep load-bearing in the demo: stream retention must never discard (LimitsPolicy with no limits that erode truth), no cross-aggregate transactional write (→ sagas), per-subject ordering only, KV/projection idempotency (version-guard KV writes against stale redelivery).

## Obsidian note append — DONE (2026-07-12)

Appended a **"Source-of-Truth Patterns: Postgres Event Store vs JetStream"** `###` section to the user's Obsidian note at `.../Distributed Systems/Patterns/Event Sourcing.md`. Adapted to the note's conventions: no callouts (the note uses none), `###`/`####` headings, real internal links to existing sections (`#Mapping NATS JetStream...`, `#Invariants Spanning Two Aggregates`, `#Can the Write Model Read the Read Model?`). Introduced one new unresolved wikilink `[[Transactional Outbox]]`.

Note on that vault: it uses `###`/`####`/`#####` headings, `[[wikilinks]]` including `[[#section]]` links, ` ```text ` flow-diagram blocks, standard MD tables — and **no callouts**. Match this if editing it again. Existing notes in the Patterns folder: `Event Sourcing.md`, `Transactional Outbox.md`, `Compensating Transaction.md`, `UI Patterns for Eventual Consistency.md` (so those wikilinks resolve). No Mermaid used elsewhere in the vault (ASCII `text` blocks instead).

## Cheatsheet created — DONE (2026-07-12)

Created `.../Patterns/Event Sourcing + CQRS + NATS — Cheatsheet.md` — a scannable "which choice, when" companion to the deep-dive `Event Sourcing.md`. Seven decisions (ES-vs-CRUD, source-of-truth A/B, read-model shapes A/B/C, cross-aggregate one-vs-two-stream, surrogate-vs-natural key, write-side safety, projection hardening), each tied to the POC's shapes/phases, with Mermaid decision trees for the big forks + a "POC map" table (decision → phase → done/planned status). First Mermaid use in this vault — deliberate, to serve the "decision dialog" ask.

## Visual PDF cheatsheets created — DONE (2026-07-12)

Two dark, design-pattern-style PDFs in the vault Patterns folder, linked from the top of the Markdown cheatsheet:
- `Event Sourcing + CQRS + NATS — Pattern Cards.pdf` — 11-page portrait booklet, one GoF-style pattern card per decision (family tag, intent, decision question, bespoke inline-SVG diagram, forces table, "POC verdict" stamp with phase) + cover + snapshots + gotchas/POC-map pages.
- `Event Sourcing + CQRS + NATS — Poster.pdf` — 2-page landscape: page 1 = 7-decision grid + families legend, page 2 = snapshots + NATS role-mapping + pitfalls row + 11-card POC-map.

Built as self-contained HTML (UniFi dark tokens: `#DEE0E3`/`#B7BCC2` text, `#3b8dff`/`#26d0e6` accents, per-family colors) rendered via **Chrome headless** `--headless=new --no-pdf-header-footer --print-to-pdf` (Chrome 150 on this machine; no wkhtmltopdf/weasyprint/qpdf installed). HTML sources in this session's scratchpad. To regenerate: edit HTML, re-run the Chrome print-to-pdf command, `cp` into the Patterns folder. Verification trick used: `--screenshot` with `--window-size` + a node one-liner injecting `.page:nth-child(...)＝display:none` to isolate specific pages.

## General (theory-only) pattern note created — DONE (2026-07-12)

Created `.../Patterns/Event Sourcing + CQRS + NATS — Decision Patterns.md` — an **implementation-agnostic** GoF-style pattern-card playbook (7 numbered decision cards: Replayability Test, Single Writer of Record, Derived Read Model, Invariant Across Boundaries, Stable Identity, Guarded Append, Idempotent Projector + Snapshots + Awareness cross-cutting). Each card: Intent / Decision / Forces / Options / Consequences / Default, Mermaid trees for the forks. **Zero repo references** — verified by grep for Ship/Container/Port/BR-/Phase/Shape/nats-tech-lab/etc. (used generic Order/Account/Subscription examples). Cross-linked bidirectionally with the POC-specific `— Cheatsheet` note. This is the sibling to the POC-tied cheatsheet: same decisions, general framing.

Three Obsidian pages now exist in the Patterns folder for this topic: `— Cheatsheet` (POC-tied), `— Decision Patterns` (theory-only), and the PDFs; all orbit the `Event Sourcing.md` deep-dive.

## Theory PDF versions created — DONE (2026-07-13)

Rendered the theory-only `— Decision Patterns` note into the same dark pattern-card PDF format as the POC ones: `Event Sourcing + CQRS + NATS — Decision Patterns (Cards).pdf` (10-page booklet, GoF pattern-named cards with "Default" rule-of-thumb footers instead of POC-verdict stamps) + `... (Poster).pdf` (2-page landscape, decision grid + at-a-glance). Built by splicing the theory body onto the *exact* `<head>`/`<style>` spliced from the POC `booklet.html`/`poster.html` (node one-liner: `src.slice(0, indexOf("<body>")+6)` + new body) so styling is pixel-identical. Verified no repo refs, code-comment overflow fixed, poster page-2 collapsed to an 8-col single row to fit. Linked from the top of the `— Decision Patterns` note. Four PDFs total now in the Patterns folder (2 POC + 2 theory).

Related: [[shipping-domain-overview]]
