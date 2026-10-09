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

Every `nats` command goes through `lab/nats.sh`. It adds `--no-context` and
keeps this demo's keys in `.run/`.

---

## Initial setup

**Concept:** three terminals, and no keys left over from an earlier run.

Open three terminals. In each one, go to the demo folder:

```bash
cd demos/06-operator-trust-and-credentials
```

- **A** = the server log
- **B** = the listener
- **C** = everything else

**Terminal C:** delete `.run/`, with every old key and credential in it. It also stops a demo 06 server, if one runs.

```bash
lab/down.sh --clean
```

## Step 1: Build the chain

**Concept:** an operator `D06`, a `SYSTEM` account with user `admin`, an account `ORDERS` with user `order-svc`. All keys stay in `.run/`.
**Terminal C:**

The operator `D06`. This also makes the `SYSTEM` account:

```bash
lab/nats.sh auth operator add D06
```

The account `ORDERS`, signed by the operator:

```bash
lab/nats.sh auth account add ORDERS --defaults
```

The folder for the `.creds` files. `nats auth` does not make it, and without it the next command fails with `no such file or directory`:

```bash
mkdir -p .run/creds
```

The user `order-svc`, signed by the account. The `.creds` file holds the user JWT and its seed (secret key):

```bash
lab/nats.sh auth user add order-svc ORDERS --defaults --credential .run/creds/order-svc.creds
```

The `SYSTEM` user `admin`. It pushes accounts to the server later:

```bash
lab/nats.sh auth user add admin SYSTEM --defaults --credential .run/creds/sys.creds
```

The trust file the server loads (`.run/trust.conf`). The exercise config
`include`s it. It holds three settings, and each value comes from the store
that `nats auth` just wrote:

```text
operator: "<demo folder>/.run/xdg-data/nats/nsc/stores/D06/D06.jwt"
system_account: A…
resolver_preload {
  A…: eyJ…
}
```

- `operator` — the path to the operator JWT. `auth operator add D06` wrote
  it to `.run/xdg-data/nats/nsc/stores/D06/D06.jwt`. This is the only key
  the server trusts.
- `system_account` — the public key of the `SYSTEM` account. It is on the
  first line of `auth account info`, in the form `Account SYSTEM (A…)`:

  ```bash
  lab/nats.sh auth account info SYSTEM
  ```

- `resolver_preload` — the `SYSTEM` account JWT, under that same key. The
  file is `.run/xdg-data/nats/nsc/stores/D06/accounts/SYSTEM/SYSTEM.jwt`.
  The server knows `SYSTEM` from the start, so the `admin` user can connect
  and push other accounts. No other account is preloaded.

You can write the file by hand from those three values. The script does the
same thing and saves typing:

```bash
lab/trust-conf.sh
```

**Purpose:** make every key and JWT, and write `.run/trust.conf`. (`lab/chain.sh` runs these commands in one go. The check script uses it.)
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

**Terminal A:**

```bash
nats-server -c exercises/config/ex01-nats-operator.conf 2>&1 | tee .run/server.log
```

**Purpose:** start the server in the foreground. Its log prints in terminal A. `2>&1 | tee .run/server.log` also writes a copy to `.run/server.log`; Step 14 counts lines in it. Run it from the demo folder: the config's resolver `dir` (`.run/resolver`) is relative to it.

**Optional — the convenience script.** It starts the same server in the background, writes the log to `.run/server.log` and waits until the server is healthy. Use it instead of the command above, not as well. Then follow the log in terminal A:

```bash
lab/up.sh ex01-nats-operator
```

```bash
tail -f .run/server.log
```

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

Stop terminals A and B with `Ctrl-C`. `Ctrl-C` in terminal A stops the server.

**Optional:** if you started the server with `lab/up.sh`, `Ctrl-C` in terminal A stops only `tail`. Stop the server in **terminal C**:

```bash
lab/down.sh
```

Leave `.run/` in place for exercise 02. To delete every key: `lab/down.sh --clean`.

## What you should have seen

- Before the push, `order-svc` was refused. The log shows `fetch took ~1.9s` and `fetching jwt timed out`. `account query` failed: `did not receive a valid token from the server`.
- After the push, `account query` returned `ORDERS`. The server's copy said `Users: 0`; your local copy said `Users: 1`. The server holds accounts, not users.
- After the push, `order-svc` got in. `/connz` named it by keys, not by user name: `authorized_user` is the user's `U…` key, `account` and `issuer_key` are the `ORDERS` `A…` key, and `name_tag` is `ORDERS` (the account's name).
- No credentials, the rogue user (before and after its push), and the wrong seed were all refused.
- The rogue push printed `Success 1 Failed 0` and left a file in `.run/resolver/`. But `/accountz` did not list it. **The push report is not evidence.**
- Terminal B received exactly one message: `order-svc-order`.
- No user file is in `.run/resolver/`. The server stores accounts, not users.
