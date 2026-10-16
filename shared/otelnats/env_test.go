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

var _ = Describe("BR-036: OTEL_CAPTURE_BODIES is the per-service opt-in for body capture", func() {
	initWith := func(capture string) (*otelnats.Provider, error) {
		GinkgoT().Setenv(otelnats.EndpointEnv, refusedEndpoint())
		GinkgoT().Setenv(otelnats.CaptureBodiesEnv, capture)
		p, err := otelnats.InitFromEnv(context.Background(), "organizations-service")
		if p != nil {
			DeferCleanup(p.Shutdown, context.Background())
		}
		return p, err
	}

	It("leaves capture off when the variable is unset", func() {
		p, err := initWith("")
		Expect(err).NotTo(HaveOccurred())
		Expect(p.CapturesBodies()).To(BeFalse())
	})

	It("turns capture on when the variable is true", func() {
		p, err := initWith("true")
		Expect(err).NotTo(HaveOccurred())
		Expect(p.CapturesBodies()).To(BeTrue())
	})

	It("leaves capture off when the variable is false", func() {
		p, err := initWith("false")
		Expect(err).NotTo(HaveOccurred())
		Expect(p.CapturesBodies()).To(BeFalse())
	})

	It("fails to start on a value that is not a boolean, never guessing", func() {
		p, err := initWith("yes please")
		Expect(err).To(MatchError(ContainSubstring(otelnats.CaptureBodiesEnv)))
		Expect(p).To(BeNil())
	})

	It("reports capture off on a nil Provider", func() {
		var p *otelnats.Provider
		Expect(p.CapturesBodies()).To(BeFalse())
	})
})
