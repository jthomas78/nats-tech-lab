# Handoff — OTel comparison: Chronicle vs Tech Lab demo 01

Target: Claude. Written 2026-10-18.

## Goal

Compare OpenTelemetry traces, logs and metrics in Chronicle
(`/Users/jeremy/dev/github/linebooker/chronicle/services`, the production
system) and Tech Lab demo 01 (`demos/01-dictionary`, the proof of concept).
Say what differs, and which lab trace parts can move to Chronicle.

The earlier session was **feedback only**. The user said: make no changes.
Keep it that way unless the user asks.

## Done so far

- Compared on 2026-10-16. Chronicle at `feature/pipelines` @ `8b7389d`.
  Lab at `bf2e8435` (54b-7a) plus the uncommitted 54b-7b work.
- Drew an HTML page with two diagrams and two tables. It lives **outside the
  repo**, in the old session's scratch folder, and may be gone:
  `/private/tmp/claude-501/-Users-jeremy-dev-github-jthomas78-nats-tech-lab/dd54db06-5355-4c65-bdd0-c6b7febb682e/scratchpad/otel-chronicle-vs-lab.html`
  (+ `.png`). Layout audit passed with 0 errors.
- Nothing committed. No repo file changed except this handoff.

## The lab moved on after the comparison — read this first

Commits after `bf2e8435`: 54b-7b (`b8b1069d`), 54c (`db2cc02c`), decision 16
switch (`bcf202f0`), 54d (`04d73e27`), docs (`46938f62`), ADR-058/059
(`34ab2b54`, `2572008c`). Effect on the findings below:

- **OTel is now the default stack**, not an overlay.
  `deploy/cell/compose.otel.yaml` is now `compose.telemetry.yaml`.
- **natstrace, the TRACES stream, otlp-bridge and Jaeger are gone** (54d).
- **54c is built**: Admin UI reads traces through the gateway; trace search
  is no longer a 501 stub.
- **ADR-058 (v3, Proposed 2026-10-18)** already turns the transferable lab
  rules into a V3 platform rule, and cites the Chronicle "Inject never
  called" finding. Any Chronicle work should start from ADR-058.
- Still true on HEAD (checked 2026-10-18): `shared/otelnats` builds a
  TracerProvider only; OTLP HTTP with retry off; `ParentBased(AlwaysSample)`;
  resource = `service.name` only. Collector disk queue 4,176,000 spans,
  retry forever, no processors. Tempo ingest limits still at defaults, so
  BR-068 "span identity after drain" is still FAIL.

## Findings — Chronicle (production)

- One `Setup()` in `services/lib/logger/logger.go:29` builds Tracer, Logger
  and Meter providers, OTLP **gRPC**, env config, and sets OTel globals.
  Logs go through `otelslog`.
- **Traces do not join.** `Inject()` (`propagation.go:11`) is never called.
  `Extract()` is called only at `loads/main.go:59` and `tracking/main.go:82`,
  but no publisher injects, so every span is a root. `msg.Respond` sends no
  headers.
- Only SERVER and INTERNAL spans. No span attributes. Span names hold the
  raw subject. Tracer name says `lib/impls/logger` (wrong path,
  `span_value.go:14`).
- Metrics: provider set, **zero instruments**. NATS server and leaf metrics
  come from `prometheus-nats-exporter` :7777, scraped by Alloy.
- Logs: OTLP only, not copied to stdout, so CloudWatch groups are near
  empty. No log level setting.
- Collector: Grafana Alloy, 2 × EC2 behind a private NLB, memory_limiter +
  batch, **memory queue only**. Exports OTLP/HTTP + basic auth to one
  Grafana Cloud gateway (Tempo, Loki, Mimir). Service → Alloy is plaintext
  gRPC over PrivateLink.
- Tenant: one resource attribute per ECS deployment
  (`OTEL_RESOURCE_ATTRIBUTES=tenant=…`).
- Missing endpoint falls back to localhost (`logger.go:35`). Leaf Alloy
  scrapes every 15 s against a stated 60 s / 1 DPM intent.
- **Zero tests** in the repo.

## Findings — Tech Lab (proof of concept)

- Traces only. Logs and metrics are Phase 55, PROPOSED, not built.
- `shared/otelnats`: NATS header carrier; `StartServer`, `StartClient`,
  `StartProducer`, `StartConsumer`; micro `Middleware`; `HTTPMiddleware`
  (otelhttp); wired into `jstream`, `natsnotify`, `natstenants`. Inject and
  extract happen inside the helpers.
- Edge rule (BR-071): inbound `api.*` / REST starts a new root; the sent
  `traceparent` becomes a link.
- Tenant (BR-069): `lab.tenant.receiving_account` on SERVER and CONSUMER
  spans, from server context, never from headers or body.
- Collector disk queue sized for 2 h at measured peak (BR-068).
- Read path: NATS trace gateway, operator creds only (BR-067, BR-070).
- Ginkgo spec per rule (BR-064 to BR-073), plus `otel-stackcheck` live runs.
- No Postgres, outbound HTTP or Temporal spans (Temporal is an accepted gap).

## Transfer verdicts (decided — do not re-open without new facts)

- **Move:** span helpers + NATS carrier; edge rule (BR-071); span kinds +
  `messaging.*` attributes; joined-trace and outage specs (BR-064, BR-066).
- **Adapt:** disk queue → Alloy (check the Alloy version supports a
  file-backed queue; re-size on Chronicle's real load); tenant rule (keep
  per-deployment tenant for siloed services, add span attribute only for
  shared services, never read tenant from a header); body capture (off by
  default, personal data); exporter retry (keep some retry, because Chronicle
  exports across PrivateLink); helpers must read Chronicle's global tracer
  provider.
- **Skip:** NATS trace gateway (Grafana Cloud covers people); the lab's
  no-globals provider design.
- **Other way:** Chronicle leads on logs and metrics. Lab Phase 55 can copy
  its one-call `Setup()` and NATS exporter scrape.

## Traps

- `demos/01-dictionary/diagrams/otel-phase54-vs-chronicle.html` is an
  **older, untracked** diagram (2026-10-16, traces only, says Phase 54 "not
  built"). It is stale. It was not touched.
- Chronicle repo has no `*_test.go` files at all, so "tests pass" there
  proves nothing about telemetry.
- `grep --include=*.go` fails in zsh without quotes; use `grep -rn -e … dir`.

## Next steps (only if the user asks)

1. Re-draw the comparison against lab HEAD (54d done, OTel default).
   Check: no "overlay" or "legacy natstrace" box remains.
2. If the user wants it in the repo: replace the stale
   `otel-phase54-vs-chronicle.html` and render with
   `html-diagram-drawer`. Check: audit exits 0.
3. If the user wants a Chronicle change plan: base it on ADR-058, starting
   with the span helpers and a joined-trace test. Work happens in the
   Chronicle repo, not here.

## Read first

- `obsidian/V3-Platform/Architecture/ADR/v3/ADR-058-v3-observability-opentelemetry-tracing.md`
- `shared/otelnats/span.go`, `shared/otelnats/carrier.go`, `shared/otelnats/otelnats.go`
- `demos/01-dictionary/otel/collector.yaml`
- `/Users/jeremy/dev/github/linebooker/chronicle/services/lib/logger/logger.go`
- `/Users/jeremy/dev/github/linebooker/chronicle/services/lib/logger/propagation.go`
