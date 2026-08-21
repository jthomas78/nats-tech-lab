package organizations_test

import (
	"context"
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
)

const trackedSecret = "sk-live-SUPERSECRET-9f3a"

type recordingSecretStore struct {
	puts    []string // keys, in order
	values  [][]byte
	failPut bool
}

func (s *recordingSecretStore) Put(_ context.Context, key string, payload []byte) error {
	if s.failPut {
		return errors.New("kv unavailable")
	}
	s.puts = append(s.puts, key)
	s.values = append(s.values, payload)
	return nil
}

type stubResolver struct{ store commands.SecretStore }

func (r stubResolver) SecretStore(string) (commands.SecretStore, error) { return r.store, nil }

type recordingCredRepo struct {
	upserts []domain.TrackingCredential
	fail    bool
}

func (r *recordingCredRepo) UpsertTrackingCredential(_ context.Context, _ string, c domain.TrackingCredential) error {
	if r.fail {
		return errors.New("postgres unavailable")
	}
	r.upserts = append(r.upserts, c)
	return nil
}
func (r *recordingCredRepo) ListTrackingCredentials(context.Context, string) ([]domain.TrackingCredential, error) {
	return nil, nil
}

type recordingProfiles struct {
	events []profiledomain.Event
	fail   bool
}

func (p *recordingProfiles) ConfigureTrackingCredential(_ context.Context, contextKey, id, provider, credentialType string) (profiledomain.State, error) {
	if p.fail {
		return profiledomain.State{}, errors.New("append failed")
	}
	p.events = append(p.events, profiledomain.Event{
		Type: profiledomain.TrackingCredentialConfiguredEvent, Context: contextKey,
		OrganizationID: id, Provider: provider, CredentialType: credentialType,
	})
	return profiledomain.State{}, nil
}

// ProfileCommands makes the fake its own resolver — the appender is
// per-tenant in production because the TRANSPORTER stream lives inside the
// tenant's NATS account.
func (p *recordingProfiles) ProfileCommands(string) (commands.ProfileEventAppender, error) {
	return p, nil
}

var _ = Describe("TrackingCredential write ordering (BR-TP52-BR-TP55)", func() {
	var (
		ctx      context.Context
		partners *fakePartnerRepo
		store    *recordingSecretStore
		creds    *recordingCredRepo
		profiles *recordingProfiles
		h        *commands.TrackingCredentialHandler
		partner  domain.Organization
	)

	BeforeEach(func() {
		ctx = context.Background()
		partners = newFakePartnerRepo()
		store = &recordingSecretStore{}
		creds = &recordingCredRepo{}
		profiles = &recordingProfiles{}
		var regErr error
		partner, regErr = partners.Register(ctx, domain.Organization{
			ID: "tp-1", Name: "Test Haulage", Type: domain.PartnerTypeTransporter, Context: "acme-default",
		})
		Expect(regErr).NotTo(HaveOccurred())
		h = commands.NewTrackingCredentialHandler(partners, creds, stubResolver{store}, profiles)
	})

	configure := func() (domain.TrackingCredential, error) {
		return h.ConfigureTrackingCredential(ctx, partner.ID, "acme",
			domain.ProviderCartrack, domain.CredentialTypeAPIKey, []byte(trackedSecret))
	}

	Context("BR-TP53: the payload is stored before anything references it", func() {
		It("writes the sealed payload, then the record, then the event", func() {
			_, err := configure()
			Expect(err).NotTo(HaveOccurred())

			Expect(store.puts).To(HaveLen(1))
			Expect(store.puts[0]).To(Equal(
				domain.TrackingCredentialSecretKey(partner.Context, partner.ID, domain.ProviderCartrack)))
			Expect(creds.upserts).To(HaveLen(1))
			Expect(profiles.events).To(HaveLen(1))
		})

		It("appends no event when the payload could not be stored", func() {
			// The failure that must never happen is an immutable log
			// asserting a credential whose secret does not exist. An event
			// can be compensated but never retracted.
			store.failPut = true

			_, err := configure()
			Expect(err).To(HaveOccurred())
			Expect(creds.upserts).To(BeEmpty())
			Expect(profiles.events).To(BeEmpty(), "BR-TP53: the log must not reference an unstored secret")
		})

		It("leaves an inert orphan — not a dangling reference — when the record fails", func() {
			creds.fail = true

			_, err := configure()
			Expect(err).To(HaveOccurred())
			Expect(store.puts).To(HaveLen(1), "the payload was already written; that is the accepted outcome")
			Expect(profiles.events).To(BeEmpty())
		})

		It("stores nothing at all when BR-TP51's guards reject the credential", func() {
			// A rejected credential must not leave a sealed payload behind
			// for a record that never existed.
			_, err := h.ConfigureTrackingCredential(ctx, partner.ID, "acme",
				domain.Provider("MixSite14"), domain.CredentialTypeAPIKey, []byte(trackedSecret))

			Expect(errors.Is(err, domain.ErrInvalidTrackingProvider)).To(BeTrue())
			Expect(store.puts).To(BeEmpty())
			Expect(creds.upserts).To(BeEmpty())
			Expect(profiles.events).To(BeEmpty())
		})
	})

	Context("BR-TP52: the secret reaches the bucket and nothing else", func() {
		It("never passes credential material to the event or the record", func() {
			_, err := configure()
			Expect(err).NotTo(HaveOccurred())

			// The bucket is the one place it may appear.
			Expect(string(store.values[0])).To(Equal(trackedSecret))

			// Everything else is searched for it, not merely inspected for
			// expected fields — a rule about non-emission is worth testing
			// by what must be absent.
			Expect(renderAll(profiles.events)).NotTo(ContainSubstring(trackedSecret))
			Expect(renderAll(creds.upserts)).NotTo(ContainSubstring(trackedSecret))
		})
	})
})

// renderAll formats values with %#v so every field, exported or not, is
// included in the search — a targeted field-by-field check could miss a
// secret that leaked into some field nobody thought to inspect.
func renderAll(v any) string {
	return fmt.Sprintf("%#v", v)
}
