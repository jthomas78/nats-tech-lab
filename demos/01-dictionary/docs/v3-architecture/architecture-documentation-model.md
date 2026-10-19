---
title: How the V3 architecture is documented
aside: false
---

# How the Proposed V3 architecture is documented

<div class="v3-meta">
<strong>Not an LB-V3 document</strong> &middot; A guide to the series &middot;
Rules of record: <code>proposed-v3-arch/Proposed-Linebooker-V3-Architecture-Authority.md</code> &middot;
Status: proposal for team review
</div>

::: warning PROPOSED, NOT BUILT
This page explains **how** the Proposed Linebooker V3 architecture is written
down. It is a proposal for the team to agree. It does not add rules of its own:
the Operational Authority holds the rules, and this page points back to it.
Nothing in the V3 series is built.
:::

## In one paragraph

The V3 architecture is drawn as a set of documents in **five levels**, `L0` to
`L4`, much like the [C4 model](https://c4model.com/) draws a system in four.
Each level is for one audience and adds detail to the level above it. Each
document answers **one question**, has **one stable ID** (for example
`LB-V3-L3-05`) and **one requirements register**. **Decisions** are not drawn
in the levels. They go in a separate **decision log** of Architecture Decision
Records (ADRs). The levels show the **shape**. The ADRs record **what we decided
and why**. The requirements register is the bridge: where a requirement rests
on a decision, it names the ADR.

## The model at a glance

<figure class="v3-figure">
<a class="v3-figure-zoom" href="/v3-architecture/model-overview.png" target="_blank" rel="noopener">
<img src="/v3-architecture/model-overview.png" alt="The documentation model: an Operational Authority governs five levels of LB-V3 documents, L0 Architecture Atlas to L4 Detailed designs. Each document has a requirements register, a drawn edition, a print edition and an optional written edition. A separate decision log of v3 and lab ADRs is the decision of record for requirements.">
</a>
<figcaption><strong>The model</strong> — five levels, one decision log, editions per document.
<a href="/v3-architecture/model-overview.png" target="_blank" rel="noopener">Open full size</a>
</figcaption>
</figure>

How to read it:

- **Left column — the levels.** Each step down is a zoom. A child adds detail
  to its parent's question. It does not repeat the parent.
- **Middle column — what each document is made of.** The register governs the
  drawing. The drawing exports the PDF. The written page explains the drawing.
- **Right column — the decision log.** ADRs sit beside the levels, not inside
  them. One ADR can govern documents at any level.
- **The top box — the Operational Authority.** It owns the rules for all of
  this. When anything disagrees with it, it wins.

## The five levels

| Level | The question it answers | Who reads it | Shows | Leaves out | Nearest C4 view |
|---|---|---|---|---|---|
| `L0` | Where is the diagram I need? | Anyone | The catalogue: IDs, titles, status, parent links | Any system design | None. C4 has no catalogue view |
| `L1` | Who uses the platform, for what, and where? | Everybody, technical or not | People, organisations, external systems, capabilities, regions | Service internals, protocols | System Context |
| `L2` | How is it built? Which technologies, and why? | Architects, tech leads, operations | Services, messaging, stores by role, deployment cells, technology cards | Subjects, schemas, permissions | Container |
| `L3` | How does **one** concern work? | Specialists in that concern | One concern in depth: tenancy, messaging, data, regions, security… | Other concerns | Component, plus flow, state and trust views |
| `L4` | What exactly do we build? | The people who build it | Subjects, streams, schemas, endpoints, permission claims, deployment specs | The full landscape again | Partly Code and Deployment. Message contracts have no C4 view |

Three rules make the levels work:

1. **The audience decides.** If a box cannot be explained to a level's reader,
   it belongs lower. If that reader would not care, it also belongs lower.
2. **Use the lowest level that answers the question**, and name its parent. A
   level is never filled in just to complete the set.
3. **Each level has a fixed list of things it may draw.** Needing something
   that is not on the list means the content belongs at another level.

L0 is the [Architecture Atlas](/v3-architecture/l0-01-architecture-atlas)
(`LB-V3-L0-01`). It is a map of the catalogue, not a design. Start there to
find a document.

## What every document has

One stable ID names **one question**, not one file. That question can have up
to three editions, and all of them share one register:

| Part | File | Kind |
|---|---|---|
| Requirements register | `proposed-v3-arch/Proposed-Linebooker-V3-L<n>-<nn>-Requirements.md` | Source, hand-written |
| Drawn edition | `proposed-v3-arch/drawings/linebooker-v3-l<n>-<nn>-<title>.html` | Source, hand-written |
| Print edition | `output/pdf/Proposed Linebooker V3 Architecture - L<n>-<nn> <Title>.pdf` | Made from the drawing |
| Written edition | `demos/01-dictionary/docs/v3-architecture/l<n>-<nn>-<title>.md` | Optional prose page |

Each requirement has its own ID (`L3-05-012`) and one of three kinds:
**Included** (drawn here), **Derived** (passed down to a named child document
or ADR) or **Open** (a choice nobody has made yet). An open choice is written
down, never quietly made in a drawing.

A document moves through four statuses: `PLANNED` (a place in the catalogue,
nothing drawn) → `DRAFT` (a drawing to review) → `AVAILABLE` (drawing and PDF
pass their checks) → `SUPERSEDED` (kept, and linked to its replacement). IDs
are never reused or renumbered.

## Where ADRs fit

An ADR records **one architecturally significant decision**: its context, the
decision, the reasons and the consequences. Where there were alternatives, it
lists them too; `ADR-054` and `ADR-056` each compare four options. A level
document records **the shape** that follows from many decisions. They do
different jobs, so they are kept apart.

A **constraint is not an ADR.** A rule imposed from outside, such as a law or a
contract, is a constraint. The ADR records what we decide to do about it.
arc42 keeps the two apart in the same way ([arc42 §2][arc42-2]).

| | LB-V3 document | ADR |
|---|---|---|
| Answers | How is it shaped? | What did we decide, and why? |
| Changes | Revised as the design grows | Fixed once accepted. Replaced, not edited |
| Numbered by | Level and position (`LB-V3-L4-02`) | One global sequence (`ADR-058`) |
| Lives in | `proposed-v3-arch/` | `obsidian/V3-Platform/Architecture/ADR/` |

How they link today — these are already in use in the series:

- **Decision of record.** When a requirement rests on a decision, the register
  names the ADR. `LB-V3-L4-02` is the detailed design of `ADR-058`, and its
  requirements cite the ADR's rule numbers.
- **The ADR wins.** If a document and its ADR disagree, the document is
  corrected. The ADR is not bent to fit the drawing.
- **A missing ADR is shown, not hidden.** An item with an accepted ADR is
  `Chosen`. An item without one is `Proposed`, and the drawing says
  `no V3 ADR yet`. That is a visible to-do, not a defect.
- **Two kinds of ADR.** A `v3` ADR is a platform principle. A `lab` ADR records
  what this lab built, and names the `v3` ADR it applies.
- **ADRs are not a level.** A principle such as `ADR-054` (portability) can
  shape documents at L2, L3 and L4 at the same time.

## The sequence

<figure class="v3-figure">
<a class="v3-figure-zoom" href="/v3-architecture/model-sequence.png" target="_blank" rel="noopener">
<img src="/v3-architecture/model-sequence.png" alt="The sequence from a question to a published document, in five lanes: proposer, decision log, LB-V3 document, team review, and outputs and lab. A question that sets or changes a significant approach or rule goes to an ADR first; the shape is registered, written and drawn; review can send an unrecorded decision back to an ADR; an accepted ADR corrects the document; the document is validated, exported and later tested in the lab, and findings start a new question.">
</a>
<figcaption><strong>The sequence</strong> — from a question to a published document, and back.
<a href="/v3-architecture/model-sequence.png" target="_blank" rel="noopener">Open full size</a>
</figcaption>
</figure>

1. **Raise one question.** It can be a new concern, a change, or a finding
   from the lab.
2. **Is there an architectural decision to record?** If the question sets or
   changes a significant approach, principle or rule, go to step 3. If an
   existing ADR already covers it, cite that ADR. If the question only
   describes the architecture under decisions already made, go to step 4.
   One question can need both an ADR and a drawing.
3. **Write or update the ADR as `Proposed`.** Context, decision, reasons and
   consequences. List the alternatives where there were any.
4. **Place and register the document.** Pick the lowest level that answers
   the question and its parent. Take the next free ID. Add a `PLANNED` row to
   the catalogue and a card to the L0 atlas.
5. **Write the register.** Included, Derived and Open requirements. Name the
   ADR of record, or write `no V3 ADR yet`.
6. **Draw it.** It is now `DRAFT`. If decisions are still missing, mark the
   drawing **directional** and date it.
7. **Review.** The team checks it against the authority. If review finds an
   architectural decision that nobody recorded, it goes back to step 3. This
   includes a decision the drawing makes without saying so.
8. **Accept the ADR.** Items move from `Proposed` to `Chosen`. If the drawing
   disagrees with the ADR, the drawing is corrected.
9. **Validate and export.** The PDF is made and checked. The status becomes
   `AVAILABLE` and the atlas is updated.
10. **Write the prose edition**, if it helps. It never changes the status.
11. **Build and measure in the lab.** A `lab` ADR names the `v3` ADR it
    applies. A finding starts a new question — or replaces a document, which
    becomes `SUPERSEDED`.

**When is the ADR written?** Before the drawing when the decision is already
clear. During review when the drawing turns out to make a decision that nobody
recorded. A drawing
may go ahead without its ADR, but it must say so.

## How this compares with other teams

We checked the model against the public methods most teams use. In short:
**the shape is standard practice. The amount of paperwork per document is
not.**

### Level by level

| Our level | C4 model | arc42 | ISO/IEC/IEEE 42010 |
|---|---|---|---|
| `L0` Atlas | No equivalent | Table of contents | The architecture description as a whole |
| `L1` Overview | System Context, System Landscape | §1 Goals, §3 Context and Scope | A business-facing view |
| `L2` Logical + Technical | Container, Deployment | §5 building blocks level 1, §7 Deployment | Logical and physical views |
| `L2` Technology Selection | No equivalent | §4 Solution Strategy | Rationale, which 42010 requires |
| `L3` Concern views | No equivalent | §8 Crosscutting Concepts | **A viewpoint**: one concern, for named stakeholders |
| `L4` Detailed designs | Component, Code | §5 levels 2–3, interfaces | Development view |

Sources: [C4][c4], [C4 diagrams][c4-diagrams], [arc42 §5][arc42-5],
[arc42 §9][arc42-9], [42010 summary][iso42010].

### What we do that others also do

- **Zoom levels**, from context down to detail — C4 and arc42 both do this.
- **One concern per view.** Our L3 is what 42010 calls a viewpoint.
- **ADRs kept apart from the views**, numbered, never reused, replaced rather
  than edited. This is Michael Nygard's original form ([Nygard][nygard]), and
  arc42 keeps decisions (§9) apart from structure (§4, §5) the same way.
- **ADRs in source control, not a wiki.** ThoughtWorks rated lightweight ADRs
  "Adopt" ([Radar][radar]). AWS, Google Cloud and the UK Government Digital
  Service say the same ([AWS][aws-adr], [Google Cloud][gcp-adr], [GDS][gds]).
- **Stable IDs and a status on every document**, like Oxide's RFDs
  ([Oxide][oxide]) and MADR ([MADR][madr]).

### What is unusual about ours

- **A requirements register with IDs in every document.** That is closer to
  systems engineering than to most software teams, who only trace a
  requirement into its ADR.
- **Three editions per document** (drawing, PDF, prose). Most teams keep one
  source and render it. Structurizr, for example, makes every view from one
  model, so the views cannot disagree ([Structurizr ADRs][structurizr]).
- **Hand-drawn diagrams**, not views made from one model.
- **A drawn L0 atlas.** Few teams draw their catalogue. Oxide solves the same
  problem with an index and short links.
- **L4 mixes things that C4 keeps apart.** It holds schemas (C4's Code
  level), deployment specs (C4's Deployment diagram) and message contracts,
  which C4 has no view for. C4 calls its Code level optional and does not
  recommend it for long-lived documents ([C4 Code][c4-code]). Google advises
  against copying schemas into design docs ([Google design docs][google-dd]).

### When other teams write the ADR

Most teams follow the same order: **question → proposal → review → decision
recorded → build → docs updated → later superseded.** Where the ADR sits
varies a little:

- **Spotify** writes a design proposal (RFC) first, then the ADR when it
  concludes. Small changes go straight to an ADR ([Spotify][spotify]).
- **AWS** and Andrew Harmel-Law open the ADR as `Proposed` at the start of
  review, and use it to collect advice ([AWS][aws-adr], [Harmel-Law][fowler]).
- **GitHub** writes it at decision time, before the code goes up
  ([GitHub][github]).

The common rule: **the ADR is written during design and settled before the
build.** Our sequence follows it. Step 3 writes it early when the decision is
clear. Step 7 catches a decision that nobody recorded. Step 8 settles it before
step 11 builds anything.

### Risks, and how others handle them

| Risk | What other teams do | What we do today |
|---|---|---|
| **Docs go stale.** Three editions make it worse | Make every output from one source. Check the code against the docs in CI | PDF and PNG are made from the drawing. The prose page is by hand. The authority says to update the prose in the same change, or say it is behind |
| **Too many levels and documents** | C4: draw only what adds value. arc42: detail only risky or complex parts. Google: no design doc when the answer is obvious | A level is never filled just to complete the set. L4 exists only where a concern needs it |
| **ADR and document say the same thing, or disagree** | arc42: point to the decision, do not repeat it. AWS: an accepted ADR is not edited | The register cites the ADR's rule numbers. The ADR wins |
| **Documents cannot be found** | Oxide keeps an index of every RFD, with short links | The L0 atlas and the catalogue |
| **Decisions are not followed** | MADR has a "Confirmation" section. AWS checks ADRs in code review | `lab` ADRs name the `v3` ADR they apply. No check beyond that |

Sources: [Teamscale][teamscale], [Google design docs][google-dd],
[arc42 §9][arc42-9], [AWS][aws-adr], [MADR][madr], [Oxide][oxide].

## What we ask the team to agree

The model as it stands is described above and is already in use. Five gaps
came out of the comparison. Each one is a proposed addition, not a rule yet:

1. **Adopt the model as described**: five levels, one decision log, the
   register as the bridge between them.
2. **Add a link from an ADR to its documents.** Today the document points to
   the ADR, but the ADR does not point back. A `documents: [LB-V3-L4-02]`
   field in the ADR front matter would close the loop, as Structurizr does
   ([Structurizr ADRs][structurizr]).
3. **Add a review window.** Say how long a `DRAFT` stays open for comment, and
   who signs it off. Rust uses a final comment period; Oxide uses named
   discussion states ([Rust RFCs][rust], [Oxide][oxide]).
4. **Add a "how we check it" line to each `v3` ADR**, like MADR's Confirmation
   section, so that a lab or code review can show the decision is followed.
5. **Decide whether to keep three editions.** Option A: keep them, and accept
   the cost of keeping prose in step. Option B: write the prose page only for
   L1 and L2, where non-specialists read it. We suggest B.

Gaps we found but do **not** propose now: a stakeholder × concern matrix
(42010), and arc42's quality scenarios, risk list and glossary. Add them if
the team finds it needs them.

[c4]: https://c4model.com/
[c4-diagrams]: https://c4model.com/diagrams
[c4-code]: https://c4model.com/diagrams/code
[arc42-2]: https://docs.arc42.org/section-2/
[arc42-5]: https://docs.arc42.org/section-5/
[arc42-9]: https://docs.arc42.org/section-9/
[iso42010]: https://quality.arc42.org/standards/iso-42010
[nygard]: https://www.cognitect.com/blog/2011/11/15/documenting-architecture-decisions
[radar]: https://www.thoughtworks.com/radar/techniques/lightweight-architecture-decision-records
[aws-adr]: https://docs.aws.amazon.com/prescriptive-guidance/latest/architectural-decision-records/adr-process.html
[gcp-adr]: https://docs.cloud.google.com/architecture/architecture-decision-records
[gds]: https://gds-way.digital.cabinet-office.gov.uk/standards/architecture-decisions.html
[oxide]: https://oxide.computer/blog/rfd-1-requests-for-discussion
[madr]: https://adr.github.io/madr/
[structurizr]: https://docs.structurizr.com/dsl/adrs
[google-dd]: https://www.industrialempathy.com/posts/design-docs-at-google/
[spotify]: https://engineering.atspotify.com/2020/04/when-should-i-write-an-architecture-decision-record
[fowler]: https://martinfowler.com/articles/scaling-architecture-conversationally.html
[github]: https://github.blog/engineering/architecture-optimization/why-write-adrs/
[rust]: https://rust-lang.github.io/rfcs/
[teamscale]: https://teamscale.com/de/news/blog/challenges-of-architecture-documentation/

## Sources

- Rules of record: `proposed-v3-arch/Proposed-Linebooker-V3-Architecture-Authority.md`
- ADR rules: `obsidian/V3-Platform/Architecture/ADR/README.md`
- C4 mapping: `.claude/skills/architecture-C4-draughtsman/references/c4-modelling.md`
- Diagram source: `proposed-v3-arch/drawings/linebooker-v3-architecture-documentation-model.html`
