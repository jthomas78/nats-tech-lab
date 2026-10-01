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
