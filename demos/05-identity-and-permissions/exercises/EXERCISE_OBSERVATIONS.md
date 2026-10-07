# Demo 05 — observations

What the machine actually did. A row is **measured** only when it came from
real output in a run listed here. Anything else says **inferred** or
**unmeasured**.

## Template — copy this for each exercise

```markdown
## Exercise NN — <question>

- **Date / machine:** YYYY-MM-DD, <host, OS>
- **Versions:** nats-server vX.Y.Z · nats CLI X.Y.Z
- **Command:** `lab/exNN-check.sh` (or the manual steps, named)
- **Runs:** N, all identical? yes / no — explain any difference

| Check | Prediction | Client reported | Server logged | Delivered? | Verdict |
|---|---|---|---|---|---|

**Gotchas hit:**
**Surprises against the docs:**
```

---

## Exercise 01 — who gets in?

- **Date / machine:** 2026-09-30, the author's Mac (Darwin 25.4.0)
- **Versions:** nats-server v2.14.6 · nats CLI 0.4.0
- **Command:** `exercises/ex01-check.sh`
- **Runs:** 3, all **ALL PASS**, 16 of 16 checks, identical

| Check | Prediction | Client reported | Server logged | Delivered? | Verdict |
|---|---|---|---|---|---|
| A1 open server, anonymous pub + sub | admitted | `Published` | — | yes | measured ✓ `D05-R1` |
| B0 users config starts | warns about plaintext | — | `[WRN] Plaintext passwords detected, use nkeys or bcrypt` | — | measured ✓ |
| B1 subscriber identity | shown in `/connz` | — | `/connz?auth=1` → `"authorized_user": "analytics-reader"` | — | measured ✓ |
| B2 wrong password | refused | `nats: error: nats: Authorization Violation`, exit 1 | `[ERR] ... authentication error - User "order-svc"` | no | measured ✓ `D05-R3` |
| B3 no credentials | refused | same text, exit 1 | `[ERR] ... authentication error` (no user named) | no | measured ✓ `D05-R3` |
| B4 unknown user | refused | same text, exit 1 | `[ERR] ... authentication error - User "mallory"` | no | measured ✓ `D05-R3` |
| B5 correct password | admitted | `Published ... to "orders.created"`, exit 0 | nothing | yes | measured ✓ `D05-R2` |
| B6–B7 delivery | only the good message | — | — | exactly 1 message received, the `order-svc` one | measured ✓ `D05-R4` |
| B11 error count | 3 | — | exactly 3 `authentication error` lines | — | measured ✓ |
| T1–T2 teardown | nothing left | — | no process, port 4522 free | — | measured ✓ |

Also measured, outside the script:

- **An unset password variable stops the server.** With
  `D05_ORDER_SVC_PASSWORD` unset, `nats-server -c exercises/config/ex01-nats-users-auth.conf -t`
  exits 1: `variable reference for 'D05_ORDER_SVC_PASSWORD' on line 21 can not
  be found`. Fail-closed.
- **The passwords never reach the server log.** `grep` for both values in
  `.run/server.log` found 0 matches.

**Gotchas hit:**

- The author's `nats` CLI had context `lab4-odometer` selected. Without
  `--no-context`, every command in this demo would have carried demo 04's
  settings. Every command and script here now passes it.

**Surprises against the docs:** none. The docs' claim that the three
failures look identical to the client held, and so did the claim that the
server log names the user.

## Exercise 02 — what may each user do?

- **Date / machine:** 2026-10-01, the author's Mac (Darwin 25.4.0), SAST
- **Versions:** nats-server v2.14.6 · nats CLI 0.4.0
- **Command:** the manual steps in `EXERCISE-02-TERMINAL-STEPS.md`, Steps 1–15,
  typed by the author in three terminals. `exercises/ex02-check.sh` was written
  afterwards from these results (see the check script note below the tables).
- **Runs:** the manual steps ran once, by the author. Step 6 was also repeated
  by Claude on a scratch server (port 4599, same config, test passwords) on
  2026-10-01: same result. The check script then ran 3 times, ALL PASS.

Part 02a — `ex02-nats-permissions-one-sided.conf` (one side listed per user):

| Check | Prediction | Client reported | Server logged | Delivered? | Verdict |
|---|---|---|---|---|---|
| Step 4 `order-svc` pub `orders.created` | allowed | `Published 14 bytes` | nothing | yes, in B | measured ✓ |
| `order-svc` pub `order.created` (typo, not `orders`) | denied | not recorded | `Publish Violation - Subject "order.created"` | not checked | measured ✓ (by accident; see gotchas) |
| Step 5 `analytics-reader` pub `orders.created` | unknown to the author | `Published 11 bytes`, no error | nothing | **yes, in B: `from-reader`** | measured ✓ — **the reader can publish** |
| Step 6 `order-svc` sub `orders.>` | unknown to the author | `Subscribing on orders.>`, no error | no `Violation` line | yes (scratch run: it received a message) | measured ✓ — **the sender can subscribe** |

Part 02b — `ex02-nats-permissions.conf` (both sides listed, `deny: ">"` on the unused side):

| Check | Prediction | Client reported | Server logged | Delivered? | Verdict |
|---|---|---|---|---|---|
| Step 9 `analytics-reader` sub `orders.>` | allowed | `Subscribing on orders.>` | nothing | — | measured ✓ |
| Step 10 `order-svc` pub `orders.created` | allowed | `Published 14 bytes` | nothing | yes, in B: `from-order-svc` | measured ✓ positive control |
| Step 11 `order-svc` pub `invoices.created` | denied | `nats: error: nats: permissions violation: Permissions Violation for Publish to "invoices.created"` | `[ERR] ... "$G/user:order-svc" - Publish Violation - Subject "invoices.created"` | no | measured ✓ |
| Step 12 `analytics-reader` pub `orders.created` | denied | `Permissions Violation for Publish to "orders.created"` | `[ERR] ... "$G/user:analytics-reader" - Publish Violation - Subject "orders.created"` | no, B unchanged | measured ✓ — Step 5, now closed |
| Step 13 `order-svc` sub `orders.>` | denied | `Subscribing on orders.>`, **then** `Permissions Violation for Subscription to "orders.>"` | `[ERR] ... "$G/user:order-svc" - Subscription Violation - Subject "orders.>", SID 1` | — | measured ✓ — Step 6, now closed |
| Step 14 `analytics-reader` sub `invoices.>` | denied | `Subscribing on invoices.>`, then `Permissions Violation for Subscription to "invoices.>"` | `[ERR] ... "$G/user:analytics-reader" - Subscription Violation - Subject "invoices.>", SID 1` | — | measured ✓ |

**Check script, `exercises/ex02-check.sh`** (2026-10-01, nats-server v2.14.6,
nats CLI 0.4.0): 24 checks, ALL PASS, 3 runs. It re-measures the table above on
a fresh server each time. In 02a it proves the hole (reader publish delivered,
sender subscribe delivered, zero violations logged). It also measures, for the
first time, the two 02a actions that were not in the by-hand run:
`order-svc` pub `invoices.created` and `analytics-reader` sub `invoices.>`.
**Both are denied in 02a**, as predicted: a side that is listed is limited by
its allow list. Only the side that is not listed is open; it logged exactly 2
violations. In 02b it proves the four
denials on three channels (client error, server log, delivery) and counts
exactly 4 violations in a clean log. It ends by proving the server is gone and
port 4522 is free. The first run of the script was written from the by-hand
results above; none of its greps needed changing.

Not measured: the exit codes of the denied commands. The script prints them but
does not assert them, because the error arrives after the command returns.

**Gotchas hit:**

- **A side a user does not list is open.** With only `publish` listed, the user
  could still subscribe to anything. With only `subscribe` listed, it could
  still publish anything. Neither raised an error or a log line. This is why
  the config lists both sides.
- **The client prints `Subscribing on …` before the denial.** The error for a
  subscribe arrives after the command has started, so a check must read the
  server log and the delivery, not only the first line or the exit code.
- A typo (`order.created` for `orders.created`) was denied by the allow-list
  and logged. It shows the allow-list works on a near-miss subject.
- The `[ERR]` line names the user as `$G/user:<name>`. `$G` is the global
  account, the only account in this demo.

**Surprises against the docs:** not compared. The two source pages were not
re-read for this exercise, so "an unlisted side is open" is recorded as
**measured behaviour**, not as a quoted claim from the docs.

## Exercise 03 — how do allow, deny, wildcards and defaults combine?

**Status: IN PROGRESS.** Parts 03a and 03b measured by hand. Parts 03c and 03d not run.
`ex03-check.sh` written 2026-10-06, not yet run.

### Part 03a — deny beats allow; what `*` and `>` match

- **Date / tools:** 2026-10-01; nats-server v2.14.6, nats CLI 0.4.0, macOS Darwin 25.4.0.
- **Config:** `exercises/config/ex03-nats-allow-deny.conf`. `order-svc` pub allow
  `orders.>` and deny `orders.internal.>`; `analytics-reader` sub allow `orders.*`;
  `audit-observer` (new) sub allow `orders.>`, so delivery to it shows what
  `order-svc` could publish.
- **Steps:** `exercises/EXERCISE-03-TERMINAL-STEPS.md`, Step 1.

| Step | Action | Client | Server log | Delivery to observer |
|---|---|---|---|---|
| 1.4 | `order-svc` pub `orders.created` | accepted | none | received |
| 1.5 | `order-svc` pub `orders.eu.created` | accepted | none | received |
| 1.6 | `order-svc` pub `orders` | `Permissions Violation for Publish to "orders"` | `Publish Violation - Subject "orders"` | not received |
| 1.7 | `order-svc` pub `orders.internal.audit` | `Permissions Violation for Publish to "orders.internal.audit"` | `Publish Violation - Subject "orders.internal.audit"` | not received |
| 1.8 | `analytics-reader` sub `orders.created` | accepted (no error in 2 s) | none | n/a |
| 1.9 | `analytics-reader` sub `orders.eu.created` | `Permissions Violation for Subscription to "orders.eu.created"` | `Subscription Violation - Subject "orders.eu.created", SID 1` | n/a |

**Measured:**

- `>` in an allow list matches one or more trailing tokens (1.4, 1.5) and does
  **not** match the bare prefix `orders` (1.6).
- When a subject matches both an allow and a deny, **deny wins** (1.7).
- `*` in a subscribe permission matches exactly one token: `orders.created`
  allowed, `orders.eu.created` denied (1.8, 1.9).
- The log held exactly 3 `[ERR]` lines (1.6, 1.7, 1.9). The observer received
  only `one-token` and `two-tokens`.
- A clean shutdown line was printed on Ctrl-C.

**Not measured:** exit codes of the denied commands.

**Gotcha:** the observer user (`audit-observer`) and `D05_AUDIT_OBSERVER_PASSWORD`
were added for 03a. `lab/secrets.sh` now writes the new variable. Run without `--rotate`, it adds
any variable an older `.run/secrets.env` lacks and keeps the existing
passwords (changed 2026-10-07; the same path adds `D05_TOKEN` for exercise 05).
Re-run `source .run/secrets.env` in every open terminal afterwards.

### Part 03b — an empty allow list

- **Date / tools:** 2026-10-01; nats-server v2.14.6, nats CLI 0.4.0, macOS Darwin 25.4.0.
- **Config:** `exercises/config/ex03-nats-empty-list.conf`. `order-svc` has
  `publish: { allow: [] }` and `subscribe: { deny: ">" }`; `analytics-reader` may
  subscribe to `>` so a listener sees anything that gets through.
- **Steps:** `exercises/EXERCISE-03-TERMINAL-STEPS.md`, Step 2.

| Step | Action | Client | Server log | Delivery to listener |
|---|---|---|---|---|
| 2.1 | start server | started, no complaint about `allow: []` | none | n/a |
| 2.2 | `analytics-reader` sub `>` | accepted | none | n/a |
| 2.3 | `order-svc` pub `invoices.created` | `Published 10 bytes` | none | received `empty-list` |
| 2.4 | `order-svc` sub `orders.>` | `Permissions Violation for Subscription to "orders.>"` | `Subscription Violation - Subject "orders.>", SID 1` | n/a |

**Measured:**

- The server accepts `allow: []`. It does not reject it.
- An empty allow list means **no restriction**, not deny-all. `order-svc` published
  `invoices.created`, a subject outside `orders.>`, and it was delivered.
- The permissions block is still live: the same user's `subscribe: { deny: ">" }`
  denied a subscribe. This is the control that makes 2.3 meaningful.
- The log held exactly 1 `[ERR]` line (Step 2.4). Step 2.3 logged nothing.

**Not measured:** exit codes of the denied commands.

**Unconfirmed in this run:** the A-terminal line count was taken from one pasted
`[ERR]` line, not from a script counting the log.

## Exercise 04 — does request / reply work under limits?

**Status: NOT RUN.** Steps, four configs and `ex04-check.sh` (22 checks)
written 2026-10-06. Nothing measured yet.

Read from the server source (v2.14.6, `server/client.go`, `server/opts.go`),
not measured: the reply check runs only after allow and deny have said no, so
`allow_responses` should beat `publish: { deny: ">" }`. Its defaults are 1
reply per request, valid 2 minutes.

## Exercise 05 — a token and an NKey, against the password

**Status: NOT RUN.** Steps, four configs, `lab/nkeys.sh` and `ex05-check.sh`
(28 checks) written 2026-10-06; 32 checks after the 2026-10-07 fix below.
Nothing measured at runtime yet.

2026-10-07 fix, before any run: A10 read `/connz` after the token listener
could already have exited, so an empty connection list would have passed.
A10 and B13 now each start a named probe connection, read `/connz` while it
is connected, and split into three: the curl succeeded and returned JSON
(a/rig), exactly one connection with the probe's name is listed (b/rig), and
only then its `authorized_user` (c/prediction). Tested on sample JSON only
(empty list, other connection, two probes, curl failure: all fail).

Checked with `nats-server -t` only (v2.14.6), 2026-10-06:

- `ex05-nats-token-and-users.conf` is rejected:
  `Can not have a token and a users array`.
- `ex05-nats-nkey-with-password.conf` is rejected:
  `Nkey users do not take usernames or passwords`.
- `nats auth nkey gen user --output <file>` writes a 59-byte seed starting
  `SU`, **mode 644**. `lab/nkeys.sh` sets 600.
