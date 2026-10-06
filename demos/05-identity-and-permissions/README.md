# Demo 05 — Identity and Permissions

**One question:**

> Can we admit a client while limiting exactly what it may publish and
> subscribe to?

Two ideas, kept apart the whole way through:

- **Authentication — who are you?** The server admits the connection, or
  refuses it.
- **Authorization — what may you do?** The server allows a publish or a
  subscribe, or denies it.

**Role: showcase.** You run it in a terminal and watch each rule work, and
fail, on purpose. Later it may become a guided page in the lab shell. The
exercises stay the same; only the front end changes.

**The scenario, used in every exercise:** an order service (`order-svc`)
publishes order events on `orders.*`. An analytics service
(`analytics-reader`) reads them.

**Sources:** [Authentication basics](https://docs.nats.io/learn/security/authentication-basics)
and [Authorization](https://docs.nats.io/learn/security/authorization) from
the NATS docs. The rest of the [security section](https://docs.nats.io/learn/security/)
— accounts, JWTs, auth callout, TLS set-up — belongs to later demos.

## Requirements

| ID | Requirement | Exercise | State |
|---|---|---|---|
| `D05-R1` | With no auth configured, any local client can publish and subscribe. | 01a | **measured** |
| `D05-R2` | A user with the correct password is admitted, and its messages are delivered. | 01b | **measured** |
| `D05-R3` | A wrong password, an unknown user and no credentials are all refused, and the client cannot tell which of the three it was. | 01b | **measured** |
| `D05-R4` | A refused client's messages never reach a subscriber. | 01b | **measured** |
| `D05-R5` | Each user can be limited separately on publish and on subscribe. | 02 | **measured** (by hand, then `ex02-check.sh`, 3 runs) |
| `D05-R6` | Allow, deny, wildcards, default permissions and empty lists behave as the docs say. | 03 | in progress (03a, 03b measured by hand) |
| `D05-R7` | Request / reply works under limits: the requester's inbox and the responder's reply. | 04 | planned |
| `D05-R8` | A shared token and an NKey are compared against the password approach. | 05 | planned |
| `D05-R9` | bcrypt protects the stored password; TLS protects it on the wire; one does not replace the other. | 06 | planned |
| `D05-R10` | Another person runs this README with no help and sees every exercise work. | all | not started |
| `D05-R11` | A pattern cards deck closes the demo (playbook stage 04). | — | not started |

**measured** = run, and recorded in [`exercises/EXERCISE_OBSERVATIONS.md`](exercises/EXERCISE_OBSERVATIONS.md)
with the date and tool versions. **planned** = not built, not verified. An
"expected" line below is a **prediction** until its observation is recorded.

## What you need

- `nats-server` and the `nats` CLI on the host. Measured with
  `nats-server v2.14.6` and `nats` CLI `0.4.0`.
- `openssl`, `curl`, `lsof` (all on macOS already).
- Nothing else. No Docker.

## Ports and names

| Thing | Value |
|---|---|
| NATS client | `127.0.0.1:4522` |
| NATS monitor | `127.0.0.1:8522` |
| Server name | `d05-server` |
| Users | `order-svc`, `analytics-reader` |
| Secrets, logs, PID file | `.run/` (not in Git) |

Both ports bind to **loopback only**. Exercise 01a has no authentication, so
nothing off this machine may reach it.

> **Always pass `--no-context` to `nats` in this demo.** Without it, the CLI
> adds the credentials of whatever context you have selected — maybe another
> demo's — and you test the wrong user.

## The exercises

| # | Question | State |
|---|---|---|
| 01 | Who gets in? Open server, then username / password (right, wrong, missing). | **built, measured** |
| 02 | Can each user publish and subscribe only where it should? | **built, measured** (by hand, then a check script) |
| 03 | How do allow, deny, wildcards, defaults and empty lists combine? | planned |
| 04 | What does request / reply need: inbox subscribe, and `allow_responses`? | planned |
| 05 | How do a shared token and an NKey compare with a password? | planned |
| 06 | What does bcrypt protect, and what does it leave to TLS? | planned |

## Exercise 01 — who gets in?

Open **three terminals**. In each one, go to this folder first:

```bash
cd demos/05-identity-and-permissions
```

- **Terminal A** — the server. You watch its log here.
- **Terminal B** — `analytics-reader`, the subscriber.
- **Terminal C** — `order-svc`, the publisher.

### 01a — no authentication (the baseline)

**Question:** with no auth block, who can connect?
**Theory:** a server with no `authorization` block admits every client.
**Prediction:** any client connects, publishes and receives. No credentials
are asked for.

Terminal A:

```bash
nats-server -c exercises/config/ex01-nats-no-auth.conf
```

Terminal B:

```bash
nats --no-context -s nats://127.0.0.1:4522 sub 'orders.>'
```

Terminal C:

```bash
nats --no-context -s nats://127.0.0.1:4522 pub orders.created '{"order":"ord-001"}'
```

**Look at:** Terminal B prints `Received on "orders.created"`. Nobody gave a
name or a password. That is the problem the rest of the demo fixes.

**Reset:** Ctrl-C in Terminal B, then Ctrl-C in Terminal A.

### 01b — username and password

**Question:** does the server now refuse clients that cannot prove who they
are, and still admit the ones that can?
**Theory:** each entry in `authorization { users: [...] }` is one identity.
Authentication only — both users may still do anything once they are in.
**Prediction:** the right password gets in. A wrong password, an unknown user
and no credentials all fail with the same client error.

Make the passwords once. They go to `.run/secrets.env`, never into a config
file or Git:

```bash
lab/secrets.sh
```

Load them **in all three terminals**. The server reads them from its
environment, and refuses to start without them:

```bash
source .run/secrets.env
```

Terminal A — look at [`exercises/config/ex01-nats-users-auth.conf`](exercises/config/ex01-nats-users-auth.conf)
first, then:

```bash
nats-server -c exercises/config/ex01-nats-users-auth.conf
```

Look for the warning `Plaintext passwords detected, use nkeys or bcrypt`.
Exercises 05 and 06 answer it.

Terminal B — the **positive control**. It stays connected all the time, so
anything that slips through will show up here:

```bash
nats --no-context -s nats://127.0.0.1:4522 --user analytics-reader --password "$D05_ANALYTICS_READER_PASSWORD" sub 'orders.>'
```

Terminal C — the three failures first:

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password not-the-password pub orders.created 'wrong-password'
```

```bash
nats --no-context -s nats://127.0.0.1:4522 pub orders.created 'no-credentials'
```

```bash
nats --no-context -s nats://127.0.0.1:4522 --user mallory --password whatever pub orders.created 'unknown-user'
```

Then the good one:

```bash
nats --no-context -s nats://127.0.0.1:4522 --user order-svc --password "$D05_ORDER_SVC_PASSWORD" pub orders.created '{"order":"ord-002"}'
```

Who is connected, and as whom:

```bash
curl -s 'http://127.0.0.1:8522/connz?auth=1'
```

**Look at three places:**

| Where | What you see |
|---|---|
| Terminal C (the client) | `nats: error: nats: Authorization Violation` three times, the **same** text each time. Then `Published ... to "orders.created"`. |
| Terminal A (the server) | Three `authentication error` lines. Two name the user (`order-svc`, `mallory`). The no-credentials one names nobody. |
| Terminal B (delivery) | Only `ord-002` arrives. None of the three refused messages do. |
| `/connz` | The subscriber shows `"authorized_user": "analytics-reader"`. |

**Why:** the server checks the name and password when the connection opens.
If they do not match, it closes the connection before any message is read.
It tells the client only "Authorization Violation", so an attacker cannot
learn whether a user name exists. The operator, reading the server log, can.

**Reset:** Ctrl-C in Terminal B, then in Terminal A.

### Run exercise 01 as a script

The same steps, with a PASS / FAIL line per check, and nothing left running:

```bash
exercises/ex01-check.sh
```

To run a server in the background instead of in Terminal A:

```bash
lab/up.sh ex01-nats-users-auth
```

```bash
tail -f .run/server.log
```

```bash
lab/down.sh
```

`lab/down.sh --clean` also deletes `.run/`, including the passwords.

## Exercise 02

Done by hand, in the terminal. The steps are in
[`exercises/EXERCISE-02-TERMINAL-STEPS.md`](exercises/EXERCISE-02-TERMINAL-STEPS.md).
The measured result is in
[`exercises/EXERCISE_OBSERVATIONS.md`](exercises/EXERCISE_OBSERVATIONS.md). The
finding: a side you do not list stays open, so list both sides.

The same steps, with a PASS / FAIL line per check, and nothing left running:

```bash
exercises/ex02-check.sh
```

## Exercise 03

**In progress.** The steps are in
[`exercises/EXERCISE-03-TERMINAL-STEPS.md`](exercises/EXERCISE-03-TERMINAL-STEPS.md).
Parts 03a and 03b are measured by hand. Parts 03c and 03d are not run yet.
The check script is written but **not yet run**. Its `prediction:` checks are
docs claims until the hand run confirms them:

```bash
exercises/ex03-check.sh
```

## Exercises 04 – 06

**Planned — not implemented, not verified.** The outline is in
[`docs/THEORY.md`](docs/THEORY.md). Each will follow the same shape as 01:
question, theory, prediction, commands per terminal, a positive control, a
deliberate denial, what to look at, why, and reset.
