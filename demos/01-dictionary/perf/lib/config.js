// Shared configuration for the Dictionary POC k6 performance harness.
//
// Every value is overridable via an environment variable so the same scripts
// run unchanged against the dockerized stack (the default target) or a
// locally-run backend. See perf/README.md for the knobs each scenario adds.

// Backend HTTP API. docker-compose publishes the backend on host port 7200.
export const BASE_URL = __ENV.BASE_URL || 'http://localhost:7200';

// Fleet context — the KV-bucket qualifier sent as the `context` field/param.
// `global` is chosen because the backend auto-seeds its ports on startup
// (backend migrate.go seedDefaultPorts), so scenarios need no port registration
// and BR-017/BR-018 (arrive/register must reference a known port) are satisfied.
export const CONTEXT = __ENV.CONTEXT || 'global';

// Ports auto-seeded for the `global` / `atlantic-fleet` / `pacific-fleet`
// contexts. Any of these is a valid arrive/depart target out of the box.
export const PORTS = ['Hamburg', 'Rotterdam', 'Singapore', 'New York', 'Shanghai', 'Sydney'];

export const JSON_HEADERS = { 'Content-Type': 'application/json' };
