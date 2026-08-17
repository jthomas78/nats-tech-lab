---
name: phase34-boundary-enforcement
description: IMPLEMENTED 2026-08-17 — mux allowlist tests (BR-040) + traceSpan.Requester (BR-041) + Admin UI two-axis filter; enforces what Phase 33 only achieved
metadata:
  type: project
---

Phase 34 closed the gap Phase 33 left open: nothing stopped a *future*
business REST route being added back to any service's `rest/handlers.go`.

**Mechanism (BR-040):** every service's `Mount` function now returns
`[]string` — the exact "METHOD /pattern" strings it registers, including
bare-prefix mounts like `/swagger/`. A test per service asserts that
returned list `ConsistOf` a hardcoded allowlist (exact match, not subset —
catches both an added and a removed route). accounts-service has two
independent `Mount`s (`accounts/handler.go`, `auth/handler.go`), so two
tests. 7 tests total across 6 services.

**Requester attribution (BR-041):** `Nats-Requestor` (BR-027) was already on
the wire as a header but only reachable by parsing `headers` by hand.
`traceSpan` (BR-036's envelope, `internal/natstrace`, duplicated across
shipping/refdata/pricing/trading-partner/accounts-service — see
[[tenants_manager_triplication]] for the sibling duplication-extraction
context) gained a `Requester string` field, populated in `finish()` via a
small `firstHeaderValue` helper. Additive/`omitempty`, no wire-contract
break. **Still never read for authorization anywhere** — confirmed by grep
before and after.

**Admin UI:** `TraceWaterfall.vue`'s toolbar gained two new filter inputs
next to the existing free-text search — subject-prefix (shield icon,
"server-enforced" — matches BR-D41's `api.*.refdata.admin.*` split, which
the NATS server itself polices) and requester (person icon,
"self-declared" — substring match on the new `Requester` field). Live
browser-verified against real traffic: subject-prefix filter narrowed
186→29 traces, requester filter narrowed 186→34.

**Test-suite audit (34.5):** grepped every `*_test.go` across all 6
services for HTTP calls to non-allowlisted routes. Zero violations. Two
harmless mentions found (a stale-rename comment, a doc-comment path
example) — not live calls.

**Delivery mechanic worth remembering:** parallelized across 6
worktree-isolated agents (one per service) + a research/audit pass. Every
worktree started from an unrelated stub commit, not the branch tip — each
agent had to `git reset --hard`/`checkout` the real branch content before
editing. Since edits were left uncommitted in each worktree, merging back
meant `git diff` + `git apply` per worktree (4 of 6 had clean scoped
diffs) plus direct file copies for new test files and for the one worktree
(pricing) whose `git status` showed the entire repo as "added" after a
`checkout branch -- .` — copying the known changed files directly by path
sidestepped that noise entirely. Full detail:
[Main-POC-Plan-ARCHIVE.md](../plans/Main-POC-Plan-ARCHIVE.md)'s Phase 34
section.
