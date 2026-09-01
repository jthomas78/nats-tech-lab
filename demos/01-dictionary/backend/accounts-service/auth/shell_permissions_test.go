package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/auth"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

// The former REST mount-split assertion, now enforced by subject grants —
// and, since the split, across a service boundary: mfe-registry-service
// serves these subjects, this service grants them, and shared/mferegistry is
// the single list both read so the two cannot drift.
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
	// The shell's half of the surface is its two READS. Everything else in
	// the exhaustive list is the operator's, and the loop is what makes a
	// subject added without a decision about who may reach it fail here
	// rather than ship open.
	shellReads := map[string]bool{mferegistry.ShellRead: true, mferegistry.HealthRead: true}
	for _, subject := range mferegistry.Subjects() {
		if shellReads[subject] {
			g.Expect(shellClaims.Permissions.Pub.Allow).To(ContainElement(subject))
		} else {
			g.Expect(shellClaims.Permissions.Pub.Allow).NotTo(ContainElement(subject))
			g.Expect(adminClaims.Permissions.Pub.Allow).To(ContainElement(subject))
		}
	}
	// Two reads and nothing else. Health is a SECOND read rather than a field
	// on the first because the two have different lifetimes: the catalogue
	// changes when an operator curates, health changes every few seconds, and
	// a shell that had to re-read the catalogue to learn a service was down
	// would be re-reading signed manifests on a five-second timer (BR-AS65).
	g.Expect(shellClaims.Permissions.Pub.Allow).To(ConsistOf(mferegistry.ShellRead, mferegistry.HealthRead, "_INBOX.>"))
	g.Expect(shellClaims.Permissions.Sub.Allow).To(ContainElement(mferegistry.HealthChanged))
	// The backend readiness probe is the registry's outbound request, and no
	// browser profile carries it — an operator's included (BR-AS62).
	g.Expect(shellClaims.Permissions.Pub.Allow).NotTo(ContainElement(mferegistry.ServiceReady("shipping-service")))
	g.Expect(adminClaims.Permissions.Pub.Allow).NotTo(ContainElement(mferegistry.ServiceReady("shipping-service")))
	g.Expect(mferegistry.Subjects()).NotTo(ContainElement(mferegistry.ServiceReady("shipping-service")))
	// Announce is service-to-service, outside both browser profiles and the
	// exhaustive browser surface. Exact grants above also exclude wildcards.
	g.Expect(mferegistry.Subjects()).NotTo(ContainElement(mferegistry.Announce))
	g.Expect(shellClaims.Permissions.Pub.Allow).NotTo(ContainElement(mferegistry.Announce))
	g.Expect(adminClaims.Permissions.Pub.Allow).NotTo(ContainElement(mferegistry.Announce))
}
