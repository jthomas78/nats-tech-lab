package auth_test

// Phase 50a, BR-AC38 — a browser session is recorded before it is signed,
// exactly as a service credential is (accounts.Provisioner.CreateUser). The
// two differ only in kind and in having an expiry: a session self-expires on
// its TTL, so a row of its that never reaches active is swept rather than
// left for an operator.

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/auth"
)

type fakeSessionRegistry struct {
	mu          sync.Mutex
	calls       []string
	recorded    []accounts.NewUser
	recordErr   error
	activateErr error
}

func (f *fakeSessionRegistry) RecordPendingUser(_ context.Context, u accounts.NewUser) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recordErr != nil {
		return f.recordErr
	}
	f.calls = append(f.calls, "record:"+u.PublicKey)
	f.recorded = append(f.recorded, u)
	return nil
}

func (f *fakeSessionRegistry) MarkUserActive(_ context.Context, publicKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.activateErr != nil {
		return f.activateErr
	}
	f.calls = append(f.calls, "active:"+publicKey)
	return nil
}

var errSessionRegistryDown = errors.New("registry unavailable")

var _ = Describe("session registry recording (BR-AC38)", func() {
	var accountPub, accountSigningSeed string
	var registry *fakeSessionRegistry
	ctx := context.Background()

	BeforeEach(func() {
		accountKP, err := nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		accountPub, err = accountKP.PublicKey()
		Expect(err).NotTo(HaveOccurred())
		signingKP, err := nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		seed, err := signingKP.Seed()
		Expect(err).NotTo(HaveOccurred())
		accountSigningSeed = string(seed)
		registry = &fakeSessionRegistry{}
	})

	It("records a pending session carrying the JWT's own NKey and TTL, then marks it active", func() {
		info, err := auth.MintBrowserToken(ctx, registry, accountPub, accountSigningSeed, "acme", "ws://localhost:9222", 15*time.Minute)
		Expect(err).NotTo(HaveOccurred())

		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())

		Expect(registry.calls).To(Equal([]string{"record:" + claims.Subject, "active:" + claims.Subject}))
		Expect(registry.recorded).To(HaveLen(1))
		rec := registry.recorded[0]
		Expect(rec.Kind).To(Equal(accounts.UserKindSession))
		Expect(rec.Account).To(Equal("acme"))
		Expect(rec.AccountKey).To(Equal(accountPub))
		Expect(rec.Name).To(Equal("browser-acme"))
		Expect(rec.Source).To(Equal(accounts.UserSourceService))
		Expect(rec.ExpiresAt).NotTo(BeNil(), "a session is the kind that expires — that is what lets a stuck row be swept")
		Expect(*rec.ExpiresAt).To(BeTemporally("~", time.Now().Add(15*time.Minute), time.Minute))
	})

	It("records the Admin UI's own session under the platform account", func() {
		_, err := auth.MintAdminToken(ctx, registry, accountPub, accountSigningSeed, "ws://localhost:9222", 15*time.Minute)
		Expect(err).NotTo(HaveOccurred())
		Expect(registry.recorded).To(HaveLen(1))
		Expect(registry.recorded[0].Account).To(Equal("platform"))
		Expect(registry.recorded[0].Name).To(Equal("admin-app"))
	})

	It("returns no credential when the pending row cannot be written", func() {
		registry.recordErr = errSessionRegistryDown
		info, err := auth.MintBrowserToken(ctx, registry, accountPub, accountSigningSeed, "acme", "ws://localhost:9222", 15*time.Minute)
		Expect(err).To(MatchError(errSessionRegistryDown))
		Expect(info.JWT).To(BeEmpty())
	})

	It("reports a failure to activate, leaving the row pending", func() {
		registry.activateErr = errSessionRegistryDown
		_, err := auth.MintBrowserToken(ctx, registry, accountPub, accountSigningSeed, "acme", "ws://localhost:9222", 15*time.Minute)
		Expect(err).To(MatchError(errSessionRegistryDown))
		Expect(registry.calls).To(HaveLen(1))
	})

	It("refuses to mint without a registry — an unrecorded session is one nothing can account for", func() {
		_, err := auth.MintRefdataAdminToken(ctx, nil, accountPub, accountSigningSeed, "ws://localhost:9222", 15*time.Minute)
		Expect(err).To(MatchError(accounts.ErrNoUserRegistry))
	})
})
