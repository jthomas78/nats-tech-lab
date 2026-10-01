# Exercise 02 — what may each user do? Step by step

> **Status: run by hand (2026-10-01), then turned into a check.** Measured
> results are in [`EXERCISE_OBSERVATIONS.md`](EXERCISE_OBSERVATIONS.md). To run
> the same steps as a script: `exercises/ex02-check.sh`.

Exercise 01 answered "who are you?" (**authentication**). Exercise 02 answers
"what may you do?" (**authorization**). Both users now get in. The server then
checks every publish and every subscribe.

Three terminals, all in `demos/05-identity-and-permissions`:

- **A** = the server
- **B** = the listener (`analytics-reader`)
- **C** = the sender (`order-svc`, and sometimes others)

Steps 1–7 are part 02a: one side listed. Steps 8–16 are part 02b: both sides
listed. Every `nats` command passes `--no-context`.

---

## Step 1: Load the passwords

**Concept:** the server reads passwords from its environment. Skip this if the terminal already has them.
**Terminals A, B and C:**

```bash
source .run/secrets.env
```

**Purpose:** put both passwords in the terminal.
**Predict:** check with `echo ${#D05_ORDER_SVC_PASSWORD}`. What length? (0 means not loaded.)

## Step 2: Start a server where each user lists one side

**Concept:** `order-svc` lists only `publish`. `analytics-reader` lists only `subscribe`. Nobody said anything about the other side.
**Terminal A:**

```bash
nats-server -c exercises/config/ex02-nats-permissions-one-sided.conf
```

**Purpose:** start the server with one-sided permissions.
**Predict:** does the log look different from exercise 01?

## Step 3: A listener that may subscribe to orders

**Concept:** this listener is the control. It shows what is really delivered.
**Terminal B:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" sub 'orders.>'
```

**Purpose:** listen on `orders.>` as `analytics-reader`.
**Predict:** does it connect?

## Step 4: The allowed publish

**Concept:** `order-svc` may publish to `orders.>`.
**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" pub orders.created 'from-order-svc'
```

**Purpose:** the positive control.
**Predict:** does C succeed? Does B show the message?

## Step 5: The reader publishes. Should it?

**Concept:** we wanted the reader to publish nothing. But we only listed `subscribe` for it.
**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" pub orders.created 'from-reader'
```

**Purpose:** test a publish nobody meant to allow.
**Predict:** allowed or denied? Does B show it?

## Step 6: The sender subscribes. Should it?

**Concept:** the same trap on the other side. `order-svc` has no `subscribe` entry.
**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" sub 'orders.>' --wait 2s
```

**Purpose:** test a subscription nobody meant to allow.
**Predict:** allowed or denied?

## Step 7: Reset

**Concept:** a side you do not list is **open**. Listing one side does not close the other.
**Terminals B, then A:** press Ctrl-C.
**Purpose:** stop the listener and the server.
**Predict:** what must we add to close the other side?

## Step 8: Start a server where both sides are set

**Concept:** each user now has `publish` and `subscribe`. The side that should do nothing has `deny: ">"`. (`>` means every subject.)
**Terminal A:**

```bash
nats-server -c exercises/config/ex02-nats-permissions.conf
```

**Purpose:** start the server with both sides set.
**Predict:** what will change in Steps 5 and 6 when we run them again?

## Step 9: The listener again

**Terminal B:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" sub 'orders.>'
```

**Purpose:** the control listener. It stays open for the rest of the exercise.
**Predict:** does it connect?

## Step 10: The allowed publish, again

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" pub orders.created 'from-order-svc'
```

**Purpose:** the positive control. Allowed things must still work.
**Predict:** does B show it?

## Step 11: The sender publishes outside its list

**Concept:** `order-svc` may publish `orders.>` only. `invoices.created` is not in that list.
**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" pub invoices.created 'x'
```

**Purpose:** a publish outside the allow list.
**Predict:** what error? Does the connection succeed first?

## Step 12: The reader publishes

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" pub orders.created 'from-reader'
```

**Purpose:** Step 5 again, with the other side closed.
**Predict:** allowed or denied now? Does B show it?

## Step 13: The sender subscribes

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" sub 'orders.>' --wait 2s
```

**Purpose:** Step 6 again, with the other side closed.
**Predict:** allowed or denied now?

## Step 14: The reader subscribes outside its list

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" sub 'invoices.>' --wait 2s
```

**Purpose:** a subscription outside the allow list.
**Predict:** what error?

## Step 15: Read the evidence in three places

**No command. Look at:**

- **C:** the four denials. What words does the client use? (`Permissions Violation for Publish` or `for Subscription`.)
- **A's log:** four `[ERR]` lines. Each says `Publish Violation` or `Subscription Violation`. Each names the user and the subject.
- **B:** only the `from-order-svc` messages arrived. Nothing from the reader.

**Concept:** the client's error and the server's log are two views. The delivery in B is the proof that the denied message did not go through. The client error may arrive after the command has returned, so do not trust the exit code alone.

## Step 16: Reset

**Terminals B, then A:** press Ctrl-C.

---

**The lesson:** authentication got both users in. Authorization limits each one.
Set **both** sides for every user. A side you leave out is open.

**Not covered here:** wildcards, `allow` plus `deny` together, and default
permissions are exercise 03. Request and reply is exercise 04.
