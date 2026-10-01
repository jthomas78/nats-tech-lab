# Exercise 01 — who gets in? Step by step

Three terminals, all in `demos/05-identity-and-permissions`:

- **A** = the server
- **B** = the listener (`analytics-reader`)
- **C** = the sender (`order-svc`)

Steps 1–4 are part 01a (no authentication). Steps 5–15 are part 01b (username
and password). Every `nats` command passes `--no-context`, so the CLI cannot
pick up another demo's saved settings.

A "Predict" line is a prediction until you have run the step. Measured results
are in [`EXERCISE_OBSERVATIONS.md`](EXERCISE_OBSERVATIONS.md).

---

## Step 1: Start a server with no locks

**Concept:** the config has no `authorization` block, so the server admits everyone.
**Terminal A:**

```bash
nats-server -c exercises/config/ex01-nats-no-auth.conf
```

**Purpose:** start the open server on `127.0.0.1:4522`.
**Predict:** which ports? Any security warning?

## Step 2: A listener with no name and no password

**Concept:** a subscriber asks for every message on a subject. This server does not ask who it is.
**Terminal B:**

```bash
nats --no-context -s nats://127.0.0.1:4522 sub 'orders.>'
```

**Purpose:** listen to every subject that starts with `orders.`.
**Predict:** does it connect? Does it wait, or return to the prompt?

## Step 3: A sender with no name and no password

**Concept:** a publisher sends one message to a subject. Every matching listener gets it.
**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 pub orders.created '{"order":"ord-001"}'
```

**Purpose:** send one order.
**Predict:** does C succeed? What does B show?

## Step 4: Reset

**Concept:** always leave a clean machine.
**Terminals B, then A:** press Ctrl-C.
**Purpose:** stop the listener, then the server.
**Predict:** will the log say anything on exit?

## Step 5: Make the passwords

**Concept:** passwords never go in a config file or Git. A script makes random ones.
**Any terminal:**

```bash
lab/secrets.sh
```

**Purpose:** write `.run/secrets.env`, readable by you only.
**Predict:** what will the script print?

## Step 6: Load the passwords

**Concept:** the server reads passwords from its environment. It refuses to start without them.
**Terminals A, B and C (all three):**

```bash
source .run/secrets.env
```

**Purpose:** put the two passwords in each terminal's environment.
**Predict:** will it print anything?

## Step 7: Start a server that checks who you are

**Concept:** the config now has a user list. This is authentication only.
**Terminal A:**

```bash
nats-server -c exercises/config/ex01-nats-users-auth.conf
```

**Purpose:** start the locked server.
**Predict:** what differs in the log from Step 1?

## Step 8: A listener that proves who it is

**Concept:** a control. It stays connected, so anything that slips through shows up here.
**Terminal B:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" sub 'orders.>'
```

**Purpose:** connect as `analytics-reader` and listen.
**Predict:** does it connect?

## Step 9: Wrong password

**Concept:** the server checks the name and password when the connection opens.
**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password not-the-password pub orders.created 'wrong-password'
```

**Purpose:** try a known user with a wrong password.
**Predict:** what error? Does B show the message?

## Step 10: No credentials

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 pub orders.created 'no-credentials'
```

**Purpose:** try with nothing.
**Predict:** same error as Step 9, or different?

## Step 11: Unknown user

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user mallory --password whatever pub orders.created 'unknown-user'
```

**Purpose:** try a user the server has never heard of.
**Predict:** same error again? Can the client tell the three failures apart?

## Step 12: The right password

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" pub orders.created '{"order":"ord-002"}'
```

**Purpose:** the positive control. One good order.
**Predict:** does C succeed? Which messages has B received so far?

## Step 13: See who is connected

**Terminal C:**

```bash
curl -s 'http://127.0.0.1:8522/connz?auth=1'
```

**Purpose:** ask the server's monitor page for its connections.
**Predict:** which user names will it list?

## Step 14: Read the evidence in four places

**No command. Look at:**

- **C:** three identical errors, then one success.
- **A's log:** three `authentication error` lines. Which name the user?
- **B:** which messages arrived?
- **Step 13 output:** who is `authorized_user`?

**Concept:** the client report is not the same as what the server did. Check both.

## Step 15: Reset

**Terminals B, then A:** press Ctrl-C.
**Optional afterwards:** `exercises/ex01-check.sh` does all of this in one go, with a PASS or FAIL for each check.

---

**The lesson:** exercise 01 is **authentication only**. It answers "who are you?".
Once in, both users can still do anything. Limiting what they can do is
**authorization**, which is exercise 02.
