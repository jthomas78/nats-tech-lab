package main

import (
	"context"
	"log/slog"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAnnouncePlugin(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Announce Plugin CLI Suite")
}

var _ = Describe("announce-plugin wrapper", func() {
	It("fails through the shared configuration contract instead of owning a second implementation", func() {
		for _, key := range []string{"NATS_CREDS_PATH", "PLUGIN_MANIFEST_PATH", "PUBLISHER_SIGNING_SEED_PATH", "RELEASE_STATE_PATH", "PUBLISHER_ID", "PLUGIN_PUBLIC_ORIGIN"} {
			GinkgoT().Setenv(key, "")
		}
		Expect(run(context.Background(), slog.Default())).To(MatchError("NATS_CREDS_PATH is required"))
	})
})
