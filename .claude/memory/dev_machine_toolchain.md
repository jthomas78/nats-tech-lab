---
name: dev-machine-toolchain
description: Jeremy works across at least two machines — a Linux box without Docker, and a Mac (Darwin) with Docker and Homebrew/Volta toolchains
metadata:
  type: project
---

Jeremy's sessions on this repo have run from at least two different machines; check `uname`/PATH before assuming either toolchain layout.

**Linux box (as of 2026-07-07):** Go 1.26.4 and Node 24.18.0 LTS are user-space installs under `~/.local/toolchain/` with symlinks in `~/.local/bin/` (go, gofmt, node, npm, npx). `~/.local/bin` is on PATH for login shells, but non-login tool shells may need `export PATH=~/.local/bin:$PATH`. **Docker is not installed** and needs root to install — `docker compose up` cannot be run here; verify backend behaviour with `go test ./...` / `ginkgo ./...` instead (integration tests use an embedded in-process NATS server; Shape B uses a fake repo in place of Postgres).

**Mac / Darwin box (confirmed 2026-07-10):** Go via Homebrew (`/opt/homebrew/bin/go`), Node/npm via Volta (`~/.volta/bin/`), `swag` and `ginkgo` installed under `~/go/bin/`. **Docker IS installed** (`/usr/local/bin/docker`, confirmed v29.6.1) — `docker compose up` should work here, unlike the Linux box.

**How to apply:** Don't assume Docker is unavailable, or that tools need `~/.local/bin` on PATH, without checking the current session's environment first — these differ by machine.
