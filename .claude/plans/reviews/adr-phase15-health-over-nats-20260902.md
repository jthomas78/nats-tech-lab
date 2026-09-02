# ADR-TBD: Frontend health over NATS, and a catalogue-reset notice (Phase 15)

**Status:** Proposed — design gate passed 2026-09-02, this review runs before 15a
**Date:** 2026-09-02
**Deciders:** Jeremy (app-shell owner)
**Reviews:** Phase 15 decisions 1–13, `Application-Shell-Microfrontend-Plan.md`

## Context

Phase 14 merged the announcer sidecar into the plugin's own container. The only
reason a plugin container still joins the `frontend` Docker network is that the
registry probes it with `GET /healthz` over that network — the browser arrives on
a published host port, not over `frontend`. That probe also needs
`REGISTRY_HEALTH_ORIGINS`, a hand-maintained per-plugin map, which is exactly the
class of deployment chore the 14d scaffolder exists to stop generating.

Separately: a plugin announces once, at start-up. If the registry loses its
catalogue while plugins keep running, nothing re-announces.

Phase 15 moves the health transport to NATS request/reply and adds a
catalogue-reset notice. Decisions 1–13 are approved.

## Decision under review

Approved as: the registry asks over `rpc._platform.health.frontend.{pluginID}.ready.v1`;
the publisher answers only after a real local `GET http://127.0.0.1:<port>/healthz`
(1s inner deadline inside a 2s outer); the subject is derived from the plugin id
in the signed entry with a one-token grant, replacing `REGISTRY_HEALTH_ORIGINS`;
the plugin drops `frontend`; a reset is stated as a fact on
`notify._platform.mfe-registry.entries.reset` over core NATS with a carried
jitter window; silence never withdraws; and a content-identical resync advances
the release watermark without a revision or audit row.

**The design holds together.** The findings below are gaps and unstated costs,
not a case for reversing it. Three are blocking.

## Findings

### P1 — `demo-catalog` loses frontend health entirely, and nothing in the plan notices

`docker-compose.yml:455` maps `http://localhost:7112` → `http://demo-catalog-frontend:80`.
It is probed today and reports real health. But `demo-catalog` is **curated**
(`mfe.source: preload`) — it has no announcer, no publisher process, and no NATS
credential, deliberately, and Phase 14 was careful to leave it alone.

After decision 4, the probe subject is derived from the plugin id and answered by
the plugin's publisher. `demo-catalog` has no publisher. **Nobody can answer.**
A plugin that is healthy today becomes permanently unanswerable.

This is the one finding that changes user-visible behaviour in the lab, and it is
not mentioned anywhere in Phase 15.

Two ways out:

- **Declare curated entries not-applicable for frontend health.** There is
  precedent — BR-AS62 already has an explicit not-applicable state, and under
  decision 2's path-prefixed production shape a curated plugin is served from the
  shell's own origin, so its frontend health genuinely *is* the shell's health.
  **Recommended**, on the condition that it becomes a stated, specced state and
  not an accident of having no subscriber.
- **Keep a narrow HTTP path for curated entries only.** Preserves the signal,
  but keeps `REGISTRY_HEALTH_ORIGINS` alive for one entry, keeps the registry on
  `frontend`, and leaves two probe mechanisms where the phase set out to have one.

### P1 — the registry's existing NATS grant does not match the new subject

`bootstrap-operator.sh:514` grants the registry publish on
`rpc._platform.health.*.ready.v1` — **one** wildcard token. Decision 3's subject
is `rpc._platform.health.frontend.{pluginID}.ready.v1` — **two** tokens after
`health`. The existing grant will not match it, and the failure mode is a silent
permissions denial at runtime, not a build error.

Recommend adding `rpc._platform.health.frontend.*.ready.v1` as a **separate**
grant entry rather than widening the existing one to `health.*.*.ready.v1` —
widening would also admit any future two-token service subject, and the whole
point of decision 3's `frontend` discriminator is to keep the two namespaces
from touching.

### P1 — "a plugin speaks, it does not listen" is being retired, and it is written down as a security property

`bootstrap-operator.sh:540` states it plainly: *"There is no subscribe grant
beyond that inbox: a plugin speaks, it does not listen, and it has no business
reading `api._platform.>` or anyone else's `rpc`."*

Phase 15 knowingly inverts this — the plan says so, and calls it one decision
rather than two. That is the right call. But the invariant is currently asserted
in a comment beside the grant that enforces it, and after 15b that comment is
false. It must be rewritten in the same change, stating the new invariant: a
plugin subscribes to **its own health token and the shared reset notice, and
nothing else** — never `>`, never another plugin's token.

Left alone, the next person to read that grant is told a security property that
no longer holds.

### P2 — the health signal now depends on the transport that carries it

Today, a plugin with a dead NATS connection but a healthy listener reads as
healthy, correctly — the browser can reach it. After Phase 15, the registry gets
no responders and the plugin reads as unavailable, while browsers are being
served perfectly well. The signal about HTTP now fails when NATS fails.

BR-AS60 keeps this from being destructive — health is decoration, it never
removes, disables or reloads content — so this is a false negative on a screen,
not an outage. But the cause vocabulary must distinguish **no-responders** from a
publisher that answered and said it was unhealthy. Those are different facts with
different fixes, and collapsing them into one word makes the common case
("someone restarted NATS") look like the rare one ("this plugin's listener is
broken").

### P2 — the jitter window is a number taken from the wire

Decision 7 carries the window in the notice so the registry can widen the spread
without redeploying plugins. That is the right shape. But a publisher that sizes
its own backoff from an untrusted number is a lever: a window of `0` turns the
notice into a synchronised stampede — precisely the outcome decision 7 exists to
prevent.

The publisher must **clamp the carried window to a locally-owned sane range**
before using it. The registry keeps the ability to widen; nobody gains the
ability to narrow it to zero. This is a small addition to 15d and should be a
spec, since it is a rule about not trusting input.

### P2 — nothing orders "subscribe" before "announce"

If a publisher announces before its health subscription is live, the registry may
probe an entry whose publisher is not listening yet and record a spurious
unavailable at every boot. The fix is trivial and belongs in the rule rather than
in implementation habit: **subscribe to the health token, then announce.**

### P2 — `example-plugin-unreachable` changes meaning, and it is a control-group fixture

It is deliberately **absent** from `REGISTRY_HEALTH_ORIGINS` today, so it reports
*not configured*. After Phase 15 it has an announcer that could subscribe, and if
it does, a self-GET against a plugin with no web server fails — so it becomes
*unhealthy* instead. Same plugin, different reported state, by accident of the
transport change.

`cmd/registry-acceptance` uses it in the step-9 control group. Decide explicitly
whether its CLI-form announcer answers the health subject at all, and pin the
resulting state in a spec, before 15h discovers it.

### P3 — the reset notice's window of usefulness is narrower than it looks

The common way the catalogue is lost in this lab is `docker compose down -v`,
which also restarts the plugins — and they announce on start-up anyway. So the
notice earns nothing in that case. It helps only when the registry's data is lost
**while the plugins keep running**: a truncated table, a restored backup, a
recreated DB volume.

The plan already calls the hole "real and small", which is honest. Worth carrying
that sentence into the rule itself, so a later reader does not mistake this for
the primary recovery path. It is the backstop; start-up announce is the primary.

### P3 — a new recurring self-GET in every plugin

Every plugin now performs a loopback HTTP request every 5 seconds, forever. It is
trivial per plugin and correct by decision 1. Stating it because it is new
steady-state work that did not exist before, and at 500 plugins it is 100
loopback requests per second across the fleet that no one has budgeted.

## Consequences

**Easier:** no per-plugin health chore; one Docker network per plugin instead of
two; health keeps working under decision 2's path-prefixed production shape,
where there is no origin for the registry to dial at all.

**Harder:** plugins now listen, which is a genuinely new posture for a plugin
credential and needs its grant reviewed per plugin rather than per fleet; a NATS
outage now shows as a wall of unavailable plugins; and curated entries need an
answer they do not currently have.

**Revisit:** whether `Accepted` advancing without a revision bump interacts with
optimistic concurrency on the entry row — decision 10's amendment is correct
about replay protection, but the write path it implies has not been looked at.

## Action items

1. [ ] Decide `demo-catalog`'s frontend health state before 15b — recommended:
       explicit not-applicable, specced, not emergent.
2. [ ] Add `rpc._platform.health.frontend.*.ready.v1` to the registry's grant as
       a separate entry, in the same change as 15b.
3. [ ] Rewrite `bootstrap-operator.sh`'s "a plugin speaks, it does not listen"
       comment to state the new, narrower invariant.
4. [ ] Split `no-responders` from an answered-unhealthy in the cause vocabulary.
5. [ ] Clamp the carried jitter window publisher-side; spec it.
6. [ ] Make subscribe-before-announce an ordering rule, not a habit.
7. [ ] Decide and pin `example-plugin-unreachable`'s health behaviour before 15h.
8. [ ] Check `Accepted`-without-revision against the entry row's concurrency
       control.

## Resolutions — walked one at a time, 2026-09-02

Every finding above was put to the app-shell owner individually with options and a
recommendation. All nine are settled. Findings 6 and 8 were amended at approval
from a Codex reading; the amendments are recorded in full because both changed
what gets tested, not just how it is worded.

1. **All plugins have frontend health — curated included.** The recommendation
   (declare curated not-applicable) was **rejected**: health is a property of
   every plugin, without exception. So `demo-catalog` gains the same
   health-answering process as any other plugin, plus a NATS credential granted
   **only its own health token** — no announce, no unregister. Curation stays a
   property of *how the entry reached the catalogue*, not of whether it can be
   asked if it is alive. The "not configured" state ceases to exist.
2. **Two narrow grants, not one wide one.** `rpc._platform.health.frontend.*.ready.v1`
   is added beside the existing `rpc._platform.health.*.ready.v1`. Never widened
   to `health.*.*.ready.v1`.
3. **A plugin subscribes to exactly two named subjects**: its own
   `rpc._platform.health.frontend.<its-own-id>.ready.v1` and the shared
   `notify._platform.mfe-registry.entries.reset`. No wildcard on the plugin side.
   `bootstrap-operator.sh`'s comment is rewritten to state this narrower rule in
   the same change.
4. **`absent` and `unhealthy` are separate causes.** No report inside the freshness window is not
   the same fact as answered-and-sick, and the UI shows them differently.
5. **The plugin clamps the carried jitter window** to a locally-owned floor and
   ceiling. The registry keeps the power to widen; nothing on the wire can narrow
   it to zero. Specced as a rule about not trusting input.
6. **Subscribe → flush/confirm → announce**, as a business rule.
   **Amended at approval, from a Codex reading.** The rule states the ordering,
   but the spec asserts the *observable* invariant, not the source order:

   > Given a plugin is starting, when its announcement becomes visible to the
   > registry, then its advertised health endpoint must already be reachable.

   The contract is **reachable before discoverable**, not "line 42 runs before
   line 48" — so the implementation can be reorganised without weakening the
   guarantee.
7. **`example-plugin-unreachable` answers, and reports `unhealthy`.** It
   subscribes like every other plugin and self-GETs against a listener that does
   not exist, which is the honest answer. Step 9 of `cmd/registry-acceptance` is
   updated to assert `unhealthy`.

   Consequence: no fixture then exercises finding 4's `absent` cause. Rather
   than add a plugin, 15h stops a running plugin's process and asserts the
   registry reports `absent` — which also exercises BR-AS54 (silence never
   withdraws) on the same step.
8. **The rule states its own scope.** **Amended at approval, from a Codex
   reading** — BR-AS73 is written as catalogue *recovery*, not as a reset
   notification:

   > Plugins must announce themselves during startup. This is the primary
   > mechanism for populating the registry catalogue.
   >
   > The registry may issue a reset notice when its catalogue must be
   > reconstructed while existing plugins remain running. On receipt, plugins
   > re-announce after the prescribed jitter interval.
   >
   > A reset notice is not required for whole-system restarts, where plugins
   > restart and perform their normal startup announcement.

   The sentence to preserve: **start-up announcement is the primary path; reset
   is the backstop for catalogue loss without plugin restart.** This also
   explains why decision 7's jitter matters — reset is an exceptional
   fleet-recovery operation, and it is exactly the case where every surviving
   plugin would otherwise re-announce at once.

   The specs follow directly from the scenario table:

   | Scenario | Expected mechanism |
   | --- | --- |
   | Plugin starts | startup announcement |
   | Registry + plugins all restart | startup announcements |
   | Registry restarts, catalogue persisted | nothing |
   | Registry loses catalogue, plugins remain alive | reset → jitter → re-announce |
   | Registry restored from stale backup | reset → jitter → re-announce |

9. **The `Accepted`-without-revision write is checked inside 15e**, the task that
   builds it — confirm a watermark-only write cannot lose a concurrent real
   announce, with a spec.

Finding 9 (a loopback GET per plugin every 5s) needed no decision. It is stated
and accepted.

## Superseded in part — decision 14, 2026-09-02 (after 15a)

Health is **pushed by the plugin**, not asked for by the registry. The subject is
`notify._platform.health.frontend.{pluginID}.v1`; the registry subscribes on
`notify._platform.health.frontend.*.v1` and does no work at rest. Heartbeat 5s,
freshness 15s — one pair, raised together or neither. What this changes above:

- **Finding 2 dissolves.** The registry no longer publishes frontend health, so
  `rpc._platform.health.*.ready.v1` stays exactly as-is, serving BR-AS62's
  backend readiness only.
- **Finding 6 restates** as "first health push, then announce" — same observable
  invariant: when an announcement reaches the registry, that plugin's health is
  already known.
- **Resolution 4's cause name is `absent`** (no report inside the freshness
  window), matching BR-AS61. "Nobody answered" no longer describes anything.
- **Resolution 7 stands** — `example-plugin-unreachable` reports `unhealthy`
  about itself, and step 9 kills a healthy plugin to exercise `absent`.
- **Finding 9's cost is unchanged in size but moves owner:** the loopback `GET`
  every 5s is now the plugin's own timer, not a response to a request.
- The **census** (registry asks everyone on start-up / reconnect / reset) is
  deferred, not rejected — the heartbeat covers every trigger, so it buys latency
  rather than correctness. Its subject is granted to plugins now, because adding
  a grant later costs a `bootstrap-operator.sh` edit and a
  `docker compose down -v` reseed.
