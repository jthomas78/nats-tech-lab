# Exercise 02 — where do permissions live, and who decides them? Step by step

> **Status: measured, then turned into a check.** The results come from the
> hand run and the script runs of 2026-10-08, recorded in
> [`EXERCISE_OBSERVATIONS.md`](EXERCISE_OBSERVATIONS.md). This file was
> written after those runs. Nobody has yet run its exact commands as one
> walkthrough (`D06-R9`). To run the same checks as a script:
> `exercises/ex02-check.sh`.

In demo 05 the permissions were in the server config. Here they are inside
the signed user JWT. The server config does not change in this exercise, and
the server is never restarted.

Part 02a: the account key signs a user and writes its permissions. Part 02b:
a **scoped signing key** — a second account key that carries a fixed role.
Any user signed with it gets the role, whatever the user asks for.

Three terminals, all in `demos/06-operator-trust-and-credentials`:

- **A** = the server log
- **B** = the listener
- **C** = everything else

---

## Step 1: A running server that knows `ORDERS`

**Concept:** if `.run/` is still there from exercise 01, skip `lab/chain.sh`. If not, run it first.
**Terminal C:**

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

## Step 2: Note the server PID

**Terminal C:**

```bash
cat .run/nats-server.pid
```

**Purpose:** at the end you check it is the same process.

## Step 3: Issue a reader while the server runs

**Concept:** `analytics-reader` may subscribe to `orders.>` and may publish nothing.
**Terminal C:**

```bash
lab/nats.sh auth user add analytics-reader ORDERS --defaults --sub-allow 'orders.>' --pub-deny '>' --credential .run/creds/analytics-reader.creds
```

**Purpose:** a new user, made after the server started.
**Predict:** do you need to push the account, or restart the server, before this user can connect?

## Step 4: Read the permissions inside the JWT

**Terminal C:**

```bash
lab/nats.sh auth user info analytics-reader ORDERS
```

**Purpose:** see the permissions the server will get from the user, not from its config.

## Step 5: The reader listens

**Terminal B:**

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/analytics-reader.creds sub 'orders.>'
```

**Predict:** does it connect?

## Step 6: `order-svc` publishes

**Terminal C:**

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/order-svc.creds pub orders.created 'from-order-svc'
```

**Predict:** does terminal B receive it?

## Step 7: The reader tries to publish

**Terminal C:**

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/analytics-reader.creds pub orders.created 'from-reader'
```

**Predict:** what does the client print? How does terminal A name the user: by name, or by key?

## Step 8: The reader tries another subject

**Terminal C:**

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/analytics-reader.creds sub 'invoices.>' --count 1 --wait 2s
```

**Predict:** allowed or denied?

## Step 9: Add a scoped signing key `reader`

**Concept:** the role "read orders, publish nothing" now lives in the account JWT, under the key.
**Terminal C:**

```bash
lab/nats.sh auth account keys add ORDERS reader --sub-allow 'orders.>' --pub-deny '>' --description 'read orders only'
```

## Step 10: A user that asks for too much

**Concept:** `greedy-reader` is signed with the `reader` key and asks to publish everywhere.
**Terminal C:**

```bash
lab/nats.sh auth user add greedy-reader ORDERS --key reader --pub-allow '>' --defaults --credential .run/creds/greedy-reader.creds
```

**Predict:** which permissions does the CLI print: the ones asked for, or the role?

## Step 11: Use it before the push

**Terminal C:**

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/greedy-reader.creds pub orders.created 'greedy-before-push'
```

**Predict:** admitted or refused? The reader in Step 5 needed no push. Why might this one?

## Step 12: Push, then publish again

**Terminal C:**

```bash
lab/nats.sh auth account push ORDERS -s nats://127.0.0.1:4922 --creds .run/creds/sys.creds
```

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/greedy-reader.creds pub orders.created 'from-greedy'
```

**Predict:** admitted now? Is the publish allowed?

## Step 13: The greedy reader listens

Stop terminal B with `Ctrl-C`. Then, **terminal B:**

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/greedy-reader.creds sub 'orders.>'
```

**Terminal C:**

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/order-svc.creds pub orders.created 'for-greedy'
```

**Predict:** does terminal B receive it?

## Step 14: Same server?

**Terminal C:**

```bash
cat .run/nats-server.pid
```

**Predict:** the same PID as Step 2?

## Step 15: Stop

Stop terminals A and B with `Ctrl-C`. Then, **terminal C:**

```bash
lab/down.sh
```

## What you should have seen

- `analytics-reader` got in with no push and no restart. Its permissions were in its JWT.
- Its publish and its `invoices.>` subscribe were denied: `Permissions Violation`.
- The server log named it `jwt:<public key>`, never `analytics-reader`.
- The CLI dropped `greedy-reader`'s `--pub-allow '>'` and printed the role.
- `greedy-reader` was refused until the push, because the server did not know the `reader` key yet. After the push it could read, and its publish was denied by the role.
- The PID did not change.
