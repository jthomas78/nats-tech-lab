# Phase 38c-ii — NATS Object Store document files

Implemented 2026-08-20 in `trading-partner-service` against BR-TP40–45. First
use of NATS Object Store in this repo.

## Facts about Object Store worth not rediscovering

- **An Object Store bucket *is* a JetStream stream** (`OBJ_<bucket>`), so it
  counts against the tenant account's `MaxStreams` and shares its
  `DiskStorage`. A default tenant gets 1 GiB / 10 streams
  (`accounts-service/accounts/handler.go`'s `defaultJSLimits`), so **document
  bytes and the event log compete for the same 1 GiB** — an uncapped upload
  path can stop event publishing for the whole tenant. Hence `MaxBytes`
  256 MiB on the bucket plus a 10 MiB per-file cap.
- **Budget measured, not assumed** (live stack, 2026-08-20): ACME held 5
  streams and GLOBEX 3, so the new bucket took them to 6 and 4 — no
  `/jslimits` raise needed. ADR-048's fear that refdata's per-context
  *versioned* KV buckets would exhaust the tenant budget is **wrong**: those
  buckets live in **PLATFORM** (limit 20, then at 9), not in tenant accounts.
- **Object Store has no presigned-URL equivalent**, so the service is a
  *mandatory* byte proxy both ways. That is permanent, and the honest point in
  S3/MinIO's favour for the pattern-cards comparison — along with failure
  isolation, since S3 would not share a failure domain with the event log.
- `max_payload` is **unset** in `demos/01-dictionary/nats/nats.conf`, so it is
  the server default 1 MiB. That is why bytes cannot ride the `micro`
  request/reply command surface at all and needed an HTTP ingress.

## Design rules that came out of it

- **Blob first, record second — forced, not preferred.** Record-then-store
  leaves a projection (and upstream an *immutable* log) asserting a document
  whose bytes were never written; an event can only be compensated, not
  retracted. Store-then-record leaves at worst an orphan object: invisible to
  readers, addressable by name, harmless. Verified live — an over-limit upload
  left a 10 MiB orphan and a file-less document, exactly the intended trade.
- **Bytes are write-once.** No replace: overwriting would purge bytes the log
  references. Correction goes through supersede-and-replace (BR-TP30), which
  is also why objects are never deleted and why `GetDocument` returns
  superseded rows that `ListDocuments` excludes.
- **Object names are entirely service-minted** —
  `{context}.transporter.{id}.{docType}.{documentID}`, mirroring the KV key
  convention. The uploaded filename is deliberately absent: a user-controlled
  name makes object *identity* user-controlled, and two uploads sharing a name
  would collapse to one object.
- File metadata is **projected into Postgres** (five nullable `file_*`
  columns), so the listing path never touches the bucket — despite the plan
  claiming this sub-phase "touches no schema".
- Transport is the **raw request body plus headers**, not
  `multipart/form-data`: one field, so an envelope would buy nothing and cost
  a parser on both ends. Filenames cross percent-encoded, since header values
  are ASCII and real filenames are not.

## Gotchas

- **nginx defaults `client_max_body_size` to 1 MiB**, so a frontend proxy
  silently 413s a legitimate upload before the service is consulted. Set it on
  any byte route (`frontend/refdata/nginx.conf`), plus
  `proxy_request_buffering off` to stream rather than spool.
- The byte routes are the first HTTP business-adjacent surface since Phase
  33.5 retired REST, so `internal/rest`'s BR-TP17 allowlist test has to be
  widened deliberately. Keep the route list **literal** in the test rather
  than deferring to the production constant, or the allowlist can grow
  silently — which is the one thing that test exists to prevent.

Auth for the ingress is its own record: [[nats_account_is_the_only_authn]].
Related: [[phase38b_transporter_vetting]], [[phase38di_transporter_ui]],
[[rest_nats_transport_consolidation]], [[phase34_boundary_enforcement]].
