// Scenario: raw command-throughput ceiling (many ships concurrently).
//
// Measures the command pipeline's sustained throughput and latency at a fixed
// concurrency level, isolated from single-ship hydration cost: each iteration
// uses a FRESH ship id, so every ship's history stays 1–2 events deep and the
// signal is the pipeline ceiling, not replay depth.
//
// Run once per concurrency level (the plan's 10 → 500 ramp is these points):
//   for v in 10 100 250 500; do
//     VUS=$v k6 run --summary-export=throughput-$v.json \
//       perf/scenarios/throughput-concurrent-ships.js
//   done
//
// Each run's http_req_duration p95, http_reqs rate, and http_req_failed are
// that level's row in the PERFORMANCE.md throughput table.

import { check } from 'k6';
import { Rate, Trend } from 'k6/metrics';
import { arrive, depart, waitForHealth } from '../lib/api.js';
import { PORTS } from '../lib/config.js';

const VUS = Number(__ENV.VUS || 100);
const DURATION = __ENV.DURATION || '45s';

const errorRate = new Rate('throughput_errors');
const latency = new Trend('throughput_cmd_latency', true);

export const options = {
  // Variant id (see ARCHITECTURE.md "Shape Classification"). The workload is
  // the Write.FR command path; note the observed ceiling is Proj.PG-bound
  // (async projection connection limit — see PERFORMANCE.md).
  tags: { shape: 'Write.FR' },
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  scenarios: {
    load: { executor: 'constant-vus', vus: VUS, duration: DURATION },
  },
};

export function setup() {
  if (!waitForHealth(30, 2)) throw new Error('backend /healthz never came up — is the stack up?');
}

export default function () {
  // Fresh ship every iteration → histories stay shallow → measures pipeline,
  // not hydrate replay. Unique across VUs and iterations.
  const ship = `thr-${VUS}-${__VU}-${__ITER}`;
  const port = PORTS[__VU % PORTS.length];

  const a = arrive(ship, `Throughput ${__VU}`, port);
  latency.add(a.timings.duration, { op: 'arrive' });
  errorRate.add(a.status !== 202);
  check(a, { 'arrive 202': (r) => r.status === 202 });

  const d = depart(ship, port);
  latency.add(d.timings.duration, { op: 'depart' });
  errorRate.add(d.status !== 202);
  check(d, { 'depart 202': (r) => r.status === 202 });
}
