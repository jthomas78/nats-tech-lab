# Demo 07 — plan and start handoff

**Target: Claude.** Plan demo 07 with the user, then start it. Work one
stage at a time and stop for the user's yes between stages.

**Output style:** the user wants ELI5 / ASD-STE100 Simplified Technical
English: short sentences, active voice, only what is needed (what I did, did
it work, what to do now). At most 2 options when a decision is needed, with a
recommendation.

## Goal

Create `demos/07-…/` (the name is a stage-01 decision) and take it through
playbook stage `01 Define the question` and stage `02 Design the rig`. Start
stage `03` only with the user's yes. The user asked for this on 2026-10-09;
the folder did not exist before that.

**What demo 07 is (already decided):** decentralized authentication with an
external **auth callout**, in NATS **operator mode** only. The NATS
operator/account trust base stays. A callout service validates a **WorkOS**
identity and maps it to NATS access through the signed NATS authorization
request/response.

## Read first (in this order)

1. `demo-playbook.html` — the one demo lifecycle, stages `01`–`04`. Grep it;
   do not read it whole.
2. `.claude/memory/nats_auth_terminology_and_demo_boundaries.md` — the terms
   and the hard scope lines for demos 05, 06, 07.
3. `.claude/memory/demos_transparent_terminal_first.md` — walkthrough /
   convenience / validation files; no inline Python; no "Step 0".
4. `.claude/memory/demos_are_small_slices_not_demo01.md`.
5. `demos/06-operator-trust-and-credentials/CLAUDE.md` and `README.md` — the
   closest model for shape, port table, `.run/`, `lab/nats.sh`, quality
   rules. Copy the **shape**, never the files.
6. `demos/06-operator-trust-and-credentials/docs/demo-06-pattern-cards.pdf`
   — what demo 06 measured, so demo 07 does not re-measure it.
7. The NATS docs: <https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_callout>
   and <https://docs.nats.io/learn/security/decentralized-auth>. Read before
   any claim is written.

## Hard rules — do not re-open

- **Operator mode only.** The NATS docs' auth callout example switches to
  configuration mode. **Never copy that example.** An online external auth
  service does not make it "centralized configuration mode".
- **Builds from nothing.** Demo 07 makes its own operator and chain. It does
  not use, copy, start or wait for demo 06's files, keys or server. Demo 06
  never starts or feeds demo 07. Same rule for demo 01: background only.
- **The folder is a sealed unit.** Its own `CLAUDE.md` is written **before
  any other work** in the folder. Then add one pointer paragraph ("Running
  demo 07") to the root `CLAUDE.md` under "Commands" — a pointer, never a
  copy.
- **Transparent and terminal-first.** Direct `nats auth` / `nats-server`
  commands in `exercises/EXERCISE-NN-TERMINAL-STEPS.md`; thin `lab/` scripts;
  `exercises/exNN-check.sh` for assertions. Exercise numbers start at `01`.
  Preparation is a section headed **Initial setup**.
- Every `nats` call passes `--no-context` and keeps keys inside the demo's
  gitignored `.run/` (demo 06's `lab/nats.sh` shows how). Never use
  `nats auth … --json` in a walkthrough: it prints seeds.
- Every requirement gets an ID (`D07-R1`, `D07-R2`, …). The demo declares
  its role: showcase, validation, or both.
- Claude never types a real WorkOS API key or password. The user puts keys
  in a gitignored env file. Never commit keys, seeds or `.creds`.
- One command per `Bash` call; no `&&` chains. Never `pkill nats-server`
  (demos 03, 05, 06 run bare servers on the same host).
- Commit only files changed in the session. Never push.

## Decisions for the user (stage 01–02)

Ask these one at a time, each with at most 2 options and a recommendation:

1. **Role:** showcase, validation, or both. (Demo 06 was both.)
2. **The primary question,** one sentence, like demo 06's "Can a NATS server
   admit a user it has never seen by name…?"
3. **WorkOS: real or stand-in.** A real WorkOS test environment needs network
   access and a key from the user. A local stand-in token issuer keeps the
   demo offline but does not prove WorkOS. Possible split: exercises 01–02
   offline, one later exercise against real WorkOS.
4. **The callout service's language.** Demo 06 had no code at all; a callout
   needs a small service. Candidates: Go with `nats.go` + `jwt/v2` (the
   repo's language), or the Synadia callout library. Keep it small and
   readable — it is part of the teaching path.
5. **Ports.** Pick a free client/monitor pair (demo 06 uses `4922`/`8922`).
   Grep the repo for the pair before choosing, and record it in the demo's
   README port table.
6. **Exercise list.** Keep it small. Candidates: callout admits a valid
   identity; callout refuses (bad token, expired token, unknown user);
   identity mapped to account + permissions; what happens when the callout
   service is down (timeout); callout response signing / the issuer
   account's role. Docs coverage alone never widens the list.

## Out of scope unless the user says so

Configuration mode; a second operator trusted by the server; clusters,
gateways, leaf nodes, JetStream, TLS; a frontend or lab-shell page (stage 03
decides, terminal first); changes to any other demo.

## State at handoff

- Branch `poc/demo06`, commit `eb778e6` (demo 06 pattern cards). Nothing
  pushed. Demo 06 is complete except `D06-R9` (another person runs its
  README with no help).
- No `demos/07-*` folder exists. No server is running for demo 06.

## Done when (this handoff)

- `demos/07-…/CLAUDE.md` and `README.md` exist, with the role, the primary
  question, the `D07-R*` requirements table, the port table and the scope
  lines.
- The root `CLAUDE.md` has the "Running demo 07" pointer, and the memory note
  `nats_auth_terminology_and_demo_boundaries.md` no longer says the folder
  does not exist.
- The user has approved the exercise list. Exercise 01 is built by hand in
  the terminal first, only after that approval.
