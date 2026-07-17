# Performance harness (k6)

Load-testing harness for the Dictionary POC backend. Built in **Phase 10 —
Performance Baseline (pull-forward)**; reused in **Phase 16** for the full
suite. See `.claude/plans/Dictionary-POC-Plan.md` for scope.

> **Measurement only.** These scenarios characterise degradation curves. They
> do **not** implement mitigations (snapshotting, etc.) — those interact with
> Phases 13–15. Results go to [`../PERFORMANCE.md`](../PERFORMANCE.md).

## Prerequisites

- **k6** — `brew install k6` (macOS). Runs outside the Go stack.
- **The dockerized backend**, published on `http://localhost:18080`:

  ```bash
  # from repo root
  docker compose -f demos/01-dictionary/docker-compose.yml up --build -d
  # wait until ready
  curl -sf http://localhost:18080/healthz && echo ok
  ```

## Layout

```
perf/
  lib/
    config.js   # BASE_URL, CONTEXT, PORTS — all env-overridable
    api.js      # thin endpoint wrappers (arrive/depart/register/load/shapeCFleet…)
    ids.js      # ISO 6346 container IDs (TCKU + 7 digits), ship-id slugs
  seed.js       # optional: pre-populate a container pool + arrived ships
  scenarios/
    hydration-single-ship.js        # baseline #2 — write-side replay-per-command
    throughput-concurrent-ships.js  # raw command-throughput ceiling
    shape-c-reconstruction.js       # baseline #1 — full-replay-per-call vs depth
```

## Running

Every script polls `/healthz` in `setup()` and fails fast if the stack is down.

```bash
# optional seed
k6 run demos/01-dictionary/perf/seed.js

# baseline scenarios (smoke defaults)
k6 run demos/01-dictionary/perf/scenarios/hydration-single-ship.js
k6 run demos/01-dictionary/perf/scenarios/shape-c-reconstruction.js

# throughput: run once per concurrency level (the 10 → 500 ramp is these points)
for v in 10 100 250 500; do
  VUS=$v k6 run --summary-export=throughput-$v.json \
    demos/01-dictionary/perf/scenarios/throughput-concurrent-ships.js
done
```

Reset the event stream between scenarios for clean, independent baselines:

```bash
docker compose -f demos/01-dictionary/docker-compose.yml down -v && \
docker compose -f demos/01-dictionary/docker-compose.yml up -d
```

Capture machine-readable results for `PERFORMANCE.md`:

```bash
k6 run --summary-export=hydration.json \
  demos/01-dictionary/perf/scenarios/hydration-single-ship.js
```

### Knobs (environment variables)

| Var | Default | Applies to | Notes |
|---|---|---|---|
| `BASE_URL` | `http://localhost:18080` | all | point at a local backend if not using docker |
| `CONTEXT` | `global` | all | fleet context; `global` has auto-seeded ports |
| `MAX_EVENTS` | `2000` | hydration | set `10000` for the full 1k–10k band |
| `VUS` | `100` | throughput | concurrent command senders (run per level: 10/100/250/500) |
| `DURATION` | `45s` | throughput | hold time at that concurrency |
| `DEPTHS` | `100,1000,10000` | shape-c | stream depths to sample at |
| `SAMPLES` | `10` | shape-c | reconstruction samples per depth |
| `SHIPS` | `10` | shape-c | ship pool used to grow the stream |
| `SEED_CONTAINERS` / `SEED_SHIPS` | `20` / `5` | seed | pool sizes |

**Smoke first**, then scale up:

```bash
DEPTHS=100,500 SAMPLES=5 k6 run demos/01-dictionary/perf/scenarios/shape-c-reconstruction.js
MAX_EVENTS=200 k6 run demos/01-dictionary/perf/scenarios/hydration-single-ship.js
```

## Custom metrics to read in the summary

- `hydration_cmd_latency` — tagged by `events` band (`0000-0100` … `10000+`) and `op`.
- `throughput_cmd_latency` + `throughput_errors` — p95 latency and error rate at the run's `VUS` level (also read `http_reqs` rate for cmd/s and `http_req_failed`).
- `shape_c_recon_latency` — tagged by `depth` (`100`, `1000`, `10000`).

## Not in this harness (deferred to Phase 16)

These need architecture that does not exist yet and would be thrown away if
scripted now:

- **Optimistic-concurrency contention** — needs the Phase 13 sequence guard.
- **Cross-stream burst / consumer lag** — needs the Phase 15 `TERMINAL` stream.
- **Cross-aggregate stale-read window** — needs the Phase 15 split.
- **SSE fan-out** — the watch endpoints are streaming, not load-shaped for k6's
  request model; measured in Phase 16.
