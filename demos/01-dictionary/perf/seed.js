// Standalone seed script — pre-populates the stack with a container pool and a
// set of arrived (docked) ships, so scenarios that read a populated fleet have
// something to work with. Safe to re-run: duplicate registrations return 422
// (BR-015) and already-docked arrivals return 422 (BR-001/002) — both tolerated.
//
//   k6 run perf/seed.js
//   SEED_CONTAINERS=100 SEED_SHIPS=20 k6 run perf/seed.js
//
// Respects the domain ordering (BUSINESS_RULES.md): ports are auto-seeded by
// the backend for the `global` context, containers are registered at a known
// origin/dest, and each ship is arrived at a registered port.

import { check } from 'k6';
import { registerContainer, arrive, waitForHealth } from './lib/api.js';
import { PORTS } from './lib/config.js';
import { containerID, shipID } from './lib/ids.js';

const SEED_CONTAINERS = Number(__ENV.SEED_CONTAINERS || 20);
const SEED_SHIPS = Number(__ENV.SEED_SHIPS || 5);

export const options = { vus: 1, iterations: 1 };

export function setup() {
  if (!waitForHealth(30, 2)) {
    throw new Error(`backend /healthz never returned 200 — is the stack up?`);
  }
}

export default function () {
  let containers = 0;
  for (let i = 0; i < SEED_CONTAINERS; i++) {
    const origin = PORTS[i % PORTS.length];
    const dest = PORTS[(i + 1) % PORTS.length];
    const res = registerContainer(containerID(i), 'General', origin, dest);
    // 202 = registered, 422 = already exists (re-run) — both acceptable.
    if (check(res, { 'container registered or exists': (r) => r.status === 202 || r.status === 422 })) {
      if (res.status === 202) containers++;
    }
  }

  let ships = 0;
  for (let i = 0; i < SEED_SHIPS; i++) {
    const port = PORTS[i % PORTS.length];
    const res = arrive(shipID('seed-ship', i), `Seed Ship ${i}`, port);
    if (check(res, { 'ship arrived or already docked': (r) => r.status === 202 || r.status === 422 })) {
      if (res.status === 202) ships++;
    }
  }

  console.log(`seed complete: +${containers} containers, +${ships} ships arrived`);
}
