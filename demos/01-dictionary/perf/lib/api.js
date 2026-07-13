// Thin wrappers over the Dictionary POC HTTP API.
//
// Each command endpoint returns HTTP 202 (async projection); queries return
// 200; POST /api/ports returns 201. Every call is tagged with `name` so k6
// groups per-endpoint metrics in the summary.

import http from 'k6/http';
import { sleep } from 'k6';
import { BASE_URL, CONTEXT, JSON_HEADERS } from './config.js';

function post(path, body, name) {
  return http.post(`${BASE_URL}${path}`, JSON.stringify(body), {
    headers: JSON_HEADERS,
    tags: { name },
  });
}

export function health() {
  return http.get(`${BASE_URL}/healthz`, { tags: { name: 'healthz' } });
}

// --- commands (202 Accepted) ---

export function registerPort(name) {
  return post('/api/ports', { context: CONTEXT, name }, 'registerPort');
}

export function registerContainer(containerID, cargo, originPort, destPort) {
  return post(
    '/api/containers/register',
    { context: CONTEXT, containerID, cargo, originPort, destPort },
    'registerContainer',
  );
}

export function arrive(shipID, shipName, port) {
  return post('/api/ships/arrive', { context: CONTEXT, shipID, shipName, port }, 'arrive');
}

export function depart(shipID, port) {
  return post('/api/ships/depart', { context: CONTEXT, shipID, port }, 'depart');
}

export function load(containerID, shipID) {
  return post('/api/containers/load', { context: CONTEXT, containerID, shipID }, 'load');
}

export function unload(containerID, shipID) {
  return post('/api/containers/unload', { context: CONTEXT, containerID, shipID }, 'unload');
}

// --- queries (200) ---

// Shape C — event-sourced whole-fleet reconstruction. Replays the entire
// stream from seq=1 on every call, so latency grows with stream depth. This is
// strongly consistent (unlike the async KV/Postgres projections).
export function shapeCFleet() {
  return http.get(`${BASE_URL}/api/shape-c/fleet`, { tags: { name: 'shapeCFleet' } });
}

// --- readiness ---

// Poll /healthz until it returns 200. Call from setup() so a scenario fails
// fast with a clear message if the stack isn't up.
export function waitForHealth(retries, pauseSec) {
  for (let i = 0; i < retries; i++) {
    const r = health();
    if (r.status === 200) return true;
    sleep(pauseSec);
  }
  return false;
}
