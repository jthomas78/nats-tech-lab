# Demo 06 — observations

What the machine actually did. A row is **measured** only when it came from
real output in a run listed here. Anything else says **inferred** or
**unmeasured**.

All three exercises below:

- **Date / machine:** 2026-10-08, the author's Mac (Darwin 25.4.0)
- **Versions:** nats-server v2.14.6 · nats CLI 0.4.0 (`nats auth`; no `nsc`)
- **Config:** `exercises/config/ex01-nats-operator.conf`, the same file for
  all three. It names one operator JWT, the `SYSTEM` account and a `full`
  resolver. It names no user and no other account (`D06-R1`, measured by
  reading `.run/trust.conf`).

## Template — copy this for each exercise

```markdown
## Exercise NN — <question>

- **Command:** `exercises/exNN-check.sh` (or the manual steps, named)
- **Runs:** N, all identical? yes / no — explain any difference

| Check | Prediction | Client reported | Server logged | Delivered? | Verdict |
|---|---|---|---|---|---|

**Gotchas hit:**
**Surprises against the docs:**
```

---

## Exercise 01 — who gets in, when the server knows only the operator?

- **Command:** the manual steps in `EXERCISE-01-TERMINAL-STEPS.md`, run by
  Claude, then `exercises/ex01-check.sh` (24 checks).
- **Runs:** the 19-check script ran 6 times. Runs 1, 2, 4, 5, 6: **ALL
  PASS**. Run 3 failed C17 only (see "Open question" below). C17 was then
  changed from "exactly 5" to "at least 5". On 2026-10-08 checks I1–I3 and
  Q1–Q2 were added (24 checks), after a hand run of the same commands; the
  24-check script ran 3 times, **ALL PASS**.

| Check | Prediction | Client reported | Server logged | Delivered? | Verdict |
|---|---|---|---|---|---|
| I1 *[inspect]* boot log | operator named | — | `Trusted Operators`, `Operator: "D06"`, `Issued : …`, `Expires : Never` | — | inspection only |
| I2 *[inspect]* `auth account info ORDERS` | issuer = operator | `Issuer: O…` = the operator's **identity** key, not its signing key `OD…` | — | — | inspection only |
| I3 *[inspect]* `order-svc` JWT claim | 1 MiB limit | `"payload": 1048576` (`--defaults`) | — | — | inspection only — **enforcement not tested** |
| C1–C2 `order-svc`, account not pushed | refused | `nats: error: nats: Authorization Violation`, exit 1 | `Account [A…] fetch took 1.90…s`, `fetching jwt timed out`, then `authentication error` | no | measured ✓ `D06-R3` |
| Q1 `auth account query ORDERS`, before the push | no server copy | `nats: error: did not receive a valid token from the server`, exit 1 | — | — | measured ✓ |
| C3 push `ORDERS` with the SYSTEM user | JWT stored | — | file `.run/resolver/<ORDERS key>.jwt` exists | — | measured ✓ |
| Q2 `auth account query ORDERS`, after the push | server copy | `Account ORDERS (A…)`, exit 0. Server copy says `Users: 0`; local `account info` says `Users: 1` | — | — | measured ✓ `D06-R4` |
| C4 subscriber identity | account shown | — | `/connz?auth=1` → `"account": "<ORDERS key>"` | — | measured ✓ |
| C5 no credentials | refused | same text, exit 1 | `authentication error` | no | measured ✓ `D06-R3` |
| C6 `ROGUE` operator's user, not pushed | refused | same text, exit 1 | `authentication error` | no | measured ✓ `D06-R3` |
| C7 push the `ROGUE` account | refused by server | **`Success 1 Failed 0`** | — | — | measured ✗ prediction wrong — see Surprises |
| C8–C9 where the `ROGUE` JWT went | not stored | — | file **is** in `.run/resolver/`; account **not** in `/accountz` | — | measured |
| C10–C11 `ROGUE` user, account pushed | refused | same text, exit 1 | `authentication error`, **no** account fetch | no | measured ✓ `D06-R3` |
| C12 real `order-svc` JWT, SYSTEM `admin` seed | refused | same text, exit 1 | `authentication error` | no | measured ✓ `D06-R3` |
| C13–C14 `order-svc` correct creds | admitted | `Published … to "orders.created"`, exit 0 | nothing | yes | measured ✓ `D06-R2` |
| C15 delivery | only the good message | — | — | exactly 1 message received | measured ✓ |
| C16 users on disk | none | — | no `U*` file in `.run/resolver/` | — | measured ✓ `D06-R4` |
| C17 error count | ≥ 5 | — | 5 in 5 runs; 7 in run 3 | — | measured ✓ |
| T1–T2 teardown | nothing left | — | no process, port 4922 free | — | measured ✓ |

**Gotchas hit:**

- **Ports 4622 / 8622 are taken** by demo 02's `za-2` container. This demo
  uses 4922 / 8922.
- **The resolver `dir` is relative to the server's working directory**, not
  to the config file. `lab/up.sh` runs the server from the demo folder.
- **`nats auth` writes to `~/.local/share/nats/nsc` by default.** `lab/nats.sh`
  redirects `XDG_DATA_HOME` and `XDG_CONFIG_HOME` into `.run/`. Checked: the
  real store did not change.

**Surprises against the docs:**

- **A push from a foreign operator says "success".** The CLI printed
  `Success 1 Failed 0`, and the server wrote the `ROGUE` account JWT into its
  resolver directory. But the server did not load the account, and the
  `ROGUE` user was still refused — instantly, with no fetch. So the trust
  check is real, but the push report is not evidence. Read `/accountz`.
- **Two refusals look the same to the client.** "Account not pushed" and
  "foreign operator" both print only `Authorization Violation`. Only the
  server log tells them apart: the first one waits ~1.9 s for a fetch, the
  second one does not fetch. **Correction (walkthrough, 2026-10-08):** that
  holds for the `ROGUE` user **after** its account push only. Before the
  push (C6), the `ROGUE` user also waited ~1.9 s for a fetch, the same as
  C1. The server refuses an unknown account key before it ever reaches the
  operator check.

**Open question — C17 in run 3.** C1 logged 3 `authentication error` lines
within 0.3 s, each after its own 1.9 s account fetch. In the other 5 runs it
logged 1. The guess: the client retried, racing the 1.9 s fetch. This is
**not proven**. Find the cause before a card says anything about how many
times a refused client tries.

## Exercise 02 — where do permissions live, and who decides them?

- **Command:** the manual steps in `EXERCISE-02-TERMINAL-STEPS.md`, run by
  Claude, then `exercises/ex02-check.sh` (15 checks).
- **Runs:** 3, all **ALL PASS**, identical.

Part 02a — permissions in the user JWT:

| Check | Prediction | Client reported | Server logged | Delivered? | Verdict |
|---|---|---|---|---|---|
| P1 `analytics-reader` JWT | perms inside | — | JWT claim `"pub": {"deny": [">"]}, "sub": {"allow": ["orders.>"]}` | — | measured ✓ `D06-R5` |
| P2 reader issued while the server runs | admitted, no push, no restart | connects, receives | nothing | yes | measured ✓ `D06-R4` |
| P3 reader publishes `orders.created` | denied | `Permissions Violation for Publish to "orders.created"` | `"<ACC>/ORDERS/jwt:<USERKEY>" - Publish Violation - Subject "orders.created"` | no | measured ✓ `D06-R5` |
| P4 reader subscribes `invoices.>` | denied | `Permissions Violation for Subscription to "invoices.>"` | violation logged | — | measured ✓ `D06-R5` |
| P5–P6 how the log names the user | by key | — | `jwt:<user public key>`; the name `analytics-reader` never appears | — | measured ✓ |
| P7 delivery | not its own message | — | — | only the `order-svc` message | measured ✓ |

Part 02b — a scoped signing key `reader`:

| Check | Prediction | Client reported | Server logged | Delivered? | Verdict |
|---|---|---|---|---|---|
| P8 `greedy-reader` asks for `--pub-allow '>'` | role wins | — | JWT `pub` is `{}`. The CLI dropped the request. | — | measured ✓ `D06-R6` |
| P9 `greedy-reader` before the push | refused | `Authorization Violation`, exit 1 | `authentication error` | no | measured ✓ |
| P10 after the push | admitted | connects, receives | nothing | yes | measured ✓ `D06-R6` |
| P11 `greedy-reader` publishes | denied by the role | `Permissions Violation for Publish to "orders.created"` | violation logged | no | measured ✓ `D06-R6` |
| P12 `order-svc` publishes | allowed | exit 0 | nothing | yes | measured ✓ |
| P13 server process | same PID throughout | — | — | — | measured ✓ |
| T1–T2 teardown | nothing left | — | no process, port 4922 free | — | measured ✓ |

**Gotchas hit:**

- **A new scoped signing key needs a push.** The server does not know the
  key until the account JWT that lists it is pushed. Before that, a user
  signed with it is refused. A user signed with the account's own key needs
  no push.

**Surprises against the docs:**

- **The CLI silently drops a scoped user's own permissions.** `nats auth user
  add … --key reader --pub-allow '>'` exits 0 and writes a JWT with an empty
  `pub`. It prints the role's permissions, not the ones asked for. Good for
  safety; confusing if you did not expect it.

## Exercise 03 — expiry and revocation

- **Command:** the manual steps in `EXERCISE-03-TERMINAL-STEPS.md`, run by
  Claude, then `exercises/ex03-check.sh` (16 checks).
- **Runs:** the 14-check script ran 3 times, **ALL PASS**. Checks Q1–Q2 were
  then added (16 checks), after a hand run of the same commands; the
  16-check script ran 3 times, **ALL PASS**.

Part 03a — a credential that expires in 10 s:

| Check | Prediction | Client reported | Server logged | Delivered? | Verdict |
|---|---|---|---|---|---|
| E1 JWT claims | `exp = iat + 10` | — | claims match | — | measured ✓ |
| E2 before expiry | admitted | connects, receives | nothing | yes | measured ✓ |
| E3 **open** connection at expiry | unknown — docs talk of new connections | `Disconnected due to: EOF, will attempt reconnect` | **nothing at the cut**; refused reconnects only | — | measured ✓ `D06-R7`. Cut within 1 s of `exp` (hand run: exp 09:13:07, cut 09:13:07) |
| E4 **new** connection after expiry | refused | `Authorization Violation`, exit 1 | `authentication error` | no | measured ✓ `D06-R7` |
| E5 delivery after the cut | none | — | — | nothing | measured ✓ |
| E6 subscriber exit code | non-zero | **exit 0** | — | — | measured ✗ prediction wrong — see Surprises |

Part 03b — revoke `analytics-reader`:

| Check | Prediction | Client reported | Server logged | Delivered? | Verdict |
|---|---|---|---|---|---|
| Q1 revoked, **not pushed**: both copies | they differ | local `account info`: `Revocations: 1`; server `account query`: `Revocations: 0` | — | — | measured ✓ `D06-R8` |
| R1 revoked, **not pushed**, new connection | admitted | connects | nothing | — | measured ✓ `D06-R8` |
| R2 revoked, **not pushed**, open connection | still receives | — | — | yes | measured ✓ `D06-R8` |
| R3 after the push, open connection | cut | `Disconnected …` | — | — | measured ✓ `D06-R8`. Cut within 1 s of the push |
| R4 after the push, new connection | refused | `Authorization Violation`, exit 1 | `authentication error` | no | measured ✓ `D06-R8` |
| R5 `order-svc`, same account | unaffected | exit 0 | nothing | yes | measured ✓ |
| Q2 after the push: server copy | matches local | `account query`: `Revocations: 1` | — | — | measured ✓ `D06-R8` |
| R6 account JWT on the server | lists the revocation | — | `/accountz?acc=<ORDERS>` contains the reader's key | — | measured ✓ |
| T1–T2 teardown | nothing left | — | no process, port 4922 free | — | measured ✓ |

**Gotchas hit:**

- **`nats auth user rm --revoke` changes only the local store.** Until
  `nats auth account push`, the server knows nothing. In that gap, the
  revoked user connects and receives as before.

**Surprises against the docs:**

- **An open connection is cut at expiry and at revocation.** The server does
  not wait for the client to reconnect. Both were measured within 1 s.
- **A cut subscriber exits 0.** `nats sub --wait 20s` printed
  `Disconnected due to: EOF, will attempt reconnect`, kept trying, and exited
  0 when the wait ended. A script that trusts the exit code will miss the cut.
- **The server logs nothing at the expiry cut.** Only the refused reconnects
  after it appear in the log.

**The general rule, and its limit.** Q1/R1/R2 then Q2/R3/R4 show that an
account change does nothing on the server until it is pushed. That was
measured for a **revocation** only. The docs' example — an edited connection
limit (`nats auth account edit ORDERS --connections 50`) — was **not**
tested. Do not claim connection-limit enforcement from this exercise.

## One-off: `system_account` removed from the config (optional, by hand)

Run once by Claude, 2026-10-08, on a scratch config (port 4923, scratch
resolver directory, outside the repo). Not in any check script.

- The scratch config was `.run/trust.conf` **without** its `system_account`
  line. The operator JWT still carries `system_account` (`auth operator info
  D06` shows `System Account: SYSTEM (A…)`).
- `nats-server -t`: `configuration file … is valid`, exit 0. That checks the
  config only; it says nothing about start-up.
- A real start: `Server is ready`, `/healthz` `{"status":"ok"}`. The boot log
  printed `System  : ""`, but `/varz` showed `"system_account": "<SYSTEM key>"`
  — taken from the operator JWT. `nats auth account push ORDERS` with
  `sys.creds` then reported success.
- **Conclusion:** removing the config line alone does **not** stop the
  server, because the operator JWT supplies the system account. The docs'
  failure ("the system account needs to be specified") needs the system
  account to be absent from **both** the config and the operator JWT. That
  case was **not measured**: `nats auth operator add` always creates
  `SYSTEM`, so it needs a hand-built operator JWT.
- **Trap:** the boot log line `System : ""` looks like "no system account".
  `/varz` is the evidence, not that line.

## Also measured

- The CLI writes every `.creds` file and seed mode 600.
- `lab/nats.sh` keeps the real store untouched: nothing under
  `~/.local/share/nats` or `~/.config/nats` changed.
- `nats auth account info SYSTEM --json` prints the account's seed and
  its users' seeds. Never use `--json` in a walkthrough.

## Re-run after removing Python and Perl (2026-10-08)

The scripts changed; the checks did not. Each check tests the same thing
as before.

- **What changed:** JWT claims are read with `base64` + `jq`
  (`d06_claims` in `lab/lib.sh`) instead of Python. `lab/trust-conf.sh`
  reads the SYSTEM key from `nats auth account info SYSTEM`. The wrong-seed
  file is made by `exercises/wrong-seed.sh` (`awk` + `sed`). `ex03-check.sh`
  stamps times with bash 5's `EPOCHREALTIME` instead of Perl, and compares
  them with `awk`.
- **Versions:** as above, plus jq at `/opt/homebrew/bin/jq`, GNU bash 5.3.15.
- **Runs:** ex01 (24 checks), ex02 (15) and ex03 (16), 3 times each, in
  that order. All 9 runs: **ALL PASS**, exit 0.
- **The helpers can fail:** `within_1s`'s `awk` test exited 1 for times
  2.5 s apart. `wrong-seed.sh` output differed from `order-svc.creds` in
  the seed line only.
- **Walkthrough change, not run as one walkthrough:** EXERCISE-03 Step 3
  now has the reader add 60 s to `date` instead of decoding the JWT. The
  `nats` CLI has no command that shows the `exp` inside a `.creds` file.
  `D06-R9` is still open.

## Walkthrough — exercise 01 by hand (2026-10-08)

The user ran `EXERCISE-01-TERMINAL-STEPS.md` in their own terminals, with
Claude guiding each step. Guided, so it does **not** close `D06-R9`.
Versions as above.

- **Result:** every step matched "What you should have seen", after the
  Step 1 fix below.
- **Step 1 was wrong.** `nats auth user add … --credential
  .run/creds/order-svc.creds` failed with `open .run/creds/order-svc.creds:
  no such file or directory`, because `.run/creds/` did not exist. The user
  JWT was still written to the store; only the `.creds` file was missing.
  `lab/chain.sh` makes the folder first, so the check scripts never hit
  this. Fixed: Step 1 now runs `mkdir -p .run/creds`. Recovery without a
  restart: `lab/nats.sh auth user credential .run/creds/order-svc.creds
  order-svc ORDERS`.
- **Step 3 changed, not yet run.** The user asked for the raw
  `nats-server -c … 2>&1 | tee .run/server.log` as the main command, with
  `lab/up.sh` optional. This run used `lab/up.sh`; the `tee` form is
  untested.
- **Rogue user before its push (Step 9):** `Account [A…] fetch took
  1.901208084s`, `fetching jwt timed out`, then `authentication error` —
  the same as Step 4. After the push (Step 10): `authentication error` only,
  no fetch. See the correction under Surprises above.
- **The rogue push logged nothing** on the server. The `ROGUE` JWT appeared
  in `.run/resolver/`; `/accountz` listed `ORDERS`, `SYSTEM` and `$G` only.
- **`/connz?auth=1`** names the user by key, not name: `authorized_user` =
  the `U…` key, `account` and `issuer_key` = the `ORDERS` `A…` key,
  `name_tag` = `ORDERS`. The name `order-svc` is only inside the `jwt` field.
- **`nats auth operator add` also made an operator signing key**
  (`O…`, listed under `Signing Keys`). `ORDERS` was still signed by the
  identity key (`Issuer:` in both the local and the server copy), as I2 says.
- **Error count:** 6 `authentication error` lines for 6 refused attempts
  (the user ran Step 8 twice; the two lines are 45 s apart). One line per
  attempt, so this run says nothing new about the C17 open question.

## Walkthrough — exercise 02 by hand (2026-10-08 to 2026-10-09)

The user ran `EXERCISE-02-TERMINAL-STEPS.md` in their own terminals, with
Claude guiding each step. Guided, so it does **not** close `D06-R9`.
Versions: `nats-server` v2.14.6, `nats` CLI 0.4.0. Started on the `.run/`
left by exercise 01 (no `lab/chain.sh`).

- **Result:** all 15 steps matched "What you should have seen". No step
  file change was needed.
- **Step 8:** the client printed `Subscribing on invoices.>` *before* the
  `Permissions Violation for Subscription` error. The first line does not
  mean the server allowed the subscribe.
- **Step 11** (`greedy-reader` before the push): client `Authorization
  Violation`; server `authentication error` only, no fetch line (the server
  already had `ORDERS`). The log gives no reason. The reason "issuer not
  known" is from source (`auth.go` L1031–1033, a debug-level message), not
  from this log.
- **Step 12:** after the push, the server logged `…/ORDERS/jwt:<greedy key>
  - Publish Violation`: admitted, then denied by the role. The push report
  (`Success 1`) alone was not the evidence; the admission was.
- **Step 14:** the PID file said 44901. `ps` confirmed process 44901 alive,
  started 2026-10-08 14:17:13 with `ex01-nats-operator.conf`, and the log
  had one `Server is ready` line. The server ran overnight; Step 13 ran the
  next morning on the same process.
- **Step 15:** `lab/down.sh` stopped 44901; port 4922 free (checked with
  `ps` and `lsof`).

### Scoped signing key: `Max Payload: 0` vs `unlimited` — source-derived, not runtime-verified

In this run, `nats auth account keys add ORDERS reader …` printed
`Max Payload: 0` for the role. The next `nats auth user add greedy-reader
… --key reader` printed `Max Payload: unlimited` for the same role.

**Status: source-derived evidence only.** Read from the source of the
installed versions, plus the raw claims and `/varz` of this run. **No
publish has tested the payload limit of a scoped user.** `greedy-reader`
cannot publish, so its denied publish proves nothing about payload.

Raw claims in this run (decoded, not verified):

- Account JWT, `signing_keys[0].template`: `pub.deny [">"]`,
  `sub.allow ["orders.>"]`, `subs: -1`, `data: -1`, **no `payload` field**.
- `greedy-reader` user JWT: `iss` = the `reader` key, `pub {}`, `sub {}`,
  no limits at all.
- `ORDERS` account limits: `payload: -1`. `/varz` `max_payload`: `1048576`.
  No `max_payload` in the server config.

**1. What the server enforces for a scoped user (source):**

- The server refuses a scoped user whose JWT has any permissions or limits
  set: [jwt v2.8.2 `signingkeys.go` L94–106](https://github.com/nats-io/jwt/blob/v2.8.2/v2/signingkeys.go#L94-L106).
- On login, the server replaces the user's whole `UserPermissionLimits`
  (pub/sub, src, times, locale, subs, data, payload, bearer, connection
  types) with the key's template:
  [nats-server v2.14.6 `auth.go` L1031–1045](https://github.com/nats-io/nats-server/blob/v2.14.6/server/auth.go#L1031-L1045).
- The server takes payload and subs from the template, then lowers them to
  the account limit and to the server's `max_payload`:
  [nats-server v2.14.6 `client.go` L942–987](https://github.com/nats-io/nats-server/blob/v2.14.6/server/client.go#L942-L987).
- The CLI never writes a scoped user's own permissions. `updateUser`
  returns early, so `--pub-allow '>'` and `--defaults` were dropped:
  [natscli v0.4.0 `auth_user_command.go` L434–437](https://github.com/nats-io/natscli/blob/v0.4.0/cli/auth_user_command.go#L434-L437).

**2. What `0` and `unlimited` mean in these CLI displays (source):**

1. `keys add` has no default for `--payload`
   ([natscli v0.4.0 `auth_account_command.go` L292](https://github.com/nats-io/natscli/blob/v0.4.0/cli/auth_account_command.go#L292)).
   `skAddAction` sets `limits.Payload = 0`, then prints that in-memory
   value ([L556–614](https://github.com/nats-io/natscli/blob/v0.4.0/cli/auth_account_command.go#L556-L614)).
   That is the `0`.
2. `payload` is `omitempty`
   ([jwt v2.8.2 `account_claims.go` L50](https://github.com/nats-io/jwt/blob/v2.8.2/v2/account_claims.go#L50)),
   so the `0` is not written. The JWT on disk has no `payload` field.
3. On decode, each scope starts as `NewUserScope()` with
   `subs`/`data`/`payload` = `-1` (no limit). A missing field stays `-1`:
   [jwt v2.8.2 `signingkeys.go` L77–82, L144–175](https://github.com/nats-io/jwt/blob/v2.8.2/v2/signingkeys.go#L77-L82).
   The CLI builds against jwt v2.8.1, which has the same code:
   [v2.8.1 `signingkeys.go`](https://github.com/nats-io/jwt/blob/v2.8.1/v2/signingkeys.go#L77-L82).
4. `user add` re-reads the account from disk and renders the scope's
   template for a scoped user
   ([natscli v0.4.0 `auth_user_command.go` L505–511](https://github.com/nats-io/natscli/blob/v0.4.0/cli/auth_user_command.go#L505-L511);
   [jwt-auth-builder v0.0.9 `scope.go` L239–241](https://github.com/synadia-io/jwt-auth-builder.go/blob/v0.0.9/scope.go#L239-L241)).
   `RenderUserLimits` prints `unlimited` only for `-1`
   ([natscli v0.4.0 `internal/auth/auth.go` L320](https://github.com/nats-io/natscli/blob/v0.4.0/internal/auth/auth.go#L320)).
   That is the `unlimited`.

**Source-derived conclusion:** `0` is a value in the CLI's memory that the
JWT can never hold. The server decodes the omitted field as `-1`, the same
as the second display. So `greedy-reader` gets no payload limit from its
role, and the effective limit is the server's `max_payload`, 1 MiB.
**Not runtime-verified.**

**Optional probe (not run).** It tests something different: whether the
server enforces an **explicit** scoped payload limit (`--payload 100` on a
throwaway `probe` key, with `probe.>` permissions). It does **not** test the
omitted-field / `0` / `unlimited` behavior above. If it is run, it passes
only with both of these:

- confirmed delivery of a message below the limit, seen by a subscriber;
- a payload rejection above the limit, logged by the **server** and tied
  to that connection.

How the client behaves above the limit is a prediction until observed.
Prediction: the client sends the message, because it knows only the
server's 1 MiB. The server then closes the connection with
`Maximum Payload Violation`.
