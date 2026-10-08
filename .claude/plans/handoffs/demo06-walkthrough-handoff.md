# Demo 06 — walkthrough handoff

**Target: Claude.** The user runs the commands in their own terminals; you
guide one step at a time and read back what they paste.

**Output style:** the user wants ELI5 / ASD-STE100 Simplified Technical
English: short sentences, active voice, only what is needed (what I did, did
it work, what to do now). At most 2 options when a decision is needed, with a
recommendation.

## Goal

Walk the user through `demos/06-operator-trust-and-credentials` exercise 01
by hand, step by step, from the steps file. Fix any step that is wrong or
hides a command. This is the start of `D06-R9` (a person runs the README
with no help); a walkthrough guided by Claude does **not** close `D06-R9`.

## Done so far (branch `poc/demo06`, nothing pushed)

- `75193ef` demo 06 built: 3 exercises, steps, check scripts, observations.
- `35da24b` terms: centralized (config mode, demo 05) vs decentralized
  (operator mode, demo 06); auth callout = demo 07, operator mode only, no
  folder yet.
- `8e35bc2` rule: auth demos are transparent and terminal-first; three kinds
  of file (walkthrough / thin convenience scripts / validation scripts).
- `f6d3c1c` demo 06 has no Python or Perl. `d06_claims` (base64 + jq),
  `exercises/wrong-seed.sh`, bash 5 `EPOCHREALTIME` in `ex03-check.sh`.
  9 runs (3 × ex01/02/03), all ALL PASS, recorded in
  `exercises/EXERCISE_OBSERVATIONS.md`.
- `8360f08` EXERCISE-01: new **Initial setup** section (3 terminals,
  `lab/down.sh --clean`); Step 1 now lists the 5 `nats auth` commands that
  `lab/chain.sh` wraps.

The walkthrough has **not started**. The user was asked to run Initial setup
and Step 1 and paste the output. Nothing came back yet.

## Next steps

1. Ask the user for the Initial setup + Step 1 output, or ask them to run it.
   **Check:** 5 commands succeed; `.run/trust.conf` exists.
2. Go through EXERCISE-01 Steps 2–15 one at a time. Before each, ask the
   user's prediction. After each, compare their paste with "What you should
   have seen" at the end of the file.
   **Check:** each step's output matches, or the step file is fixed.
3. Fix each wrong or unclear step in the file as you go. Record a real
   surprise in `EXERCISE_OBSERVATIONS.md` (dated, as "walkthrough"), not as a
   new measurement unless it was measured.
   **Check:** if a step or script changed, run `exercises/ex01-check.sh`;
   it must print ALL PASS.
4. Commit only files changed in the session. Never push.
5. Ask the user before you start exercise 02 or 03.

## Decisions made — do not re-open

- No "Step 0" anywhere. Preparation is a section headed **Initial setup**.
  Exercise numbers start at 01.
- Walkthrough shows direct `nats auth` / `nats-server` commands. `lab/`
  scripts are thin conveniences; `exNN-check.sh` and `wrong-seed.sh` are
  validation / negative-test machinery and must be labelled so.
- No inline Python in demos. Prefer native NATS commands to decoding JWTs.
- Every `nats` call goes through `lab/nats.sh` (adds `--no-context`,
  keeps keys in `.run/`).
- Demo 06 scope: one operator, one account `ORDERS`, `full` resolver. A
  throwaway untrusted operator `ROGUE` is allowed only to be refused. No auth
  callout, no second account, no bearer tokens.

## Traps

- `nats auth … --json` prints seeds. Never use it in a walkthrough.
- The rogue `account push` prints `Success 1 Failed 0` but the server does
  not load it (`/accountz`). The push report is not evidence.
- The client always prints `Authorization Violation`; causes differ only in
  the server log (e.g. `fetch took ~1.9s` for an unknown account).
- The `authentication error` count in the log can be higher than the number
  of refused attempts (7 instead of 5, once in 6 runs). Cause not proven.
- Never `pkill nats-server`; demos 03 and 05 run bare servers too. Use
  `lab/down.sh`.
- `.run/` currently holds keys left by the last `ex03-check.sh` run. Initial
  setup deletes them; `lab/chain.sh` refuses to run over an existing operator.
- EXERCISE-02 Step 1 and EXERCISE-03 Step 1 still call `lab/chain.sh`.
  That is acceptable because exercise 01 shows the commands; revisit only if
  the user asks.
- Git identity is auto-set (`Jeremy <jeremy@Jeremys-MacBook-Pro.local>`).
  The user knows; do not change it.
- One command per Bash call; no `&&` chains (repo rule).

## Read first

- `demos/06-operator-trust-and-credentials/CLAUDE.md`
- `demos/06-operator-trust-and-credentials/exercises/EXERCISE-01-TERMINAL-STEPS.md`
- `.claude/memory/demos_transparent_terminal_first.md`
