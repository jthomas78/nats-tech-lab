package main

import "time"

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
)

const (
	clusterZA  = "za"
	clusterArb = "arb"
	clusterAU  = "au"
)

// clusters is the triangle, in the page's order: za left, arb top, au right.
var clusters = []string{clusterZA, clusterArb, clusterAU}

// server is one T4 process. Name is the server_name and the config file's
// stem; Short is the PID file's stem (rig's start_server writes
// run/pid/<short>.pid).
type server struct {
	Name    string
	Short   string
	Cluster string
	Monitor int
}

var servers = []server{
	{"t-za-1", "za-1", clusterZA, 8231},
	{"t-za-2", "za-2", clusterZA, 8232},
	{"t-za-3", "za-3", clusterZA, 8233},
	{"t-arb-1", "arb-1", clusterArb, 8541},
	{"t-arb-2", "arb-2", clusterArb, 8542},
	{"t-arb-3", "arb-3", clusterArb, 8543},
	{"t-au-1", "au-1", clusterAU, 8241},
	{"t-au-2", "au-2", clusterAU, 8242},
	{"t-au-3", "au-3", clusterAU, 8243},
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
