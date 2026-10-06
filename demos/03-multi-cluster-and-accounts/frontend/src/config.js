// Where the control service is.
//
// Standalone, the page is served on 20301 and calls 20302 directly; the
// service grants that one origin. Embedded in `lab-shell`, the same absolute
// URL is cross-origin, so the embedded entry moves the base onto the shell's
// own origin — see `setCommandApi`, as demo 04. `let`, not `const`: an ES
// module export is a live binding, and every call site reads it per call.
export let COMMAND_API = import.meta.env.VITE_COMMAND_API ?? 'http://127.0.0.1:20302'

// This demo's directory name: the key of the shell's `/demo-api/` route.
export const DEMO_NAME = '03-multi-cluster-and-accounts'

// The shell's public path layout for a demo's declared API routes (app-shell
// BR-AS82). `plugin.spec.js` asserts the value.
export const EMBEDDED_COMMAND_API = `/demo-api/${DEMO_NAME}`

/**
 * Point the command API at a different base. Called once by the embedded
 * entry, before anything renders.
 *
 * @param {string} base An origin or an absolute path, with no trailing slash.
 */
export function setCommandApi(base) {
  COMMAND_API = base
}

// How often the page asks for /state. The service polls the monitors every
// 0.5 s, so asking faster shows nothing new.
export const POLL_MS = 500

// A reading younger than this is fresh. Must match `metaFreshFor` in
// playground/names.go; the service decides, this is only the label.
export const FRESH_S = 1.5

// The rig, fixed by the T4 configs in exercises/config/.
export const CLUSTERS = ['za', 'arb', 'au']
export const ROLE = { za: 'region', arb: 'hub', au: 'region' }
export const STREAM = { za: 'ODOMETER_ZA', arb: 'ODOMETER_ARB', au: 'ODOMETER_AU' }
export const MONITOR_PORT = {
  't-za-1': 8231, 't-za-2': 8232, 't-za-3': 8233,
  't-arb-1': 8541, 't-arb-2': 8542, 't-arb-3': 8543,
  't-au-1': 8241, 't-au-2': 8242, 't-au-3': 8243,
}

// The two client settings and their allowed values. The service refuses
// anything else with 400.
export const VIA_CHOICES = ['auto', 'za', 'arb', 'au']
export const TIMEOUT_CHOICES = [1, 2, 5, 10, 30]
