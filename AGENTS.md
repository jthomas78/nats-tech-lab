# AGENTS.md

This file provides guidance to Codex (Codex.ai/code) when working with code in this repository.

## Session Memory

At the start of every session, read all files in `.Codex/memory/` — starting with `MEMORY.md` as the index — and apply them as persistent context. When saving new memories during a session, write them to `.Codex/memory/` (not `~/.Codex/projects/`) so they are shared across devices via git.

## Purpose

A lab for evaluating NATS.io patterns relevant to a V3 greenfield logistics platform. Each demo is self-contained: the user picks a demo from the lab shell, reads an intro, launches it via Docker, and tears it down when done.

The core architectural question: **what is the correct responsibility split between JetStream (event backbone), NATS KV (fast lookup/watch/cache), Postgres (transactional source of truth), and CQRS projections?**

## Repository Layout

```
nats-tech-lab/
  lab-shell/              # Vue 3 + PrimeVue + Pinia — demo menu + intro pages
  demos/
    01-dictionary/        # First demo: Dictionary POC
      backend/
        shipping-service/ # Go service (hexagonal layout)
      frontend/
        admin/            # Vue 3 demo UI
      docker-compose.yml  # Postgres + NATS + shipping-service + admin
      README.md           # Intro text shown in lab shell
```

Each demo has its own `docker-compose.yml` and does **not** share a network with the lab shell or other demos.

## Commands

### Backend (Go — `demos/01-dictionary/backend/shipping-service/`)

```bash
go build ./...

# Tests — preferred runner is Ginkgo (install once: go install github.com/onsi/ginkgo/v2/ginkgo@latest)
ginkgo ./...                    # runs suite and prints spec tree at the end
ginkgo watch ./...              # re-run on file changes

# Fallback (no install required)
go test ./...
go test ./path/to/package/...   # run a single package

docker compose up --build       # from demos/01-dictionary/
docker compose down             # tear down
```

### Frontend (Vue 3 — `demos/01-dictionary/frontend/admin/` or `lab-shell/`)

```bash
npm install
npm run dev
npm run build
```

## Demo 01 — Dictionary POC

### What it demonstrates

Two side-by-side shapes for serving dictionary/reference data (dropdowns, enums, locale config, CQRS read-model lookup):

- **Shape A — KV as read model**: JetStream event handlers project directly into NATS KV; reads go straight to KV with no Postgres read table.
- **Shape B — KV as cache in front of Postgres**: canonical CQRS projection in Postgres; KV is an eager write-through cache — the same JetStream event handler that upserts Postgres also overwrites the KV entry; cache miss falls through to Postgres.

### Stream / KV design

```
Stream:   DICTIONARY
Subjects: DICTIONARY.entry.created, DICTIONARY.entry.updated
Retention: LimitsPolicy (enables replay — NOT InterestPolicy)

KV buckets: dict-a-{context} (Shape A read model), dict-b-{context} (Shape B cache)
Key format: {entityType}.{id}   — NATS KV keys only allow [-/_=.a-zA-Z0-9]; ':' is illegal
Value: JSON-encoded DictionaryEntry
```

### Backend package layout

```
cmd/main.go                       # bootstraps monolith, calls Startup on each module
internal/monolith/                # Monolith + Module interfaces
internal/jstream/stream.go        # JetStream wrapper (LimitsPolicy)
internal/kvstore/kv.go            # NATS KV wrapper
dictionary/
  composition.go
  internal/
    domain/                       # DictionaryEntry entity, events, repo interface
    application/commands/         # CreateEntry, UpdateEntry
    application/queries/          # GetEntry (Shape A: KV; Shape B: KV→Postgres)
    postgres/                     # repo impl + migration (Shape B only)
    eventhandler/                 # JetStream consumer → projects into KV
    rest/                         # HTTP handlers
```

## Architectural Notes

- **Hexagonal layout** throughout the Go backend: domain has no framework deps; adapters (postgres, rest, eventhandler) live in their own packages and wire in via `composition.go`.
- **Pinia stores** in the frontend are an intentional analogue to server-side materialized views — both are projected read models derived from an event source. This parallel should be preserved in UI and docs.
- **LimitsPolicy** (not InterestPolicy) on JetStream streams — required to support event replay.
- **Context-scoped KV keys**: every lookup includes a tenant/region/locale prefix — no global unscoped lookups.
- The demo frontend updates reactively via KV watch → SSE (or WebSocket) → frontend panels.

## Quality Rules

These apply to every task — new features, changes, and bug fixes alike:

1. **Every business rule must have a test.** If a domain rule is added or changed, a corresponding integration test must be added or updated in the same task before marking it complete.
2. **All tests must be green before a task is complete.** Run `ginkgo ./...` from `demos/01-dictionary/backend/shipping-service/` as the final step of any backend change. A task is not done until this passes clean.
3. **Business rules live in the domain layer** (`dictionary/internal/domain/`). Rule enforcement must not leak into handlers or application services.
4. **The business rules summary must be kept in sync.** When a rule is added or removed, update `demos/01-dictionary/BUSINESS_RULES.md` as part of the same task.

## AI Agent Workflow

Any exploration touching more than 3 files should be delegated to an Explore subagent rather than done with inline `Read`/`grep` calls — keeps the main conversation's context window free for the actual task.

When updating or implementing a plan phase, the agent should follow this sequence:

1. **Ask for business rules first.** Before writing any code or updating a plan, ask the user to confirm or supply the applicable business rules for the feature. If rules are already in `BUSINESS_RULES.md`, confirm they are complete and up to date.
2. **Derive tests from rules, not from implementation.** Each business rule maps to one `Context` block in Ginkgo with one or more `It` assertions. Write the specs before writing the implementation (red → green → refactor).
3. **Update `BUSINESS_RULES.md` and the plan together.** New rules go into `BUSINESS_RULES.md` and the plan checklist in the same commit.

## AI Skill Roles (Future)

Skill files (`.Codex/skills/`) will be introduced to give the AI agent specialised personas for different stages of delivery. Planned roles:

| Skill | Responsibility |
|---|---|
| `product-owner` | Captures business requirements; challenges scope; owns acceptance criteria |
| `technical-analyst` | Translates requirements into technical consequences; identifies constraints and risks |
| `software-developer` | Implements the requirement; follows the red→green→refactor cycle |
| `tester` | Validates correctness against the business rules; owns the Ginkgo spec tree |

These skills are **not yet implemented**. The note is here to record the intent: as the lab grows, plan phases should be walked through each role in sequence rather than going directly from requirement to code.

## Implementation Status

See `.Codex/plans/Dictionary-POC-Plan.md` for the full phased plan and checkbox tracking. Current branch `poc/dictionary` is at Phase 0 (scaffolding not yet started).
