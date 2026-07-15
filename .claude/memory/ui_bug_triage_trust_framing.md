---
name: ui-bug-triage-trust-framing
description: When the user reports a bug as "the UI" doing something wrong, prioritize frontend investigation over backend/persistence audits
metadata:
  type: feedback
---

When diagnosing a bug the user describes as happening in a specific UI (e.g. "There's an issue on the Dictionary UI"), don't default to auditing the backend/persistence layer as the first hypothesis — even when a plausible backend story exists (e.g. "maybe the transaction that clears the old default isn't atomic").

**Why:** During the Phase 11.9 locale-radio bug (both `en` and `es` radios showing checked), the first response was to dispatch agents to audit the Postgres set-default transaction and the REST response shape. The user then said "Please not this is a UI change requirement" — the backend was already correct (an atomic `UPDATE ... SET is_default=false` + upsert in one transaction), and the actual bug was a frontend rendering issue: each PrimeVue `RadioButton` in the `v-for` kept its own independent local `checked` state (see [primevue-radiobutton-group-for-shared-state](primevue_radiobutton_group_for_shared_state.md)), not a backend data problem.

**How to apply:** When a user names the surface where a bug is observed (UI, a specific page, a specific panel), treat that as a strong signal about where the root cause lives, not just where the symptom is visible. It's fine to spin up a background agent to *confirm* backend correctness in parallel (cheap insurance), but the primary investigation thread — and where code gets read/edited first — should start at the layer the user named. Don't let "there's a plausible backend explanation" override that framing without evidence.
