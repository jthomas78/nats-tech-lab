# Demo 06 — walkthrough handoff

**Target: Claude.** The user runs the commands in their own terminals; you
guide one step at a time and read back what they paste.

**Output style:** the user wants ELI5 / ASD-STE100 Simplified Technical
English: short sentences, active voice, only what is needed (what I did, did
it work, what to do now). At most 2 options when a decision is needed, with a
recommendation.

## Goal

All three exercises of `demos/06-operator-trust-and-credentials` are now
walked by hand and recorded. What is left: the three open walkthrough
design points from the Codex review, then the demo's completion items. A
walkthrough guided by Claude does **not** close `D06-R9` (another person
runs the README with no help).

## Done so far (branch `poc/demo06`, nothing pushed)

Earlier: `75193ef` built, `35da24b` terms, `8e35bc2` file kinds, `f6d3c1c`
no Python/Perl, `8360f08` EXERCISE-01 Initial setup.

This walkthrough (2026-10-08 to 2026-10-09):

- `fa9956e` exercise 01 by hand; steps fixed (mkdir creds, raw
  `nats-server` step).
- `95c3bdd` exercise 02 by hand, all 15 steps matched. Scoped-key
  `Max Payload: 0` vs `unlimited` recorded as **source-derived, not
  runtime-verified**, with pinned source links.
- `19557f8` optional payload probe (explicit 100-byte scoped limit):
  **inconclusive** for server enforcement — the client refused the 200 B
  message itself (`nats: maximum payload exceeded`); the server logged
  nothing.
- `2cb7438` exercise 03 by hand (expiry + revocation), all matched.
- `bc14004` EXERCISE-03 Step 8 gets a **Timing** line (run it in the ~30 s
  window after the cut, before `--wait 90s` ends). ex03-check ALL PASS.
- `0dd7c85` check scripts hardened after a Codex review: preflight before
  the cleanup trap (it used to stop an already-running server; tested);
  `E5a` positive control; R1 needs a received message; R6/C9 parse
  `/accountz` with `jq`; README seed claim relabelled "from the docs, not
  measured". ex01/02/03 each ALL PASS (2026-10-09 21:51).

All results are in `exercises/EXERCISE_OBSERVATIONS.md` under the
"Walkthrough — exercise NN by hand" and "Check-script fixes after a Codex
review" sections.

**State now:** no server running; port 4922 free. `.run/` holds a chain
left by the last `ex03-check.sh` — not the walkthrough's keys. Any new hand
run starts with `lab/down.sh --clean`.

## Next steps

Ask the user which one to start. Do not do them all at once.

1. **Codex point 5 — EXERCISE-03 Step 3.** "To within a second" ignores the
   typing gap between `date` and the credential command; Steps 3–5 can also
   eat the 60 s. Option: one timed sequence, plus "re-issue if you missed
   the pre-expiry message".
   **Check:** `exercises/ex03-check.sh` prints ALL PASS.
2. **Codex point 6 — EXERCISE-01 Step 1.** `lab/trust-conf.sh` writes three
   lines (`operator`, `system_account`, `resolver_preload`) that the step
   does not show. Step 2 shows the result with `cat`. Option: show the
   three lines and how each value is read; keep the script as a shortcut.
   **Check:** `exercises/ex01-check.sh` prints ALL PASS.
3. **Codex point 7 — refusals checked by client text only:** ex02
   P4/P9/P11, ex03 E4/R4. Add a server-log assertion (count of
   `authentication error` or the violation line) per refusal, and the
   matching command in the steps files.
   **Check:** all three check scripts print ALL PASS.
4. Commit only files changed in the session. Never push.
5. Later, not this session unless asked: `D06-R9` (another person) and the
   pattern cards deck (`pattern-cards` skill → `docs/demo-06-pattern-cards.html`/`.pdf`).

## Decisions made — do not re-open

- No "Step 0" anywhere. Preparation is a section headed **Initial setup**.
  Exercise numbers start at 01.
- Walkthrough shows direct `nats auth` / `nats-server` commands. `lab/`
  scripts are thin conveniences; `exNN-check.sh` and `wrong-seed.sh` are
  validation machinery and must be labelled so.
- No inline Python in demos. Prefer native NATS commands to decoding JWTs.
- Every `nats` call goes through `lab/nats.sh` (adds `--no-context`, keeps
  keys in `.run/`).
- Demo 06 scope: one operator, one account `ORDERS`, `full` resolver. A
  throwaway untrusted operator `ROGUE` only to be refused. No auth callout,
  no second account, no bearer tokens.
- EXERCISE-02 Step 1 and EXERCISE-03 Step 1 call `lab/chain.sh`. Accepted,
  because exercise 01 shows the commands; revisit only if the user asks.
- The payload probe stays optional and recorded as inconclusive. It tested
  an explicit 100-byte limit, never the omitted/0/unlimited case. Do not
  describe it as proving both.

## Traps

- `nats auth … --json` prints seeds. Never use it in a walkthrough.
- The rogue `account push` prints `Success 1 Failed 0` but the server does
  not load it (`/accountz`). The push report is not evidence.
- The client always prints `Authorization Violation`; causes differ only in
  the server log.
- **The check scripts delete `.run/`.** Never run one in the middle of a
  hand walkthrough. Ask first.
- Expiry: the cut listener exits **0**. The exit code does not show the
  cut. A listener with no `--wait` keeps reconnecting until `Ctrl-C` (exit
  130); every try is one `authentication error` line in the log.
- `auth user rm … --revoke` changes only the local account JWT. Nothing
  happens until `auth account push`. The `revoked_user` time is the `rm`
  time, not the push time.
- Not explained: on the first expiry run, reconnect lines ran ~50 s past
  `--wait 90s`. Recorded; not reproduced.
- Never `pkill nats-server`; demos 03 and 05 run bare servers too. Use
  `lab/down.sh`.
- Git identity is auto-set (`Jeremy <jeremy@Jeremys-MacBook-Pro.local>`).
  The user knows; do not change it.
- One command per Bash call; no `&&` chains (repo rule).

## Read first

- `demos/06-operator-trust-and-credentials/CLAUDE.md`
- `demos/06-operator-trust-and-credentials/exercises/EXERCISE_OBSERVATIONS.md`
  (grep the section you need; it is long)
- `.claude/memory/demos_transparent_terminal_first.md`
