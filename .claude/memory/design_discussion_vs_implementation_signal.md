---
name: design-discussion-vs-implementation-signal
description: This user runs multi-round design discussions (propose, question, revert, revise) before signaling readiness to implement — treat every design statement as provisional until an explicit go-ahead
metadata:
  type: feedback
---

When this user raises an architecture/design change, don't start planning or editing code on the first statement of intent — keep responding with clarifying questions, tradeoff write-ups, and confirmations until they explicitly signal readiness ("let's plan", "let's implement", "let's plan now", or a direct "yes, do it").

**Why:** During the 2026-07-22/23 subject-taxonomy session, the user proposed a full redesign, then iterated through several follow-up rounds (region vs. tenant semantics, tenant-in-subject-vs-header, NATS Accounts implications) and at one point explicitly reversed a requirement mid-thread ("I'm reverting the cardinality reduction requirement"). Only after that did they say "Let's plan now." Acting on an earlier statement would have built the wrong thing at least twice over (an id-in-header design that was later reverted, and a context-first token order that turned out to violate a JetStream server constraint discovered only during implementation).

**How to apply:** Treat "what do you think" / "give me feedback" / open-ended design questions as analysis-only turns — answer with a recommendation plus the main tradeoff, don't reach for `EnterPlanMode` or `Edit`. Reserve implementation for an explicit signal. Also: when presenting `AskUserQuestion` choices with a clearly labeled "(Recommended)" option, this user tends to accept it across the board (confirmed repeatedly in this session) — invest real effort in making the recommended option genuinely correct, since it's the one likely to ship.

**Reconfirmed 2026-08-17 (REST→NATS transport consolidation thread):** proposed dropping the `.v1` subject-version suffix as its own mechanical phase, complete with a full blast-radius analysis (~288 refs, trust-chain regeneration plan) and the user's initial "happy for this to be its own mechanical phase" go-ahead — then one turn later said "Please drop my suggestion to remove the suffix" and reverted it entirely, no design behind the reversal beyond wanting versioning kept as a wholly separate future exploration. Nothing had reached disk, so the revert was free — but it's the same lesson again: an apparent go-ahead earlier in a design thread is not load-bearing until the thread stops iterating. See [rest_nats_transport_consolidation](rest_nats_transport_consolidation.md) for the resulting decision (subject stays versioned; versioning strategy is out of scope until the user raises it again).
