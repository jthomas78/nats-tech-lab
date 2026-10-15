package otelnats_test

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

const (
	specExportTimeout   = 200 * time.Millisecond
	specShutdownTimeout = 1 * time.Second
	// slack covers scheduling noise on a busy laptop; the bound under test is
	// the configured timeout, not this.
	slack = 500 * time.Millisecond
)

// refusedEndpoint returns a URL on a port nothing listens on: the Collector
// is stopped.
func refusedEndpoint() string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	addr := l.Addr().String()
	Expect(l.Close()).To(Succeed())
	return "http://" + addr
}

// hungEndpoint returns a URL on a listener that accepts connections and never
// answers: the Collector is paused (docker pause). accepted counts the
// connections, so a spec can prove an export was really attempted.
func hungEndpoint() (string, *atomic.Int32) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	var accepted atomic.Int32
	var mu sync.Mutex
	var held []net.Conn
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			mu.Lock()
			held = append(held, c) // keep it open, say nothing
			mu.Unlock()
		}
	}()
	DeferCleanup(func() {
		_ = l.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			_ = c.Close()
		}
	})
	return "http://" + l.Addr().String(), &accepted
}

func otlpProvider(endpoint string) *otelnats.Provider {
	p, err := otelnats.Init(context.Background(), otelnats.Config{
		ServiceName:     "organizations-service",
		Endpoint:        endpoint,
		ExportTimeout:   specExportTimeout,
		ShutdownTimeout: specShutdownTimeout,
	})
	Expect(err).NotTo(HaveOccurred())
	return p
}

// endSpans starts and ends n spans and returns how long that took.
func endSpans(p *otelnats.Provider, n int) time.Duration {
	start := time.Now()
	for i := 0; i < n; i++ {
		_, span := p.StartClient(context.Background(), "refdata.item.get.v1", "refdata")
		span.End()
	}
	return time.Since(start)
}

var _ = Describe("BR-066: a dead Collector never holds up a service", func() {
	Context("BR-066: Collector stopped (connection refused)", func() {
		It("does not block span ends, and Shutdown returns within its timeout", func() {
			p := otlpProvider(refusedEndpoint())

			Expect(endSpans(p, 200)).To(BeNumerically("<", 100*time.Millisecond))

			start := time.Now()
			_ = p.Shutdown(context.Background()) // an export error is expected; the bound is the rule
			Expect(time.Since(start)).To(BeNumerically("<", specShutdownTimeout+slack))
		})
	})

	Context("BR-066: Collector hung (accepts, never answers)", func() {
		It("does not block span ends, really tries to export, and Shutdown returns within its timeout", func() {
			endpoint, accepted := hungEndpoint()
			p := otlpProvider(endpoint)

			Expect(endSpans(p, 200)).To(BeNumerically("<", 100*time.Millisecond))

			start := time.Now()
			// No deadline on this context: the provider's own ShutdownTimeout
			// must bound it, because a service's stop path may not set one.
			_ = p.Shutdown(context.Background())
			Expect(time.Since(start)).To(BeNumerically("<", specShutdownTimeout+slack))

			Expect(accepted.Load()).To(BeNumerically(">", 0),
				"the exporter must have connected; a provider that never exports would pass the timing checks trivially")
		})
	})

	Context("BR-066: configuration", func() {
		It("rejects a config with no service name", func() {
			_, err := otelnats.Init(context.Background(), otelnats.Config{Endpoint: "http://127.0.0.1:4318"})
			Expect(err).To(HaveOccurred())
		})

		It("rejects a config with neither an endpoint nor an exporter", func() {
			_, err := otelnats.Init(context.Background(), otelnats.Config{ServiceName: "organizations-service"})
			Expect(err).To(HaveOccurred())
		})
	})
})
