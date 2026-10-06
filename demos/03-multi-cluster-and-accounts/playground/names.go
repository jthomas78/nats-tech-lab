package main

import (
	"strings"
	"time"
)

// One place for the rig's names, ports and timings, like demo 04's names.go.
// The servers and ports are exercises/config/t-*.conf; rig-t4.sh's http_of
// says the same thing.

const (
	serviceAddr = "127.0.0.1:20302"

	// metaPeers is the size of T4's one meta group: three clusters of three.
	metaPeers = 9
	// metaMajority is DERIVED from metaPeers, never measured: a Raft majority
	// of nine. The page labels it so.
	metaMajority = metaPeers/2 + 1

	// metaPollEvery and metaFreshFor: /raftz is read every 0.5 s, and an
	// answer is fresh for three polls.
	metaPollEvery = 500 * time.Millisecond
	metaFreshFor  = 1500 * time.Millisecond

	// gatewayPollEvery and gatewayFreshFor: /gatewayz is read once a second.
	// The same rule as the meta reading — fresh for three polls — gives 3 s.
	gatewayPollEvery = time.Second
	gatewayFreshFor  = 3 * gatewayPollEvery

	// transitionWatchFor closes a transition that saw no change, or no
	// agreement, in this long (D03-R29).
	transitionWatchFor = 15 * time.Second

	// monitorFetchFor is the HTTP client timeout for every monitor read, the
	// same 1 s as classify-10.py's FETCH_TIMEOUT_S.
	monitorFetchFor = time.Second
	// jszPollEvery and procPollEvery: meta size and process state, once a
	// second each.
	jszPollEvery  = time.Second
	procPollEvery = time.Second
)

// Command limits (rule 9). A command that outlives its limit by
// watchdogGrace is closed by the watchdog, whatever its body is doing.
const (
	watchdogGrace = time.Second

	// signalConfirmFor and signalConfirmEvery: after SIGSTOP or SIGCONT, ps
	// is read every 0.2 s until all three processes are confirmed, for at
	// most 5 s. The plan sets neither number.
	signalConfirmFor   = 5 * time.Second
	signalConfirmEvery = 200 * time.Millisecond

	// verifyRequestFor is per request (direct_get --timeout 3s); a verify
	// makes several, so the command as a whole gets verifyLimit.
	verifyRequestFor = 3 * time.Second
	verifyLimit      = 15 * time.Second

	// probeCallFor is per call (--timeout 10s); a probe makes three.
	probeCallFor = 10 * time.Second
	probeLimit   = 3 * probeCallFor

	// leaderReplyFor is the step-down request; leaderObserveFor is how long
	// the summary is then watched for a new agreed leader.
	leaderReplyFor   = 3 * time.Second
	leaderObserveFor = 10 * time.Second

	// startLimit is rig-t4.sh up's 120 s. attachLimit is the nine identity
	// checks and one /jsz read. stopWaitFor is restart's 30 s wait.
	startLimit  = 120 * time.Second
	attachLimit = 10 * time.Second
	stopWaitFor = 30 * time.Second
	stopLimit   = stopWaitFor + 5*time.Second

	// publishDefaultS is DARK_PUB_S=5, exercise 10 step 6.
	publishDefaultS = 5
)

// publishTimeoutsS are the only ack waits a publish may ask for (D03-R30).
var publishTimeoutsS = []int{1, 2, 5, 10, 30}

// viaChoices are the only client connection choices (D03-R30).
var viaChoices = []string{"auto", clusterZA, clusterArb, clusterAU}

// allowedOrigins may send a POST: the lab shell (7110) and the page's own
// dev server (20301). Exact match, never a wildcard.
var allowedOrigins = []string{
	"http://localhost:7110",
	"http://127.0.0.1:7110",
	"http://localhost:20301",
	"http://127.0.0.1:20301",
}

// streamOf is a site's stream, as exercise 10 names it: ODOMETER_ZA.
func streamOf(site string) string {
	return "ODOMETER_" + strings.ToUpper(site)
}

// subjectOf is a site's subject: evt.odo.za.v1.
func subjectOf(site string) string {
	return "evt.odo." + site + ".v1"
}

const (
	clusterZA  = "za"
	clusterArb = "arb"
	clusterAU  = "au"
)

// clusters is the triangle, in the page's order: za left, arb top, au right.
var clusters = []string{clusterZA, clusterArb, clusterAU}

// server is one T4 process. Name is the server_name and the config file's
// stem; Short is the PID file's stem (rig's start_server writes
// run/pid/<short>.pid). Client is the port a NATS command connects to.
type server struct {
	Name    string
	Short   string
	Cluster string
	Monitor int
	Client  int
}

var servers = []server{
	{"t-za-1", "za-1", clusterZA, 8231, 4231},
	{"t-za-2", "za-2", clusterZA, 8232, 4232},
	{"t-za-3", "za-3", clusterZA, 8233, 4233},
	{"t-arb-1", "arb-1", clusterArb, 8541, 4541},
	{"t-arb-2", "arb-2", clusterArb, 8542, 4542},
	{"t-arb-3", "arb-3", clusterArb, 8543, 4543},
	{"t-au-1", "au-1", clusterAU, 8241, 4241},
	{"t-au-2", "au-2", clusterAU, 8242, 4242},
	{"t-au-3", "au-3", clusterAU, 8243, 4243},
}

// serverNamed returns the server with that server_name.
func serverNamed(name string) (server, bool) {
	for _, s := range servers {
		if s.Name == name {
			return s, true
		}
	}
	return server{}, false
}

// serversIn returns one cluster's three servers, in order.
func serversIn(cluster string) []server {
	var out []server
	for _, s := range servers {
		if s.Cluster == cluster {
			out = append(out, s)
		}
	}
	return out
}

// clusterOf returns the cluster of a server_name, or "".
func clusterOf(name string) string {
	for _, s := range servers {
		if s.Name == name {
			return s.Cluster
		}
	}
	return ""
}
