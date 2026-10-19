# Handoff — Chronicle OTel tracing update

Target: Claude. Written 2026-10-19. Replaces `otel-chronicle-comparison-handoff.md`
for any Chronicle work.

## Goal

Bring Chronicle's trace code in line with ADR-OBS-TRACE, and log every change.
At the end, build a before/after artifact from the change log.

## Where the work lives

- Repo: `/Users/jeremy/dev/github/linebooker/chronicle`. It is **production**
  code, not a POC (see `.claude/memory/chronicle_is_production_not_poc.md`).
- Branch: `feature/otel-tracing-update`, cut from `develop` at `8fb80c4`.
- Docs folder: `documentation/architecture/L3_L4/observability/`.
- Local stack: `testing/local/`.

## Done so far

Chronicle commit `e4c2ece` (not pushed) holds all of this:

- `testing/local/`: Docker Compose stack. It has a control plane, za and au
  regions with 3 servers each, a tenant leaf per region, `loads` and
  `tracking` in za, and `grafana/otel-lgtm`.
  - `./local.sh check`: 34 ok, 0 fail (re-run 2026-10-19).
  - `./local.sh trace`: records the header-case baseline.
- `otel-tracing-update.md`: the change log. It has the Baseline section and
  an empty Changes section. Entry format `OT-NN` is in the file.
- `ADR-OBS-TRACE.md`: the decision, rules 1–12, plus a
  "Not decided here" list. Diagram: `ADR-OBS-TRACE-diagram.{html,png}`.
- `otel-lab-comparison.md`: what moves from the lab, per rule.
- `observability-tracing-design.html`: 5 A3 sheets, copied from the lab L4-02
  drawing and rewritten for Chronicle. Sheet 2 shows Chronicle's NATS layout.
  Sheet 5 shows local mode and live mode as Chronicle has them. Each sheet
  was checked at 1920 wide.
- `render-html.py`: makes the `.html` from each `.md`.
- `README.md` in that folder is still a stub.

## Next steps, in order

1. **Ask the user** which change to make first. Do not start code without a
   yes. The likely first change is rule 2: make the NATS carrier read keys in
   any letter case, and call `Inject` on publish, request and reply.
   Check: `./local.sh trace` shows the lowercase `traceparent` row joins.
2. For each change, add an `OT-NN` entry to `otel-tracing-update.md`, then run
   `python3 /Users/jeremy/dev/github/linebooker/chronicle/documentation/architecture/L3_L4/observability/render-html.py`.
   Check: the `.html` shows the new entry.
3. When a change makes a "today" fact on the design sheets false, update
   `observability-tracing-design.html`. Check: export each sheet (command
   below) and look for overlaps.
4. Build the before/after artifact from the change log only. Check: every
   `OT-NN` entry appears, and nothing that is not logged.

## Decisions made — do not re-open

- Do not call the topology "T7" in Chronicle. That is a lab name.
- No lab references in Chronicle docs (no V3, no Linebooker V3 IDs, no
  L4-02-Oxx, no ADR-058) except in `otel-lab-comparison.md`, which exists to
  compare.
- The leaf websocket is an outside call (rule 5). So the right result is a
  new root plus a span link, not "joins the caller's trace". The `trace` check
  must change when rule 5 lands.
- Telemetry never goes over NATS (rule 3).
- Local mode keeps `grafana/otel-lgtm` as the stand-in for Alloy and
  Grafana Cloud. A local Alloy is a target, not built.

## Standing rules from the user

- Do not fix the lowercase `traceparent` bug without the user's yes.
  Documenting it is fine.
- Terraform and the AWS CLI: account `962450756731` only.
- Do not commit unless asked. Never push.
- One command per Bash call. No `&&`, no `cd`.
- Reports in ELI5 / ASD-STE100 style.

## Traps

- `ENV=local` turns OTel off completely (`services/lib/logger/logger.go:30-32`).
  The local stack does not set `ENV=local` for that reason.
- A missing endpoint only logs an error; the SDK then falls back to
  `localhost:4317`. It is not a Chronicle fallback.
- NATS header keys are case-sensitive. `propagation.HeaderCarrier` changes
  keys to `Traceparent`, so a lowercase `traceparent` is never read.
- Chronicle has no `*_test.go` files. "Tests pass" there proves nothing
  about telemetry. `./local.sh check` is the real check.
- zsh: `grep --include=*.tf` fails with "no matches found". Quote it or drop
  the flag.
- `render-html.py` needs full paths.
- The Browser pane cannot act on `file://` pages. Export PNGs instead:
  `node /Users/jeremy/dev/github/jthomas78/nats-tech-lab/demos/01-dictionary/diagrams/export-html-png.mjs <in.html> <out.png> 1920 "--clip=body > section:nth-of-type(N) figure"`
- The Chronicle commit used an auto-set git identity
  (`Jeremy <jeremy@Jeremys-MacBook-Pro.local>`). The user may want to fix it
  with `git commit --amend --reset-author`.

## Read first

- `/Users/jeremy/dev/github/linebooker/chronicle/documentation/architecture/L3_L4/observability/otel-tracing-update.md`
- `/Users/jeremy/dev/github/linebooker/chronicle/documentation/architecture/L3_L4/observability/ADR-OBS-TRACE.md`
- `/Users/jeremy/dev/github/linebooker/chronicle/services/lib/logger/propagation.go`
- `/Users/jeremy/dev/github/linebooker/chronicle/services/lib/logger/logger.go`
- `/Users/jeremy/dev/github/linebooker/chronicle/testing/local/local.sh` (the `check` and `trace` parts only)
