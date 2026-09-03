---
adr: 48
title: Document Blob Storage: NATS Object Store
status: Accepted
date: 2026-08-20
scope: lab
context: organizations
decision: Compliance document bytes go in a NATS Object Store bucket per tenant account, not S3, MinIO or GCS.
why: The lab evaluates NATS patterns and tenant isolation already comes from the NATS account. The cost is real limits on size and durability, and the ADR records them.
related: [46, 47, 50]
---

# ADR-048: Document Blob Storage — NATS Object Store

**Status:** Accepted, with required amendments (see Punch List)
**Date:** 2026-08-20
**Deciders:** Jeremy (repo owner) — part of Phase 38 design review
**Related:** [ARCHITECTURE-ORGANIZATIONS.md](../Dictionary-POC/ARCHITECTURE-ORGANIZATIONS.md) §§ "Document storage — NATS Object Store," "Data sections" (Documents, GIT Certificate, Tracking Credentials); [ADR-046](ADR-046-lab-organizations-transporter-aggregate-split.md); [ADR-047](ADR-047-lab-organizations-transporter-vetting-temporal-saga.md) (compensation must be forward-only — the same constraint reappears here at the blob layer); [ARCHITECTURE-ACCOUNTS.md](../Dictionary-POC/ARCHITECTURE-ACCOUNTS.md) (tenant account resource limits)

## Context

Phase 38 (sub-phase 38c) needs real document blobs behind
`ComplianceDocument.Reference`, which today is metadata-only and says so
explicitly in the code:
`organizations-service/organizations/internal/domain/compliance_document.go:55-68`
— *"Storage is metadata-only in v1: Reference is an opaque external
locator, no file bytes are held here."* Validation is non-emptiness only.
Linebooker V2's real backend is Google Cloud Storage
(`GoogleCloudStorageServiceImpl`); the design deliberately diverges to NATS
Object Store because this is a NATS-pattern evaluation lab.

Verified starting position:

- **No Object Store usage anywhere in the repo** — zero hits for
  `ObjectStore`/`CreateObjectStore`/`ObjectStoreConfig` across all Go, and
  nothing object-store related in `demos/01-dictionary/nats/nats.conf`
  (which carries only a bare `jetstream { store_dir: "/data" }`). This is a
  first-of-its-kind introduction, like Temporal in ADR-047.
- **No blob-capable ingress anywhere in the repo** — zero hits for
  `multipart`, `FormFile`, `ParseMultipartForm`, `io.Copy`,
  `MaxBytesReader`, or any body-size cap in any `.go` file.
- **No dependency bump needed** — every module pins
  `github.com/nats-io/nats.go v1.52.0` and uses the new `jetstream` package
  exclusively (49 files; zero legacy `JetStreamContext` hits), and
  `jetstream.ObjectStore` is available there.

## Decision

**Affirm NATS Object Store over S3/MinIO/GCS** — for a lab whose explicit
purpose is evaluating NATS patterns, and where tenant isolation already
comes free from the NATS account boundary, this is the right call and this
review does not re-litigate it. **But the design doc's stated rationale is
incomplete in a way that matters:** "avoids a new infra dependency purely
for file bytes" frames the choice as cost-free. It isn't. Object Store
bytes land in the *same* per-tenant JetStream disk quota as the event log
itself, which means document uploads and event publishing now share a
failure domain they would not share under S3/MinIO. That is an acceptable
price for what the POC is testing, but it must be a *documented, bounded*
price rather than an unnoticed one. Five findings; four require amendments.

### 1. Tenant JetStream quota is shared with the event log — must fix, with real numbers

An Object Store bucket is a JetStream stream (`OBJ_<bucket>`, chunk + meta
subjects), so it draws on the tenant account's JetStream limits, which are
**already configured and tight** (verified,
`accounts-service/accounts/provisioner.go:179-184`, values at
`accounts/handler.go:344` and `cmd/main.go:238-240`):

| Account | MemoryStorage | DiskStorage | Streams | Consumers |
|---|---|---|---|---|
| platform | 1 GiB | 5 GiB | 20 | 100 |
| acme / globex / any new tenant | 256 MiB | **1 GiB** | **10** | 20 |

Two distinct consequences:

**(a) Disk: documents compete with events for 1 GiB per tenant.** When a
tenant's JetStream disk quota is exhausted, *publishes fail* — so a large
enough pile of PDFs can stop `TransporterProfile` (and `SHIPPING`) from
appending events at all. Under S3/MinIO these are independent failure
domains; here they are one. This is the actual trade-off being made, and it
is the thing the design doc's rationale omits.

**(b) Streams: the 10-stream cap is closer than it looks.** Every KV bucket
is also a stream (`KV_<bucket>`). Counting what Phase 38 implies per tenant:
`SHIPPING`, `TRANSPORTER` (new, 38a), `KV_ships`, `KV_container`, `KV_meta`,
a KV bucket for the organizations read cache (38a), `KV_organizations-secrets`
(Tracking Credentials), `OBJ_organizations-docs` (38c) — eight — **plus**
refdata-service's `REFDATA` stream and its per-context KV buckets, which
are themselves multiplied by version: `refdata-service/refdata/internal/kvstore/kv.go:125`
creates versioned corpus buckets named `{prefix}-{context}-v{n}`, one
stream each. Phase 38 could plausibly push a tenant over `MaxStreams: 10`,
and the failure mode (bucket creation refused at startup) would present as
an unrelated-looking service boot error.

**Required amendment:** (i) set an explicit `MaxBytes` on the Object Store
bucket — note that **no KV bucket in this repo sets any limit today**
(verified: all four creation sites pass only `Bucket`, one adds `TTL`;
`MaxBytes`/`MaxValueSize`/`History`/`Storage`/`Replicas` are never set
anywhere), so this is a new convention, not a copied one; (ii) enforce a
per-file size cap at the service boundary, documented, since there is no
body-size limit anywhere in the repo to inherit; (iii) count the per-tenant
stream budget against `MaxStreams: 10` before 38c and raise tenant
`DiskStorage`/`MaxStreams` if needed — there is already a runtime endpoint
for it (`POST /api/accounts/{name}/jslimits`,
`accounts-service/accounts/handler.go:288`), so this is configuration, not
new code.

### 2. Object name embeds `{filename}` — user-controlled identity, silent overwrite. Must fix

The proposed name is `{context}.transporter.{id}.{docType}.{filename}`.
Three problems, escalating:

**(a) Identity is user-controlled.** `ComplianceDocument.Reference` stores
this name, so the operator's chosen filename becomes part of the durable
key that an *immutable event* will reference forever.

**(b) Re-upload silently destroys the prior document's bytes.** Two
uploads of the same `docType` with the same filename resolve to the same
object name; an Object Store `Put` to an existing name replaces the object
(the prior revision's chunks are purged — behavior to confirm against the
current client, but this is the documented intent). So renewing a GIT
certificate named `cert.pdf` erases the expired one's bytes, while the
event log still records both uploads. **The log then asserts the existence
of a document that cannot be retrieved** — the exact class of failure
ADR-047 finding 2 rejected at the event layer, reappearing at the blob
layer. It undercuts the same auditability rationale that justified event
sourcing in the first place.

**(c) It collides with the GIT-status design — and with the existing schema.**
The design derives `GitStatus` as V2 does: the *worst* status across the
transporter's `GOODS_IN_TRANSIT` documents, **plural**. But
`compliance_documents`' primary key today is `(organization_id, type)`
(`organizations-service/organizations/internal/postgres/migrate.go:45`),
so the existing schema physically permits **one document per type** and
`document-add` is an upsert. Multi-document GIT derivation is therefore not
implementable without a `organizations` schema change — which ADR-046
promised there would be none of (see ADR-049 finding 5, where the same
promise breaks for a second, independent reason). Either the PK gains a
document ID, or GIT derivation degenerates to "the one GIT document," which
should be stated rather than discovered during 38c.

**Required amendment:** object name becomes
`{context}.transporter.{id}.{docType}.{documentID}` with a
service-minted `documentID` (UUID) — stable, collision-free, and *not*
derived from anything a user types. The original filename goes into Object
Store metadata plus the Postgres projection, never the name. Separately, a
character-legality check is worth doing regardless: KV keys in this repo
are restricted to `[-/_=.a-zA-Z0-9]` (the repo already documents this and
uses `.` instead of `:` for that reason), and real filenames contain
spaces, parentheses, and non-ASCII — verify the current client's object-name
rules rather than assuming they are permissive. Keeping filenames out of the
name makes this moot either way.

### 3. Blob/event write ordering is unspecified — must fix, and the order is forced

Nothing spans Object Store and the event stream transactionally. Only two
orders exist, and they are not symmetric:

- **Publish first, then upload** → a *dangling reference in an immutable
  log*. Unrecoverable in kind: an event cannot be retracted, only
  compensated (ADR-047 finding 2), so the projection advertises a document
  that will never download, and the fix is a second event apologising for
  the first.
- **Upload first, then publish** → an *orphan blob*. Benign: invisible to
  every reader, since nothing references it, and garbage-collectable by
  name later.

**Required amendment:** state explicitly that the blob is written first and
the event second, never the reverse. This also composes correctly with
ADR-047's dedup requirement: a Temporal Activity retry that re-runs
upload-then-publish is safe only because finding 2's stable, service-minted
object name makes the upload idempotent — with `{filename}` in the name it
would still be idempotent, but with F2's overwrite hazard intact. The two
amendments depend on each other.

### 4. Deletion vs. an append-only log — must be stated, not resolved

Objects can be deleted; events cannot. Any real erasure (operator error,
retention policy, a POPIA/GDPR-style request) leaves the log permanently
referencing bytes that no longer exist.

**Required amendment (statement, not mechanism):** for this POC, objects are
never deleted; record that as a deliberate policy with the erasure question
named as out of scope. Worth keeping for the pattern-cards doc, because the
tension resolves in an architecturally interesting direction: the blob store
is *where erasure can live precisely because the log holds references
rather than payloads*. That is the same reasoning that already put Tracking
Credentials in encrypted KV instead of the event stream — one principle,
two applications, which is a stronger finding than either alone.

### 5. Byte transport — the design assumes an ingress this service does not have

"Upload/download goes through `organizations-service` (never a raw NATS
Object Store client credential handed to the browser)" is the right
security posture and consistent with the repo's "browser never gets
`rpc.>`" rule. **But the service has no binary-capable ingress at all**, and
the design doesn't note it:

- `organizations-service`'s entire command surface is NATS `micro`
  request/reply (`internal/browserrpc/adapter.go:151-165`, fourteen
  handlers, JSON bodies), and REST was deliberately retired — *"Phase 33.5
  retired the REST half: … all fourteen /api/organizations/* routes were
  deleted outright"* (`internal/rest/handlers.go`), which now serves only
  `GET /healthz`.
- `micro` request/reply is not a streaming transport and is bounded by the
  server's `max_payload` (1 MB by default; `nats.conf` sets no override).
  File bytes cannot travel this path.
- There is no multipart handling, no `io.Copy`, and no body-size cap
  anywhere in the repo to model one on.

So 38c must **reintroduce an HTTP ingress** to a service that intentionally
removed its own, purely for blob transfer — real, currently unbudgeted
architectural work, and a reversal (narrow and justified, but a reversal)
of a prior phase's decision. Related inherent property worth recording:
Object Store has no presigned-URL equivalent, so the service is a
*mandatory* byte proxy for both directions — where an S3/MinIO design would
hand the browser a time-boxed URL and carry none of the bytes itself. That
is a genuine point in the alternative's favour, and belongs in the
pattern-cards comparison rather than being quietly omitted.

**Required amendment:** name the transport explicitly (a dedicated HTTP
upload/download endpoint on the service, with its own max-body limit and
auth), acknowledge it as a scoped, deliberate partial reversal of Phase
33.5's REST retirement, and budget it into 38c.

## Options Considered

### Option A: NATS Object Store — ACCEPTED

| Dimension | Assessment |
|---|---|
| Complexity | Medium — no dependency bump (`nats.go v1.52.0` already ships `jetstream.ObjectStore`, and the repo already uses the new `jetstream` package everywhere), but first-of-its-kind in this repo, plus a new HTTP ingress (finding 5). |
| Tenant isolation | **Best of the options** — comes free from the NATS account boundary that already isolates every stream and KV bucket. Nothing new to design, and no second credential system. |
| Operational cost | Low — no new container, no new backup target, no new credential rotation story. |
| Failure isolation | **Worst of the options** — shares the tenant's 1 GiB JetStream disk quota and 10-stream cap with the event log itself (finding 1). |
| Byte path | Service is a mandatory proxy; no presigned-URL equivalent (finding 5). |
| POC value | **Highest** — the whole point is evaluating NATS patterns, and this produces a real pattern card. |

### Option B: MinIO (or any S3-compatible) sidecar

| Dimension | Assessment |
|---|---|
| Complexity | Medium — new container, new SDK, but a very well-worn path. |
| Tenant isolation | Must be built from scratch (bucket-per-tenant or prefix + policy), duplicating what NATS accounts already give for free. |
| Operational cost | Higher — another service in `docker-compose.yml`, another credential set. |
| Failure isolation | **Best** — document bytes cannot exhaust the event log's quota. |
| Byte path | Presigned URLs remove the service from the byte path entirely, which also removes finding 5's HTTP-ingress work. |
| POC value | Low — teaches nothing about NATS, which is this lab's stated purpose. |

**Rejected**, but on honest grounds: it is genuinely better on failure
isolation and byte transport, and worse on tenant isolation and on the only
thing this repo exists to measure. The right framing is "deliberately
accepting a shared failure domain to evaluate the NATS pattern," not
"avoiding a new infra dependency."

### Option C: Keep `Reference` metadata-only (defer blobs entirely)

Leave `compliance_document.go`'s v1 comment true and let 38c slip.

**Rejected** — the vetting saga's document-review branch is the core of what
Phase 38 tests, and "approve a document" with no document is a materially
weaker exercise. Worth naming only because it is the honest fallback if
finding 5's HTTP-ingress work turns out to be larger than 38c's budget.

## Trade-off Analysis

The decision is not "which blob store is better" — Option B is better on
two real dimensions. It is "does this lab learn more from paying Object
Store's shared-failure-domain cost than from avoiding it," and for a
NATS-pattern evaluation lab the answer is clearly yes. What changes as a
result of this review is not the choice but its honesty: findings 1 and 5
convert two silent assumptions ("quota is free," "the service can already
receive bytes") into explicit, budgeted work items.

## Consequences

- 38c is larger than the design doc implies: bucket config with real limits,
  a new HTTP ingress with a body cap, a documented per-file size limit, and
  a stream-budget check against `MaxStreams: 10`.
- The Documents data-section row and the GIT-status derivation are coupled
  through finding 2c: multi-document GIT status needs a `compliance_documents`
  PK change, which is a `organizations` schema change ADR-046 said would not
  be needed.
- Two independent findings (this ADR's 2b, ADR-047's 2) now say the same
  thing at different layers: **an append-only log must never end up
  referencing state that something else can silently mutate or destroy.**
  That generalisation is the most transferable result of both reviews and
  should lead the pattern card.
- Object Store's lack of presigned URLs is a permanent property, not a POC
  shortcut — worth stating in the pattern card so the comparison is fair to
  S3.

## What Would Change This Decision

- If tenant `DiskStorage` has to be raised so far that documents plainly
  dominate the quota, the shared failure domain has stopped being a bounded
  cost and Option B's isolation is worth the extra container.
- If finding 5's HTTP ingress meaningfully re-litigates Phase 33.5's REST
  retirement (e.g. it drags business routes back with it), that is a signal
  to reach for presigned URLs (Option B) and keep bytes out of the service.
- If a real erasure requirement (finding 4) ever becomes in-scope, revisit —
  not because Object Store can't delete, but because the surrounding
  retention/audit story deserves a deliberate design rather than an
  inherited one.

## Punch List

**Must fix in the design doc / must implement correctly in 38c:**
1. [ ] Explicit `MaxBytes` on the Object Store bucket, an explicit per-file
       size cap at the service boundary, and a per-tenant stream-budget
       check against `MaxStreams: 10` (raise tenant JS limits via
       `POST /api/accounts/{name}/jslimits` if needed) — finding 1.
2. [ ] Object name becomes `{context}.transporter.{id}.{docType}.{documentID}`
       with a service-minted UUID; original filename moves to Object Store
       metadata + the projection. Verify current object-name character rules
       — finding 2a/2b.
3. [ ] Resolve multi-document GIT derivation vs. `compliance_documents`' PK
       `(organization_id, type)` — either add a document ID to the PK
       (and correct ADR-046's "zero changes" claim) or state that GIT status
       derives from a single document — finding 2c.
4. [ ] State the write order explicitly: **blob first, event second, never
       the reverse** — finding 3.
5. [ ] Name the byte transport: a dedicated HTTP upload/download endpoint
       with its own max-body limit and auth, acknowledged as a scoped
       partial reversal of Phase 33.5's REST retirement, and budgeted into
       38c — finding 5.

**Acceptable POC-scope gaps — record as deliberate, don't silently omit:**
6. [ ] Objects are never deleted in this POC; erasure/retention is named as
       out of scope — finding 4.

**Confirmed sound, no action needed:**
- Bucket naming and tenant scoping mirror the existing KV convention exactly
  (one bucket per role per account, `{context}` in the key, not the bucket
  name) — verified against `shipping-service/internal/kvstore/kv.go:64`.
- The browser never receives a raw Object Store credential; all access is
  service-mediated — consistent with the repo's `rpc.>` rule.
- No dependency change required: `nats.go v1.52.0` + the new `jetstream`
  package are already in place across all ten modules.
