# CLAUDE.md

Canonical guidance for all AI coding agents in this repo. Agent-specific entry
files should reference this file, not duplicate it.

## Session Memory

At session start, read `.claude/memory/MEMORY.md` (an index of one-line hooks) and
keep it as context. **Do not read every file under `.claude/memory/`** — open an
individual memory file only when its `MEMORY.md` hook looks relevant to the task.
Save new memories to `.claude/memory/` (not `~/.claude/projects/`) so they sync via
git; keep the `MEMORY.md` hook line short enough to judge relevance without opening
the file.

## General preferences

- If asked to do too much at once, stop and say so.
- If computer use helps complete or verify work, shell out to Codex (`codex:rescue`
  skill / `codex:codex-rescue` agent).
- **Don't read large docs whole by default.** Before `Read` with no `offset`/`limit`
  on a file over ~150 lines, prefer `grep`/`Grep` or a targeted `Read`. Full reads
  are fine for a first pass on a new doc or a short file. Applies especially to
  `BUSINESS_RULES-*.md`, `PERFORMANCE.md`, `.claude/plans/*`, and the
  `ARCHITECTURE*.md` docs (see "Architecture Docs").
- Any exploration touching more than 3 files → delegate to an Explore subagent.
- **One command per `Bash` call — don't chain with `&&`.** The permission allowlist
  matches a rule against the *start* of a command, so a broad rule like
  `Bash(grep:*)` fires for `grep -n foo bar.md` but never for
  `echo x && grep -n foo bar.md`. A chain is judged whole, matches nothing, and
  prompts the user every time; "Always allow" then saves the entire chain verbatim
  as a one-off rule that never matches again. Issue the commands separately, in
  parallel tool calls where they're independent. Also don't prefix `cd <repo
  root> &&` — Bash calls already start there.

## Purpose

A lab for evaluating NATS.io patterns for a V3 greenfield logistics platform. Each
demo is self-contained: pick a demo from the lab shell, read an intro, launch via
Docker, tear down when done.

Core question: **the correct responsibility split between JetStream (event
backbone), NATS KV (fast lookup/watch/cache), Postgres (transactional source of
truth), and CQRS projections.**

## The life of a demo

**`demo-playbook.html` (and its PDF export `demo-playbook.pdf`) is the one
lifecycle. Read it before you start, design, measure or close a demo.** Do not
keep a second step list here — there is only one.

Four stages, `01` to `04`. The shape comes from `development-playbook.pdf`, but a
demo has no Plan/Enable/Build/Release, so its `04 Learn` is that playbook's
`08 Learn`, renumbered:

| Stage | Name | Question it answers |
|---|---|---|
| `01` | Define the question | What are we asking, and is this a showcase or a validation? |
| `02` | Design the rig | What is held still, and what is the one variable? |
| `03` | Validate — build the slice and measure it | What did the machine actually do? |
| `04` | Learn — the card, and the next question | What choice does this let somebody make? |

Two rules from the playbook that bite outside it:

- **Every demo declares its role** — showcase (a person sees a feature work),
  validation (a decision gets measured evidence), or both. A demo that never
  says which job it is doing will do neither well.
- **Every requirement gets an ID** (`D03-R1`, `D03-R2`, …). Without an ID,
  stage `04` has nothing to point back at.

### The pattern cards are the closing deliverable

**When a demo completes, it gets a pattern cards PDF.** That is stage `04`. Full
workflow — deck location, export command, card shape, provenance, retraction,
guard spec — is the `pattern-cards` skill. Read it before closing a phase.

## Repository Layout

Read the tree with `ls` — it changes faster than this file. Not visible from the
tree:

- Each demo owns its Compose files and does **not** share a network with the lab
  shell or other demos. Demo 01's live under `demos/01-dictionary/deploy/` and are
  split into bands (ADR-055) — see "Running demo 01" below. **There is no flat
  `demos/01-dictionary/docker-compose.yml` any more**; it was retired 2026-09-08.
- **Every demo folder is a sealed unit and owns its own `CLAUDE.md`.** That file
  is the demo's rules, and an agent working inside the folder reads it *instead
  of* this one. A new demo gets one before any other work starts. This file
  keeps only a pointer per demo, under "Commands" below — never a copy.
- A demo's top-level `README.md` is the **intro text rendered in the lab shell** —
  edit it with that audience in mind.
- `lab-shell/` is the demo menu / intro pages; per-demo UIs live under
  `demos/<demo>/frontend/`.
- Two obsidian vaults, different jobs — see "Obsidian Vault" and "Architecture
  Docs".

## Docker Host Port Allocation

Two fixed 4-digit ranges:

- **7100–7199** — frontend dev servers
- **7200–7299** — backend/API services

Datastores (Postgres, etc.) and NATS keep their conventional ports. Assign the next
free port in sequence within a demo and record it in that demo's `README.md` port
table.

Shared lab tools sit outside both bands, because the bands are per-demo and a tool
is not a demo. Allocated: **31311** — NUI, the NATS web GUI (`tools/nui/`), which
reaches every demo's NATS over the host.

## Frontend Design System

Every UI in this repo (`lab-shell/` and each app under
`demos/01-dictionary/frontend/`) shares one visual identity and one page shell in
`shared/unifi-theme/`. **This overrides design skills' instinct (`frontend-design`,
`artifact-design`) to invent a new palette/type system/shell per task.** Reuse the
frame; spend creative effort on the content.

- **Theme** — `shared/unifi-theme/unifi.css` + `preset.js`: colors, typography
  (Inter, 13px/20px body), PrimeVue v4 preset, dark mode (`.p-dark` on `<html>`).
  Imported via the `@unifi-theme` alias (see each app's `vite.config.js`). A new
  frontend wires it the same way the existing four do. Add a genuinely missing
  token there rather than forking locally.
- **Layout** — `shared/ui-shell/AppShell.vue` (+ `app-shell.css`): topbar /
  collapsible sidebar / main content shell, imported via `@ui-shell`. Read
  `shared/unifi-theme/LAYOUT.md` before building or redesigning any top-level
  screen — it documents the slot API (`#brand`, `#breadcrumb`, `#topbar-right`,
  `#sidebar`, default) and per-app notes. Consume `AppShell.vue`, don't hand-roll
  topbar/sidebar markup. `shared/unifi-theme/app-shell-reference.html` is the
  static visual reference.
- **Sidebar collapse control** — exactly **one** rail toggle in the repo, in
  `AppShell.vue`, identical in all four apps: a borderless 26px icon button in a
  `.sidebar-foot` row at the **bottom-right** of the rail (centred when collapsed),
  drawing an inline panel-toggle SVG (not a PrimeIcon, not a `«`/`»` glyph), with
  `aria-label` (`Collapse sidebar`/`Expand sidebar`) and `aria-expanded`. Don't add
  a per-app collapse affordance; don't override `.sidebar-foot` /
  `.sidebar-collapse-btn` in an app's CSS — a needed change is a change to
  `AppShell.vue` for everyone. Bottom placement is deliberate (top leaves dead
  space below the topbar). `AppShell.spec.js` (in `admin/src/components/`) enforces
  placement, glyph, and ARIA.
- **Exception — `demos/01-dictionary/docs/` (VitePress, Phase 37).** Has its own
  theming layer, so it does not import `@unifi-theme`/`@ui-shell`. Still must not
  invent a palette: reuse the same colors by overriding VitePress's `--vp-c-*`
  properties in `.vitepress/theme/custom.css` (dark `#131416`/`#006fff`, light
  `#f4f5f7`/`#005fdb`). Presentational idioms (eyebrow labels, "DECISION" callout,
  verdict badges) live as local theme components in `.vitepress/theme/`, not forked
  into `shared/unifi-theme/` unless a second app needs them.
- **Design viewport — 1920x1080. Always judge layout there.** The Browser pane
  opens at ~800px and `resize_window`'s `desktop` preset returns to the *pane's*
  size, not a design width. **Before assessing any layout, call `resize_window`
  with `{width: 1920, height: 1080}`**; reset with `desktop` when done. Narrower
  widths are worth a graceful-degradation look but aren't the target — don't spend
  column budget or add horizontal scroll for them.

### Generated reports, sketches, and diagrams

Same override for one-off generated artifacts (architecture review reports, ad hoc
HTML sketches, drawio diagrams). **Ignore a skill's default styling (light
backgrounds, stone/slate, emerald/indigo) in favor of the dark UniFi palette:**

- Canvas / page background: `#131416` (`drawio-architecture-drawer` uses `#14171B`
  for diagram canvases — either is fine, don't mix both in one doc)
- Panels / cards: `#1A1E23`
- Primary accent (UI/service nodes, links, primary badges): `#006FFF`
- Authoritative-data accent (Postgres/source-of-truth): `#27C07F`
- Lane / lifeline / border strokes: `#4A515B`
- Primary text: `#DEE0E3`
- Secondary/muted text: `#B7BCC2`
- Warning / fallback accent: `#9A7B1E`
- Typography: Inter (fall back to `-apple-system, 'Segoe UI', Roboto,
  'Helvetica Neue', Arial, sans-serif`), 13px / 20px line-height.

## Obsidian Vault (`obsidian/POC-Dictionaries/`)

Narrative/research vault for demo 01 — research notes, POC problem statement /
design write-ups / findings, and the shareable stakeholder narrative (including
exported PDFs). Living documents: when a phase produces a finding or decision,
update the relevant note here as well as the code-side docs. Split of
responsibility: `BUSINESS_RULES.md` + `ARCHITECTURE*.md` = *how the code works*;
`POC-Dictionaries/` = *why* and *what we learned*.

## Architecture Docs (`obsidian/V3-Platform/Architecture/Dictionary-POC/`)

Code-facing architecture reference (relocated out of the repo tree). Read targeted,
not whole. The `ARCHITECTURE*.md` set:

- `ARCHITECTURE.md` — CQRS shape taxonomy; event sourcing vs plain CRUD
- `ARCHITECTURE-DICTIONARY.md` — refdata-service seeding, Postgres schema/ER,
  data access paths (Postgres/REST/KV), cross-service consumption
- `ARCHITECTURE-COMMUNICATIONS.md` — REST/Swagger + NATS `rpc.*` dual transport,
  subject taxonomy
- `ARCHITECTURE-ACCOUNTS.md` — NATS operator-mode trust chain, tenant account
  lifecycle, user auth / token lifecycle
- `ARCHITECTURE-ADMIN.md` — Admin UI's SYSTEM → NATS navbar group; per-panel
  architecture and data-flow, plus its shared UI design system
- `ARCHITECTURE-PLATFORM.md` — entry point for the "Tech Lab Operator" frontend
  (`refdata` app's nav + feature surface); owns nav taxonomy / cross-feature design
- `ARCHITECTURE-APP-SHELL.md` — extensible app shell: frontend plugin registry,
  contribution kinds, host-owned extension points, Module Federation loader,
  migration map. Phase plan:
  `.claude/plans/Application-Shell-Microfrontend-Plan.md`

Same directory holds the editable `architecture-dictionary.drawio` and exported
PNGs (`images/`). Diagram scripts stay in the repo
(`demos/01-dictionary/diagrams/{sync-unifi-assets.mjs,export-png.sh}`) and resolve
into that vault dir; see the `drawio-architecture-drawer` skill.

### Architecture Decision Records (`obsidian/V3-Platform/Architecture/ADR/`)

All ADRs live under this folder in a subfolder per scope — `lab/` for
decisions implemented in this repo, `v3/` for Proposed Linebooker V3 platform
principles — as `ADR-<nnn>-<scope>-<context>-<slug>.md` with a YAML front
matter block. The folder must match the front matter's `scope`; the build
script fails when it does not. Read
`obsidian/V3-Platform/Architecture/ADR/README.md` before creating, moving or
renaming one. Numbers are global and never renumbered; `ADR-INDEX.md` and the card PDF
are generated by `demos/01-dictionary/diagrams/build-adr-cards.mjs` — re-run it
after any front matter change.

### Proposed Linebooker V3 architecture levels

- For any creation, revision, catalogue, or review work in this document series,
  first read
  `obsidian/V3-Platform/Architecture/Dictionary-POC/Proposed-Linebooker-V3-Architecture-Authority.md`.
  It is the central operational authority for the L0-L4 hierarchy, catalogue,
  IDs, statuses, scope, traceability and branching rules. Then follow
  `.claude/skills/architecture-draughtsman/SKILL.md` for the execution,
  rendering and validation workflow. Do not redefine those rules here or in
  memory; the architecture discussion is background only.
- **One stable ID, up to three editions of one document.** The drawn (editable
  HTML) and print (PDF) editions belong to `architecture-draughtsman`; the
  written edition — a prose page in the docs site's own `/v3-architecture/`
  section, port 7106 — belongs to
  `.claude/skills/architecture-document-writer/SKILL.md`. They share one primary
  question and one requirements register. The written edition is optional and
  gates no catalogue status, and it never describes architecture its drawing does
  not show. The rest of that docs site documents what the lab **built**; this
  series is a **proposal** and stays in its own section for that reason.
- **C4 views are a separate, ungoverned deliverable.**
  `.claude/skills/architecture-C4-draughtsman/SKILL.md` draws C4-model Context,
  Container, Component, Deployment and Dynamic views in the dark UniFi house
  style via `html-diagram-drawer`. It adopts C4's concepts and rejects C4's
  notation, matching the authority's visual standard. A C4 view never claims an
  `LB-V3-*` ID, never appears in the L0 atlas and never changes a catalogue
  status. If the deliverable is a numbered LB-V3 document, that is
  `architecture-draughtsman`, not this skill.

## Commands

Standard `go build ./...` / `go test ./...` / `npm run dev` / `docker compose up
--build` work as expected. Non-standard:

### Tests — Ginkgo is the preferred runner

```bash
go install github.com/onsi/ginkgo/v2/ginkgo@latest   # once
ginkgo ./...          # runs suite, prints spec tree
ginkgo watch ./...    # re-run on change
```

`go test ./...` is the no-install fallback. **Beware:** Postgres-backed specs SKIP
silently without their `*_TEST_DATABASE_URL` env var, and `go test` still prints
`ok` — green is not proof they ran.

### Running demo 01

**`demos/01-dictionary/` has its own `CLAUDE.md`. Read that, not this file,
for anything inside that folder.** It's the biggest demo here — a
Postgres-backed, multi-service, multi-frontend POC — sealed the same way
demo 02, demo 03 and demo 04 are. It covers running the stack, what the POC
demonstrates, Stream/KV design, storage and credential naming, entity
identity, subject families, architectural notes, quality rules, the AI agent
workflow, and the phased plan's file layout.

### Running demo 02

**`demos/02-multi-region/` has its own `CLAUDE.md`. Read that, not this file,
for anything inside that folder.** It is a sealed unit — six NATS servers and one
small Go binary, no Postgres, no services, no frontends. Most rules here (the
frontend design system, the 7100–7299 port range, Ginkgo, `BUSINESS_RULES-*.md`,
the `ARCHITECTURE*.md` set, the plan files, `shared/natsconn`) do **not** apply
there, and its own file says so explicitly.

Three rules worth knowing from outside: all multi-region work — clustering,
gateways, superclusters, JetStream domains — lives there and must never be added
back to demo 01; `odometer/` is that demo's only JetStream + CQRS example; and
demo 02 uses the **host** `nats`/`nsc` CLIs, not a container (the toolbox was
removed 2026-09-09 on the user's instruction). Demo 01's own tooling rules are
unaffected.

### Running demo 03

**`demos/03-multi-cluster-and-accounts/` has its own `CLAUDE.md`. Read that, not
this file, for anything inside that folder.** It is a sealed unit and the odd
one out: bare `nats-server` processes started on the **host**, no Docker, no
`nsc` trust chain, no `nats` contexts, no Go code and no UI. Accounts are plain
user/password pairs inside six hand-written `.conf` files.

Its role is **validation only**, and the **topology itself is the variable** —
five shapes (T1–T5) needing 6, 7 or 9 servers depending on the run. That is the
mirror image of demo 02, which holds one topology still and varies the account
model.

Three rules worth knowing from outside: every scratch config and `server_name`
carries a **`t-` prefix**, because an unprefixed `pkill` has already killed the
live lab twice; a region is cut with `kill -STOP`, never with Docker; and the
answer is read from `curl "localhost:8231/jsz?meta=1"`, never from the logs.

### Running demo 04

**`demos/04-jetstream-cqrs/` has its own `CLAUDE.md`. Read that, not this file,
for anything inside that folder.** It is a sealed unit — one NATS server (port
4422), one small Go binary and one Vue app, no Postgres, no services, no
cluster and no operator mode. Its plan, business rules and diagrams all live
inside the folder, not in `.claude/plans/` or `obsidian/`.

The Vue app and its HTTP shim (`cqrs serve`) arrived in phase 04.6 — the
"no frontend" line above was lifted then, and only then. Everything else on
the not-in-scope list still holds.

It lifts demo 02's odometer domain and adds the write side demo 02 has not got:
a command checked against the state the log already holds, rehydrated **with
and without a snapshot** so the cost of each is a measured number. Its `nats`
CLI contexts all start `lab4-`.
