# Exercise 01 — who gets in, when the server knows only the operator? Step by step

> **Status: measured, then turned into a check.** The results come from the
> hand run and the script runs of 2026-10-08, recorded in
> [`EXERCISE_OBSERVATIONS.md`](EXERCISE_OBSERVATIONS.md). This file was
> written after those runs. Nobody has yet run its exact commands as one
> walkthrough (`D06-R9`). To run the same checks as a script:
> `exercises/ex01-check.sh`.

This is **decentralized authentication**. In demo 05 the server config held
a list of users. Here the server trusts one **operator** key. The operator
signs **accounts**. An account signs **users**. To get in, a user shows its
signed JWT and signs a random challenge (a **nonce**) with its secret key
(its **seed**). The server checks three things: the nonce signature, the
user signed by the account, and the account signed by the operator.

Three terminals, all in `demos/06-operator-trust-and-credentials`:

- **A** = the server log
- **B** = the listener
- **C** = everything else

Every `nats` command goes through `lab/nats.sh`. It adds `--no-context` and
keeps this demo's keys in `.run/`.

---

## Step 1: Build the chain

**Concept:** an operator `D06`, a `SYSTEM` account with user `admin`, an account `ORDERS` with user `order-svc`. All keys stay in `.run/`.
**Terminal C:**

```bash
lab/chain.sh
```

**Purpose:** make every key and JWT, and write `.run/trust.conf`.
**Predict:** how many user names will the server config hold?

## Step 2: Read what the server will trust

**Concept:** the server's trust lives in one small file.
**Terminal C:**

```bash
cat .run/trust.conf
```

**Purpose:** see the operator JWT path, the `SYSTEM` account key and one preloaded `SYSTEM` JWT.
**Predict:** is `ORDERS` there? Is `order-svc` there?

**Inspection only** — these commands read claims. They prove nothing about what the server enforces.

```bash
lab/nats.sh auth operator info D06
```

```bash
lab/nats.sh auth account info ORDERS
```

```bash
lab/nats.sh auth user info order-svc ORDERS
```

**Look for:** the account's `Issuer` is the operator's key (the `O…` key on the first line of `operator info`). The user's `Max Payload` is `1,048,576`: `--defaults` put a 1 MiB limit in the JWT. This exercise does not test that the server enforces it.

## Step 3: Start the server and watch its log

**Terminal C:**

```bash
lab/up.sh ex01-nats-operator
```

**Terminal A:**

```bash
tail -f .run/server.log
```

**Purpose:** start the server in the background, then follow its log.
**Predict:** does the log name any account other than `SYSTEM`?
**Inspection only:** find the `Trusted Operators` block in the log. It names `Operator: "D06"`, when it was issued, and `Expires : Never`.

## Step 4: Try `order-svc` before the server knows `ORDERS`

**Concept:** the user is signed correctly. But the server has never seen the `ORDERS` account JWT.
**Terminal C:**

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/order-svc.creds pub orders.created 'before-push'
```

**Purpose:** a correct user, an unknown account.
**Predict:** admitted or refused? Watch terminal A: how long does the server wait before it answers?

Now ask the server for **its** copy of `ORDERS`:

```bash
lab/nats.sh auth account query ORDERS -s nats://127.0.0.1:4922 --creds .run/creds/sys.creds
```

**Predict:** you have `ORDERS` on your disk. Does the server have it?

## Step 5: Push the `ORDERS` account

**Concept:** the `SYSTEM` user `admin` sends the account JWT to the server. The server checks it is signed by the trusted operator, then stores it.
**Terminal C:**

```bash
lab/nats.sh auth account push ORDERS -s nats://127.0.0.1:4922 --creds .run/creds/sys.creds
```

```bash
ls .run/resolver/
```

```bash
lab/nats.sh auth account query ORDERS -s nats://127.0.0.1:4922 --creds .run/creds/sys.creds
```

**Purpose:** give the server the account, then read the server's copy back.
**Predict:** how many files are in the resolver directory? Is any of them a user? Compare `Users:` in the server's copy with `Users:` in `lab/nats.sh auth account info ORDERS`.

## Step 6: The listener

**Terminal B:**

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/order-svc.creds sub 'orders.>'
```

**Purpose:** listen on `orders.>`. This is the control: it shows what really gets delivered.
**Predict:** does it connect now?

## Step 7: Who is connected?

**Terminal C:**

```bash
curl -s 'http://127.0.0.1:8922/connz?auth=1'
```

**Purpose:** see how the server names the listener.
**Predict:** a user name, or an account key?

## Step 8: No credentials

**Terminal C:**

```bash
lab/nats.sh -s nats://127.0.0.1:4922 pub orders.created 'no-creds'
```

**Predict:** admitted or refused? Does terminal B get anything?

## Step 9: A user from a different operator

**Concept:** `ROGUE` is a second, throwaway operator in its own store (`.run/rogue/`). It makes an account and a user with the **same names**: `ORDERS` and `order-svc`. `lab/rogue.sh` is `lab/nats.sh` pointed at that store.
**Terminal C:**

```bash
lab/rogue.sh auth operator add ROGUE
```

```bash
lab/rogue.sh auth account add ORDERS --defaults
```

```bash
lab/rogue.sh auth user add order-svc ORDERS --defaults --credential .run/creds/rogue-order-svc.creds
```

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/rogue-order-svc.creds pub orders.created 'rogue'
```

**Purpose:** same names, wrong signer.
**Predict:** admitted or refused?

## Step 10: Push the rogue account

**Terminal C:**

```bash
lab/rogue.sh auth account push ORDERS -s nats://127.0.0.1:4922 --creds .run/creds/sys.creds
```

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/rogue-order-svc.creds pub orders.created 'rogue-pushed'
```

**Purpose:** try to smuggle in a foreign account.
**Predict:** what does the push command report? Is the rogue user admitted now? Compare terminal A with Step 4: is there a wait this time?

## Step 11: Which accounts did the server load?

**Terminal C:**

```bash
curl -s 'http://127.0.0.1:8922/accountz'
```

```bash
ls .run/resolver/
```

**Purpose:** compare what the server loaded with what is on disk.
**Predict:** is the rogue account key in both lists?

## Step 12: The right JWT, the wrong seed

**Concept:** copy `order-svc.creds`, but swap in the `admin` seed. The JWT is real; the signer is not. This is what a person with a stolen JWT, but no seed, has.
**Terminal C:**

`exercises/wrong-seed.sh` is **negative-test machinery**, not normal setup. You never build a `.creds` file by hand. It copies the first file and replaces its seed line with the seed line of the second file:

```bash
exercises/wrong-seed.sh .run/creds/order-svc.creds .run/creds/sys.creds .run/creds/wrong-seed.creds
```

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/wrong-seed.creds pub orders.created 'wrong-seed'
```

**Predict:** admitted or refused?

## Step 13: The positive control

**Terminal C:**

```bash
lab/nats.sh -s nats://127.0.0.1:4922 --creds .run/creds/order-svc.creds pub orders.created 'order-svc-order'
```

**Purpose:** prove the server still admits the real user.
**Predict:** how many messages has terminal B received in total?

## Step 14: Count the refusals in the log

**Terminal C:**

```bash
grep -c 'authentication error' .run/server.log
```

**Purpose:** the client always printed the same `Authorization Violation`. The log is where the refusals differ.
**Predict:** how many? (Steps 4, 8, 9, 10 and 12 were refused.)

## Step 15: Stop

Stop terminals A and B with `Ctrl-C`. Then, **terminal C:**

```bash
lab/down.sh
```

Leave `.run/` in place for exercise 02. To delete every key: `lab/down.sh --clean`.

## What you should have seen

- Before the push, `order-svc` was refused. The log shows `fetch took ~1.9s` and `fetching jwt timed out`. `account query` failed: `did not receive a valid token from the server`.
- After the push, `account query` returned `ORDERS`. The server's copy said `Users: 0`; your local copy said `Users: 1`. The server holds accounts, not users.
- After the push, `order-svc` got in, and `/connz` named it by account key.
- No credentials, the rogue user (before and after its push), and the wrong seed were all refused.
- The rogue push printed `Success 1 Failed 0` and left a file in `.run/resolver/`. But `/accountz` did not list it. **The push report is not evidence.**
- Terminal B received exactly one message: `order-svc-order`.
- No user file is in `.run/resolver/`. The server stores accounts, not users.
