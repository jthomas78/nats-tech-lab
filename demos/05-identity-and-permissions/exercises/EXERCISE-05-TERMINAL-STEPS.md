# Exercise 05 — a token and an NKey, against the password. Step by step

> **Status: drafted and syntax-checked, but runtime-unverified.** The four
> configs were checked with `nats-server -t`: the two working ones pass, and
> the two "meant to be rejected" ones are rejected (see each config's comment).
> No server has run them. The check script `ex05-check.sh` (32 checks) is written
> but **not yet run**. Every "Predict" line is a prediction, not a result. Nothing
> here is measured until it is recorded in
> [`EXERCISE_OBSERVATIONS.md`](EXERCISE_OBSERVATIONS.md).

Exercise 01 used a user name and a password. Exercise 05 tries two other ways
to prove who a client is (**authentication**), and asks what each one costs
for **authorization** (what the client may then do).

| Part | Question | Configs |
|---|---|---|
| 05a | One shared token: who is connected, and what may they do? | `ex05-nats-token.conf`, `ex05-nats-token-and-users.conf` |
| 05b | An NKey: what does the server store, and do per-user limits still work? | `ex05-nats-nkey.conf`, `ex05-nats-nkey-with-password.conf` |

- A **token** is one secret string. Every client sends the same one.
- An **NKey** is a key pair. The private half is the **seed** (it starts
  `SU`). The public half starts `U`. The server keeps only the public half. At
  connect, the server sends a one-time random value (a **nonce**). The client
  signs it with the seed, and the server checks the signature.

Three terminals, all in `demos/05-identity-and-permissions`:

- **A** = the server
- **B** = the listener
- **C** = the sender

Every `nats` command passes `--no-context`.

---

## Step 1: Part 05a — one shared token

### Step 1.1: Make sure there is a token

**Concept:** `lab/secrets.sh` now writes `D05_TOKEN` as well. An older `.run/secrets.env` does not have it. Run without `--rotate`, the script adds only the missing variables. It keeps every password that is already there.
**Terminal A:**

```bash
lab/secrets.sh
```

**Predict:** it prints `added    D05_TOKEN …` (an older file), `kept     …` (the token is already there) or `wrote    …` (no file yet).

**Terminals A, B and C:**

```bash
source .run/secrets.env
```

**Why all three:** a terminal holds the values it sourced last. After the script adds or changes anything, every open terminal needs this line again.

**Restart rule:** adding the token does not change a password, so a running exercise 01–04 server can keep running. Only `lab/secrets.sh --rotate` changes passwords. After `--rotate`, stop any running demo 05 server (Ctrl-C, or `lab/down.sh`) and start it again from a terminal that has re-sourced the file. Do not use `--rotate` for this exercise.

### Step 1.2: Can a token sit next to named users?

**Concept:** if a token could be mixed with named users, the users could still have their own limits. `ex05-nats-token-and-users.conf` tries it.
**Terminal A:**

```bash
nats-server -t -c exercises/config/ex05-nats-token-and-users.conf
```

**Purpose:** check the config without starting a server.
**Predict:** valid or rejected? If rejected, what does it say?

### Step 1.3: Start the token server

**Terminal A:**

```bash
nats-server -c exercises/config/ex05-nats-token.conf
```

**Purpose:** start 05a. The whole `authorization` block is one line: `token: $D05_TOKEN`.
**Predict:** does it start? Does it warn about a plaintext secret, as exercise 01 did for passwords?

### Step 1.4: A listener with the token

**Terminal B:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --token "$D05_TOKEN" sub '>'
```

**Purpose:** the control listener. It may listen to everything, because nothing limits a token holder.

### Step 1.5: The positive control

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --token "$D05_TOKEN" pub orders.created 'with-token'
```

**Predict:** does B show `with-token`?

### Step 1.6: Outside `orders`

**Concept:** in exercise 02, `order-svc` could not publish `invoices.created`. With a token there is no `order-svc`. There is only "someone with the token".
**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --token "$D05_TOKEN" pub invoices.created 'anything-goes'
```

**Predict:** allowed or denied? Does B show it?

### Step 1.7: A wrong token, then no token

**Terminal C, one after the other:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --token wrong pub orders.created 'bad-token'
```

```bash
nats --no-context -s nats://127.0.0.1:4522 pub orders.created 'no-token'
```

**Purpose:** the deliberate refusals.
**Predict:** what does C say each time? Is it the same text as a wrong password in exercise 01? What does A log?

### Step 1.8: Who is connected?

**Concept:** in exercise 01, the server named the user in `/connz`. B is still connected.
**Terminal C:**

```bash
curl -s 'http://127.0.0.1:8522/connz?auth=1'
```

**Purpose:** see what the server knows about a token client.
**First check:** the list must hold B's connection (subscription `>`). An empty `connections` list proves nothing; start B again and re-read.
**Predict:** is there an `authorized_user` field? If yes, what is in it?

### Step 1.9: Reset

**Terminals B, then A:** press Ctrl-C.

---

## Step 2: Part 05b — NKeys

### Step 2.1: Make the keys

**Terminal A:**

```bash
lab/nkeys.sh
```

```bash
ls -l .run/nkeys
```

```bash
cat .run/nkeys.env
```

**Purpose:** make three key pairs: `order-svc`, `analytics-reader` and `stranger`. `stranger` is a real key that no config lists. The seeds are in `.run/nkeys/`. The public keys are in `.run/nkeys.env`.
**Predict:** what mode are the seed files? Does `nkeys.env` hold anything that starts `SU`?

**Terminals A, B and C:**

```bash
source .run/nkeys.env
```

### Step 2.2: Can an NKey user also have a password?

**Terminal A:**

```bash
nats-server -t -c exercises/config/ex05-nats-nkey-with-password.conf
```

**Predict:** valid or rejected? If rejected, what does it say?

### Step 2.3: Start the NKey server

**Concept:** same permissions as exercise 02b. `order-svc` may publish `orders.>` and nothing else. `analytics-reader` may subscribe `orders.>` and nothing else. But the users have no names now, only public keys.
**Terminal A:**

```bash
nats-server -c exercises/config/ex05-nats-nkey.conf
```

**Predict:** does it start? Does the plaintext warning appear?

### Step 2.4: The reader listens, with its seed

**Terminal B:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --nkey .run/nkeys/analytics-reader.nk sub 'orders.>'
```

### Step 2.5: The positive control

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --nkey .run/nkeys/order-svc.nk pub orders.created 'with-nkey'
```

**Predict:** does B show `with-nkey`?

### Step 2.6: Per-user limits still work

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --nkey .run/nkeys/analytics-reader.nk pub orders.created 'reader-publishes'
```

**Purpose:** compare with Step 1.6. A token could not tell users apart. An NKey can.
**Predict:** allowed or denied? Does B show it?

### Step 2.7: A key nobody listed, then no key

**Terminal C, one after the other:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --nkey .run/nkeys/stranger.nk pub orders.created 'stranger'
```

```bash
nats --no-context -s nats://127.0.0.1:4522 pub orders.created 'no-key'
```

**Predict:** what does C say? Is it the same text as Step 1.7?

### Step 2.8: Who is connected?

**Terminal C:**

```bash
curl -s 'http://127.0.0.1:8522/connz?auth=1'
```

**First check:** the list must hold B's connection. An empty list proves nothing.
**Predict:** what is in `authorized_user` for B? Compare it with `D05_ANALYTICS_READER_NKEY` in `.run/nkeys.env`.

### Step 2.9: Read the evidence, then reset

**Look at:**

- **A:** the two `authentication error` lines from Step 2.7, the one `Publish Violation` from Step 2.6, and no seed anywhere.
- **B:** `with-nkey` only.
- **Steps 1.6 and 2.6 side by side:** a token cannot limit one client and not another. An NKey user can.

**Terminals B, then A:** press Ctrl-C.

---

## Not covered here

- "No secret crosses the wire" is a docs claim. Proving it needs a packet
  capture, and this demo does not do one.
- The token, like a password, is plaintext in `.run/secrets.env` and is sent
  to the server as it is. Exercise 06 (bcrypt) is about the stored copy. TLS,
  for the wire, is a later demo.
