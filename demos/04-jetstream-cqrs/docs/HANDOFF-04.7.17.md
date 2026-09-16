# Handoff prompt — task 04.7.17

Paste everything below the line into a new session (Claude Code or Codex).
It is written to stand alone: a session that reads no other file still has
enough to start, and enough to know what it must not do.

---

Work in `/Users/jeremy/dev/github/jthomas78/nats-tech-lab/demos/04-jetstream-cqrs`,
branch `poc/dictionary3.1`.

**Read these three files first, in this order:**

1. `CLAUDE.md` in that folder — this demo is a sealed unit and that file
   overrides the repo root's `CLAUDE.md` for anything inside it.
2. `docs/Demo-04-Plan.md`, entry **04.7.17** (search for `04.7.17`). It is the
   task. It is long on purpose: every choice in it has its reason next to it,
   and the reasons are the part that matters.
3. `diagrams/lesson-01-three-tabs.html` — the approved mockup. Open it in a
   browser. It draws all three tabs and lists what moves.

**The job.** Lesson 01's tab strip becomes three tabs instead of five:
Overview, Showcase, Performance. Nothing that is on screen today is deleted;
things move. The plan entry says exactly what goes where, and the mockup
shows it.

**These were decided by the user. Do not reopen them.**

- The rail row `Guide · How it works` is deleted. The Overview tab reuses
  `AboutPanel.vue` whole, with a new lesson summary and reference links above
  it. Do not write a second copy of that explanation.
- **The reference links are exactly these two, and they are outside links:**
  `https://docs.nats.io/learn/jetstream/` and
  `https://learn.microsoft.com/en-us/azure/architecture/patterns/cqrs`.
  Do not render `BUSINESS_RULES-ODOMETER.md` in the app. Do not add a third
  link.
- **Overview is still the tab that opens first.** Keep the tab key `overview`
  and the current default. Do not make Showcase the landing tab.
- **The vehicle picker moves out of the pagehead and into the Showcase tab.**
  `App.vue` loses it and stops owning the selected vehicle;
  `StreamCqrsPanel.vue` owns it, above the four groups. Performance needs a
  vehicle too, so `RehydratePanel.vue` gains its own live-vehicle picker next
  to the bench target it already has. Two pickers is the accepted answer, not
  an oversight — the user chose it over one global control that is dead on
  two tabs in three. `crumbFor()` then no longer takes a vehicle: the crumb
  becomes the lesson and its title only.
- In the KV Stores group, a picked vehicle shows THAT VEHICLE'S DOCUMENT on
  top and the full key list under it, on both sides.
- The tab is named `Showcase`. Not `Demo`, not `Live` — lesson 02 already has
  a tab keyed `live`.
- No sub-tab strip under Performance while Rehydrate is the only thing there.
- Showcase always reads `ODOMETER`. The bench fixture stays under
  Performance, on `ODOMETER_BENCH`.

**Rules that will fail the task if broken.**

- **Two buckets, side by side.** `CLAUDE.md`: "Two buckets, not one. The split
  is the demo." The KV Stores group shows `odometer-write` and `odometer-read`
  together. A layout that leaves one visible has broken the demo.
- **A count is never shown without its bytes.** Standing rule, set by the user
  2026-09-16, for `ODOMETER` and `ODOMETER_BENCH` only. Use `formatBytes()` in
  `frontend/src/view/format.js`. `useOdometer.js` already carries `bytes`
  beside `messages` and nothing draws it yet — this task draws it.
- **Every panel that shows a number names the command that produced it.** The
  KV Stores group and the Stream group each print their `nats` line.
- **Business rules live in `cqrs/domain.go`.** This is a layout task. It must
  not move, add or soften a single rule.
- **`frontend/src/view/commands.spec.js` is a live guard.** It parses
  `cqrs/main.go` for the subcommands and flags that really exist and fails
  when the UI prints a command the binary would reject. Do not weaken it to
  make a screen pass. If it fails, the screen is wrong.

**Specs come before the implementation.** Rewrite
`frontend/src/components/StreamCqrsPanel.spec.js` around three tabs first,
watch it go red, then build. The plan entry lists what those specs must
assert. `App.spec.js` and `lessons.spec.js` change too: the pagehead picker
and the crumb's vehicle both go.

**Done means all of this, from `frontend/`:**

```bash
npx vitest run                      # was 318 specs / 21 files before this task
npx eslint src --ext .js,.vue       # 0 errors; 7 PoolPanel.vue warnings are the baseline
npm run build
```

and from `cqrs/`:

```bash
go build -o cqrs . && go vet ./... && ginkgo ./...   # 122 specs, all green
```

**Then look at it.** The design viewport is 1920x1080. The stack must already
be running:

```bash
docker compose -f deploy/compose.yaml up -d
```

```bash
cd cqrs && ./cqrs serve
```

```bash
cd frontend && npm run dev     # http://localhost:20401
```

**Finish by** marking 04.7.17 `[x]` in `docs/Demo-04-Plan.md` with a short
note on what was actually built, updating the demo's `README.md` if the tab
names it describes changed, and moving section 10.9's bullet "Lesson 01's
Overview tab showing one bucket instead of two (D10)" onto the Showcase tab —
all in the same commit as the code.

**Do not** push, and do not rename any stream or KV bucket. A bucket name is
a stream name: renaming one orphans its data instead of moving it.
