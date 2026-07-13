// Scenario: single-ship write-side hydration degradation.
//
// Gap under test (Phase 10 baseline #2): hydrate() in commands.go replays ALL
// prior events for a ship on every command before validating and publishing.
// The HTTP command latency therefore includes the replay cost, which grows
// with the ship's history.
//
// Method: one VU drives an arrive/depart toggle against ONE ship, strictly
// sequentially. Command N replays N prior events, so latency bucketed by the
// current event count reveals the degradation curve.
//
//   k6 run perf/scenarios/hydration-single-ship.js                 # smoke (2k events)
//   MAX_EVENTS=10000 k6 run perf/scenarios/hydration-single-ship.js # full curve
//
// The arrive/depart toggle on a single port is always valid: after a depart the
// ship is at sea, so arriving the same port again does not trip BR-001.

import { check } from 'k6';
import { Trend, Counter } from 'k6/metrics';
import { arrive, depart, waitForHealth } from '../lib/api.js';
import { PORTS } from '../lib/config.js';

// Default 2000 keeps a smoke run to a few minutes; set 10000 for the full
// 1k–10k degradation band the plan calls for.
const MAX_EVENTS = Number(__ENV.MAX_EVENTS || 2000);
const SHIP = __ENV.SHIP_ID || 'hydration-ship';

const latency = new Trend('hydration_cmd_latency', true);
const errors = new Counter('hydration_errors');

// Prior-event bands that will actually contain data given MAX_EVENTS.
const ALL_BANDS = ['0000-0100', '0100-1000', '1000-10000', '10000+'];
const BAND_MIN = { '0000-0100': 0, '0100-1000': 100, '1000-10000': 1000, '10000+': 10000 };
// Permissive per-band thresholds (always pass) force a per-band breakdown in the
// summary; only bands MAX_EVENTS actually reaches are declared.
const thresholds = {};
for (const b of ALL_BANDS) {
  if (MAX_EVENTS > BAND_MIN[b]) thresholds[`hydration_cmd_latency{events:${b}}`] = ['p(95)>=0'];
}

export const options = {
  // Variant id (see ARCHITECTURE.md "Shape Classification") tagged on every
  // metric so results are comparable across implementations/phases.
  tags: { shape: 'Write.FR' },
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  thresholds,
  scenarios: {
    hydration: {
      executor: 'shared-iterations',
      vus: 1, // single VU → strictly sequential → monotonically growing history
      iterations: MAX_EVENTS,
      maxDuration: '30m',
    },
  },
};

export function setup() {
  if (!waitForHealth(30, 2)) throw new Error('backend /healthz never came up — is the stack up?');
}

// Bucket the prior-event count so the summary shows latency per depth band.
function band(events) {
  if (events < 100) return '0000-0100';
  if (events < 1000) return '0100-1000';
  if (events < 10000) return '1000-10000';
  return '10000+';
}

export default function () {
  const priorEvents = __ITER; // 0-based command index == events already on this ship
  const b = band(priorEvents);
  const op = priorEvents % 2 === 0 ? 'arrive' : 'depart';

  const res = op === 'arrive' ? arrive(SHIP, 'Hydration Ship', PORTS[0]) : depart(SHIP, PORTS[0]);

  latency.add(res.timings.duration, { events: b, op });
  if (!check(res, { 'status 202': (r) => r.status === 202 })) {
    errors.add(1, { events: b });
    console.error(`cmd ${priorEvents} (${op}) failed: ${res.status} ${res.body}`);
  }
}
