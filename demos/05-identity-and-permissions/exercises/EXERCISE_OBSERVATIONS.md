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
  `D05_ORDER_SVC_PASSWORD` unset, `nats-server -c exercises/ex01-nats-users-auth.conf -t`
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
  typed by the author in three terminals. There is **no `ex02-check.sh` yet**.
- **Runs:** 1 by the author. Step 6 was also repeated by Claude on a scratch
  server (port 4599, same config, test passwords) on 2026-10-01: same result.
  So this exercise is **measured once**, by hand.

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

Not measured: the exit codes of the denied commands (not read in this run), and
a count of `[ERR]` lines across a clean run (the log held the earlier typo line).

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
