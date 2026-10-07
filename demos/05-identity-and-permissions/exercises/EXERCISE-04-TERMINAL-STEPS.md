# Exercise 04 — does request / reply work under limits? Step by step

> **Status: drafted and syntax-checked, but runtime-unverified.** The four
> configs pass `nats-server -t`. No server has run them. The check script
> `ex04-check.sh` (36 checks, rewritten 2026-10-07) is written but **not yet run**. Every "Predict" line is a
> prediction from the NATS docs (and, where marked, the server source), not a
> result. Nothing here is measured until it is recorded in
> [`EXERCISE_OBSERVATIONS.md`](EXERCISE_OBSERVATIONS.md).

Exercises 02 and 03 limited one-way messages. Exercise 04 limits a **request
and its reply**. A request needs two permissions on each side, so it fails in
more ways.

The scenario: `analytics-reader` asks `order-svc` for an order summary on
`orders.summary`.

How a request works:

1. The requester first **subscribes** to a private inbox, `_INBOX.<random>`.
2. It **publishes** the request to `orders.summary`. The request carries the
   inbox name as its reply subject.
3. The responder receives the request. It **publishes** the reply to that
   inbox.

So the requester needs *subscribe* on its inbox, and the responder needs
*publish* on the requester's inbox.

| Part | Question | Configs | THEORY.md |
| --- | --- | --- | --- |
| 04a | What happens when the requester may not subscribe to its inbox? | `ex04-nats-inbox-allowed.conf`, then `ex04-nats-inbox-denied.conf` | Exercise 04, bullet 1 |
| 04b | Can the responder reply without a broad `_INBOX.>` publish grant? | `ex04-nats-no-responses.conf`, then `ex04-nats-allow-responses.conf` | Exercise 04, bullet 2 |

Three terminals, all in `demos/05-identity-and-permissions`:

- **A** = the server
- **B** = the responder (`order-svc`), or a listener
- **C** = the requester (`analytics-reader`), or a sender

Every `nats` command passes `--no-context`. A request waits 5 seconds by
default. Here, `--timeout 2s` keeps it short.

**A timeout alone proves nothing.** It looks the same as "nobody is
listening". So each part shows the request **working first**, on the same
users, before it shows the request failing.

---

## Step 1: Part 04a — the requester's inbox

### Step 1.1: Load the passwords

**Terminals A, B and C:**

```bash
source .run/secrets.env
```

**Purpose:** put the passwords in each terminal. If `.run/secrets.env` is missing, run `lab/secrets.sh` first.

### Step 1.2: Start the server, with the inbox allowed

**Concept:** `analytics-reader` may publish `orders.summary` and subscribe `_INBOX.>`. `order-svc` may subscribe `orders.summary` and publish `_INBOX.>`.
**Terminal A:**

```bash
nats-server -c exercises/config/ex04-nats-inbox-allowed.conf
```

**Purpose:** start the 04a server, the working shape.
**Predict:** does it start?

### Step 1.3: The responder

**Terminal B:**

```bash
mkdir -p .run/ex04
```

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" reply orders.summary --command "$PWD/exercises/ex04-responder-hook.sh"
```

**Purpose:** `order-svc` answers every request on `orders.summary`. It stays open until Step 1.8.
**Concept:** the reply comes from a small hook, `exercises/ex04-responder-hook.sh`. For each request, it first writes one line to `.run/ex04/receipts.log` (the subject, a tab, the body), then prints `summary: 3 orders` as the reply. So the responder itself records what it received, even when its reply is later denied. `ex04-check.sh` uses the same command in every case.
**Predict:** does it connect? Does it print an error?

**Any free terminal, any time:**

```bash
cat .run/ex04/receipts.log
```

**Purpose:** see what reached the responder. One line per request.

### Step 1.4: The request that should work

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" --timeout 2s request orders.summary 'how many?'
```

**Purpose:** the positive control. Both sides have what they need.
**Predict:** does C print `summary: 3 orders`? Does `.run/ex04/receipts.log` gain one line for it?

### Step 1.5: The broad grant has a cost

**Concept:** `order-svc` may publish to **any** `_INBOX.…` subject. Nothing ties a reply to a real request. So it can write into any requester's inbox, at any time.
**Terminal C, first a listener:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" sub '_INBOX.>'
```

**Then a fourth terminal (D), with the passwords loaded:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" pub _INBOX.forged 'not a reply'
```

**Purpose:** show that `order-svc` can send a "reply" that nobody asked for.
**Predict:** does C show `not a reply`? Does A log anything?
**Then:** stop the listener in C with Ctrl-C.

### Step 1.6: Restart the server with the inbox denied

**Concept:** only one thing changes. `analytics-reader` may now subscribe to nothing.
**Terminal B:** press Ctrl-C. **Terminal A:** press Ctrl-C, then:

```bash
nats-server -c exercises/config/ex04-nats-inbox-denied.conf
```

**Terminal B:** start the responder again, as in Step 1.3. Run `: > .run/ex04/receipts.log` first, so the file holds only this part.

**Purpose:** the same users and the same request. Only the inbox is denied.

### Step 1.7: The same request, with no inbox

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" --timeout 2s request orders.summary 'how many?'
```

**Purpose:** the deliberate denial.
**Predict:** a reply, an error, or a timeout? Does `.run/ex04/receipts.log` still gain a line? Does A log a `Subscription Violation`, and for which subject?

### Step 1.8: Read the evidence, then reset

**Look at:**

- **C:** Step 1.4 (the reply) next to Step 1.7 (what came instead). Does C say *why* it failed, or only that it timed out? Note the exit code (`echo $?` straight after) and whether each line came on stdout or stderr: run the request again with `2>/dev/null` to see only stdout.
- **`.run/ex04/receipts.log`:** did the request of Step 1.7 arrive? If yes, the request got through and only the reply was lost.
- **A:** the violation for Step 1.7, and nothing for Step 1.5.

**Terminals B, then A:** press Ctrl-C.

---

## Step 2: Part 04b — the responder's reply

### Step 2.1: Start the server, with no publish for the responder

**Concept:** `analytics-reader` has its inbox back. `order-svc` now has `publish: { deny: ">" }`. It cannot send anything, the reply included.
**Terminal A:**

```bash
nats-server -c exercises/config/ex04-nats-no-responses.conf
```

**Terminal B:** run `: > .run/ex04/receipts.log`, then start the responder, as in Step 1.3.

**Purpose:** start 04b without `allow_responses`.

### Step 2.2: A request the responder cannot answer

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" --timeout 2s request orders.summary 'how many?'
```

**Purpose:** the reply is denied this time, not the inbox.
**Predict:** what does C show? What does B show? Does A log a `Publish Violation` for an `_INBOX.…` subject?

### Step 2.3: Restart with `allow_responses`

**Concept:** the same config, plus `allow_responses: true` on `order-svc`. The docs say: it may now publish **one** reply to the reply subject of each request it received. Nothing else.
**Terminal B:** press Ctrl-C. **Terminal A:** press Ctrl-C, then:

```bash
nats-server -c exercises/config/ex04-nats-allow-responses.conf
```

**Terminal B:** run `: > .run/ex04/receipts.log`, then start the responder, as in Step 1.3.

### Step 2.4: The same request, with `allow_responses`

**Terminal C:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" --timeout 2s request orders.summary 'how many?'
```

**Purpose:** the positive control for 04b.
**Predict:** does C get `summary: 3 orders`? `order-svc` still has `deny: ">"`. Which wins? (The server source says the reply check runs after deny. Is that what happens?)

### Step 2.5: The forged reply again

**Terminal C, a listener:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" sub '_INBOX.>'
```

**Terminal D:**

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" pub _INBOX.forged 'not a reply'
```

**Purpose:** compare with Step 1.5. In 1.5 the broad grant let this through.
**Predict:** allowed or denied? Does C show `not a reply`?

### Step 2.6: Read the evidence, then reset

**Look at:**

- **C:** Step 2.2 (no reply) next to Step 2.4 (the reply).
- **D:** the error of Step 2.5, next to the silence of Step 1.5.
- **A:** the `Publish Violation` lines for Steps 2.2 and 2.5, and none for Step 2.4.

**Terminals C, B, then A:** press Ctrl-C.

---

## Not covered here

- `allow_responses` also takes `max` (replies per request) and `expires`
  (how long the reply permission lasts). The defaults are 1 and 2 minutes
  (server source, v2.14.6). The `nats reply` CLI sends one reply per request,
  so it cannot test `max`. A small client program could. This demo has none.
- `analytics-reader` may subscribe to all of `_INBOX.>`, so it can also see
  replies meant for other requesters. A per-user inbox prefix
  (`--inbox-prefix`) narrows this. It is not tested here.
