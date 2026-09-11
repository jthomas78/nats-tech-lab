---
name: gateway_double_capture_and_option3
description: RESOLVED 2026-09-08 — the double capture was a test artefact, not a needed shape; option 3 (one account per tenant) is what the repo already does; options 1 and 2 dropped for tenant data
metadata:
  type: project
---

Recorded 2026-09-08, **resolved the same day** by a business clarification. **No code
changed for this.**

## Resolution (read this first)

The business confirmed that a tenant is a *marketplace domain* — `linebooker-south-africa`
and `linebooker-australia` each run their own tenders and serve many companies and
transporters in their region. See [[v3_tenancy_axes_decision]], which this narrows but
does **not** overturn.

That collapses the decision:

- **Option 3 wins, and is not new work.** "Account per tenant-region" and "account per
  tenant" are the same sentence once a tenant is already regional. There is no
  `acme-za`/`acme-au` compound name. It is one account per tenant — exactly
  [[nats_account_is_the_only_authn]], unchanged. The accounts x regions cost feared
  below does not exist.
- **Option 1 (region token) is dropped.** Never needed. It also broke our own rule.
- **Option 2 (one authoritative stream + mirrors) is dropped for tenant data.** No
  tenant's operational data legitimately lives in two cells.
- **The double capture was never a real shape.** We put one account in two cells and ran
  a `SHIPPING` stream in each. No real tenant does that. It stays a genuine trap worth
  knowing, but it was a test artefact, not a design we had to fix.

**Precision that matters:** "a tenant never spans two regions" is *not* a structural law
— it is a fact about today's tenants. A tenant is a marketplace business, and a future
white-label tenant could span regions. Two tenants can also share one region
(`thornlands` in za). Do not write the one-per-region shape into code as an invariant.

**What still crosses regions**, now that operational tenant data does not:

| Direction | What | Built? |
|---|---|---|
| Down | PLATFORM refdata to every cell | Yes — [[phase21_account_exports_imports]] |
| Up | Head-office roll-up reporting | No — this is what the hub cell is for |
| Sideways | Load fulfilment handoff | No — see [[cross_region_load_handoff]] |

The sideways case is real and was missed in the original analysis. It is an
**integration** problem (two owners, a contract), not a **replication** problem (one
record, two copies) — which is why it does not revive option 2.

## What the mesh does today

**MOVED 2026-09-09 — the mesh is in `demos/02-multi-region/`, not demo 01.** Every demo 01
path below is a pre-move record. See [[multi-region-lives-in-demo-02]].

- `gateway {}` on port 7222 in every cluster, never on a host port. Remote lists are
  one file per cluster: `demos/02-multi-region/nats/gateway-remotes-{za,au}.conf`, mounted
  onto the same path so `demos/02-multi-region/nats/nats.conf` stays shared. NATS refuses
  to start when a cluster names itself in its own gateway list.
- Two clusters of three now, not three of three — there is no hub.
- `nats server report gateways` (za system creds) lists all six servers in two
  clusters. The mesh itself works.

## The failure that started this

A JetStream `domain` protects the **API**, not the **subject space** — see
[[jetstream_domain_per_cluster_is_mandatory]].

Clean test: purge SHIPPING in za and au (0/0), publish ONE message in za → za = 1,
**au = 1**. A gateway carries account interest. `acme` is one account everywhere and
both cells run a `SHIPPING` stream with the same subject filter, so each stream is a
valid listener for the other cluster's publish. Worse: each cell also runs its own
`shipping-service`, so one event lands in **both** regions' Postgres and KV. Probes
purged; lab is clean.

64e (3-way replicas) was deliberately **not** started — it would harden the wrong shape.

## The three options as originally stated (historical)

1. **Region token in the subject.** Stops the filters overlapping. Breaks our own rule
   that region is never a subject token (see [[shipping_domain_overview]]); touches
   every publisher and consumer.
2. **One authoritative stream + mirrors.** One `SHIPPING` in one cluster; the other
   region gets a one-way JetStream mirror. Claude's recommendation at the time.
3. **Account per tenant-region** (`acme-za`, `acme-au`), identical subjects inside each,
   explicit stream placement, selective cross-account mirror/source. Codex's
   recommendation, from a docs review — no adoption stats exist.

Codex's argument was that 2 and 3 are not exclusive: **3 gives the isolation, 2 gives
the deliberate copies.** Its own qualification proved to be the decisive one —
*geography alone is not a reason to split accounts; the NATS JWT guide ties accounts to
teams and applications, not infrastructure.* The business clarification showed the split
was never geographic: `linebooker-za` and `linebooker-au` are two businesses.

## Naming debt this exposed

`bootstrap-operator.sh` seeds tenants `ACME` and `GLOBEX`. Those are **customer** names,
not marketplace names — Acme is a shipper *inside* a tenant, an organisation scoped by
`{context}`. The placeholder names caused most of the confusion in this analysis.
[[v3_tenancy_axes_decision]] already asks for an ISO-code convention before seeding, so
the target is `LINEBOOKER_ZA` / `LINEBOOKER_AU` with creds `linebooker-za.creds`.
Not free: renaming an nsc account is delete-and-re-add, needing `down -v` and a reseed.

## Where else this is written

D11 in `demos/02-multi-region/docs/Multi-Region-Plan.md`; 64d marked part-done and open question 6
updated (there is no cheap connectivity test here — every account is bind-mounted into
all nine servers, so connecting proves nothing). Diagram of the original problem and the
three options: `demos/02-multi-region/diagrams/gateway-double-capture-options.html` →
`obsidian/V3-Platform/Architecture/Dictionary-POC/images/gateway-double-capture-options.png`.
That diagram now shows a decision that is settled — read it as history.
