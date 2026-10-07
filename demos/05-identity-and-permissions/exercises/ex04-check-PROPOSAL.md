# Proposal — `ex04-check.sh` changes (not implemented)

Status: **approved and implemented 2026-10-07, with review corrections**
(36 checks; not run). This file is kept as the design record. Where it and
`ex04-check.sh` differ, the script wins. The corrections:

- Delivery checks read stdout **and** stderr; only `C` checks name a stream,
  and those are predictions.
- Responder receipt comes from `exercises/ex04-responder-hook.sh`, run by
  `nats reply --command` in all four cases. Not `--echo`, not a third client
  (section 5 below is superseded).
- Each case ends with a verdict. A case is "demonstrated" only when its
  positive control, captures, `S` and `D` checks all passed.
- A listener on `_INBOX.>` in AA and BR also gets the real reply, as its own
  positive control.

The exercise scope does not change: same four configs, same four cases.

## Why

Three checks in the current script assume CLI behaviour nobody has measured,
and two of them contradict the docs:

| Now | Problem |
|---|---|
| A7 "no reply, exit not 0" (04a denied) | The Authorization page shows the requester printing only `Sending request on …`, waiting out the timeout, and exiting 0 with no error. |
| A8 "the requester saw a timeout" | Same docs example: no timeout text is shown. |
| B1 "no reply, exit not 0" (04b, responder denied) | Copied from A7. A denied responder publish is a different case. The docs say nothing about what the requester prints for it. |

Also, the script mixes three kinds of evidence in one list, and keeps only a
merged `2>&1` output with `exit=` appended. The quality rules ask for them
apart.

## 1. Three evidence groups, with their own ID letters

Each case gets its checks in three groups. A group never stands in for another.

| Group | ID | Source | What it proves |
|---|---|---|---|
| Server enforcement | `S` | the server log for that part (reset by `lab/up.sh`) | the server denied a named user, a named side (Publish / Subscription), on the expected subject |
| Delivery | `D` | the listener, responder and requester output files | what actually arrived, and what did not |
| CLI observation | `C` | stdout, stderr and exit code, each kept apart | what the tool tells a person |

IDs become e.g. `A-S1`, `A-D2`, `A-C1`. The tag rule from ex03 / ex05 applies:
every NATS check is `[pred]` until a run is recorded; `[rig]` is set-up only.

## 2. Record, do not merge

A new helper replaces `as_order` / `as_reader` for every request and publish:

```bash
# capture <tag> <user> <nats args...>
#   writes .run/ex04/<tag>.stdout, <tag>.stderr, <tag>.exit, <tag>.secs
```

- stdout and stderr go to separate files. The exit code goes to its own file.
  Elapsed seconds go to `.secs` (info only, never a pass/fail: it shows
  whether the requester waited out `--timeout 2s`).
- At the start: `.run/ex04/versions.txt` gets `nats-server --version`,
  `nats --version`, `uname -sr` and the date.
- Each part's server log is copied to `.run/ex04/<part>-server.log`, as now.
- The `info` lines stay, and print the three captured streams per command.

## 3. The four cases

### 04a, inbox allowed — the positive control for 04a denied

| ID | Group | Check | Basis |
|---|---|---|---|
| A-C1 | C | requester stdout has `summary: 3 orders`; exit 0 | pred |
| A-D1 | D | responder output has the request body `how many?` | pred — assumes `nats reply` prints the body |
| A-S1 | S | zero violations in this part's log | pred |
| A-C2, A-D2, A-S2 | C, D, S | forged `_INBOX.forged`: exit 0; listener got `not a reply`; still zero violations | pred (unchanged A4–A6) |

### 04a, inbox denied

Same responder, same request command, same run. Only the config differs.

| ID | Group | Check | Basis |
|---|---|---|---|
| A-S3 | S | log has `analytics-reader` … `Subscription Violation - Subject "_INBOX.` | pred — docs |
| A-S4 | S | exactly 1 violation in the part, and it is that one (no Publish Violation) | pred — replaces A11, stricter |
| A-D3 | D | requester stdout does **not** have `summary: 3 orders` | pred — replaces A7's "no reply" half |
| A-D4 | D | responder output has `how many?` (the request arrived; only the reply was lost) | pred (was A9) |
| A-C3 | C | stdout has `Sending request on`; stderr has no `error`; exit 0 | pred — **from the docs example**, labelled so |

Dropped: A7's "exit not 0" (contradicts the docs) and A8 "timeout" (the docs
show no such text). Neither is replaced by a weaker check: the proof of the
denial moves to A-S3 / A-S4 (server) and A-D3 / A-D4 (delivery), next to the
04a-allowed control. A-C3 is as strict as before, but on the documented
behaviour.

### 04b, no response grant — responder publish denied

Positive control: the 04b-allow case below, same responder and same request,
same run. A-C1 / A-D1 show the request path itself works.

| ID | Group | Check | Basis |
|---|---|---|---|
| B-S1 | S | log has `order-svc` … `Publish Violation - Subject "_INBOX.` | pred — server source |
| B-S2 | S | exactly 1 violation in the part, and it is that one | pred — new, stricter |
| B-D1 | D | responder output has `how many?` | pred (was B2) |
| B-D2 | D | requester stdout does **not** have `summary: 3 orders` | pred — replaces B1's "no reply" half |
| B-C1 | C | responder output has `Permissions Violation for Publish` | pred (was B4) |
| B-C2 | C | requester stdout / stderr / exit: **recorded only, no pass/fail** | unknown |

B-C2 is not inferred from 04a: a denied responder publish is a different case
from a denied requester subscribe. After the hand run, B-C2 becomes a check
on the measured value, with the run cited.

### 04b, `allow_responses: true`

| ID | Group | Check | Basis |
|---|---|---|---|
| B-C3 | C | requester stdout has `summary: 3 orders`; exit 0 | pred — server source (was B5) |
| B-D3 | D | requester got the reply (same text as B-C3, kept as the delivery record) | pred |
| B-C4 | C | forged publish: client told `Permissions Violation for Publish` | pred (was B6) |
| B-D4 | D | forged `not a reply` did not reach the listener | pred (was B7) |
| B-S3 | S | log has `order-svc` … `Publish Violation - Subject "_INBOX.forged"` | pred (was B8, now exact subject) |
| B-S4 | S | exactly 1 violation in the part (the real reply logged none) | pred (was B9) |

T1, T2 stay as `[rig]`.

## 4. Size

22 checks now. About 30 after: no case is added; checks split by group, two
exact-count server checks are added, and A8 / B1-exit are removed.

## 5. One open choice

A-D1, A-D4 and B-D1 trust `nats reply` to print the request body. If the
hand run shows it does not, the fix is either `nats reply --echo` (the CLI
0.4.0 flag that sends the request back as the reply; it only helps where the
reply gets through), or a third client that subscribes `orders.summary` as an independent
observer. The second needs a user added to all four configs. That is a rig
change, not new scope, but it is not proposed until the hand run shows a need.
