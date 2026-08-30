package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/auth"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/registry"
)

// The former REST mount-split assertion, now enforced by subject grants.
// "Ungated" here means no operator credential is needed: the shell has its
// own read-only profile. Every other registered subject requires the admin.
func TestShellReadIsUngatedAndEverythingElseIsNot(t *testing.T) {
	g := NewWithT(t)
	kp, err := nkeys.CreateAccount()
	g.Expect(err).NotTo(HaveOccurred())
	pub, err := kp.PublicKey()
	g.Expect(err).NotTo(HaveOccurred())
	seed, err := kp.Seed()
	g.Expect(err).NotTo(HaveOccurred())
	shell, err := auth.MintShellToken(context.Background(), &fakeSessionRegistry{}, pub, string(seed), "/nats", 15*time.Minute)
	g.Expect(err).NotTo(HaveOccurred())
	admin, err := auth.MintAdminToken(context.Background(), &fakeSessionRegistry{}, pub, string(seed), "/nats", 15*time.Minute)
	g.Expect(err).NotTo(HaveOccurred())
	shellClaims, err := jwt.DecodeUserClaims(shell.JWT)
	g.Expect(err).NotTo(HaveOccurred())
	adminClaims, err := jwt.DecodeUserClaims(admin.JWT)
	g.Expect(err).NotTo(HaveOccurred())
	for _, subject := range registry.Subjects() {
		if subject == "api._platform.registry.frontend-plugins.read.v1" {
			g.Expect(shellClaims.Permissions.Pub.Allow).To(ContainElement(subject))
		} else {
			g.Expect(shellClaims.Permissions.Pub.Allow).NotTo(ContainElement(subject))
			g.Expect(adminClaims.Permissions.Pub.Allow).To(ContainElement(subject))
		}
	}
	g.Expect(shellClaims.Permissions.Pub.Allow).To(ConsistOf("api._platform.registry.frontend-plugins.read.v1", "_INBOX.>"))
}
