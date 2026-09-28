# Handoff — demo 03, build `lab/09-switch-t4-t5.sh`

Target: **Claude** (Codex is out of credits as of 2026-09-28).

## Goal

Measure `D03-R11` (T4 → T5) and `D03-R12` (T5 → T4): can a running system be
converted **in place**, with traffic paused and stores kept, and keep every
acknowledged message, consumer position and KV value — then carry on working?

## Done so far

- `D03-R11` / `D03-R12` defined in `demos/03-multi-cluster-and-accounts/README.md`
  (section "D03-R11 and D03-R12 — switching topology in place"). **That section
  is the spec.** Read it before anything else.
- Added to the "Still open" list in `demos/03-multi-cluster-and-accounts/CLAUDE.md`.
- The decision guide (`docs/nats-topology-decision-guide-v0.1.html`) already
  marks the switch as not tested. Change it only after a verdict exists.
- Commit: see `git log --oneline -3` — the "topology decision guide v0.1 …
  define D03-R11/R12" commit.

## Next steps, in order

1. **Stage 02 design first, in words, before code.** Write the procedure for
   each direction as a short list in the script header: which servers stop, which
   config lines change, which start again, and in what order.
   *Check:* the user agrees with the procedure before you run it.
2. **Write `lab/09-switch-t4-t5.sh`.** Two independent runs in one script:
   - Run A: build T4 (copy the shape from `06-arbiter3.sh`), seed, stop, rewrite
     configs to T5, start, verify.
   - Run B: build a **fresh** T5 (from `04-hub-and-leaf.sh` /
     `08-hub-leaf-per-region.sh`), seed, convert to T4, verify. Run B twice:
     clean names, and the same stream name in account `LB` on both sides.
   *Check:* `bash -n` passes; the script only ever creates `t-*` configs.
3. **Seed with identity.** Each message carries a unique ID (`Nats-Msg-Id`
   header or ID in the payload). Keep the JetStream ack for each one (a
   JetStream publish that waits for the ack — check `nats pub --help` for the
   flag on the installed CLI — **not** a core `nats pub`). Record each durable
   consumer's acked sequence and one KV bucket's keys and revisions before the
   stop.
   *Check:* the seed step writes an ID list file into `lab/run/`.
4. **Verify after the switch.** Match every acked ID and payload (not counts).
   Compare consumer acked position and KV revisions. Then publish new messages,
   resume the consumers, update the KV key, restart the destination once, and
   check again.
   *Check:* each is a `check` row with an exact expected answer. Use `note`
   for timings and anything you cannot predict.
5. **Wire it in.** Add the script to `LABS` in `lab/run-all.sh`, and add its
   rows to `lab/render-report.py` (lines ~210, ~319, ~781, ~1043 list every
   script — add yours the same way). Re-render and audit:
   `node demos/01-dictionary/diagrams/audit-svg-layout.mjs demos/03-multi-cluster-and-accounts/REPORT.html`
   → `0 errors, 0 warnings`.
6. **Record the verdict** per direction in `README.md`: *passed with a measured
   interruption*, *failed*, or *inconclusive*, with the procedure and check IDs.
   Update `CLAUDE.md` "Measured facts" / "Still open" to match. Then update the
   guide's open item.

## Decisions already made — do not re-open

- Convert **in place** only. No live writers, no switch interrupted partway,
  no rollback, no side-by-side migration. Those are later slices.
- Each direction starts from a fresh system. No T4 → T5 → T4 round trip.
- Writers and consumers are paused during the switch.
- Match by ID and payload, never by count alone.
- Verdict words are fixed: *passed with a measured interruption*, *failed*,
  *inconclusive*. A failure may show a symptom, not a proven cause.
- New requirement IDs are `D03-R11` and `D03-R12`. `D03-R10` is taken.

## Traps found

- **`lab_init` in `lab/_common.sh` deletes `run/js`**, and its `trap lab_down
  EXIT` kills every server when the script ends. A conversion must stop the
  servers **without** calling `lab_init` again, or the stores are gone and the
  test proves nothing.
- **Store directories are keyed by server name**: `store_dir: "./js/<name>"`
  (`conf_head`). T4's third site is `arb-1..3` (ports 454x/854x). T5's hub is
  `hub-1..3` (ports 455x/855x). If the third site becomes the hub, decide on
  purpose whether its name (and so its store dir) carries over. Write that down
  in step 1.
- **T4 has no `jetstream.domain`; T5 sets one per side** (`hub`, `za`, `au`).
  The switch changes the domain on an existing store. That is the heart of the
  question. Do not guess what it does. Measure it.
- A **core `nats pub` proves nothing** about storage. It exits 0 even when
  nothing was stored.
- **Never read `/jsz?meta=1` once.** Use `wait_live_leader` / `wait_no_leader`.
  A new leader can also be named before it can act: retry the create (see
  `06-arbiter3.sh` lines ~95–110).
- After a leaf-link change, wait about **5 s** for interest to spread.
- Kill only with `pkill -f "nats-server -c t-"`. Stop a server with
  `kill_server` (TERM), not `kill -STOP`, when you mean to restart it with a
  new config. `kill -STOP` is for "region goes dark" only.
- `REPORT.md` and `REPORT.html` are generated. Never hand-edit them.
- The git index held another session's staged work (pattern cards v0.3,
  findings v0.9, a `CLAUDE.md` block). Commit only your own paths.

## Read first

1. `demos/03-multi-cluster-and-accounts/CLAUDE.md` (the demo's rules; read
   instead of the root file)
2. `demos/03-multi-cluster-and-accounts/README.md` — the D03-R11/R12 section
3. `demos/03-multi-cluster-and-accounts/lab/_common.sh`
4. `demos/03-multi-cluster-and-accounts/lab/06-arbiter3.sh` (T4)
5. `demos/03-multi-cluster-and-accounts/lab/08-hub-leaf-per-region.sh` (T5)
