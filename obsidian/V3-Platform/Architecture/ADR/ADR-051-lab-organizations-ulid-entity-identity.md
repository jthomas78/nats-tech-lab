---
adr: 51
title: Entity Identity Is a ULID, Not a UUID
status: Accepted
date: 2026-08-24
scope: lab
context: organizations
decision: organizations-service mints 26-character ULIDs in Go. ID columns are TEXT with no default. shipping-service and accounts-service stay on UUID.
why: IDs sit in NATS subject tokens and KV keys, so they must be subject-safe and immutable. ULIDs sort by time and never contain a dot.
related: [46, 49]
---

# ADR-051: Entity Identity Is a ULID, Not a UUID — and Not a Country-Prefixed Registration Number

**Status:** **Accepted 2026-08-24** — ULID, scoped to `organizations-service`. `shipping-service` and `accounts-service` stay on UUID for now (see "Scope and what was deliberately left alone").
**Date:** 2026-08-24
**Deciders:** Jeremy (repo owner)
**Related:** [ADR-046](ADR-046-lab-organizations-transporter-aggregate-split.md) (the aggregate split that made `TransporterProfile`'s id a subject token); [ADR-049](ADR-049-lab-organizations-cross-aggregate-concurrency.md) (per-subject optimistic concurrency, which is keyed on that token); [ARCHITECTURE-COMMUNICATIONS.md](../Dictionary-POC/ARCHITECTURE-COMMUNICATIONS.md) § 2 (subject families, fixed arity, positional parsing); [ARCHITECTURE-ORGANIZATIONS.md](../Dictionary-POC/ARCHITECTURE-ORGANIZATIONS.md) § "Entity identity"; `CLAUDE.md` § "Storage naming"; `BUSINESS_RULES-ORGANIZATIONS.md` BR-TP73

## Context

Two questions arrived together, and they turned out to have opposite answers.

**The first was a proposal.** The organization ID — a 36-character UUID —
is uncomfortably long in URLs and in subject tokens. The suggested
replacement was a human-meaningful composite:

```
<Country-Code>-<Company-Registration-Number>
```

**The second was a challenge to the subject taxonomy.** Given

```
evt.acme.organizations.transporter.2c38c578-a69a-4723-b74b-f1afcc1d6d44.created
```

— what does the id token buy, when the same value is already in the
payload? Could it be dropped?

The length complaint is legitimate. Both proposed remedies are not.

### Why the country-prefixed registration number fails

Two of the objections are blockers in this service's own model, before any
argument about regions:

- **The number does not exist when the ID is needed.** `Register` requires
  only name, type and context (BR-TP02); `registrationNo` is optional and
  "fillable incrementally as KYC/vetting proceeds" (BR-TP35). An identity
  is needed at registration, which is before vetting. A prospective
  transporter, or a sole trader with no company number at all, would have
  no ID.
- **The number is mutable.** `registrationNo` sits in `Details`, the
  editable field set (BR-TP32). Registration numbers change: restructuring,
  re-domiciling, or a typo corrected during vetting. For an event-sourced
  `TransporterProfile` that means the identity of an aggregate changing
  underneath its own event stream.

And the regional reasoning does not hold either:

- **"Country" is the wrong granularity for a registry namespace.** US
  company numbers are per-state, Canada federal vs provincial, Germany
  per-*Amtsgericht* (`HRB 12345` is unique only with the court), Spain and
  Switzerland regional. `DE-HRB12345` collides across registries.
- **Which country?** Incorporation, operation, registered office and VAT
  jurisdiction are four different answers. An Irish-incorporated carrier
  operating from Northern Ireland has one row and two plausible prefixes.
- **ISO 3166-1 codes get reassigned** (CS → RS/ME, AN → CW/SX), so
  immutable IDs would be minted under codes that later mean something else.
- **The character sets break our own constraints.** France SIREN
  `123 456 789`, Germany `HRB 12345`, Poland `KRS 0000123456`, Italy
  `IT/12345` — spaces and slashes. A `.` would silently split a subject
  token and break the fixed-arity positional parsing every subject family
  depends on. Normalization would be required, and lossy normalization
  manufactures collisions.
- **No check digit.** A typo in `GB-12345678` silently addresses a
  different organization. LEI uses ISO/IEC 7064 MOD 97-10 and GLN uses
  mod-10 for exactly this reason.
- **It is PII-adjacent, in an immutable log.** In several jurisdictions a
  sole trader's company number *is* a personal tax identifier. In a subject
  token that value is in the `LimitsPolicy` `TRANSPORTER` stream
  permanently, plus consumer names, Temporal workflow IDs and `obs.rpc.*`
  output. A GDPR erasure request against a subject token is far worse than
  against a payload field.
- **It does not cover every partner** we must represent: government bodies,
  charities, foreign branches, brokers unregistered in a market.

### What the logistics industry actually does

Checked against the standards rather than assumed. Every established
standard separates **internal identity** from **external party codes**, and
none uses a country-prefixed registration number as an entity's primary
identifier:

| Standard | Format | Country encoded? |
|---|---|---|
| **LEI** (ISO 17442) | 20 alphanumeric: 4-char LOU prefix + 14 entity + 2 check digits | **No — deliberately** |
| **GLN** (GS1) | 13 numeric: company prefix + location ref + mod-10 check digit | No |
| **DUNS** | 9 numeric | No |
| **SCAC** (NMFTA) | 2–4 letters, mandatory for US CBP AMS and X12/EDIFACT | No |
| **EORI** (EU/UK) | ISO country code + up to 15 alphanumeric, 3–17 chars total | **Yes** |

Two findings settle it:

**GLEIF explicitly rejected this design.** LEI was built jurisdiction-neutral
on purpose — the code carries no "embedded intelligence or country codes,"
because those create unnecessary complexity. That is the global
entity-identification body, after a G20-mandated design process, reaching
the same conclusion from the same premises.

**EORI is a live demonstration of the cost.** EORI *is*
`<Country-Code><National-Number>` — the closest real instance of the
proposal. Look what it inherited: the body format varies per member state
(Germany 14 digits from the tax number, Netherlands the KvK number, Italy
11, UK 12), total length swings 3–17 characters, and Northern Ireland
needed an entirely separate `XI` prefix — the "which country?" ambiguity
above, except it actually happened and cost a schema change across every EU
customs system. EORI is also EU/UK-only, so it could not be a global
identifier regardless.

The pattern the industry does use is **scheme-qualified codes**. DCSA's
party model carries an `identifyingCodes` *array* of
`{codeListProvider, partyCode}` pairs, mirroring UN/EDIFACT's long-standing
`C082 PARTY IDENTIFICATION DETAILS` (identifier + code list qualifier +
responsible agency). Three consequences: an organization has **many**
external identifiers rather than one; every external code needs a
**qualifier** to be meaningful; and `GB-12345678` is a two-element
qualified code with the qualifier hardcoded to "this country's default
registry" — the assumption that breaks in Germany, the US, Canada, Spain
and Switzerland.

### Why the id token cannot leave the subject

Three things in this service depend on it, and one has no workaround:

1. **Per-subject optimistic concurrency.** ADR-049 settled on
   `Nats-Expected-Last-Subject-Sequence-Subject` against the filter
   `evt.{context}.organizations.transporter.{id}.>`. That header is
   *defined* in terms of a subject. No id in the subject means no
   per-aggregate expected sequence, so server-side conflict detection is
   gone and an application-level lock has to be invented — the thing
   ADR-049 chose this mechanism to avoid.
2. **Per-aggregate replay.** `orchestration/event_store.go` builds its
   ordered consumer with
   `FilterSubjects: []string{InstanceSubject(contextKey, organizationID)}`.
   The *server* filters. Without the id token, every `hydrate()` on the
   write-side hot path becomes "consume every transporter event in the
   tenant and discard most of them," client-side, over the wire.
3. **Authorization and scoped consumers.** NATS subject permissions are the
   only place "this credential sees only this aggregate" can be expressed.
   The payload is invisible to the auth layer.

"The data is already in the payload" is true but the wrong comparison: the
payload is opaque to the **broker**. Filtering, sequence-per-subject,
dedupe and authorization all operate on the subject and nothing else.

### The length complaint, taken seriously

NATS itself routes on long opaque ids: `$SYS.ACCOUNT.<account-id>.CONNECT`
carries a 56-character NKey public key, and this repo's own operator config
is full of them. `_INBOX.<nuid>` carries a 22-character base62 NUID. So a
36-character token is well inside what the platform's designers accepted on
hot paths — but that is an argument for the complaint being survivable, not
for ignoring it. Subject length is per-message overhead and it is stored in
the stream.

The community convention for event sourcing on NATS is
`category.entity-type.entity-id` (`sales.orders.1`) paired with
`ExpectedLastSequencePerSubject` — which is the shape already built here,
so no divergence. Neither that convention nor the NATS maintainers
prescribe an id *format*; the illustrative `1` is not a recommendation.

## Decision

**Identity in `organizations-service` is a ULID.** 26 Crockford-base32
characters: a 48-bit millisecond timestamp followed by 80 bits of entropy.
Minted by the service (`internal/identity`, wrapping `oklog/ulid/v2`),
never by Postgres.

Concretely:

- **Every `id` and `organization_id` column becomes `TEXT`**, and every
  `DEFAULT gen_random_uuid()` is removed. Postgres cannot generate a ULID,
  so the choice was between the service minting identity and the database
  not having one to give.
- **The subject taxonomy is unchanged.**
  `evt.{context}.organizations.transporter.{id}.{event}` keeps its id
  token, for the three reasons above. What changes is only the token's
  contents: 26 characters instead of 36, a 28% cut on a token that is in
  every event on a `LimitsPolicy` stream.
- **`countryCode` + `registrationNo` are business attributes, not
  identity.** They stay where they are.

Why ULID over the alternatives actually on the table:

- **vs UUIDv4** — 26 chars vs 36, and time-sortable. A v4 key scatters
  Postgres B-tree inserts; a ULID's timestamp prefix makes them
  near-sequential.
- **vs UUIDv7** — same 128 bits and same time-ordering, but v7 spends 10
  characters on hyphens and hex inefficiency (36 vs 26) and has 74 bits of
  per-millisecond entropy against ULID's 80. No advantage except native
  Postgres `uuid` storage, which is exactly what a hyphen-free
  human-readable token is trading away on purpose.
- **vs NUID** (NATS's own, 22 chars) — 4 characters shorter, but its
  collision resistance is really the 12-character random prefix (~71 bits,
  below ULID's 80), and because that prefix is random while only the
  10-character suffix is sequential, NUIDs sort by insertion order *within
  a process lifetime only*. A restart resets the ordering. Losing global
  time-sortability to save 4 characters is the wrong trade when B-tree
  locality is one of the reasons for the change.
- **vs a short display reference** (`TRP-8FK2QX`, 6 Crockford chars) — a
  good idea for URLs and for humans reading an ID over the phone, and
  Crockford's case-insensitive `I`/`L`→`1`, `O`→`0` folding is designed for
  it. But 30 bits gives a 50% collision at ~38,600 blind draws, so it needs
  a unique index and a retry loop, and it is a *display* concern. Not
  adopted now; recorded as a candidate.

## Consequences

**Accepted costs.**

- **Existing rows keep 36-character UUID text.** The migration converts
  column *types* and deliberately does **not** invent ULIDs for existing
  rows. It cannot: a `TransporterProfile`'s id is embedded in every event
  subject it has ever published on the `LimitsPolicy` `TRANSPORTER` stream,
  so renumbering a row would orphan its whole history and the aggregate
  would silently rehydrate as **empty** — no error, just a blank
  aggregate. The supported path to uniformity is a reseed: drop the streams
  and KV buckets, re-run `cmd/seed-transporters`.
- **Losing `uuid` as a type constraint.** A `TEXT` column will accept
  anything. `identity.IsValid` exists as the replacement guard, and it
  checks three things a naive length test would miss — the Crockford
  alphabet, and the overflow case where 26 base32 characters address 130
  bits against a ULID's 128, capping the first character at `7`.
- **A ULID leaks creation time.** 48 bits of it, to anyone holding the ID.
  For an organization record that is not sensitive; worth remembering
  before reusing this package for anything where it would be.
- **`oklog/ulid/v2` is a new direct dependency** of
  `organizations-service`. Note that this module's `go.mod` documents that
  `go mod tidy` **must not** be run — it drops a deliberate `genproto` pin
  and the build breaks with ambiguous imports. Use `go get`.

**Explicitly not decided here.**

- The **short display reference** for URLs and humans.
- The **scheme-qualified external-code table** the DCSA/EDIFACT survey
  points at — `(organization_id, code_list_provider, party_code)` with
  `codeListProvider` as a refdata type (`GLEIF`, `EORI`, `GS1`, `DUNS`,
  `SCAC`, `VAT`, `NATIONAL_REGISTRY`, `ZZZ`). This is the right home for
  `registrationNo` and `vatRegistrationNo` eventually, and the straight
  path to EDI interoperability. Recorded as a candidate, not in scope.

### Scope and what was deliberately left alone

`shipping-service` and `accounts-service` keep UUIDs. Both were considered
and consciously excluded:

- **`shipping-service`** would be cheap — its `id` columns are already
  `TEXT` and there is one minting seam (`newSurrogateID()`) — but Ship and
  Container ids are subject tokens on the `SHIPPING` stream with the same
  orphaning problem, and nothing about the POC's questions needs them
  changed.
- **`accounts-service`** has four `uuid` columns with `gen_random_uuid()`
  defaults. Note that NATS *account* identity is an NKey public key and is
  unaffected either way.

The result is a repo where two ID formats coexist by decision rather than
by accident. That is the honest cost of scoping this to one service, and it
is recorded here so a later reader does not read `shipping-service`'s UUIDs
as an oversight.
