# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Investigation Context

This folder contains research and design exploration for the **Dictionary / Reference Data layer** in the V3 Linebooker logistics platform greenfield architecture. The investigation focuses on:

- How to model application dictionary data (UI dropdowns, enums, i18n, locale-specific values, tenant config)
- The role of NATS components: **JetStream (event backbone) + KV (cache/distribution) + Postgres (source of truth)**
- CQRS and event-sourcing patterns for reference data
- Multi-tenancy and multi-region strategies for NATS in a logistics SaaS context

**Not included here:** implementation code, tests, or build artifacts. This folder contains analysis notes and design decisions only.

---

## Folder Structure

```
Dictionaries/
├── Dictionary - Problem Statement.md          # Clarification questions and working assumptions
├── Dictionary - Research - Claude.md          # Verified research findings (adversarial-reviewed)
├── Dictionary - Research - OpenCode.md        # OpenCode's research findings (separate)
├── Design - Event Sourcing + JetStream + KV Stores.md  # Architectural design patterns
├── _raw/                                      # Raw research data, discussion transfers, prompts
└── CLAUDE.md                                  # This file
```

**Important notes:**
- Keep Claude and OpenCode research findings in separate files. Do not merge them; they represent independent perspectives on the same questions.
- The `_raw/` folder contains raw/external content for reference only. Only access and use these files when explicitly specified in the prompt.

---

## Key Research Findings

### Multi-Tenancy Subject Patterns

For multi-tenant systems (identifier-first subjects):
```
{tenant}.{region}.{bounded_context}.{event}
```

**Decision:** Use **subject-prefix isolation within one NATS Account** for soft tenancy (simpler operations), or **one NATS Account per tenant** for hard isolation (stronger security).

### Dictionary Data Storage — Two Models

**Model 1: NATS KV as the read model** (event handlers → KV directly, no Postgres read table)
- Faster but less resilient
- Useful for stateless reference data

**Model 2: NATS KV alongside Postgres** (Postgres is canonical, KV is derived cache)
- Postgres is source of truth; KV is distribution layer
- Better for governed data with audit/approval workflows

**Recommended assumption:** Postgres remains source of truth; NATS KV is a cache/watch distribution layer.

### Event Sourcing Architecture

- **JetStream Streams** = immutable event log (system of record)
- **JetStream Consumers** = track processed events and enable replay
- **KV Buckets** = entity snapshots (avoid replaying all events)
- **Services load snapshots from KV, replay deltas, emit new events**

---

## When Adding to This Investigation

1. **New research findings:** Add to the appropriate `Dictionary - Research - *.md` file (Claude or OpenCode)
2. **Design refinements:** Update `Design - Event Sourcing + JetStream + KV Stores.md` with proven patterns
3. **Clarifications needed:** Add to or reference [[1. Problem Statement - Dictionary]] clarification questions
4. **Raw data, discussions, prompts:** Keep in `_raw/` folder to preserve traceability

---

## Related Investigations & Memory

- [[NATS Event Sourcing Architecture]] — persisted understanding of JetStream + KV for event sourcing
- [[NATS Dictionaries research page split by tool]] — note about keeping Claude and OpenCode findings separate
- **Problem context:** V3 greenfield logistics platform; this folder explores the reference data layer specifically

---

## Obsidian Markdown Conventions for This Vault

- Use `[[Note Name]]` wikilink syntax for internal references
- Always leave a blank line before Markdown tables (Obsidian rendering requirement)
- Use `$...$` for inline math and `$$...$$` for display math (not `\(...\)`)
- All files use Obsidian Flavored Markdown (OFM)

---

## Research Method (for context)

The research in this folder uses adversarial verification:
1. Multiple search angles to surface claims
2. Sources fetched and claims extracted
3. Top claims put through multi-agent refutation panel (need 2/3 votes to refute)
4. Findings tagged **[Verified]** (survived panel) or **[Reported]** (not yet verified but worth weighing)

When reading findings, weight **[Verified]** claims more heavily but still check the original source.
