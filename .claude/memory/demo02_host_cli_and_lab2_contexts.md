# Demo 02 uses the host nats/nsc CLI — no toolbox container

**[demo 02]** Decided 2026-09-09 by the user: *"can we remove the toolbox.sh and
the usage thereof. For now I prefer the local NATS cli to handle things on the
system instead of in a docker container."*

Deleted: `demos/02-multi-region/deploy/toolbox.sh`,
`demos/02-multi-region/nats/toolbox-entrypoint.sh`, the `toolbox:` service and
the `other-region:` network in `deploy/compose.za.yaml`.

The old repo rule "never run the toolbox's `nats` CLI from your Mac" is
**reversed for demo 02 only**. Demo 01's tooling rules are unchanged.

## What replaced it

`demos/02-multi-region/deploy/contexts.sh` registers one host `nats` context per
file in `nats/creds/`. `up.sh` calls it; `up.sh down -v` calls
`./contexts.sh remove`.

**Every context name starts `lab2-`.** Not decoration: demo 01 already owns
contexts named `sys` and `platform` in the same store
(`~/.config/nats/context/`). Unprefixed names silently overwrite them.

A shared account gets `lab2-<base>-shared-<region>`, because
`linebooker.creds` and `linebooker-za.creds` would otherwise both produce
`lab2-linebooker-za`. That collision really happened and made
`stream ls` return `No Streams defined` instead of `ODOMETER`.

## The cost

Nothing pins the tool versions now. If a bug looks machine-specific, check
`nats --version` and `nsc --version` first. Known good: nats 0.4.0, nsc 2.15.0,
jq. macOS has no `timeout`, so `lab/01-the-wall.sh` uses a background
`sleep`+`kill` instead.

Rules and the context table live in `demos/02-multi-region/CLAUDE.md`.
See also [[multi_region_lives_in_demo_02]].
