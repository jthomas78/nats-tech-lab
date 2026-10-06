# Exercise 03 — how do allow, deny, wildcards and defaults combine? Step by step

> **Status: 03a and 03b measured by hand. 03c and 03d runtime-unverified.**
> The check script `ex03-check.sh` (36 checks) is written but **not yet run**.
> It was written before the 03c and 03d hand run, so its checks marked
> `prediction:` test docs claims, not results. Every "Predict" line is a
> prediction from the NATS docs. Nothing here is measured until it is recorded
> in [`EXERCISE_OBSERVATIONS.md`](EXERCISE_OBSERVATIONS.md).

Exercise 02 closed the open side. Exercise 03 asks how the rules inside one side
combine. It is bigger than 01 and 02, so it has **four parts**. Do one part, then
stop. Each part has its own server config.

| Part | Question | Config | Claims (THEORY.md) |
| --- | --- | --- | --- |
| 03a | Does deny beat allow? What do `*` and `>` match? | `ex03-nats-allow-deny.conf` | 1, 2, 3 |
| 03b | Does an empty list mean "allow all" or "deny all"? | `ex03-nats-empty-list.conf` | 4 |
| 03c | Do defaults apply, and does a user's own block replace them? | `ex03-nats-defaults.conf` | 5 |
| 03d | What does a wildcard subscription do when it overlaps a deny? | `ex03-nats-wildcard-overlap.conf` | 6 |

Claim 1 (an `allow` list denies everything not on it) was already measured in
exercise 02 (`invoices.created` was denied). It is not repeated here.

Three terminals, all in `demos/05-identity-and-permissions`:

- **A** = the server
- **B** = the listener (`analytics-reader`)
- **C** = the sender (`order-svc`, and sometimes `analytics-reader`)

Every `nats` command passes `--no-context`. A denied subscribe prints
`Subscribing on …` first and the error after, so use `--wait 2s` and read the
whole output.

---

## Step 1: Part 03a — deny beats allow, and what `*` and `>` match

Two jobs, kept apart:

- **Steps 1.3–1.7: what `order-svc` may publish.** An observer (`audit-observer`) may subscribe to all of `orders.>`. So what arrives is the proof.
- **Steps 1.8–1.9: what `analytics-reader` may subscribe to.** Its allow is `orders.*`.

### Step 1.1: Load the passwords

**Concept:** the server reads passwords from its environment. Skip this if the terminal already has them.
**Terminals A, B and C:**

```bash
source .run/secrets.env
```

**Purpose:** put the passwords in the terminal. (`audit-observer` is new, so its password is new in this file.)
**Predict:** `echo ${#D05_AUDIT_OBSERVER_PASSWORD}` prints what length? (0 means not loaded.)

### Step 1.2: Start the server

**Concept:** `order-svc` may publish `orders.>` but not `orders.internal.>`. `audit-observer` may subscribe to `orders.>`. `analytics-reader` may subscribe to `orders.*` only.
**Terminal A:**

```bash
nats-server -c exercises/config/ex03-nats-allow-deny.conf
```

**Purpose:** start the 03a server.
**Predict:** does the log look different from exercise 02?

### Step 1.3: The observer listens on `orders.>`

**Terminal B:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user audit-observer --password "$D05_AUDIT_OBSERVER_PASSWORD" sub 'orders.>'
```

**Purpose:** B sees every `orders.…` message the server delivers. It stays open until Step 1.7 is done.
**Predict:** does it connect?

### Step 1.4: The allowed publish

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" pub orders.created 'one-token'
```

**Purpose:** the positive control.
**Predict:** does B show it?

### Step 1.5: Two tokens after `orders`

**Concept:** `>` in the allow list matches one **or more** tokens.
**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" pub orders.eu.created 'two-tokens'
```

**Purpose:** show that the publish is allowed and delivered.
**Predict:** does C get an error? Does B show it?

### Step 1.6: Bare `orders`, zero tokens after it

**Concept:** `>` needs at least **one** token. `orders` alone has none.
**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" pub orders 'zero-tokens'
```

**Purpose:** test the edge of `>`.
**Predict:** allowed or denied? Does B show it? What does A log?

### Step 1.7: A subject that matches both allow and deny

**Concept:** `orders.internal.audit` matches the allow `orders.>` and the deny `orders.internal.>`.
**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" pub orders.internal.audit 'secret'
```

**Purpose:** test which one wins. B must not show it.
**Predict:** allowed or denied? What does A log?

### Step 1.8: Reader subscribes to a one-token subject

**Concept:** the reader's allow is `orders.*`. Now we test **subscribe** permissions, not publish.
**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" sub 'orders.created' --wait 2s
```

**Purpose:** a subscription `orders.*` covers.
**Predict:** allowed or denied?

### Step 1.9: Reader subscribes to a two-token subject

**Concept:** `*` in a permission also means exactly one token.
**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" sub 'orders.eu.created' --wait 2s
```

**Purpose:** a subscription `orders.*` does not cover.
**Predict:** allowed or denied?

### Step 1.10: Read the evidence

**No command. Look at:**

- **C:** Steps 1.6, 1.7 and 1.9: the exact error words.
- **A's log:** how many `[ERR]` lines, and which subjects.
- **B:** which of `one-token`, `two-tokens`, `zero-tokens` and `secret` arrived.

### Step 1.11: Reset

**Terminals B, then A:** press Ctrl-C.

---

## Step 2: Part 03b — an empty list

### Step 2.1: Start the server

**Concept:** `order-svc` has `publish: { allow: [] }`. The reader may subscribe to everything (`>`), so B can see any message that gets through.
**Terminal A:**

```bash
nats-server -c exercises/config/ex03-nats-empty-list.conf
```

**Purpose:** start the 03b server.
**Predict:** does the server start, or does it complain about the empty list?

### Step 2.2: A listener on everything

**Terminal B:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" sub '>'
```

**Purpose:** the control listener.
**Predict:** does it connect?

### Step 2.3: Publish to a subject outside `orders.>`

**Concept:** in exercise 02 the allow `orders.>` denied `invoices.created`. Now the allow list is empty.
**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" pub invoices.created 'empty-list'
```

**Purpose:** test whether an empty list means "allow all" or "deny all".
**Predict:** allowed or denied? Does B show it?

### Step 2.4: Prove the permissions block is live

**Concept:** if Step 2.3 was allowed, we must know the block still does something. `order-svc` has `subscribe: { deny: ">" }`.
**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" sub 'orders.>' --wait 2s
```

**Purpose:** a control that must be denied.
**Predict:** denied?

### Step 2.5: Reset

**Terminals B, then A:** press Ctrl-C.

---

## Step 3: Part 03c — `default_permissions`

### Step 3.1: Start the server

**Concept:** the defaults say: subscribe `orders.>`, publish nothing. `analytics-reader` has no block, so it gets the defaults. `order-svc` has its own block with only `publish`.
**Terminal A:**

```bash
nats-server -c exercises/config/ex03-nats-defaults.conf
```

**Purpose:** start the 03c server.
**Predict:** does it start?

### Step 3.2: The reader, on the defaults

**Terminal B:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" sub 'orders.>'
```

**Purpose:** the control listener, allowed by the defaults.
**Predict:** does it connect?

### Step 3.3: The reader publishes. The defaults say no

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" pub orders.created 'from-reader'
```

**Purpose:** prove the defaults apply to a user with no block.
**Predict:** allowed or denied? Does B show it?

### Step 3.4: The sender, on its own block

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" pub orders.created 'from-order-svc'
```

**Purpose:** the positive control. The defaults say publish nothing, but this user's own block says `orders.>`.
**Predict:** does B show it?

### Step 3.5: Does the sender's block replace the defaults, or merge with them?

**Concept:** the defaults allow only `orders.>` to be subscribed. `order-svc` lists no `subscribe` at all. If its block **replaces** the defaults, that side is unlisted, so open (exercise 02). If the two **merge**, the defaults still limit it.
**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" sub 'invoices.>' --wait 2s
```

**Purpose:** tell "replace" and "merge" apart.
**Predict:** allowed (replaced) or denied (merged)? This is the surprise to watch for.

### Step 3.6: Reset

**Terminals B, then A:** press Ctrl-C.

---

## Step 4: Part 03d — a wildcard that overlaps a deny

### Step 4.1: Start the server

**Concept:** the reader may subscribe `orders.>` but not `orders.internal.>`. The sender may publish all of `orders.>`.
**Terminal A:**

```bash
nats-server -c exercises/config/ex03-nats-wildcard-overlap.conf
```

**Purpose:** start the 03d server.
**Predict:** does it start?

### Step 4.2: A wildcard subscription that overlaps the deny

**Concept:** `orders.>` includes `orders.internal.>`, which is denied.
**Terminal B:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" sub 'orders.>'
```

**Purpose:** the docs say this is accepted. Check it.
**Predict:** accepted or denied?

### Step 4.3: Publish a sibling and a denied subject

**Terminal C (two commands, one after the other):**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" pub orders.created 'sibling'
```

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" pub orders.internal.audit 'denied-subject'
```

**Purpose:** the sibling is the positive control. It proves B is live.
**Predict:** which of `sibling` and `denied-subject` shows in B?

### Step 4.4: A literal subscription to the denied subject

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" sub 'orders.internal.audit' --wait 2s
```

**Purpose:** compare with Step 4.2. Same deny, different subscription.
**Predict:** allowed or denied? Does A log anything for Step 4.2?

### Step 4.5: Read the evidence, then reset

**Look at:** B (what arrived), C (Step 4.4's error) and A (violations for Step 4.2 and Step 4.4).
**Terminals B, then A:** press Ctrl-C.

---

**Not covered here:** request and reply is exercise 04. Tokens and NKeys are exercise 05.
