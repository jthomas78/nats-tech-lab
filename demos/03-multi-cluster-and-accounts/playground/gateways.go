package main

// The gateway arrows (D03-R32). Pure: fed each server's last /gatewayz
// answer and the time, it says the state of the six arrows of T4's full
// mesh. The arrow from A to B is read only from A's servers; B's readings
// never decide it.
//
// What `outbound_gateways` means, from nats-server v2.14.6 source
// (server/monitor.go, createOutboundsRemoteGatewayz): the map holds one entry
// per LIVE outbound gateway connection, keyed by the remote gateway's name. A
// configured gateway with no connection is absent, not listed with a null.
// So "lists B" is "has a key B".
//
// Listed is not proof that B answers: a server keeps a connection to a
// SIGSTOPped peer while the socket stays open. How long is unmeasured.

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

const (
	arrowListed    = "listed"
	arrowPartial   = "partial"
	arrowNotListed = "not_listed"
	arrowUnknown   = "unknown"

	serverListed    = "listed"
	serverNotListed = "not_listed"
	serverNoReading = "no_fresh_reading"
)

// gatewayLinks is the six directions, in the page's order: the two arb–za
// arrows, the two arb–au arrows, then the za–au pair across the regions.
var gatewayLinks = [][2]string{
	{clusterArb, clusterZA}, {clusterZA, clusterArb},
	{clusterArb, clusterAU}, {clusterAU, clusterArb},
	{clusterZA, clusterAU}, {clusterAU, clusterZA},
}

// gatewayReading is one server's last valid /gatewayz answer.
type gatewayReading struct {
	Server   string
	At       time.Time
	Outbound []string // the keys of outbound_gateways, sorted
}

// parseGatewayz reads one /gatewayz body from a server of cluster `cluster`.
// It refuses a body that is not JSON, has no outbound_gateways object, or
// names a gateway other than the server's own cluster (the wrong source).
func parseGatewayz(srv, cluster string, at time.Time, body []byte) (gatewayReading, error) {
	var g struct {
		Name     string                     `json:"name"`
		Outbound map[string]json.RawMessage `json:"outbound_gateways"`
	}
	if err := json.Unmarshal(body, &g); err != nil {
		return gatewayReading{}, fmt.Errorf("%s: /gatewayz is not JSON: %v", srv, err)
	}
	if g.Name != cluster {
		return gatewayReading{}, fmt.Errorf("%s: /gatewayz names gateway %q, not %q", srv, g.Name, cluster)
	}
	if g.Outbound == nil {
		return gatewayReading{}, fmt.Errorf("%s: /gatewayz has no outbound_gateways", srv)
	}
	r := gatewayReading{Server: srv, At: at}
	for name := range g.Outbound {
		r.Outbound = append(r.Outbound, name)
	}
	sort.Strings(r.Outbound)
	return r, nil
}

type arrowServer struct {
	Server string        `json:"server"`
	Status string        `json:"status"`
	Age    time.Duration `json:"ageNs,omitempty"` // age of the last reading; zero if never
	Never  bool          `json:"never,omitempty"`
}

type arrow struct {
	From    string        `json:"from"`
	To      string        `json:"to"`
	State   string        `json:"state"`
	Listed  int           `json:"listed"`
	Fresh   int           `json:"fresh"`
	Tag     string        `json:"tag"`
	Servers []arrowServer `json:"servers"`
}

// gatewayArrows returns the six arrows. last holds each server's last valid
// reading, keyed by server name; a missing key is never answered.
func gatewayArrows(last map[string]gatewayReading, now time.Time) []arrow {
	out := make([]arrow, 0, len(gatewayLinks))
	for _, link := range gatewayLinks {
		from, to := link[0], link[1]
		a := arrow{From: from, To: to}
		for _, s := range serversIn(from) {
			r, ok := last[s.Name]
			as := arrowServer{Server: s.Name}
			switch {
			case !ok:
				as.Status, as.Never = serverNoReading, true
			case now.Sub(r.At) >= gatewayFreshFor:
				as.Status, as.Age = serverNoReading, now.Sub(r.At)
			default:
				a.Fresh++
				as.Age = now.Sub(r.At)
				as.Status = serverNotListed
				if contains(r.Outbound, to) {
					as.Status = serverListed
					a.Listed++
				}
			}
			a.Servers = append(a.Servers, as)
		}
		n := len(a.Servers)
		switch {
		case a.Fresh == 0:
			a.State = arrowUnknown
			a.Tag = fmt.Sprintf("%s → %s no reading", from, to)
		case a.Fresh == n && a.Listed == n:
			a.State = arrowListed
		case a.Fresh == n && a.Listed == 0:
			a.State = arrowNotListed
		default:
			a.State = arrowPartial
		}
		if a.State != arrowUnknown {
			a.Tag = fmt.Sprintf("%s → %s %d/%d", from, to, a.Listed, n)
		}
		out = append(out, a)
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
