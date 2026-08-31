# Phase 8c manifest drift (2026-08-31)

- `REGISTRY_FETCH_ORIGINS` maps allowlisted browser origins to service origins as JSON;
  Compose covers 7111–7115. No fallback to browser localhost, no new allowlist grant.
- Checks preload provenance only (first accepted audit actor), not every static entry.
  Existing manually seeded rows may still say curated even if present in registry.json.
- `domain.CompareManifest` ignores platform state, names differing top-level fields;
  invalid/unknown fields are not checked, including nested unknowns.
- `manifesthttp` GETs `/manifest.json`: 2s, 1 MiB, no redirects or environment proxy.
  Application worker retries once after 200ms, polls 1 minute after each serial pass.
- Observations live only in memory. Failure replaces success immediately; edits invalidate
  stale observations. No entry/revision/audit/KV/notify writes from the checker.
- Startup now takes the service lifetime; its own 60s setup context is separate. Stop
  cancels and joins the worker. Do not pass main's startupCtx to this long-lived worker.
- `EntryView.Drift` is Admin-only; Manifest stays separate from Source and State.
  Refresh observations reads NATS snapshot only; saveDraft strips the derived field.
- Verification: registry 221/221, 0 Skipped, also race-clean; Admin 330/330, lint/build green.
