# CLAUDE.md — demos/04-jetstream-cqrs

**This folder is a sealed unit. Read this file instead of the root `CLAUDE.md`
for anything inside it.**

The root file covers the whole lab shell; demo 01's own rules moved to
`demos/01-dictionary/CLAUDE.md` — a Postgres-backed, multi-service, multi-
frontend POC. Demo 04 is one NATS server and one small Go binary. Most of the
root file does not apply here.

Three things still apply, repo-wide: the session memory rules, the general
preferences (stop if asked to do too much; don't read large docs whole;
delegate wide exploration), and **the life of a demo** — the four steps every
demo runs, ending in a pattern cards PDF. Read that section in the root file
before closing a phase; demo 04's deck is `docs/demo-04-pattern-cards.html`
and `docs/demo-04-pattern-cards.pdf`.

## Everything for this demo lives in this folder

Plan, business rules, diagrams, Compose file, Go module, README. **Do not put a
demo 04 file anywhere else** — not in `.claude/plans/`, not in `obsidian/`, not
in a shared `diagrams/` directory.

| What | Where |
|---|---|
| Plan and phases | `docs/Demo-04-Plan.md` |
| Business rules | `BUSINESS_RULES-ODOMETER.md` |
| Diagrams (HTML + exported PNG) | `diagrams/` |
| Lab shell intro text, and lesson 01 | `README.md` |
| Lesson 02 | `docs/LESSON-02.md` |
| UI layout mockups | `diagrams/*.html` (dark UniFi palette) |
| Pattern cards (HTML + exported PDF) | `docs/demo-04-pattern-cards.*` |
| Go module | `cqrs/` |
| Compose | `deploy/compose.yaml` |
| Compose overlay — containers for the lab shell | `deploy/compose.shell.yaml` |
| Images | `frontend/Dockerfile`, `cqrs/Dockerfile` |
| Start / stop the whole demo | `deploy/start.sh`, `deploy/stop.sh` |

The one exception is `go.work` at the repo root, which must list `cqrs/` for
the module to build.

## Starting it

`deploy/start.sh` starts the WHOLE demo and `deploy/stop.sh` reverses it. Four
pieces have to be up before `/readyz` can answer yes: the NATS container,
`cqrs serve` on `20402`, and the snapshotter and projector that fill the two
KV buckets. `deploy/compose.yaml` holds only the container, so the compose
command alone is not enough — that was app-shell task 16k, finding 4, where
`demo.json`'s `runCommand` showed a reader a one-liner that left the demo in
the same `unknown` state the command was printed to fix.

The stream and both KV buckets need no separate step. `cqrs/main.go` runs
`ensureStream` and both `ensureKV` calls before it dispatches any subcommand,
so any `cqrs` process creates them.

The script converges — run it again on a demo that is already up and it starts
only what is missing. `stop.sh` stops only the processes the script's own PID
files name, so a `cqrs` started by hand is left alone. Run state lives in
`deploy/.run/` and is gitignored.

`public/demo.json`'s `runCommand` names `deploy/start.sh` and nothing else.
`lab-shell/src/shell/demos/demoRunCommand.spec.js` fails if that path stops
existing or stops being executable.

### Starting it for a PACKAGED lab shell

`start.sh` is the developer path: one container plus three Go processes on the
host. A packaged `plugin-source: registry` lab shell cannot reach a host
process, and it serves no plugin assets of its own (app-shell BR-AS03), so it
needs this demo's own **containers** instead. That is `deploy/compose.shell.yaml`,
added by app-shell task 16l:

```bash
docker network create lab-shell-plugins
```

```bash
docker compose -f compose.yaml -f compose.shell.yaml up -d --build
```

It adds four containers — `demo04-frontend` (nginx, the built Vue app),
`demo04-cqrs` (the command API and `/readyz`), `demo04-snapshotter` and
`demo04-projector`. Run both commands from `deploy/`.

**The network is the whole point, so do not change it casually.** The shell and
this demo do **not** join each other's private networks. They meet on a third
network, `lab-shell-plugins`, which is `external: true` on both sides — Compose
will not create it, so a plain `docker compose up` of either stack is unchanged
and cannot drift onto it. Only `demo04-frontend` and `demo04-cqrs` sit on it.
`nats` does not, and the shell cannot reach it. The shell joins from its own
side with `demos/01-dictionary/deploy/cell/compose.plugins.yaml`, named on the
command line — opt-in on both sides, never silent.

`frontend/public/demo.json`'s `assets.hostedUpstream` names
`http://demo04-frontend:80`, and the shell generates its `/plugins/demo-04/`
proxy from it. It is an **origin, with no path**: this image is built with
`base: '/plugins/demo-04/'`, so the prefix passes through unrewritten and a
chunk URL the compiler wrote cannot drift from the proxy.

`cqrs/Dockerfile` builds the same binary the host path builds, and
`demo04-cqrs` passes only `-url` and `-addr` — **never `-origin`**. F-3 still
holds: no CORS is added to this demo's backend. The browser reaches the command
API through the shell's own origin (app-shell BR-AS82).

`frontend/Dockerfile` copies this demo's `README.md`, `docs/` and `diagrams/`
into the build. They are **build inputs**, not runtime assets — the About page
imports them with `?raw` — and the image build fails without them.

## What this demo is

One question, answered with a number:

> **Rehydrating an aggregate — how much does a snapshot buy you?**

Demo 02's odometer has no write side: it publishes, then folds the result into
KV. Demo 04 adds the write side — a command that is checked against the state
the log already holds — and then shows the same rehydration with and without a
snapshot.

## What this demo is NOT

Agreed with the user 2026-09-14. Do not add any of it:

- no Postgres
- no cluster, no gateway, no supercluster, no JetStream domain
- no operator mode (a plain server, no `nsc` trust chain)
- no Temporal
- no `{context}` subject token

**Changed 2026-09-15 — a UI is now in scope.** "no frontend" and "no services"
were lifted for phase 04.6: a Vue app plus one thin HTTP shim (`cqrs serve`)
in front of the existing `domain.go`. The shim holds no rules. The rest of the
list above is unchanged. Read `docs/Demo-04-Plan.md` section 9 before touching
`frontend/`, `cqrs/serve.go` or `deploy/nats.conf`.

Multi-region belongs to demo 02. Accounts and topologies belong to demo 03.
Services, Postgres and UIs belong to demo 01.

## Relationship to demo 02

The domain is **lifted** from `demos/02-multi-region/odometer/`, then given a
lifecycle. The two are independent copies on purpose — demo 02 is sealed too,
and a shared package would couple two demos that are meant to be read alone.

Both demos name their stream `ODOMETER` and both run on their own NATS server.
That is not a clash, because they never share a server. Do not "fix" it by
renaming.

## Ports and contexts

| Thing | Value |
|---|---|
| NATS client | `4422` |
| NATS monitor | `8422` |
| NATS WebSocket | `20403` |
| Frontend | `20401` |
| Command API (`cqrs serve`) | `20402` |
| `nats` CLI context | `lab4-odometer` |

This demo's host ports follow `20<2-digit demo><2-digit increment>`, so they
never collide with demo 01's `7100-7299` bands. `4422` / `8422` predate the
scheme and stay.

**Every context name starts `lab4-`.** Demo 01 owns `sys` and `platform`, and
demo 02 owns `lab2-*`, in the same store (`~/.config/nats/context/`). An
unprefixed name here would silently overwrite one of theirs.

## One build, two entries

Added 2026-09-23 by task 16d of the app shell plan. `frontend/` is built once
and answers in two places:

| Entry | Built file | Chrome |
|---|---|---|
| Standalone | `index.html` → `src/main.js` → `App.vue` | its own `@ui-shell/AppShell` |
| Embedded | `remoteEntry.js` → `src/plugin.js` | supplied by `lab-shell` |

Neither entry is built separately and neither file is edited to switch between
them. The rules that hold this together:

- **The plugin entry must never render `@ui-shell/AppShell`.** `lab-shell`
  supplies the topbar, the rail and the theme toggle when the demo is embedded,
  and a second frame inside the first nests two chromes. `src/plugin.spec.js`
  fails if `AppShell` is even named in `src/plugin.js` or
  `src/plugin/OdometerRoute.vue`.
- **The body is one component, not two.** `src/components/LessonPanels.vue`
  holds the pagehead, both lesson panels and the wiring footer, and both entries
  render it. `src/view/useDemoState.js` holds the state both entries need — one
  call, one NATS connection. Everything else in `src/components/` is untouched:
  no panel became a route, and no menu, breadcrumb or tab was redesigned.
- **Where the rail sits is the only difference.** Standalone it goes in
  `AppShell`'s sidebar slot; embedded it goes in the route's own left column,
  because the shell declares no extension point for its own sidebar. Same
  `NavList`, same two lessons, same behaviour.
- **One route contribution.** `public/manifest.json` declares a single `route`
  (`/demo-04`) and one `navigation` entry. Splitting the lessons into two routes
  would change how the demo is navigated, and was rejected.

### The public path prefix

`vite.config.js` sets `base: '/plugins/demo-04/'`. The shell serves every one of
this plugin's assets under that one prefix — the entry, the lazy chunks, the
CSS, the fonts and the images, not the entry alone. The dev server follows the
same layout, so `http://localhost:20401/` now redirects to
`http://localhost:20401/plugins/demo-04/`. The proxied URL and the direct URL
are then the same path, and a relative asset URL is correct in both.

Port `20401` is unchanged, and `lab-shell` learns it from
`public/demo.json` — never from `public/manifest.json`, which has to be
byte-identical whichever source discovered the plugin.

**Task 16e added `/readyz`, and only `/readyz`.** The shell asks this demo
whether it is ready before it mounts it, and that question is same-origin: the
shell proxies its own `/demo-readiness/04-jetstream-cqrs` to this demo's
`/readyz`. `readyHandler` deliberately never calls `setCORS`, because nothing
cross-origin ever reaches it. The demo declares the route, the port and the
hosted upstream in `public/demo.json`; the shell generates both proxy rules
from that.

**Task 16j added the rest of the command API, route by route.** Until then the
write side was dead inside the shell: the page sat on the shell's origin, this
service granted only `http://localhost:20401`, and every button reported
`Failed to fetch`. The repair was the readiness route's sibling, not a widened
CORS grant — `cqrs/names.go` is unchanged and must stay so.

`public/demo.json` now carries an `api` block naming the dev port, the hosted
upstream and **every route, one by one**. The shell serves them at
`/demo-api/04-jetstream-cqrs/…` on its own origin. Two rules fall out of that,
and both matter:

- **A route this file does not name is not forwarded.** That is what stops the
  arrangement being a general tunnel to this service. Add a route to
  `cqrs/serve.go` and it stays unreachable from the shell until it is named
  here too.
- **`/readyz` is deliberately absent from `api.routes`.** It has its own
  declaration under `readiness`, because it is the shell's question to ask
  before mounting, not the page's to ask after. It is refused through the API
  prefix, and a spec holds that.

`src/config.js` exports `COMMAND_API` with `let`, not `const`: every call site
reads it as a default argument, so moving it once moves every later call.
`plugin.js`'s `activate()` is what moves it, and only when the shell mounts the
plugin. The standalone app on `20401` is unchanged, and the built bytes are the
same in both catalogue sources.

**The live view needed a second, separate change.** A WebSocket is not subject
to CORS at all, so the proxy above did nothing for it. It is gated by
`allowed_origins` in `deploy/nats.conf`, which named only `20401`, so the
embedded page sat at **Not connected**. That list now names `7110` as well, and
the file says why each entry is there.

Keep the two apart when reading this. The command API on `20402` is HTTP, gated
by CORS, and reaches the shell **through a proxy** so that no CORS grant has to
widen. The WebSocket on `20403` is gated by one list, and an origin is either in
it or refused. Loopback only, in both cases.

## The shim's routes

`cqrs serve` (port `20402`) is the only HTTP surface. It holds no rules — it
calls the same functions the CLI calls. Reads do NOT come through it: the
browser watches NATS over the WebSocket on `20403`.

| Route | Method | What it does |
|---|---|---|
| `/readyz` | GET | is the demo ready — the log and both KV buckets, asked each time |
| `/rehydrate` | GET | rehydrates one vehicle, with and without a snapshot |
| `/bench` | GET | the `ODOMETER_BENCH` fixture's size, in messages AND bytes |
| `/bench/seed` | POST | fills the fixture — 10 000, 100 000 or 1 000 000 |
| `/pool` | GET | lesson 02's state: stream size, buckets, whether a run is going |
| `/pool/run` | POST | runs the pool once, and answers with what that run measured |
| `/pool/stop` | POST | ends a run that is going (only Live runs open-ended) |
| `/pool/seed` | POST | fills `ODOMETER_POOL` |
| `/pool/rm` | POST | deletes `ODOMETER_POOL` and its three buckets |
| `/commands/` | POST | register, travel, retire — lesson 01's write side |

**Lesson 02 runs itself** (04.9). Every tab on the pool screen has a Run
button, and every number it shows comes from a run the reader just made.
There are no recorded measurements left in `frontend/src/view/` — deleting
them was 04.9.9, and `frontend/src/view/recorded.spec.js` fails if any come
back. `cqrs/docs_test.go` reads the routes out of `serve.go` and fails if
this table falls behind.

A run outlives the request that started it (D11), so `/pool/run` answers when
the run ENDS. `/pool/stop` is the only way to end an open-ended one. The gate
in `serve.go` allows one run at a time.

## Storage names

Root rule, and it holds here: **streams are `SCREAMING_SNAKE`, KV buckets are
`lowercase-kebab`.**

| Kind | Name | Role |
|---|---|---|
| Stream | `ODOMETER` | the log — the only source of truth |
| KV | `odometer-write` | write-side snapshot: `{state, lastSeq}` |
| KV | `odometer-read` | read model, denormalised |
| Stream | `ODOMETER_POOL` | lesson 02's OWN log — nothing else reads it |
| KV | `odometer-pool` | lesson 02 — a THIRD fold, deliberately wrong |
| KV | `odometer-pool-truth` | lesson 02 — the same log folded correctly |
| KV | `odometer-pool-workers` | lesson 02 — one key per worker |
| Stream | `ODOMETER_BENCH` | the rehydrate fixture, disposable |
| KV | `odometer-bench-write` | the fixture's snapshots |

Two buckets, not one. The split is the demo — you can see it in `nats kv ls`.

`ODOMETER_POOL` and its three buckets belong to lesson 02 and to nothing else.
Its subject is `evt.odometer-pool.>`, which does not overlap `evt.odometer.>`
because the second token differs — a DOT instead of the hyphen would put the
pool inside the demo's own filter, the same trap `ODOMETER_BENCH` avoids the
same way. Build it with `cqrs pool -seed N`.

The split exists because the pool MISBEHAVES on purpose: its consumer starves
and redelivers, and under `-kill-at` it abandons messages unacked. Before
04.8 that happened on `ODOMETER`, the log every other screen is drawn from.

Careful: `ODOMETER` is a PREFIX of `ODOMETER_POOL`, so a half-copied name
still reads as plausible. A check for "does this name appear" must match on a
boundary — the frontend spec uses `/ODOMETER(?!_POOL)/`.

`odometer-pool` is kept apart from `odometer-pool-truth` on purpose: the pool
is deliberately wrong, and a demo that damaged the correct fold to show that
would have nothing left to compare against. The damage is measured against
`odometer-pool-truth` and never against `odometer-read` — that bucket folds a
different log.

`ODOMETER_BENCH` is a fixture, not the demo. Its subject is
`evt.odometer-bench.>`, which does not overlap `evt.odometer.>` because the
second token differs — a DOT instead of the hyphen would put the fixture
inside the demo's own filter. Build it with `cqrs bench -size N`, delete it
with `cqrs bench -rm`. Nothing else may be added to it, and nothing on the
other tabs may read from it.

## A count is never shown without its bytes

Standing rule, set by the user 2026-09-16. Anywhere this demo reports a
stream's message count — on screen, in the CLI, in a JSON body — it reports
the bytes that count consumes as well. A length is a number nobody can price:
100 000 000 events sounds reasonable right up to the moment you learn it is
7.6 GB.

It applies to `ODOMETER`, `ODOMETER_BENCH` and `ODOMETER_POOL`, and to
nothing else for now.
`frontend/src/view/format.js` has `formatBytes()`; `useOdometer.js` already
carries `bytes` beside `messages`.

## Quality rules

Same as the root file, scaled down:

1. Every business rule has a Ginkgo spec, written before the implementation.
2. `ginkgo ./...` from `cqrs/` is green before a phase is done.
3. Business rules live in `domain.go`. No rule enforcement in `main.go`,
   `write.go` or `read.go`.
4. `BUSINESS_RULES-ODOMETER.md` and `docs/Demo-04-Plan.md` change in the same
   commit as the rule.

The frontend has its own three, and all three must pass before a UI task is
done. Run them from `frontend/`:

```bash
npx vitest run                      # 502 specs, 34 files
npx eslint src --ext .js,.vue       # 0 errors; 3 warnings are the baseline
npm run build
```

Seven live guards, not unit tests. Do not weaken one to make a screen pass —
adding to the list a guard checks is the right move.

- `frontend/src/view/commands.spec.js` parses `cqrs/main.go` for the
  subcommands and flags that actually exist, and fails when the UI prints a
  command the binary would reject.
- `frontend/src/view/recorded.spec.js` walks `src/` and fails if a deleted
  recorded-measurement file or constant comes back.
- `cqrs/docs_test.go` guards the route table above, in two halves. It reads
  the routes out of `serve.go` and the rows out of this file, and fails
  unless the two sets match EXACTLY — a row deleted, a row left behind, or a
  route added without a row. Then it stands the shim up under `httptest`
  with every collaborator stubbed and sends a request per row: the Method
  column must be accepted, and the other method must answer 405. A route
  registration carries no method, so only a request can check that column.
- `frontend/src/view/lesson-docs.spec.js` reads `README.md`,
  `docs/LESSON-02.md` and this file. It fails both ways round: if a lesson 02
  heading comes back into the intro, and if the lesson 02 document stops
  holding one.
- `frontend/src/components/AboutPanel.spec.js` reads `AboutPanel.vue` and
  fails if it names a lesson file again.
- `frontend/src/about/how-it-works.spec.js` reads
  `diagrams/lesson-02-how-it-works.html` and fails if a drawing loses its
  `aria-label`, if a figure goes missing, or if a measured number or a worker
  count is written onto the page (D18, D19).
- `cqrs/cards_test.go` reads `docs/demo-04-pattern-cards.html` and fails if a
  card title goes missing, if a card loses its pro or con panel, if the
  exported PDF is absent, or if the "Where every number came from" page stops
  naming the NATS version and the date. It checks the deck's SHAPE. It cannot
  check whether a number is still true — only a re-run can do that.

## One lesson, one file

Set 04.12 (2026-09-17). `README.md` was the lab shell's intro AND lesson 01's
Overview AND lesson 02 — so a reader who opened lesson 01's Overview was
handed the pool lesson as well, measurements and all.

| Lesson | Source | Rendered as |
|---|---|---|
| 01 | `README.md` + `diagrams/demo04-jetstream-cqrs.html` | Overview → `What it does` / `Classes and sequences` |
| 02 | `docs/LESSON-02.md` + `diagrams/lesson-02-how-it-works.html` | Overview → `What it does` / `How this works` |

`frontend/src/components/AboutPanel.vue` is ONE component used twice. It takes
its files as props and names neither (D21). The naming lives in
`frontend/src/about/sources.js`, one entry per lesson. A new lesson is an
entry there, not a copy of the panel.

A lesson with no drawings shows ONE sub-tab. A tab that opens on nothing is a
promise the screen does not keep. 04.11 supplied lesson 02's page, so both
lessons show two sub-tabs today; a lesson added tomorrow with no drawings
still shows one.

**The drawings hold no numbers** (04.11, D18 and D19). Lesson 02's page draws
the mechanism — the key gap, the in-flight window, the redelivery wait — and
names no worker count, no vehicle count, no cap value and no measurement. Every
number on the screen comes from the run the reader just made, and a constant
printed beside a live result is a constant that will disagree with it. Each
running tab carries a one-line pointer to the page instead of a second copy of
the explanation.

## The design gate

A phase entry in `docs/Demo-04-Plan.md` stays **PROPOSED** until the user
approves it. No tasks, no tests, no code before that. An entry marked PROPOSED
is a request for a decision, not a backlog item to pick up.

Nothing is PROPOSED right now.

**04.12 is COMPLETE** (2026-09-17) — one Overview per lesson, one source file
per lesson. See `docs/Demo-04-Plan.md` section 15, decisions D20 to D23.

**04.11 is COMPLETE** (2026-09-17) — lesson 02 explains itself with four
drawings in `diagrams/lesson-02-how-it-works.html`, rendered as the Overview
sub-tab 04.12 made room for. D16 (a sixth top-level tab) is superseded by
D20; the page is both the mockup and the shipped page, which keeps D17's
point (SVG, not PNG). See section 14.

**04.10 is COMPLETE** (2026-09-17) — lesson 02's log is seeded over THREE
vehicles, not ten. Round robin puts two events of one vehicle
`len(PoolVehicles)` apart, and ten was so wide the pool never folded anything
out of order: every run reported `dropped = 0` and the lesson taught nothing.
Three is below every worker count the lesson runs but one. One worker still
drops nothing. See `docs/Demo-04-Plan.md` section 13, decisions D13 to D15.

**04.9 is COMPLETE** (2026-09-17) — lesson 02 runs itself: a Run button on
every tab, results from a real run instead of recorded constants, and a
progress bar for the whole of a multi-run set (D12, the condition it was
approved on). See `docs/Demo-04-Plan.md` section 12 and
`diagrams/lesson-02-run-buttons.html`.

**04.8 is COMPLETE** (2026-09-16) — lesson 02 got its own log,
`ODOMETER_POOL`, so the pool consumer stops misbehaving on the demo's own
stream. See `docs/Demo-04-Plan.md` section 11, decisions D8 to D12.

04.7.18 (Performance gets one target picker and one seed button) was approved
and completed 2026-09-16.

## Two mechanics that are easy to get wrong

**LimitsPolicy, not InterestPolicy.** This demo replays from sequence 1.
InterestPolicy discards a message once every consumer has acked it, and the
replay then returns nothing — with no error.

**The snapshot is always stale.** The write consumer is asynchronous, so the
snapshot trails the stream. Rehydration reads the snapshot and then replays the
tail from `lastSeq + 1`. Code that stops at the snapshot is a bug, not a
shortcut.
