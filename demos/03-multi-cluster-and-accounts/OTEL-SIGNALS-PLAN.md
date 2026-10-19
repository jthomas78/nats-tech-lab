# Exercise 12 — OpenTelemetry signals across T3 and T5 (plan)

Status: **PLAN ONLY.** Nothing here is built. No rule in this demo has
changed yet. Section 7 lists the rule changes, and they take effect only
when they are made in `CLAUDE.md` and `README.md`.

## 0. Why, and what is held still

**Question.** Can one small workload give correlated traces, logs and
metrics across NATS cluster boundaries, and do those signals stay true when a
region goes dark and comes back?

**Role.** Validation and showcase. The check script is the validation. Grafana
is the showcase. A Grafana session is never evidence, in the same way as the
playground.

**Held still.** The servers of T3 / C and T5 / D, their accounts, and the
host-process freeze (`kill -STOP`). **The variable** is the topology, and
where the two services connect.

**Why demo 03.** It already has the topologies and the freeze mechanism.
Demo 01 shows tracing in a large application. This exercise shows all three
signals on a small workload, across topologies and failures.

### Revisions inspected (local checkouts, not remote state)

| Repo | Branch | Commit | State |
|---|---|---|---|
| lab | `poc/phase54-otel` | `bcf202f0` | `shared/otelnats` has uncommitted edits (9 modified, 2 untracked files) |
| Chronicle | `feature/pipelines` | `8b7389dc` (2026-10-14) | clean; **behind `origin/feature/pipelines` by 1** at the last fetch. No fetch was made. |

Host: `nats-server` 2.14.6, `nats` 0.4.0, Go 1.27.0, Docker 29.7.2.

## 1. Requirements

IDs continue from `D03-R36`.

| ID | Requirement | Kind |
|---|---|---|
| D03-R37 | T3 / C and T5 / D each run as a **persistent** rig of host processes, started and stopped by command, with the same freeze and thaw as the lab. | rig |
| D03-R38 | A driver calls service A, and A calls service B. Each service adds internal spans, so one trace has at least **four levels** of nesting. A and B can run in different clusters. | showcase |
| D03-R39 | Trace context is injected at every send (CLIENT, PRODUCER) and extracted at every receive (SERVER, CONSUMER). W3C `traceparent` and `baggage`, in `nats.Header`. | validation |
| D03-R40 | Application location is on the **resource** (`service.name`, `service.instance.id`, `cloud.region`, `deployment.environment.name`). Broker location is on **every messaging span** (`nats.server.name`, `nats.cluster.name`, `server.address`), read when the span starts. | validation |
| D03-R41 | After a reconnect to another cluster, new spans keep the same `cloud.region` and show the new `nats.cluster.name`. | validation |
| D03-R42 | The run ID is on every application span and structured log line. It is **never** a metric label. | validation |
| D03-R43 | Every application log line written during a request carries `trace_id` and `span_id`. Grafana links a log line to its trace, and a trace to its log lines. | showcase + validation |
| D03-R44 | The services record real counters and duration histograms with bounded labels only: service, scenario, topology, outcome, broker cluster. Exemplars where the backend supports them. | validation |
| D03-R45 | NATS server metrics exist for every running server in the run window. A frozen server shows a failed scrape. | validation |
| D03-R46 | NATS server log files are ingested with `server` and `cluster` labels. They carry no run ID; they are found by time window. | validation |
| D03-R47 | A controlled error, a timeout and a "no responders" each give an ERROR span status, an error metric and an error log line. | validation |
| D03-R48 | Region freeze, recovery, and requests after recovery give the topology-specific outcomes in section 2. | validation |
| D03-R49 | Readiness gates before traffic. A flush before checks. Clean up leaves no `t-` process and no lab container. Checks retry up to a deadline, never `sleep`. | rig |
| D03-R50 | Exercise 12 never writes to `results.tsv`, `REPORT.md`, `REPORT.html` or the findings deck, and is not in `run-all.sh`. | safety |
| D03-R51 | The Go module builds and tests with `GOWORK=off`. Its reusable packages import no lab-specific code. | validation |
| D03-R52 | *(Optional, section 5.)* NATS server message-trace events for one T3 and one T5 request are shown inside the application trace in Tempo. | showcase |

### Exclusions

- T1, T2 / A, T2 / B, T4 / F, T5 / H, and every other shape.
- **Cross-account export and import.** T3 and T5 here use one account (`LB`).
  Nothing in this exercise is evidence for variant E.
- JetStream. Core request-reply and core publish-subscribe only. JetStream
  spans are a later question.
- Running NATS servers in Docker. Docker is for the telemetry side only.
- Grafana Cloud, AWS, and any cloud service.
- Latency numbers as findings. One laptop cannot give them (as in
  `D03-R4`). Durations are notes.
- Changes to Chronicle, architecture documents, and ADRs.

## 2. Scenario and check matrix

**Placement.** "A `za`, B `au`" means A connects to cluster `za` and B to
`au`. A **URL list** is the server list a service is given.

### Healthy

| ID | Scenario | T3 / C | T5 / D | Expected (check) |
|---|---|---|---|---|
| H1 | Request, same cluster | A, B on `za` | A, B on `za` | One trace. Spans: driver CLIENT → A SERVER → A internal → A CLIENT → B SERVER → B internal. Parent links exact. All broker spans say `za`. |
| H2 | Request, cross cluster | A `za`, B `au` (over a gateway) | A `za`, B `au` (through the hub: two leaf hops) | As H1. A spans `nats.cluster.name=za`; B spans `au`. Resource `cloud.region` matches. |
| H3 | Publish, cross cluster | A publishes, B subscribes | same | A PRODUCER is the parent of B CONSUMER. Same trace. |
| H4 | Logs | every H scenario | same | Each app log line in the window has the run ID, a `trace_id` that exists in Tempo, and a `span_id` inside that trace. |
| H5 | App metrics | every H scenario | same | Request counter increase equals the number of requests the driver sent, per scenario and outcome. The histogram count equals it too. |
| H6 | Server metrics | all servers | all servers | One series per server for the chosen exporter metrics, with `server` and `cluster` labels. |
| H7 | Server logs | all servers | all servers | At least one line per server in the window, with labels. |

### Controlled failures

| ID | Scenario | Expected (check) |
|---|---|---|
| E1 | B returns a service error | B SERVER and A CLIENT status ERROR. Error counter +1 per call. One error log line with the trace ID. |
| E2 | B is slower than A's timeout | A CLIENT status ERROR (timeout). B's late span is in the same trace. |
| E3 | B is stopped | A CLIENT status ERROR (no responders). No B span in the trace. |

### Freeze and recovery

Freeze uses the lab's own `freeze`, and `wait_dark` must prove it. Every
service sets a short client ping (decided in phase 12.1), so a lost server is
found in seconds, not minutes.

| ID | Freeze | B's URL list | T3 / C expected | T5 / D expected |
|---|---|---|---|---|
| F1 | `au` | `au` only | Cross-cluster requests fail. Requests inside `za` still succeed. | Same. |
| F2 | `au` | `au`, then a fallback (T3: `za`; T5: `hub`) | B reconnects. Requests succeed again. New B spans: `cloud.region=au`, `nats.cluster.name=za`. | Same, with `nats.cluster.name=hub`. |
| F3 | `arb` | — | **T3 only.** Cross-cluster requests keep succeeding: core traffic goes `za`↔`au` direct. | n/a |
| F4 | `hub` | — | n/a | **T5 only.** Cross-cluster requests fail. Requests inside `za` still succeed. |
| R1 | thaw, after each F | — | Requests succeed again before a deadline. | Same. |
| R2 | after F2 + thaw | — | *Note, not check:* does B stay on the fallback cluster? | Same. |

**Check or note.** A check needs an exact answer in advance. These are
**notes** until phase 12.1 shows they are stable: the kind of failure
(timeout or no responders) during a freeze, the recovery time, and the log
text a peer writes about a frozen link. They may then become checks.

## 3. Layout

### Go module (isolated)

`demos/03-multi-cluster-and-accounts/otel/`, its own `go.mod`. **Not** in
the root `go.work`; build and test with `GOWORK=off`. It does not import
`shared/otelnats` (decision 1).

```
otel/
  go.mod
  telemetry/          reusable: Setup for traces, metrics and logs (OTLP),
                      resource from env, sampler, export and shutdown
                      timeouts, ForceFlush, slog bridge with trace IDs
  natsotel/           reusable: header carrier, Inject/Extract, Start
                      Client/Server/Producer/Consumer, messaging attributes,
                      broker location, request and handler wrappers,
                      messaging metrics, baggage-to-attribute processor
  internal/lab/       lab only: run ID, scenario names, topology names,
                      placement and URL lists
  cmd/t-otel-driver/  lab only: runs one scenario, prints trace IDs
  cmd/t-otel-svc/     lab only: one binary, role A or B by flag
```

An import-graph test fails if `telemetry/` or `natsotel/` imports
`internal/lab`, or names a lab subject, port or account.

### Scripts and configs

```
lab/_rig.sh            shared up/status/freeze/thaw/down for persistent rigs
                       (new; rig-t4.sh is left as it is)
lab/rig-t3.sh          T3 / C rig: copies exercises/config/t3/*.conf
lab/rig-t5.sh          T5 / D rig: copies exercises/config/t5/*.conf
lab/telemetry.sh       up | status | down for the Docker side, with readiness
lab/12-otel-signals.sh the checks: parts step1..stepN; RESULTS=run/12-<part>.tsv;
                       evidence in lab/run/evidence/12-<stamp>/
exercises/config/t3/   static, hand-editable configs (taken once from 03-arbiter.sh output)
exercises/config/t5/   same, from 04-hub-and-leaf.sh
exercises/ex12-check.sh                  exec lab/12-otel-signals.sh "$@"
exercises/EXERCISE-12-TERMINAL-STEPS.md  Step N / Step N.M walkthrough
telemetry/compose.yaml                   alloy, otel-lgtm, prometheus-nats-exporter,
                                         nats-surveyor (spike (b) decides which stay)
telemetry/alloy/config.alloy
telemetry/grafana/dashboards/exercise-12.json
```

Why new rigs: `03-arbiter.sh` and `04-hub-and-leaf.sh` always tear down on
exit, and `lab_init` kills every `t-` server. They cannot be left running.
`rig-t4.sh` is the pattern for a rig that stays up.

Rules for every new process:
- Binary and container names start `t-otel-`.
- A PID file for every host process. Stop by verified PID only. No new
  pattern kill. The one existing pattern (`nats-server -c t-`) stays the
  only one.
- `rig-t3.sh up` and `rig-t5.sh up` refuse to start if any `t-` server runs
  or any rig port is busy (copy rig-t4's guard). T3, T4 and T5 share the
  `za` and `au` ports, so only one rig runs at a time.

### Ports

New ports stay in the demo's `203xx` family, beside the playground.

| Port | Use |
|---|---|
| 20303 | Grafana (inside `otel-lgtm`) |
| 20304 | Alloy UI |
| 20305 | Alloy OTLP gRPC (apps send here) |
| 20306 | Alloy OTLP HTTP |

Exporters, Tempo, Loki and Prometheus get no host port. The check script
queries them through Grafana's datasource API on 20303.

### Image versions

Pin exact versions in `compose.yaml`. Record image digests in each evidence
`env.txt`. Start from what Chronicle already uses: `grafana/alloy:v1.11.0`
and `natsio/prometheus-nats-exporter:0.20.2`. Pin `grafana/otel-lgtm` and
`natsio/nats-surveyor` to exact tags chosen in phase 12.1. Never `latest`.

## 4. Telemetry pipeline and Grafana

```
host: t-otel-driver, t-otel-svc (A, B) --OTLP gRPC :20305--> Alloy
host: nats-server monitor ports <--scrape-- prometheus-nats-exporter (Docker) --> Alloy
host: lab/run/log/*.log  --read-only folder mount--> Alloy (file tail)
Alloy --OTLP--> otel-lgtm (Tempo, Prometheus, Loki) --> Grafana :20303
```

- **Alloy** receives OTLP, scrapes the exporters, tails the server log files,
  adds `server` and `cluster` labels from the file name, and sends all three
  signals to `otel-lgtm`. Batch and memory limiter, as in Chronicle. Queue
  settings are recorded, not tuned (production reliability is later).
- **Log file replacement.** `lab_reset_run_dir` wipes `lab/run/log/`. Alloy
  mounts the folder, not the files, and must survive a file being replaced.
- **Run ID.** The driver makes it (`12-<stamp>`, the same as the evidence
  folder). It travels as baggage. A span processor copies it onto every span.
  The slog handler adds it to every log line. Metrics never get it.
- **Metric export interval** 5 s in the lab (the SDK default is 60 s).
  Services and the driver call `ForceFlush` and `Shutdown` before exit.

**Grafana dashboard** (`exercise-12.json`, provisioned):
1. Traces: Tempo search on `lab.run_id`, by scenario.
2. Application: request rate, error rate and duration by scenario, outcome
   and broker cluster, with exemplars.
3. NATS servers: up, connections, gateway or leaf links, per server.
4. Logs: application lines for the run ID; server lines for the window.
5. Links: trace → logs by `trace_id`; log → trace by a derived field.

**Automated check.** `12-otel-signals.sh` asks Tempo, Prometheus and Loki
(through Grafana's API) for each scenario's trace IDs and run window, and
compares the answer with section 2. It retries to a deadline. A row is a
check or a note, as in the rest of `lab/`.

## 5. Broker tracing (optional, not a prerequisite)

**What app spans cannot show.** Application spans show the two ends: A sent
on `za`, B received on `au`. They do not show the path between: which
gateway or leaf link, which hub server, whether the message was routed
twice.

**What NATS message tracing adds.** `nats-server` 2.11 and later can emit
trace events for a message: ingress, each route, gateway and leaf hop, and
egress to each subscriber. That is broker-routing evidence, independent of
the applications.

**What it is not.** The events are NATS JSON messages sent to a subject. They
are not OTLP. **Alloy does not convert them.** To see them in Tempo, a small
converter must subscribe to the trace subject, turn each hop into a span with
the application span as parent, and send it over OTLP.

**Bounded experiment (D03-R52).**
1. Find how tracing is triggered on 2.14.6: the `Nats-Trace-Dest` header, or
   an account setting that traces messages with a sampled `traceparent`.
   Record which.
2. One H2 request on T3, one on T5. Save the raw events in the evidence
   folder.
3. Write the converter as `cmd/t-otel-msgtrace` (lab only).
4. Success: Tempo shows the hop spans inside the H2 trace, and the hops match
   the topology (one gateway hop on T3; two leaf hops through `hub` on T5).

Phase 63 in `.claude/plans/Main-POC-Plan-Candidates.md` has a design for the
same events in demo 01. Read it for ideas; do not depend on it.

## 6. Phases

Each phase ends with its checks green before the next starts.

| Phase | Goal | Done when |
|---|---|---|
| 12.0 | Rule and README changes (section 7) | Approved and committed |
| 12.1 | **Spikes.** (a) Can a container reach host `127.0.0.1` monitor and client ports? (b) **Server metrics: Surveyor vs `prometheus-nats-exporter`** on T3 and T5 (section 6.1). (c) `otel-lgtm` tag, dashboard provisioning, exemplars, OTLP logs into Loki. (d) Does the module build and test on Go 1.26.4? (e) Client ping and reconnect settings for F2. (f) What peers log, and when, about a frozen link. | Each answer in `EXERCISE_OBSERVATIONS.md`; section 2's check/note split updated |
| 12.2 | Persistent rigs `rig-t3.sh`, `rig-t5.sh` | up, status, freeze, thaw, down work; port guard refuses a second rig |
| 12.3 | Telemetry side | `telemetry.sh up` is ready; server metrics and logs show in Grafana (H6, H7) |
| 12.4 | Module: `telemetry/`, `natsotel/` | Unit specs green with in-memory exporters and an embedded `nats-server`; import-graph test green; `GOWORK=off go test ./...` green |
| 12.5 | Workload, healthy scenarios | H1–H5 green on T3 and T5. **First shippable slice.** |
| 12.6 | Failures, freeze, recovery | E1–E3, F1–F4, R1 green; R2 recorded |
| 12.7 | Exercise packaging | `EXERCISE-12-TERMINAL-STEPS.md`, `ex12-check.sh`, one full cited run per topology with evidence |
| 12.8 | *Optional.* Broker message trace | Section 5 success line met, or the reason it was not |
| 12.9 | Module boundary freeze | Public API listed in `otel/README.md`; lab-only code only under `internal/` and `cmd/` |

### 6.1 Spike (b) — Surveyor vs `prometheus-nats-exporter`

**Question.** Can NATS Surveyor replace `prometheus-nats-exporter` for
server metrics? Chronicle asks this too: today it runs one exporter per node
and one sidecar per tenant leaf.

**How they differ.**

| | `prometheus-nats-exporter` | NATS Surveyor |
|---|---|---|
| Reads | Each server's HTTP monitor port | `$SYS` stats requests over NATS |
| Processes | One per server | One per reachable server set |
| Needs | Monitor port reachable | A **system account** user |
| Metric names | `gnatsd_*` | `nats_core_*` |

**Expected answers, to verify.** These follow from how `$SYS` requests
travel. They are expectations, not results.

| Topology | Surveyor connects to | Expected to see | Why |
|---|---|---|---|
| T3 / C | any `za` server | all 7 servers (`za`, `au`, `arb`) | `$SYS` requests cross gateways |
| T5 / D | a `hub` server | the 3 `hub` servers only | leaf links carry account `LB` only, not `$SYS` |
| T5 / D | a `za` server | the 3 `za` servers only | same |

T5 / D has the same shape as Chronicle's tenant leaf links, which bind no
system account. So T5 tests Chronicle's leaf gap directly.

**Steps.**
1. Run both on the same rig, at the same time, for the same window.
   Exporter: one per server, with Chronicle's flags
   (`-varz -connz -routez -subz -leafz -gatewayz -jsz=all -healthz`).
   Surveyor: one instance, `--count` set to the expected server count, with
   `--gatewayz --jsz=all --accounts`. Connect as the lab's `$SYS` user
   (`admin`).
2. For each topology, record which servers each tool reports.
3. Map the metrics Chronicle's Alloy keep lists use
   (`regional-nats` and `nats-leaf` configs) to Surveyor names. Mark each:
   same, renamed, or missing.
4. Freeze one cluster. Record what each tool reports for the frozen servers
   (missing series, `nats_up`, scrape failure).
5. Stop the server Surveyor is connected to. Record whether it reconnects
   to another URL in `--servers`, and the gap in its data.

**Result.** A table in `EXERCISE_OBSERVATIONS.md`: per topology, servers
seen by each tool; the metric map; the freeze and failover behaviour. The
counts in step 2 become **checks** in `12-otel-signals.sh` once stable.

**Then decide** which tool exercise 12 uses for H6 and D03-R45. Both may
stay if each covers what the other misses. The Chronicle choice is made
later, in section 9.

**Not answered here:** high availability in production (one Surveyor is a
single point of failure; two give duplicate series) and how Chronicle stores
the SYS credential. Both go to the Chronicle production plan.

### Retained evidence

`lab/run/evidence/12-<stamp>/`, never reused or deleted:
- `env.txt`: tool versions, image digests, git commit, module checksum.
- `12-<part>.tsv` result rows.
- Copies of the `t-*.conf` files, `config.alloy`, and server logs.
- One trace per scenario as JSON from the Tempo API.
- The Prometheus and Loki answers the checks used.

## 7. Guidance changes (authorised in this discussion)

**`demos/03-multi-cluster-and-accounts/CLAUDE.md`**
- Tools rule (line ~166): exercise 12's scripts may also use **Go** (to build
  `otel/`) and **Docker for the telemetry side only**: Alloy, `otel-lgtm`
  (which holds Grafana), `prometheus-nats-exporter` and `nats-surveyor`
  *(the metric collectors are decision 2)*. NATS servers stay host processes. A region is still cut with
  `kill -STOP`.
- The `t-otel-` naming, PID-file stop, and one-rig-at-a-time rules from
  section 3.
- Playground: never run it beside exercise 12 or `rig-t3.sh` / `rig-t5.sh`.
  They share ports, and `lab_init` kills every `t-` server.
- Exercise 12 is not in `run-all.sh`; its rows go to `run/12-*.tsv` only.
- Ports 20303–20306 in the port table.
- `otel/` is not in `go.work`, on purpose.

**`demos/03-multi-cluster-and-accounts/README.md`**: `D03-R37`–`R52`, the
port rows, and one paragraph for exercise 12.

**Root `CLAUDE.md`**, "Running demo 03": it says "no Docker" and names the
playground as the one exception to "no code". Change both lines to name
exercise 12 too.

## 8. Unresolved decisions

| # | Question | Recommended default |
|---|---|---|
| 1 | Import `shared/otelnats`, or build a new module in demo 03? | **New module.** Demo 03 stays sealed. `otelnats` carries demo 01 rules (tenant `Trust`, `api.*` root policy, `obs.pubsub.*`) and has uncommitted edits. Its good parts (carrier, four span kinds, timeouts, sampler) are copied and adapted, then compared in step 1 of section 9. |
| 2 | Run the metric collectors (`prometheus-nats-exporter`, `nats-surveyor`) in Docker, or on the host? | **Docker**, beside Alloy. No new host tool. This widens the Docker exception by two images, so it needs a yes. Which collector stays is decided by spike (b), not here. |
| 3 | Which `go` line does the module declare? | **`go 1.26.4`**, Chronicle's version, if spike (d) passes. Spike (d) needs a Go 1.26.4 toolchain download. |
| 4 | Name space for custom attributes | **`nats.server.name`, `nats.cluster.name`, `nats.server.id`** for reusable keys; `lab.*` only for lab keys (`lab.run_id`, `lab.scenario`). |

## 9. After the POC (deferred, not part of this plan's work)

1. **Three-way comparison.** `shared/otelnats`, `demos/03-…/otel`, and
   Chronicle's `services/lib/logger`. For each feature: reuse, adapt,
   replace, or lab only. Name the affected Chronicle files and the
   acceptance test for each. Fetch Chronicle first and record the commit.
2. **Chronicle gaps to check then** (local checkout `8b7389dc`): `Inject` is
   never called; SERVER spans only; no broker location attributes; the
   header carrier is case-sensitive; default sampler; the endpoint falls back
   to `localhost:4317`; `ENV=local` turns telemetry off; central Alloy has a
   memory queue only and an unpinned version; NATS server logs are not
   shipped. Use spike (b)'s result to choose Surveyor, the exporter, or both
   for Chronicle's regional, control-plane and tenant leaf servers.
3. **Chronicle local stack.** Docker only, no cloud service. Reuse the
   exercise 12 telemetry side.
4. **Chronicle production plan.** Collection, authentication, storage,
   retention and reliability (persistent queue, sampling, version pinning).
   AWS is not the same as Grafana Cloud, and production differs by more than
   an endpoint.
5. **Documents.** Architecture notes, ADRs and pattern cards, written from
   measured results only, after 12.7.
6. **Incremental integration** into Chronicle, one acceptance test at a time.

## Appendix — additions to the Codex prompt

These six points were added to the original prompt before this plan was
written:
1. The Go workload may be built and run by `lab/` scripts for exercise 12.
   Section 3 says where it lives.
2. Every new process follows the `t-` naming rule and stops by verified PID.
3. The playground never runs beside exercise 12 or its rigs.
4. New ports use the demo's `203xx` family.
5. Every image version is pinned and its digest recorded.
6. The plan is phased, so the first healthy-trace phase (12.5) can ship
   alone.
