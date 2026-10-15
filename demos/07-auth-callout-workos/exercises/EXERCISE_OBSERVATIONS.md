# Demo 07 — exercise observations

Measured results only. A line without a date and versions here is a
prediction, not a result.

Tools for every entry below unless the entry says otherwise:
`nats-server` 2.14.6, `nats` CLI 0.4.0, `nsc` 2.15.0, Go 1.27.0, macOS
(Darwin 25.4.0). Times are SAST (UTC+2).

## Exercise 01 — run of 2026-10-09

Server `d07-server`, operator mode, full resolver, `.run/trust.conf`, config
`exercises/config/ex01-nats-callout.conf`. Account `AUTH` JWT:
`auth_users` = `callout-svc` (`UBRNRLMR…`), `allowed_accounts` = `ORDERS`
(`ACB4IZJL…`). No Go service yet: a `nats sub '$SYS.REQ.USER.AUTH'`
listener, connected as `callout-svc`, stood in for it and never answered.
That listener is a probe, not the callout service.

### Is the `sentinel` user required? — yes (measured)

| Case | Client sent | Callout request? | Client saw | Server log |
|---|---|---|---|---|
| A | `--token` only, no creds | **no** | `Authorization Violation` at once | `22:48:54.171 [ERR] cid:8 - authentication error - Token "[REDACTED]"` |
| C | `sentinel` creds + `--token` | **yes** | `i/o timeout` | `22:49:03.760 [WRN] cid:9 - Readloop processing time: 2.001372792s`, then `[ERR] cid:9 - maximum account active connections exceeded` |

Case C's captured request: `aud` = `nats-authorization-request`, `sub` =
`AB3BKQ7W…` (the `AUTH` account), `nats.client_info.name_tag` = `sentinel`,
`nats.connect_opts.auth_token` = the probe string. So in operator mode a
bare token never reaches the callout. The client must present a valid user
JWT of the callout account (`sentinel`) for the server to ask the callout.

**The request carries the client token in clear inside its payload.** The Go
service must never log the raw request.

Kept for exercise 04 (`D07-R5`): with no answer the server waited
≈ 2.0 s, and the client reported `i/o timeout`, **not**
`Authorization Violation`. The server's error text names connection limits,
not the callout. Re-measure with the real service down.

### `D07-R6` bypass checks — partial

| Check | Result |
|---|---|
| 1. `callout-svc` connects without a callout round | **met.** A second `callout-svc` connection published at 22:49:34. The listener's request count stayed at 1. No server error. `/connz?auth=1` shows `callout-svc` (cid 6) in `AUTH`. |
| 2. `sentinel` with no token still goes through the callout | **met (request side).** A second request arrived for `sentinel`, `auth_token` absent; client `i/o timeout` (server log 22:49:42.908, cid 11). Re-run with the Go service answering an explicit refusal. |
| 3. `sentinel` + valid WorkOS token lands in `ORDERS` | **not yet run** — needs the WorkOS environment. |

### Isolation of the real `nats` / `nsc` stores — held

A listing of every file under `~/.config/nats` and `~/.local/share/nats`
with its mtime, taken before the chain was built, matched the listing taken
after the chain was built, pushed and edited (`diff` empty, 22 files).
`~/.nkeys` does not exist before or after.
