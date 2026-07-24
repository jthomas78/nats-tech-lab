// Scenario: Shape C fleet reconstruction vs stream depth.
//
// Gap under test (Phase 10 baseline #1): GET /api/shape-c/fleet replays the
// entire stream from seq=1 on every call, so reconstruction latency grows with
// stream depth. This scenario grows the stream in stages and samples the
// reconstruction latency at each target depth to plot the degradation curve.
//
//   k6 run perf/scenarios/shape-c-reconstruction.js                 # 100/1k/10k
//   DEPTHS=100,500 SAMPLES=10 k6 run perf/scenarios/shape-c-reconstruction.js  # smoke
//
// A single VU appends events by round-robin arrive/depart across a small ship
// pool (per-ship docked tracking keeps every command valid), then samples
// /api/shape-c/fleet SAMPLES times at each depth.

import { check } from 'k6';
import { Trend } from 'k6/metrics';
import { arrive, depart, shapeCFleet, waitForHealth } from '../lib/api.js';
import { PORTS } from '../lib/config.js';
import { shipID } from '../lib/ids.js';

const DEPTHS = (__ENV.DEPTHS || '100,1000,10000').split(',').map(Number);
const SAMPLES = Number(__ENV.SAMPLES || 10);
const SHIPS = Number(__ENV.SHIPS || 10);

const reconLatency = new Trend('shape_c_recon_latency', true);

// Permissive per-depth thresholds (always pass) exist only to make k6 report a
// sub-metric breakdown per depth in the summary — otherwise the summary shows
// just the aggregate across all depths.
const thresholds = {};
for (const d of DEPTHS) thresholds[`shape_c_recon_latency{depth:${d}}`] = ['p(95)>=0'];

export const options = {
  // Variant id (see obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md "Shape Classification") tagged on every
  // metric so results are comparable across implementations/phases.
  tags: { shape: 'Read.FR.AGG' },
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  thresholds,
  scenarios: {
    reconstruction: {
      executor: 'shared-iterations',
      vus: 1,
      iterations: 1,
      maxDuration: '60m',
    },
  },
};

export function setup() {
  if (!waitForHealth(30, 2)) throw new Error('backend /healthz never came up — is the stack up?');
}

export default function () {
  const docked = new Array(SHIPS).fill(false); // per-ship state so every op is valid
  let events = 0;
  let step = 0;

  for (const depth of DEPTHS.sort((a, b) => a - b)) {
    // Grow the stream until it reaches this target depth.
    while (events < depth) {
      const idx = step % SHIPS;
      const ship = shipID('recon-ship', idx);
      const port = PORTS[idx % PORTS.length];
      const res = docked[idx] ? depart(ship, port) : arrive(ship, `Recon ${idx}`, port);
      if (res.status === 202) {
        docked[idx] = !docked[idx];
        events++;
      } else {
        console.error(`grow-to-${depth} cmd failed: ${res.status} ${res.body}`);
      }
      step++;
    }

    // Sample reconstruction latency at the current depth.
    for (let k = 0; k < SAMPLES; k++) {
      const r = shapeCFleet();
      check(r, { 'shape-c 200': (x) => x.status === 200 });
      reconLatency.add(r.timings.duration, { depth: String(depth) });
    }
    console.log(`sampled Shape C reconstruction at ~${events} events`);
  }
}
