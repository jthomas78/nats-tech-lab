# Exercise 03 — what happens when a credential expires, or is revoked? Step by step

> **Status: measured, then turned into a check.** The results come from the
> hand run and the script runs of 2026-10-08, recorded in
> [`EXERCISE_OBSERVATIONS.md`](EXERCISE_OBSERVATIONS.md). This file was
> written after those runs. Nobody has yet run its exact commands as one
> walkthrough (`D06-R9`). To run the same checks as a script:
> `exercises/ex03-check.sh`.

The server stores no users, so it cannot "delete" one. Two other tools end
a user's access:

- **Expiry** — the user JWT carries an end time (`exp`). After it, the JWT is no good.
- **Revocation** — the account JWT lists users it no longer trusts. The server
  learns this only when the account JWT is pushed.

Each one is tested twice: on a **new** connection, and on an **open**
connection that was already in when the access ended. The docs talk about
new connections. Do not assume an open one behaves the same.

Three terminals, all in `demos/06-operator-trust-and-credentials`:

- **A** = the server log
- **B** = the listener being cut
- **C** = everything else

---

## Step 1: Start clean

**Concept:** a new chain, so `analytics-reader` does not exist yet.
**Terminal C:**

```bash
lab/down.sh --clean
```

```bash
lab/chain.sh
```

```bash
lab/up.sh ex01-nats-operator
```

```bash
lab/nats.sh auth account push ORDERS -s nats://127.0.0.1:4922 --creds .run/creds/sys.creds
```

**Terminal A:**

```bash
tail -f .run/server.log
```

---

## Part 03a — expiry

## Step 2: A credential that lives for 60 seconds

**Concept:** the same user `order-svc`, a new credential file with an end time. (The script uses 10 s.)
**Timing:** the 60 s start when this command runs. Steps 4 and 5 must both be done before the end time. Read Steps 3 to 5 first, and have the Step 4 command ready to paste in terminal B.
**Terminal C:**

One line: make the credential, then print the time at once. `date` runs right after the JWT is signed, so its time is the signing time. `--force` lets you re-issue the file if you must start again.

```bash
lab/nats.sh auth user credential .run/creds/order-svc-60s.creds order-svc ORDERS --expire 60s --force; date '+%H:%M:%S'
```

## Step 3: Work out the end time

**Concept:** `--expire 60s` writes an end time (`exp`) into the new user JWT: the time it was signed plus 60 s. The server reads `exp` on every connection.
**Purpose:** add 60 s to the time `date` printed. Write it down. That is the end time, to within about a second. Then go straight to Steps 4 and 5.

**If you miss the window:** Step 4 is refused, or terminal B does not show `before-expiry` before the end time. Then the rest of Part 03a shows nothing. Stop terminal B with `Ctrl-C` and go back to Step 2. `--force` overwrites the old file with a new 60 s credential.

The `nats` CLI has no command that shows the end time inside a `.creds` file. `exercises/ex03-check.sh` reads it from the JWT. It checks that the end time is exactly 10 s after the signing time (check `E1`).

## Step 4: Listen with the short credential

**Terminal B:**

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/order-svc-60s.creds sub 'orders.>' --wait 90s
```

**Predict:** does it connect?

## Step 5: Send one message before the end time

**Terminal C:**

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/order-svc.creds pub orders.created 'before-expiry'
```

**Predict:** does terminal B receive it?

## Step 6: Wait for the end time

**Concept:** do nothing. Watch terminal B.
**Predict:** at the end time, does the open connection stay, or is it cut? If it is cut, note the time on the `Disconnected` line. Does terminal A log anything at that moment?

## Step 7: A new connection after the end time

**Terminal C:**

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/order-svc-60s.creds pub orders.created 'after-expiry'
```

**Predict:** admitted or refused?

## Step 8: Is the cut listener still receiving?

**Timing:** run this after terminal B is cut and before its `--wait 90s` ends. That window is only about 30 s. Too early, and terminal B is still connected; too late, and it has already exited.
**Terminal C:**

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/order-svc.creds pub orders.created 'after-expiry-sent'
```

**Predict:** does terminal B receive it?

## Step 9: The cut listener's exit code

**Concept:** wait for terminal B's `--wait 90s` to end on its own. Do not press `Ctrl-C`.
**Terminal B:**

```bash
echo $?
```

**Predict:** 0 or not 0? Would a script notice the cut?

**The server's side of Step 7.** While terminal B ran, it retried about every 2 s, and each try logged one `authentication error` line. Now it has exited, so the log is quiet. Count the lines, run Step 7's command again, and count again.
**Terminal C:**

```bash
grep -c 'authentication error' .run/server.log
```

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/order-svc-60s.creds pub orders.created 'after-expiry'
```

```bash
grep -c 'authentication error' .run/server.log
```

**Predict:** by how much does the count go up?

---

## Part 03b — revocation

## Step 10: Issue the reader, and note its key

**Terminal C:**

```bash
lab/nats.sh auth user add analytics-reader ORDERS --defaults --sub-allow 'orders.>' --pub-deny '>' --credential .run/creds/analytics-reader.creds
```

```bash
lab/nats.sh auth user info analytics-reader ORDERS
```

**Purpose:** write down the reader's public key (it starts with `U`). You need it in Step 16.

## Step 11: The reader listens

**Terminal B:**

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/analytics-reader.creds sub 'orders.>'
```

## Step 12: Revoke the reader — but do not push

**Terminal C:**

```bash
lab/nats.sh auth user rm analytics-reader ORDERS --revoke -f
```

**Purpose:** the revocation is now in the account JWT on your disk. The server has not seen it.

See the two copies drift apart — your local copy, then the server's copy:

```bash
lab/nats.sh auth account info ORDERS
```

```bash
lab/nats.sh auth account query ORDERS -s nats://127.0.0.1:4922 --creds .run/creds/sys.creds
```

**Predict:** what does `Revocations:` say in each?

## Step 13: Is the reader still in?

**Terminal C:**

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/analytics-reader.creds sub 'orders.>' --count 1 --wait 1s
```

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/order-svc.creds pub orders.created 'revoked-not-pushed'
```

**Predict:** does the new connection print `Authorization Violation`? Does terminal B receive `revoked-not-pushed`?

## Step 14: Push the account

**Terminal C:**

```bash
lab/nats.sh auth account push ORDERS -s nats://127.0.0.1:4922 --creds .run/creds/sys.creds
```

**Predict:** watch terminal B. Is the open connection cut? How soon?

Then read the server's copy again:

```bash
lab/nats.sh auth account query ORDERS -s nats://127.0.0.1:4922 --creds .run/creds/sys.creds
```

**Predict:** does `Revocations:` match your local copy now?

## Step 15: New connections, both users

First stop terminal B with `Ctrl-C`. Its connection was cut in Step 14. It now retries about every 2 s, and each try logs one `authentication error` line.

**Terminal C:** count the lines, try the revoked user, and count again:

```bash
grep -c 'authentication error' .run/server.log
```

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/analytics-reader.creds sub 'orders.>' --count 1 --wait 1s
```

```bash
grep -c 'authentication error' .run/server.log
```

Then the positive control:

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/order-svc.creds pub orders.created 'after-revoke'
```

**Purpose:** the revoked user, then a positive control in the same account.
**Predict:** which one gets in? By how much does the count go up?

## Step 16: The revocation on the server

**Terminal C:**

```bash
lab/nats.sh auth account info ORDERS
```

**Purpose:** write down the `ORDERS` account key (it starts with `A`). Then put it in place of `ORDERS-KEY` below.

```bash
curl -s 'http://127.0.0.1:8922/accountz?acc=ORDERS-KEY'
```

**Predict:** is the reader's key from Step 10 in the output?

## Step 17: Stop

Stop terminal A with `Ctrl-C`. Then, **terminal C:**

```bash
lab/down.sh
```

## What you should have seen

- **Expiry, open connection:** cut at the end time, within one second. The client printed `Disconnected due to: EOF, will attempt reconnect`. The server logged nothing at the cut, only the refused reconnects.
- **Expiry, new connection:** refused. The server logged one `authentication error` line for it.
- **The trap:** the cut listener still exited 0.
- **Revoked, not pushed:** nothing changed. New connections got in; the open one kept receiving. `account query` showed why: local `Revocations: 1`, server `Revocations: 0`.
- **The general rule this shows:** an account change does nothing until it is pushed. Measured here for a revocation only. Other account changes (for example a connection limit) were not tested.
- **Revoked, pushed:** the open connection was cut within a second, and new connections were refused, one `authentication error` line each. `order-svc` in the same account was not touched.
- **Choice this supports:** a revocation is only as fast as your push. Short-lived credentials end access with no push at all.
