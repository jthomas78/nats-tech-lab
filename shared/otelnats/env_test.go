package otelnats_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

var _ = Describe("BR-066: InitFromEnv is the one switch onto the OTel path", func() {
	It("returns no Provider, and no error, when OTEL_EXPORTER_OTLP_ENDPOINT is unset", func() {
		GinkgoT().Setenv(otelnats.EndpointEnv, "")
		p, err := otelnats.InitFromEnv(context.Background(), "organizations-service")
		Expect(err).NotTo(HaveOccurred())
		Expect(p).To(BeNil())
	})

	It("builds a Provider for the endpoint when it is set", func() {
		GinkgoT().Setenv(otelnats.EndpointEnv, refusedEndpoint())
		p, err := otelnats.InitFromEnv(context.Background(), "organizations-service")
		Expect(err).NotTo(HaveOccurred())
		Expect(p).NotTo(BeNil())
		_ = p.Shutdown(context.Background())
	})

	It("lets a caller defer Shutdown on a nil Provider", func() {
		var p *otelnats.Provider
		Expect(p.Shutdown(context.Background())).To(Succeed())
	})
})
