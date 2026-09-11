---
name: cross_region_load_handoff
description: A load crossing regions is two loads and one handoff (integration, not replication); origin tenant owns journey completion via a per-dropoff POD checklist
metadata:
  type: project
---

Design discussion 2026-09-08, from the business clarification in
[[gateway_double_capture_and_option3]]. **Nothing implemented. No plan phase covers
this.** Per [[design_discussion_vs_implementation_signal]], do not start building it.

## The case

A load is tendered in Linebooker ZA, ships to Australia, and an **Australian**
transporter fulfils the delivery legs once it lands. This is the first genuine
*sideways* cross-region flow — refdata goes down to the cells, reporting goes up to the
hub, and this goes across.

## Finding 1 — it is two loads and one handoff

The load does not live in two places. It becomes two loads that reference each other:

- `LOAD-1` in Linebooker ZA — the origin leg and the handoff.
- `LOAD-2` in Linebooker AU — the delivery legs.
- `LOAD-2` carries `originTenant` and `originLoadId`.

The business facts force this, not the technology:

- The AU transporter contracts with Linebooker **AU**, never with ZA.
- The AU leg is invoiced in AUD by Linebooker AU.
- The AU transporter is an organisation inside the AU tenant. It does not exist in ZA.

This is the industry's own pattern — interline in aviation, master vs house bill in
ocean freight. One journey, two contracts, two owners.

## Finding 2 — integration, not replication

**This is the load-bearing distinction.** Every difficulty in the option-1/2/3 analysis
was replication pain: who owns the record, did the copies drift, what happens in a
split brain.

| | Replication | Integration |
|---|---|---|
| Idea | one record, two copies, kept identical | two systems, each owns its half |
| Tool | JetStream mirror | account export + import |
| Risk | ownership and drift | none — nothing is shared |

Nobody shares a load, so no load can drift. Modelling this as replication would
re-import every problem the tenancy decision just removed — it is **not** a reason to
revive option 2.

The contract is two narrow exports each way, not a mirror of `SHIPPING`:

1. **ZA exports the handoff** — this load is yours, here are the dropoff IDs you own.
2. **AU exports POD back** — one message *per dropoff*, plus exceptions.

Same shape as [[phase21_account_exports_imports]], which already works, but
tenant-to-tenant instead of platform-to-tenant.

## Finding 3 — origin owns journey completion

"Load complete = every dropoff has a POD" spans the boundary, and **neither side sees
the whole set**. So completion has to have a named owner. There are two states, not one:

| State | Owner | Question it answers |
|---|---|---|
| **Leg complete** | each tenant, locally | can I invoice my transporter? |
| **Journey complete** | the **origin** tenant | is my customer's freight delivered? |

AU's leg may be COMPLETE while ZA's journey is still OPEN. That is correct — they are
two loads.

Origin owns the journey because origin owns the commercial relationship: the customer
booked with ZA, phones ZA, and is invoiced by ZA. Rejected alternative: let the hub
derive completion. That puts a reporting system on the critical path to getting paid,
and a hub outage would block completion.

**The checklist.** The dropoff list is born at origin — the customer named the delivery
addresses at booking time. So ZA already knows what to expect. The handoff names which
dropoff IDs AU owns; AU reports **one POD per dropoff, not "leg done"**, or ZA cannot
tell partial delivery from full. The last tick closes the journey.

The checklist's best property is that it makes a **missing** POD loud. With plain
integration a lost message is silent; the checklist is what turns it into "waiting on
AU, 2 of 3".

## Finding 4 — link origin as a field, never a subject token

The instinct to tie completion back to the start region is right, but the
implementation must not be a region token. `{context}` is the business unit, and region
is never in a subject (CLAUDE.md, [[phase16_tenancy_taxonomy]]). The link travels as
`originTenant` / `originLoadId` **fields inside the handoff message**.

An importing account must also rename incoming subjects on the way in, so AU can always
tell "this came from ZA" and local subjects cannot collide. NATS import supports the
mapping; the shape is undecided.

## Traps to design for

- **POD arrives twice.** Retries happen. Tick off by dropoff ID, never a counter.
- **AU cannot deliver.** That is an exception, not a POD. The journey becomes failed or
  part-delivered — a business rule, unwritten.
- **The two sides disagree.** Events get lost. You need a scheduled *query* ("list my
  open dropoffs"), not just an event feed. This is the step people skip and the one that
  hurts.
- **Export only what the other side must act on.** Not the whole load. ZA does not need
  AU driver names or AU pricing.
- **Split the POD fact from the POD file.** The fact (dropoff X delivered at T, signed
  by name) crosses; the image can stay in AU behind a reference. But if ZA invoices the
  customer, ZA may be legally required to produce the evidence — confirm with legal
  before locking it away.

## Open business rules — answer before any code

Per the repo's plan-phase workflow, these come first:

- What state must a load be in before it can be handed off?
- May AU refuse a handoff? May a load be handed back?
- What does "delivered" mean when 2 of 3 dropoffs succeed?
- How late may a POD be before someone is alerted?
- May a completed journey be reopened?
- Who owns the load while it is on the water?
- Who may see the AU POD image?

## Related

A transporter operating in **both** markets (one login or two?) is parked. It is an
auth question, not a topology one, and does not change any of the above.
