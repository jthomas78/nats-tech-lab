package main

import (
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// gatewayzBody builds a /gatewayz answer in nats-server v2.14.6's shape
// (server/monitor.go: Gatewayz, RemoteGatewayz). Built from the source's
// structs, not captured from a live server: step 5's by-hand check is where
// a live answer is first read.
func gatewayzBody(name string, outbound ...string) []byte {
	out := map[string]any{}
	for _, o := range outbound {
		out[o] = map[string]any{
			"configured": true,
			"connection": map[string]any{"cid": 7, "ip": "127.0.0.1", "port": 7541, "name": "N" + o},
		}
	}
	b, err := json.Marshal(map[string]any{
		"server_id":         "NSERVER",
		"now":               "2026-10-06T09:00:00Z",
		"name":              name,
		"host":              "127.0.0.1",
		"port":              7231,
		"outbound_gateways": out,
		"inbound_gateways":  map[string]any{},
	})
	Expect(err).NotTo(HaveOccurred())
	return b
}

var _ = Describe("the gateway arrows (D03-R32)", func() {
	now := time.Unix(1_800_000_000, 0)

	// meshAt is every server's reading at time at, each listing the other
	// two clusters, as a healthy T4 mesh would.
	meshAt := func(at time.Time) map[string]gatewayReading {
		last := map[string]gatewayReading{}
		for _, s := range servers {
			var others []string
			for _, c := range clusters {
				if c != s.Cluster {
					others = append(others, c)
				}
			}
			r, err := parseGatewayz(s.Name, s.Cluster, at, gatewayzBody(s.Cluster, others...))
			Expect(err).NotTo(HaveOccurred())
			last[s.Name] = r
		}
		return last
	}

	find := func(arrows []arrow, from, to string) arrow {
		for _, a := range arrows {
			if a.From == from && a.To == to {
				return a
			}
		}
		Fail("no arrow " + from + " → " + to)
		return arrow{}
	}

	It("draws six arrows, one per direction, in the page's order", func() {
		arrows := gatewayArrows(meshAt(now), now)

		var dirs []string
		for _, a := range arrows {
			dirs = append(dirs, a.From+"→"+a.To)
		}
		Expect(dirs).To(Equal([]string{"arb→za", "za→arb", "arb→au", "au→arb", "za→au", "au→za"}))
		for _, a := range arrows {
			Expect(a.State).To(Equal(arrowListed))
			Expect(a.Listed).To(Equal(3))
		}
		Expect(find(arrows, clusterZA, clusterAU).Tag).To(Equal("za → au 3/3"))
	})

	It("is unknown from a cluster with no fresh reading, while the far side still lists it", func() {
		// za dark: its last readings are 10 s old. arb and au still list za —
		// listed is not proof that za answers.
		last := meshAt(now)
		for _, s := range serversIn(clusterZA) {
			r := last[s.Name]
			r.At = now.Add(-10 * time.Second)
			last[s.Name] = r
		}
		arrows := gatewayArrows(last, now)

		Expect(find(arrows, clusterZA, clusterArb).State).To(Equal(arrowUnknown))
		Expect(find(arrows, clusterZA, clusterAU).State).To(Equal(arrowUnknown))
		Expect(find(arrows, clusterZA, clusterAU).Tag).To(Equal("za → au no reading"))
		Expect(find(arrows, clusterArb, clusterZA).State).To(Equal(arrowListed))
		Expect(find(arrows, clusterAU, clusterZA).State).To(Equal(arrowListed))

		za := find(arrows, clusterZA, clusterAU).Servers
		Expect(za).To(HaveLen(3))
		for _, s := range za {
			Expect(s.Status).To(Equal(serverNoReading))
			Expect(s.Age).To(Equal(10 * time.Second))
		}
	})

	It("never reads an arrow from the far side's servers", func() {
		// Only za answers. za → au is decided by za alone; au → za is unknown.
		last := map[string]gatewayReading{}
		for k, v := range meshAt(now) {
			if clusterOf(k) == clusterZA {
				last[k] = v
			}
		}
		arrows := gatewayArrows(last, now)

		Expect(find(arrows, clusterZA, clusterAU).State).To(Equal(arrowListed))
		au := find(arrows, clusterAU, clusterZA)
		Expect(au.State).To(Equal(arrowUnknown))
		for _, s := range au.Servers {
			Expect(s.Never).To(BeTrue())
		}
	})

	It("is partial when some of the origin's fresh servers do not list the far side", func() {
		last := meshAt(now)
		r, err := parseGatewayz("t-za-2", clusterZA, now, gatewayzBody(clusterZA, clusterArb))
		Expect(err).NotTo(HaveOccurred())
		last["t-za-2"] = r

		a := find(gatewayArrows(last, now), clusterZA, clusterAU)
		Expect(a.State).To(Equal(arrowPartial))
		Expect(a.Tag).To(Equal("za → au 2/3"))
		Expect(a.Servers[1]).To(Equal(arrowServer{Server: "t-za-2", Status: serverNotListed}))
	})

	It("is partial when only some of the origin's servers answer fresh", func() {
		last := meshAt(now)
		delete(last, "t-za-3")

		a := find(gatewayArrows(last, now), clusterZA, clusterAU)
		Expect(a.State).To(Equal(arrowPartial))
		Expect(a.Fresh).To(Equal(2))
		Expect(a.Tag).To(Equal("za → au 2/3"))
	})

	It("is not listed when three fresh servers list none", func() {
		last := meshAt(now)
		for _, s := range serversIn(clusterZA) {
			r, err := parseGatewayz(s.Name, clusterZA, now, gatewayzBody(clusterZA, clusterArb))
			Expect(err).NotTo(HaveOccurred())
			last[s.Name] = r
		}

		a := find(gatewayArrows(last, now), clusterZA, clusterAU)
		Expect(a.State).To(Equal(arrowNotListed))
		Expect(a.Tag).To(Equal("za → au 0/3"))
	})

	It("treats a reading exactly three polls old as no fresh reading", func() {
		last := meshAt(now.Add(-gatewayFreshFor))
		Expect(find(gatewayArrows(last, now), clusterZA, clusterAU).State).To(Equal(arrowUnknown))

		last = meshAt(now.Add(-gatewayFreshFor + time.Millisecond))
		Expect(find(gatewayArrows(last, now), clusterZA, clusterAU).State).To(Equal(arrowListed))
	})

	Describe("parsing /gatewayz", func() {
		It("refuses an answer that names another gateway — the wrong source", func() {
			_, err := parseGatewayz("t-za-1", clusterZA, now, gatewayzBody(clusterAU, clusterZA))
			Expect(err).To(MatchError(ContainSubstring(`names gateway "au", not "za"`)))
		})

		It("refuses a body that is not JSON", func() {
			_, err := parseGatewayz("t-za-1", clusterZA, now, []byte("<html>"))
			Expect(err).To(MatchError(ContainSubstring("not JSON")))
		})

		It("refuses an answer with no outbound_gateways", func() {
			_, err := parseGatewayz("t-za-1", clusterZA, now, []byte(`{"name":"za"}`))
			Expect(err).To(MatchError(ContainSubstring("no outbound_gateways")))
		})

		It("accepts an empty outbound map as listing nothing", func() {
			r, err := parseGatewayz("t-za-1", clusterZA, now, []byte(`{"name":"za","outbound_gateways":{}}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(r.Outbound).To(BeEmpty())
		})
	})
})
