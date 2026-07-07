---
name: dev-machine-toolchain
description: Go/Node are user-space installs in ~/.local on Jeremy's Linux box; Docker is NOT installed
metadata:
  type: project
---

On the primary dev machine (Linux, as of 2026-07-07): Go 1.26.4 and Node
24.18.0 LTS are user-space installs under `~/.local/toolchain/` with symlinks
in `~/.local/bin/` (go, gofmt, node, npm, npx). `~/.local/bin` is on PATH for
login shells, but non-login tool shells may need `export PATH=~/.local/bin:$PATH`.

**Docker is not installed** and needs root to install, so `docker compose up`
for the demos cannot be run by Claude here — verify backend behaviour with
`go test ./...` instead (integration tests use an embedded in-process NATS
server; Shape B uses a fake repo in place of Postgres).
