# Exercises 12–14 — OpenTelemetry traces, logs and metrics across T3, T5 and T7 (plan)

Status: **PLAN ONLY.** Nothing here is built. No rule in this demo has
changed yet. Section 7 lists the rule changes, and they take effect only
when they are made in `CLAUDE.md` and `README.md`.

**One signal per exercise.** Each exercise is built, checked and closed
before the next starts:

| Exercise | Signal | Adds | Needs |
|---|---|---|---|
| **12** | Traces | The base: rigs for T3, T5 and T7, the workload, Alloy, `otel-lgtm`, the trace checks | — |
| **13** | Logs | Application logs with trace IDs; NATS server log files | Exercise 12 |
| **14** | Metrics | Application metrics; NATS server metrics; Surveyor vs `prometheus-nats-exporter` | Exercise 12 |

Exercise 13 comes before 14 because a log line links to its trace, and
exercise 13 checks that link. Exercise 14 does not need exercise 13.

## 0. Why, and what is held still

**Question.** Can one small workload give correlated traces, logs and
metrics across NATS cluster, leaf and account boundaries, and do those
signals stay true when a region goes dark and comes back? Each exercise
answers it for one signal.

**Role.** Validation and showcase, for each exercise. The check script is
the validation. Grafana is the showcase. A Grafana session is never
evidence, in the same way as the playground.

**Held still.** The servers of T3 / C, T5 / D and T7 / J (phase 1 wiring,
no `jetstream.domain`), their accounts, and the host-process freeze
(`kill -STOP`). **The variable** is the topology, and where the services
connect.

**Why demo 03.** It already has the topologies and the freeze mechanism.
Demo 01 shows tracing in a large application. These exercises show each
signal on a small workload, across topologies and failures.

**Why these three topologies.**

| Topology | Shape | Boundary it adds |
|---|---|---|
| T3 / C | `za`, `au` and a 1-node `arb`, joined by gateways; 7 servers; account `LB` | A gateway between regions |
| T5 / D | `hub` plus leaf clusters `za` and `au`; 9 servers; account `LB` | Two leaf hops through a hub |
| T7 / J | T4 (`za`, `au`, `arb`, 3 servers each, gateways) plus a tenant leaf per region; 11 servers; accounts `TENANT_ZA`, `TENANT_AU` | A leaf hop inside a region, and a **tenant account boundary**. No path between the regions, by design (`J14`). Closest to Chronicle's shape. |

### Revisions inspected (local checkouts, not remote state)

| Repo | Branch | Commit | State |
|---|---|---|---|
| lab | `poc/phase54-otel` | `bcf202f0` | `shared/otelnats` has uncommitted edits (9 modified, 2 untracked files) |
| lab (T7) | `poc/phase54-otel` | `9ce6047e` | `lab/11-tenant-leaves.sh` read for the T7 wiring |
| Chronicle | `feature/pipelines` | `8b7389dc` (2026-10-14) | clean; **behind `origin/feature/pipelines` by 1** at the last fetch. No fetch was made. |

Host: `nats-server` 2.14.6, `nats` 0.4.0, Go 1.27.0, Docker 29.7.2.

## 1. Requirements

IDs continue from `D03-R36`. Each ID belongs to one exercise.

### Exercise 12 — traces (and the shared base)

| ID | Requirement | Kind |
|---|---|---|
| D03-R37 | T3 / C, T5 / D and T7 / J each run as a **persistent** rig of host processes, started and stopped by command, with the same freeze and thaw as the lab. | rig |
| D03-R38 | `lab/telemetry.sh` starts Alloy and `otel-lgtm` with pinned image versions, waits until they are ready, and records the image digests. | rig |
| D03-R39 | A driver calls service A, and A calls service B. Each service adds internal spans, so one trace has at least **four levels** of nesting. A and B can connect to different servers, clusters or leaves. | showcase |
| D03-R40 | Trace context is injected at every send (CLIENT, PRODUCER) and extracted at every receive (SERVER, CONSUMER). W3C `traceparent` and `baggage`, in `nats.Header`. | validation |
| D03-R41 | Application location is on the **resource** (`service.name`, `service.instance.id`, `cloud.region`, `deployment.environment.name`). Broker location is on **every messaging span** (`nats.server.name`, `nats.cluster.name`, `server.address`), read when the span starts. | validation |
| D03-R42 | Every messaging span carries `nats.account.name`, taken from the service's own configuration, never from a header. On T7, **no trace holds spans from two tenant accounts.** | validation |
| D03-R43 | After a reconnect to another cluster, new spans keep the same `cloud.region` and show the new `nats.cluster.name`. | validation |
| D03-R44 | The run ID is on every application span. | validation |
| D03-R45 | A controlled error, a timeout and a "no responders" each give an ERROR span status. | validation |
| D03-R46 | Region freeze, recovery, and requests after recovery give the topology-specific trace outcomes in section 2. | validation |
| D03-R47 | Readiness gates before traffic. A flush before checks. Clean up leaves no `t-` process and no lab container. Checks retry up to a deadline, never `sleep`. Applies to exercises 12, 13 and 14. | rig |
| D03-R48 | Exercises 12, 13 and 14 never write to `results.tsv`, `REPORT.md`, `REPORT.html` or the findings deck, and are not in `run-all.sh`. | safety |
| D03-R49 | The Go module builds and tests with `GOWORK=off`. Its reusable packages import no lab-specific code. | validation |
| D03-R50 | *(Optional, section 5.)* NATS server message-trace events for one request on each of T3, T5 and T7 are shown inside the application trace in Tempo. | showcase |

### Exercise 13 — logs

| ID | Requirement | Kind |
|---|---|---|
| D03-R51 | Every application log line written during a request carries `trace_id`, `span_id` and the run ID. Grafana links a log line to its trace, and a trace to its log lines. | showcase + validation |
| D03-R52 | A controlled error, a timeout and a "no responders" each give one error log line with the trace ID. | validation |
| D03-R53 | NATS server log files are ingested with `server` and `cluster` labels, for every server of the running topology, the T7 leaves included. They carry no run ID; they are found by time window. | validation |
| D03-R54 | During each freeze in section 2, the peers' server logs show the lost link inside the run window. A **note** until spike 13.1 (b) shows the text is stable; then a check. | validation |

### Exercise 14 — metrics

| ID | Requirement | Kind |
|---|---|---|
| D03-R55 | The services record real counters and duration histograms with bounded labels only: service, scenario, topology, outcome, broker cluster, account. The run ID is **never** a metric label. Exemplars where the backend supports them. | validation |
| D03-R56 | A controlled error, a timeout and a "no responders" each add 1 to an error counter. | validation |
| D03-R57 | NATS server metrics exist for every running server in the run window, the T7 leaves included. A frozen server shows a failed scrape or a missing series. | validation |
| D03-R58 | For T3, T5 and T7, the servers that Surveyor and `prometheus-nats-exporter` each report are recorded, and become checks once stable (section 6.3). | validation |

### Exclusions

- T1, T2 / A, T2 / B, T4 / F, T5 / H, and every other shape.
- T7's phase 2 wiring (`jetstream.domain core`). It changes only the
  JetStream API path, and JetStream is excluded.
- **Cross-account export and import.** T3 and T5 use one account (`LB`).
  T7 uses `TENANT_ZA` and `TENANT_AU` with no export or import. Nothing in
  these exercises is evidence for variant E.
- JetStream. Core request-reply and core publish-subscribe only. JetStream
  spans are a later question.
- Running NATS servers in Docker. Docker is for the telemetry side only.
- Grafana Cloud, AWS, and any cloud service.
- Latency numbers as findings. One laptop cannot give them (as in
  `D03-R4`). Durations are notes.
- Changes to Chronicle, architecture documents, and ADRs.

## 2. Scenario and check matrix

All three exercises run the **same scenarios**. Each exercise checks only
its own signal on them. Exercise 12 builds the scenarios; 13 and 14 reuse
them.

**Placement.** "A `za`, B `au`" means A connects to cluster `za` and B to
`au`. The driver connects where A connects. A **URL list** is the server
list a service is given.

| Placement | T3 / C | T5 / D | T7 / J |
|---|---|---|---|
| P1, same cluster | A, B on `za` | A, B on `za` | A, B on `za`, as `tenant_za` |
| P2, across a boundary | A `za`, B `au`, over a gateway | A `za`, B `au`, two leaf hops through `hub` | A on `t-za-leaf-1` as `fe`, B on `za` as `tenant_za`: one leaf hop, same region |

On T7 there is no cross-region placement. The regions have no path between
them, by design (`J14`).

### Healthy

| ID | Scenario | Exercise 12 checks (traces) | Exercise 13 checks (logs) | Exercise 14 checks (metrics) |
|---|---|---|---|---|
| H1 | Request, P1 | One trace. Spans: driver CLIENT → A SERVER → A internal → A CLIENT → B SERVER → B internal. Parent links exact. All broker spans say `za`. | Each app log line has the run ID, a `trace_id` that exists in Tempo, and a `span_id` inside that trace. | Request counter increase equals the requests the driver sent, per scenario and outcome. The histogram count equals it too. |
| H2 | Request, P2 | As H1. A spans and B spans each show their own `nats.cluster.name` (T7: A `nats.server.name=t-za-leaf-1`). Resource `cloud.region` matches. | As H1. | As H1, by broker cluster. |
| H3 | Publish, P2 | A PRODUCER is the parent of B CONSUMER. Same trace. | As H1. | As H1. |
| X1 | **T7 only.** Both tenants at once: ZA A on `t-za-leaf-1` → ZA B on `za`; AU A on `t-au-leaf-1` → AU B on `au`. Same subject in both accounts. | Every span in a trace has one `nats.account.name`. Every B answer comes from A's own region. | Log lines of one trace all come from one account's services. | Counters split by account; each equals that tenant's requests. |
| S1 | Server signals, all servers | — | At least one server log line per server in the window, with `server` and `cluster` labels (T3: 7, T5: 9, T7: 11). | One series per server for the chosen metrics, with `server` and `cluster` labels. Collector coverage per section 6.3. |

### Controlled failures

| ID | Scenario | Exercise 12 (traces) | Exercise 13 (logs) | Exercise 14 (metrics) |
|---|---|---|---|---|
| E1 | B returns a service error | B SERVER and A CLIENT status ERROR. | One error log line with the trace ID. | Error counter +1 per call. |
| E2 | B is slower than A's timeout | A CLIENT status ERROR (timeout). B's late span is in the same trace. | As E1. | As E1. |
| E3 | B is stopped. **T7:** the ZA B is stopped and the AU B keeps running. | A CLIENT status ERROR (no responders). No B span in the trace. **T7:** no AU span either; the AU backend never answers for ZA (`J14`). | As E1. | As E1. |

### Freeze and recovery

Freeze uses the lab's own `freeze`, and `wait_dark` must prove it. Every
service sets a short client ping (decided in spike 12.1 (c)), so a lost
server is found in seconds, not minutes.

These are the **outcomes**. Exercise 12 checks them through spans.
Exercise 13 adds the server log rows (`D03-R54`). Exercise 14 adds the
scrape rows (`D03-R57`).

| ID | Freeze | B's URL list | T3 / C | T5 / D | T7 / J |
|---|---|---|---|---|---|
| F1 | `au` | `au` only | Cross-cluster requests fail. Requests inside `za` still succeed. | Same. | The AU tenant's requests fail. The ZA tenant's requests still succeed (as `J17`, `J20`). |
| F2 | `au` | `au`, then a fallback (T3: `za`; T5: `hub`) | B reconnects. Requests succeed again. New B spans: `cloud.region=au`, `nats.cluster.name=za`. | Same, with `nats.cluster.name=hub`. | n/a. A tenant account lives in one region; there is no fallback, by design (`J20a`). |
| F3 | `arb` | — | Requests keep succeeding: core traffic goes `za`↔`au` direct. | n/a | Requests keep succeeding: tenant traffic never crosses a gateway. |
| F4 | `hub` | — | n/a | Cross-cluster requests fail. Requests inside `za` still succeed. | n/a |
| F5 | The one `au` server that `t-au-leaf-1`'s link is on (read from `/leafz`) | B on a different `au` server | n/a | n/a | **T7 only.** The leaf reconnects to another `au` server. AU requests succeed again before a deadline. A's spans do not change: A connects to the leaf, so the move is not visible in app spans (section 5 can show it). |
| R1 | thaw, after each F | — | Requests succeed again before a deadline. | Same. | Same. |
| R2 | after F2 + thaw | — | *Note, not check:* does B stay on the fallback cluster? | Same. | n/a |

**Check or note.** A check needs an exact answer in advance. These are
**notes** until a spike shows they are stable: the kind of failure (timeout
or no responders) during a freeze, the recovery time, and the log text a
peer writes about a frozen link. They may then become checks.

## 3. Layout

### Go module (isolated)

`demos/03-multi-cluster-and-accounts/otel/`, its own `go.mod`. **Not** in
the root `go.work`; build and test with `GOWORK=off`. It does not import
`shared/otelnats` (decision 1). Each exercise adds to it:

```
otel/
  go.mod
  telemetry/          reusable. 12: TracerProvider (OTLP), resource from env,
                      sampler, export and shutdown timeouts, ForceFlush.
                      13: LoggerProvider, slog bridge with trace IDs.
                      14: MeterProvider, 5 s export interval.
  natsotel/           reusable. 12: header carrier, Inject/Extract, Start
                      Client/Server/Producer/Consumer, messaging attributes,
                      broker location, account, request and handler wrappers,
                      baggage-to-attribute processor.
                      14: messaging metrics.
  internal/lab/       lab only: run ID, scenario names, topology names,
                      placement and URL lists
  cmd/t-otel-driver/  lab only: runs one scenario, prints trace IDs
  cmd/t-otel-svc/     lab only: one binary, role A or B by flag
```

An import-graph test fails if `telemetry/` or `natsotel/` imports
`internal/lab`, or names a lab subject, port or account.

### Scripts and configs

```
lab/_rig.sh              shared up/status/freeze/thaw/down for persistent rigs
                         (new; rig-t4.sh is left as it is)                 [12]
lab/rig-t3.sh            T3 / C rig: copies exercises/config/t3/*.conf     [12]
lab/rig-t5.sh            T5 / D rig: copies exercises/config/t5/*.conf     [12]
lab/rig-t7.sh            T7 / J rig: copies exercises/config/t7/*.conf     [12]
lab/telemetry.sh         up | status | down for the Docker side            [12]
lab/12-otel-traces.sh    trace checks; RESULTS=run/12-<part>.tsv           [12]
lab/13-otel-logs.sh      log checks;   RESULTS=run/13-<part>.tsv           [13]
lab/14-otel-metrics.sh   metric checks; RESULTS=run/14-<part>.tsv          [14]
exercises/config/t3/     static, hand-editable configs (taken once from 03-arbiter.sh output)
exercises/config/t5/     same, from 04-hub-and-leaf.sh
exercises/config/t7/     same, from 11-tenant-leaves.sh phase 1: 9 cluster
                         servers + t-za-leaf-1, t-au-leaf-1
exercises/ex12-check.sh  exec lab/12-otel-traces.sh "$@"                   [12]
exercises/ex13-check.sh  exec lab/13-otel-logs.sh "$@"                     [13]
exercises/ex14-check.sh  exec lab/14-otel-metrics.sh "$@"                  [14]
exercises/EXERCISE-12-TERMINAL-STEPS.md   Step N / Step N.M walkthrough    [12]
exercises/EXERCISE-13-TERMINAL-STEPS.md                                    [13]
exercises/EXERCISE-14-TERMINAL-STEPS.md                                    [14]
telemetry/compose.yaml           alloy, otel-lgtm                          [12]
telemetry/compose.metrics.yaml   collectors spike 14.1 (b) keeps           [14]
telemetry/alloy/config.alloy     12: OTLP traces; 13: + logs and file
                                 tail; 14: + metrics and scrapes
telemetry/grafana/dashboards/exercise-12-traces.json                       [12]
telemetry/grafana/dashboards/exercise-13-logs.json                         [13]
telemetry/grafana/dashboards/exercise-14-metrics.json                      [14]
```

Every check script uses the same rigs and the same driver scenarios, so a
person can run 12, 13 and 14 one after the other on one running rig.

Why new rigs: `03-arbiter.sh`, `04-hub-and-leaf.sh` and
`11-tenant-leaves.sh` always tear down on exit, and `lab_init` kills every
`t-` server. They cannot be left running. `rig-t4.sh` is the pattern for a
rig that stays up. T7 cannot reuse `rig-t4.sh`'s configs: T7 adds the
tenant accounts, `system_account`, leaf listeners and two leaf servers.

Rules for every new process:
- Binary and container names start `t-otel-`.
- A PID file for every host process. Stop by verified PID only. No new
  pattern kill. The one existing pattern (`nats-server -c t-`) stays the
  only one.
- `rig-t3.sh up`, `rig-t5.sh up` and `rig-t7.sh up` refuse to start if any
  `t-` server runs or any rig port is busy (copy rig-t4's guard). T3, T4,
  T5 and T7 share the `za` and `au` ports, so only one rig runs at a time.
- `rig-t7.sh` starts the nine cluster servers first and the two leaves
  last, as `11-tenant-leaves.sh` does. Its port guard includes the leaf
  listeners (7571–7573, 7581–7583) and the leaf client and monitor ports
  (4571, 8571, 4581, 8581).

### Ports

New ports stay in the demo's `203xx` family, beside the playground.

| Port | Use | From |
|---|---|---|
| 20303 | Grafana (inside `otel-lgtm`) | 12 |
| 20304 | Alloy UI | 12 |
| 20305 | Alloy OTLP gRPC (apps send here) | 12 |
| 20306 | Alloy OTLP HTTP | 12 |

Collectors, Tempo, Loki and Prometheus get no host port. The check scripts
query them through Grafana's datasource API on 20303.

### Image versions

Pin exact versions. Record image digests in each evidence `env.txt`.
Exercise 12: `grafana/alloy:v1.11.0` (Chronicle's version) and
`grafana/otel-lgtm` at an exact tag chosen in spike 12.1 (a). Exercise 14:
`natsio/prometheus-nats-exporter:0.20.2` (Chronicle's version) and
`natsio/nats-surveyor` at an exact tag chosen in spike 14.1 (b). Never
`latest`.

## 4. Telemetry pipeline and Grafana

```
[12] host: t-otel-driver, t-otel-svc (A, B) --OTLP gRPC :20305--> Alloy
[13] host: lab/run/log/*.log  --read-only folder mount--> Alloy (file tail)
[14] host: nats-server monitor ports <--scrape-- prometheus-nats-exporter (Docker) --> Alloy
[14] host: nats-server $SYS          <--NATS--   nats-surveyor (Docker)            --> Alloy
     Alloy --OTLP--> otel-lgtm (Tempo, Loki, Prometheus) --> Grafana :20303
```

- **Alloy, exercise 12.** Receives OTLP traces and sends them to
  `otel-lgtm`. Batch and memory limiter, as in Chronicle. Queue settings are
  recorded, not tuned (production reliability is later).
- **Alloy, exercise 13.** Adds OTLP logs, and tails the server log files.
  Adds `server` and `cluster` labels from the file name.
  `lab_reset_run_dir` wipes `lab/run/log/`, so Alloy mounts the folder, not
  the files, and must survive a file being replaced.
- **Alloy, exercise 14.** Adds OTLP metrics and the collector scrapes.
- **Run ID.** The driver makes it (`<nn>-<stamp>`, the same as the evidence
  folder). It travels as baggage. Exercise 12: a span processor copies it
  onto every span. Exercise 13: the slog handler adds it to every log line.
  Exercise 14: metrics never get it.
- **Flush.** Services and the driver call `ForceFlush` and `Shutdown`
  before exit. Exercise 14 sets the metric export interval to 5 s (the SDK
  default is 60 s).

**Grafana dashboards** (provisioned, one per exercise):
- `exercise-12-traces.json`: Tempo search on `lab.run_id`, by scenario,
  topology and account.
- `exercise-13-logs.json`: application lines for the run ID; server lines
  for the window; trace → logs by `trace_id`; log → trace by a derived
  field.
- `exercise-14-metrics.json`: request rate, error rate and duration by
  scenario, outcome, broker cluster and account, with exemplars; NATS
  servers: up, connections, gateway or leaf links, per server.

**Automated checks.** Each check script asks its own backend (Tempo, Loki
or Prometheus, through Grafana's API) for each scenario's trace IDs and run
window, and compares the answer with section 2. It retries to a deadline. A
row is a check or a note, as in the rest of `lab/`.

## 5. Broker tracing (exercise 12, optional, not a prerequisite)

**What app spans cannot show.** Application spans show the two ends: A sent
on `za`, B received on `au`. They do not show the path between: which
gateway or leaf link, which hub server, whether the message was routed
twice. On T7, they cannot show which `za` or `au` server a leaf's link is on
(F5).

**What NATS message tracing adds.** `nats-server` 2.11 and later can emit
trace events for a message: ingress, each route, gateway and leaf hop, and
egress to each subscriber. That is broker-routing evidence, independent of
the applications.

**What it is not.** The events are NATS JSON messages sent to a subject. They
are not OTLP. **Alloy does not convert them.** To see them in Tempo, a small
converter must subscribe to the trace subject, turn each hop into a span with
the application span as parent, and send it over OTLP.

**Bounded experiment (D03-R50).**
1. Find how tracing is triggered on 2.14.6: the `Nats-Trace-Dest` header, or
   an account setting that traces messages with a sampled `traceparent`.
   Record which. On T7, record whether the trace subject must be in the
   tenant account.
2. One H2 request on each of T3, T5 and T7. Save the raw events in the
   evidence folder.
3. Write the converter as `cmd/t-otel-msgtrace` (lab only).
4. Success: Tempo shows the hop spans inside the H2 trace, and the hops match
   the topology: one gateway hop on T3; two leaf hops through `hub` on T5;
   one leaf hop from `t-za-leaf-1` into `za` on T7.

Phase 63 in `.claude/plans/Main-POC-Plan-Candidates.md` has a design for the
same events in demo 01. Read it for ideas; do not depend on it.

## 6. Phases

Each phase ends with its checks green before the next starts. Each
exercise closes with its own pattern card before the next exercise starts.

### 6.1 Exercise 12 — traces

| Phase | Goal | Done when |
|---|---|---|
| 12.0 | Rule and README changes for exercise 12 (section 7) | Approved and committed |
| 12.1 | **Spikes.** (a) `otel-lgtm` tag, dashboard provisioning, Tempo search by span attribute. (b) Does the module build and test on Go 1.26.4? (c) Client ping and reconnect settings for F2 and F5. (d) T7: how a script reads which `au` server `t-au-leaf-1`'s link is on. | Each answer in `EXERCISE_OBSERVATIONS.md`; section 2's check/note split updated |
| 12.2 | Persistent rigs `rig-t3.sh`, `rig-t5.sh`, `rig-t7.sh` | up, status, freeze, thaw, down work on all three; port guard refuses a second rig |
| 12.3 | Telemetry side: Alloy and `otel-lgtm`, traces only | `telemetry.sh up` is ready; a test span shows in Tempo |
| 12.4 | Module: `telemetry/` (traces), `natsotel/` | Unit specs green with in-memory exporters and an embedded `nats-server`; import-graph test green; `GOWORK=off go test ./...` green |
| 12.5 | Workload, healthy scenarios | H1–H3 green on T3, T5 and T7; X1 green on T7. **First shippable slice.** |
| 12.6 | Failures, freeze, recovery | E1–E3, F1–F5, R1 green where they apply; R2 recorded |
| 12.7 | Exercise packaging | `EXERCISE-12-TERMINAL-STEPS.md`, `ex12-check.sh`, one full cited run per topology with evidence |
| 12.8 | *Optional.* Broker message trace | Section 5 success line met, or the reason it was not |
| 12.9 | Close | Trace API listed in `otel/README.md`; one pattern card from the measured results (`pattern-cards` skill) |

### 6.2 Exercise 13 — logs

| Phase | Goal | Done when |
|---|---|---|
| 13.0 | Rule and README changes for exercise 13 | Approved and committed |
| 13.1 | **Spikes.** (a) OTLP logs into Loki: labels and structured fields; trace ↔ log links in `otel-lgtm`. (b) What peers log, and when, about a frozen gateway, leaf or route link. (c) Alloy survives `lab/run/log/` being wiped. | Each answer in `EXERCISE_OBSERVATIONS.md`; `D03-R54` stays a note or becomes a check |
| 13.2 | Module: `telemetry/` adds the LoggerProvider and the slog bridge | Unit specs green: every log line in a span has `trace_id`, `span_id`, run ID |
| 13.3 | Server log files in Alloy | S1 log column green on T3 (7), T5 (9) and T7 (11) |
| 13.4 | Application logs on the scenarios | H1–H3, X1 and E1–E3 log columns green |
| 13.5 | Freeze rows | F1–F5 server log rows recorded as notes or green as checks |
| 13.6 | Packaging and close | `EXERCISE-13-TERMINAL-STEPS.md`, `ex13-check.sh`, one cited run per topology; log API listed; one pattern card |

### 6.3 Exercise 14 — metrics

| Phase | Goal | Done when |
|---|---|---|
| 14.0 | Rule and README changes for exercise 14, including the collector images (decision 2) | Approved and committed |
| 14.1 | **Spikes.** (a) Can a container reach host `127.0.0.1` monitor and client ports? (b) **Server metrics: Surveyor vs `prometheus-nats-exporter`** on T3, T5 and T7 (section 6.4). (c) Exemplars from the Go SDK into `otel-lgtm`. | Each answer in `EXERCISE_OBSERVATIONS.md` |
| 14.2 | Collectors in `compose.metrics.yaml` | S1 metric column green on T3, T5 and T7 |
| 14.3 | Module: `telemetry/` adds the MeterProvider; `natsotel/` adds messaging metrics | Unit specs green with an in-memory reader; no run ID label |
| 14.4 | Application metrics on the scenarios | H1–H3, X1 and E1–E3 metric columns green; exemplar link recorded |
| 14.5 | Freeze rows | F1–F5 scrape rows green |
| 14.6 | Packaging and close | `EXERCISE-14-TERMINAL-STEPS.md`, `ex14-check.sh`, one cited run per topology; **module boundary freeze**: whole public API in `otel/README.md`, lab-only code only under `internal/` and `cmd/`; one pattern card |

### 6.4 Spike 14.1 (b) — Surveyor vs `prometheus-nats-exporter`

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

| Topology | Surveyor connects to | Expected to see | Exporter sees | Why |
|---|---|---|---|---|
| T3 / C | any `za` server | all 7 servers (`za`, `au`, `arb`) | 7 | `$SYS` requests cross gateways |
| T5 / D | a `hub` server | the 3 `hub` servers only | 9 | leaf links carry account `LB` only, not `$SYS` |
| T5 / D | a `za` server | the 3 `za` servers only | 9 | same |
| T7 / J | any `za` server | the 9 cluster servers (`za`, `au`, `arb`), **not** the 2 tenant leaves | 11 | `$SYS` crosses gateways; the leaf links bind `TENANT_ZA` / `TENANT_AU` only |
| T7 / J | a tenant leaf | nothing | — | the leaf configs define no system user, so Surveyor cannot connect |

T5 / D and T7 / J both have Chronicle's leaf gap: leaf links that bind no
system account. **T7 is the closer match**: regional clusters joined by
gateways, with a tenant leaf per region. If Surveyor misses the T7 leaves,
it misses Chronicle's tenant leaves too.

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
counts in step 2 become **checks** in `14-otel-metrics.sh` once stable
(`D03-R58`).

**Then decide** which tool exercise 14 uses for S1 and `D03-R57`. Both may
stay if each covers what the other misses (for example, Surveyor for the
clusters and the exporter for the T7 leaves). The Chronicle choice is made
later, in section 9.

**Not answered here:** high availability in production (one Surveyor is a
single point of failure; two give duplicate series) and how Chronicle stores
the SYS credential. Both go to the Chronicle production plan.

### Retained evidence

`lab/run/evidence/<nn>-<stamp>/` (`nn` is 12, 13 or 14), never reused or
deleted:
- `env.txt`: tool versions, image digests, git commit, module checksum.
- `<nn>-<part>.tsv` result rows.
- Copies of the `t-*.conf` files, `config.alloy`, and server logs.
- Exercise 12: one trace per scenario as JSON from the Tempo API.
- Exercise 13: the Loki answers the checks used.
- Exercise 14: the Prometheus answers the checks used.

## 7. Guidance changes (authorised in this discussion)

Each exercise makes its own changes in its `.0` phase.

**`demos/03-multi-cluster-and-accounts/CLAUDE.md`**
- *(12.0)* Tools rule (line ~166): exercises 12–14 may also use **Go** (to
  build `otel/`) and **Docker for the telemetry side only**: Alloy and
  `otel-lgtm` (which holds Grafana). NATS servers stay host processes. A
  region is still cut with `kill -STOP`.
- *(14.0)* Add `prometheus-nats-exporter` and `nats-surveyor` to the Docker
  list, if decision 2 is yes.
- *(12.0)* The `t-otel-` naming, PID-file stop, and one-rig-at-a-time rules
  from section 3, with `rig-t3.sh`, `rig-t5.sh` and `rig-t7.sh`.
- *(12.0)* Playground: never run it beside exercises 12–14 or their rigs.
  They share ports, and `lab_init` kills every `t-` server.
- *(12.0)* Exercises 12–14 are not in `run-all.sh`; their rows go to
  `run/12-*.tsv`, `run/13-*.tsv` and `run/14-*.tsv` only.
- *(12.0)* Ports 20303–20306 in the port table.
- *(12.0)* `otel/` is not in `go.work`, on purpose.

**`demos/03-multi-cluster-and-accounts/README.md`**: *(12.0)* `D03-R37`–`R50`,
the port rows, and one paragraph for exercise 12. *(13.0)* `D03-R51`–`R54`
and a paragraph for exercise 13. *(14.0)* `D03-R55`–`R58` and a paragraph
for exercise 14.

**Root `CLAUDE.md`**, "Running demo 03": *(12.0)* it says "no Docker" and
names the playground as the one exception to "no code". Change both lines
to name exercises 12–14 too.

## 8. Unresolved decisions

| # | Question | Needed before | Recommended default |
|---|---|---|---|
| 1 | Import `shared/otelnats`, or build a new module in demo 03? | 12.4 | **New module.** Demo 03 stays sealed. `otelnats` carries demo 01 rules (tenant `Trust`, `api.*` root policy, `obs.pubsub.*`) and has uncommitted edits. Its good parts (carrier, four span kinds, timeouts, sampler) are copied and adapted, then compared in step 1 of section 9. |
| 2 | Run the metric collectors (`prometheus-nats-exporter`, `nats-surveyor`) in Docker, or on the host? | 14.0 | **Docker**, beside Alloy. No new host tool. This widens the Docker exception by two images, so it needs a yes. Which collector stays is decided by spike 14.1 (b), not here. |
| 3 | Which `go` line does the module declare? | 12.4 | **`go 1.26.4`**, Chronicle's version, if spike 12.1 (b) passes. That spike needs a Go 1.26.4 toolchain download. |
| 4 | Name space for custom attributes | 12.4 | **`nats.server.name`, `nats.cluster.name`, `nats.server.id`, `nats.account.name`** for reusable keys; `lab.*` only for lab keys (`lab.run_id`, `lab.scenario`). |

## 9. After the POC (deferred, not part of this plan's work)

1. **Three-way comparison.** `shared/otelnats`, `demos/03-…/otel`, and
   Chronicle's `services/lib/logger`. For each feature: reuse, adapt,
   replace, or lab only. Name the affected Chronicle files and the
   acceptance test for each. Fetch Chronicle first and record the commit.
   The trace part can start after exercise 12 closes.
2. **Chronicle gaps to check then** (local checkout `8b7389dc`): `Inject` is
   never called; SERVER spans only; no broker location attributes; the
   header carrier is case-sensitive; default sampler; the endpoint falls back
   to `localhost:4317`; `ENV=local` turns telemetry off; central Alloy has a
   memory queue only and an unpinned version; NATS server logs are not
   shipped. Use spike 14.1 (b)'s result to choose Surveyor, the exporter, or
   both for Chronicle's regional, control-plane and tenant leaf servers.
3. **Chronicle local stack.** Docker only, no cloud service. Reuse the
   exercises' telemetry side.
4. **Chronicle production plan.** Collection, authentication, storage,
   retention and reliability (persistent queue, sampling, version pinning).
   AWS is not the same as Grafana Cloud, and production differs by more than
   an endpoint.
5. **Documents.** Architecture notes and ADRs, written from measured results
   only, after the exercise they depend on closes.
6. **Incremental integration** into Chronicle, one acceptance test at a time.

## Appendix — additions to the Codex prompt

These six points were added to the original prompt before this plan was
written. The plan was later split into exercises 12–14 and T7 was added.
1. The Go workload may be built and run by `lab/` scripts for exercises
   12–14. Section 3 says where it lives.
2. Every new process follows the `t-` naming rule and stops by verified PID.
3. The playground never runs beside exercises 12–14 or their rigs.
4. New ports use the demo's `203xx` family.
5. Every image version is pinned and its digest recorded.
6. The plan is phased, so the first healthy-trace phase (12.5) can ship
   alone.
