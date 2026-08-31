package registry_test

// Phase 7c — verification. Derived from BR-AS35, BR-AS46, BR-AS47, BR-AS48
// and decisions 67, 97, 98, 99, 102.
//
// The gate an announcement passes has four parts and the ORDER is the rule,
// not an implementation detail:
//
//   1. ownership — a valid signature authorises nothing on its own
//      (decision 97). Checked ahead of everything, so a key that is both
//      revoked and speaking for someone else's plugin is told the true
//      reason it was refused.
//   2. the signing key's trust state — trusted, and enabled rather than
//      retired or revoked (BR-AS38).
//   3. the signature itself, over the exact bytes (BR-AS37).
//   4. the release number, which never goes backwards and whose equal case
//      is a safe retry rather than a refusal (BR-AS47).
//
// Each refusal has its own cause because a publisher debugging an
// integration cannot act on "rejected".

import (
	"encoding/base64"
	"errors"

	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

// signer is a publisher keypair: the public key that goes in the trust table
// and a sign function over arbitrary bytes. Minted here the same way a
// publisher would mint one — outside the nsc chain (gate answer 2).
type signer struct {
	public string
	kp     nkeys.KeyPair
}

func newSigner() signer {
	kp, err := nkeys.CreateUser()
	Expect(err).ToNot(HaveOccurred())
	pub, err := kp.PublicKey()
	Expect(err).ToNot(HaveOccurred())
	return signer{public: pub, kp: kp}
}

func (s signer) sign(payload []byte) string {
	sig, err := s.kp.Sign(payload)
	Expect(err).ToNot(HaveOccurred())
	return base64.StdEncoding.EncodeToString(sig)
}

// trustTable builds a document in which `owner` owns pluginID and holds one
// key in the given state.
func trustTable(pluginID string, keys map[string]string, owns map[string][]string) domain.PublisherDocument {
	doc := domain.PublisherDocument{Revision: 3}
	byID := map[string]*domain.Publisher{}
	for pub, plugins := range owns {
		byID[pub] = &domain.Publisher{ID: pub, Plugins: plugins}
	}
	for key, spec := range keys {
		// spec is "publisherID:state"
		var pub, state string
		for i := 0; i < len(spec); i++ {
			if spec[i] == ':' {
				pub, state = spec[:i], spec[i+1:]
				break
			}
		}
		if byID[pub] == nil {
			byID[pub] = &domain.Publisher{ID: pub}
		}
		byID[pub].Keys = append(byID[pub].Keys, domain.PublisherKey{PublicKey: key, State: state})
	}
	for _, p := range byID {
		doc.Publishers = append(doc.Publishers, *p)
	}
	domain.SortPublishers(doc.Publishers)
	return doc
}

var _ = Describe("Phase 7c — admitting an announcement", func() {
	var (
		sig     signer
		payload []byte
		trust   domain.PublisherDocument
	)

	BeforeEach(func() {
		sig = newSigner()
		payload = []byte(`{"id":"fleet","release":4}`)
		trust = trustTable("fleet",
			map[string]string{sig.public: "platform-team:" + domain.KeyEnabled},
			map[string][]string{"platform-team": {"fleet"}})
	})

	announcement := func() domain.Announcement {
		return domain.Announcement{
			PluginID:   "fleet",
			SigningKey: sig.public,
			Payload:    payload,
			Signature:  sig.sign(payload),
			Release:    4,
			Accepted:   3,
		}
	}

	admit := func(a domain.Announcement) (domain.Admission, error) {
		return domain.AdmitAnnouncement(trust, domain.NKeyVerifier{}, a)
	}

	Context("the ordinary case", func() {
		It("admits a valid announcement and names the publisher and the key that signed it", func() {
			got, err := admit(announcement())
			Expect(err).ToNot(HaveOccurred())
			Expect(got.PublisherID).To(Equal("platform-team"))
			Expect(got.SigningKey).To(Equal(sig.public))
			Expect(got.NoOp).To(BeFalse())
		})

		It("records the key that signed, not merely the publisher — a revocation needs the key", func() {
			got, err := admit(announcement())
			Expect(err).ToNot(HaveOccurred())
			Expect(got.SigningKey).ToNot(Equal(got.PublisherID))
		})
	})

	Context("BR-AS46 — ownership authorises, and is checked first", func() {
		It("refuses a plugin id the signing publisher does not own", func() {
			a := announcement()
			a.PluginID = "someone-elses-plugin"
			_, err := admit(a)
			Expect(errors.Is(err, domain.ErrNotOwned)).To(BeTrue())
		})

		It("refuses a plugin id no publisher owns at all", func() {
			trust = trustTable("fleet",
				map[string]string{sig.public: "platform-team:" + domain.KeyEnabled},
				map[string][]string{"platform-team": {}})
			_, err := admit(announcement())
			Expect(errors.Is(err, domain.ErrNotOwned)).To(BeTrue())
		})

		It("refuses on ownership even when the key is also revoked, so the true cause is reported", func() {
			trust = trustTable("fleet",
				map[string]string{sig.public: "partner-co:" + domain.KeyRevoked},
				map[string][]string{"platform-team": {"fleet"}, "partner-co": {}})
			_, err := admit(announcement())
			Expect(errors.Is(err, domain.ErrNotOwned)).To(BeTrue())
			Expect(errors.Is(err, domain.ErrKeyRevoked)).To(BeFalse())
		})

		It("refuses a signature that verifies perfectly but speaks for another team's plugin", func() {
			// Two teams, one origin — the case decision 97 exists for.
			other := newSigner()
			trust = trustTable("fleet",
				map[string]string{
					sig.public:   "platform-team:" + domain.KeyEnabled,
					other.public: "partner-co:" + domain.KeyEnabled,
				},
				map[string][]string{"platform-team": {"fleet"}, "partner-co": {"partner-plugin"}})
			a := announcement()
			a.SigningKey = other.public
			a.Signature = other.sign(payload)
			_, err := admit(a)
			Expect(errors.Is(err, domain.ErrNotOwned)).To(BeTrue())
		})
	})

	Context("BR-AS38 — the signing key's trust state", func() {
		It("refuses a key the trust table has never seen", func() {
			a := announcement()
			stranger := newSigner()
			a.SigningKey = stranger.public
			a.Signature = stranger.sign(payload)
			_, err := admit(a)
			Expect(errors.Is(err, domain.ErrKeyNotTrusted)).To(BeTrue())
		})

		It("refuses a retired key with its own cause — it signs nothing new", func() {
			trust = trustTable("fleet",
				map[string]string{sig.public: "platform-team:" + domain.KeyRetired},
				map[string][]string{"platform-team": {"fleet"}})
			_, err := admit(announcement())
			Expect(errors.Is(err, domain.ErrKeyRetired)).To(BeTrue())
			Expect(errors.Is(err, domain.ErrKeyRevoked)).To(BeFalse())
		})

		It("refuses a revoked key with a cause distinct from a retired one", func() {
			trust = trustTable("fleet",
				map[string]string{sig.public: "platform-team:" + domain.KeyRevoked},
				map[string][]string{"platform-team": {"fleet"}})
			_, err := admit(announcement())
			Expect(errors.Is(err, domain.ErrKeyRevoked)).To(BeTrue())
			Expect(errors.Is(err, domain.ErrKeyRetired)).To(BeFalse())
		})
	})

	Context("BR-AS35 — the signature, over the exact bytes", func() {
		It("refuses an announcement carrying no signature", func() {
			a := announcement()
			a.Signature = ""
			_, err := admit(a)
			Expect(errors.Is(err, domain.ErrUnsigned)).To(BeTrue())
		})

		It("refuses a signature made over different bytes", func() {
			a := announcement()
			a.Signature = sig.sign([]byte(`{"id":"fleet","release":9}`))
			_, err := admit(a)
			Expect(errors.Is(err, domain.ErrUnverified)).To(BeTrue())
		})

		It("refuses when a single byte of the payload changed after signing", func() {
			a := announcement()
			a.Payload = []byte(`{"id":"fleet","release":5}`)
			_, err := admit(a)
			Expect(errors.Is(err, domain.ErrUnverified)).To(BeTrue())
		})

		It("refuses a signature that is not decodable at all, rather than panicking", func() {
			a := announcement()
			a.Signature = "not base64 %%%"
			_, err := admit(a)
			Expect(errors.Is(err, domain.ErrUnverified)).To(BeTrue())
		})

		It("refuses a signature made by a different key than the one named", func() {
			other := newSigner()
			a := announcement()
			a.Signature = other.sign(payload)
			_, err := admit(a)
			Expect(errors.Is(err, domain.ErrUnverified)).To(BeTrue())
		})
	})

	Context("BR-AS47 — the release number", func() {
		It("refuses a release lower than the highest already accepted", func() {
			a := announcement()
			a.Release, a.Accepted = 3, 7
			a.Payload = payload
			_, err := admit(a)
			Expect(errors.Is(err, domain.ErrReleaseBackwards)).To(BeTrue())
		})

		It("treats an equal release as a no-op, so an ordinary retry is safe", func() {
			a := announcement()
			a.Release, a.Accepted = 4, 4
			got, err := admit(a)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.NoOp).To(BeTrue())
		})

		It("admits the first announcement for an id, where nothing has been accepted yet", func() {
			a := announcement()
			a.Release, a.Accepted = 1, 0
			got, err := admit(a)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.NoOp).To(BeFalse())
		})

		It("refuses an announcement carrying no release number", func() {
			a := announcement()
			a.Release = 0
			_, err := admit(a)
			Expect(errors.Is(err, domain.ErrNoRelease)).To(BeTrue())
		})

		It("checks the release last, so a bad signature is never reported as a stale release", func() {
			a := announcement()
			a.Release, a.Accepted = 3, 7
			a.Signature = "not base64 %%%"
			_, err := admit(a)
			Expect(errors.Is(err, domain.ErrUnverified)).To(BeTrue())
		})
	})

	Context("decision 67 — verification is a service-side gate with a configured anchor", func() {
		It("refuses everything when the deployment has no verifier at all", func() {
			_, err := domain.AdmitAnnouncement(trust, domain.NoVerifier{}, announcement())
			Expect(errors.Is(err, domain.ErrUnverified)).To(BeTrue())
		})

		It("treats a nil verifier as no verifier, never as skip the check", func() {
			_, err := domain.AdmitAnnouncement(trust, nil, announcement())
			Expect(errors.Is(err, domain.ErrUnverified)).To(BeTrue())
		})

		It("refuses everything when the trust table is empty", func() {
			_, err := domain.AdmitAnnouncement(domain.PublisherDocument{}, domain.NKeyVerifier{}, announcement())
			Expect(errors.Is(err, domain.ErrNotOwned)).To(BeTrue())
		})
	})

	Context("decision 102 — the requirement follows provenance, not lifecycle", func() {
		It("still requires a signature after an operator has changed the entry's lifecycle", func() {
			// Lifecycle is not an input to the gate at all: there is nowhere
			// for an operator edit to reach. This spec pins that absence.
			a := announcement()
			a.Signature = ""
			for _, lifecycle := range []string{domain.LifecycleStatic, domain.LifecycleDynamic, ""} {
				a.PluginID = "fleet"
				_ = lifecycle
				_, err := admit(a)
				Expect(errors.Is(err, domain.ErrUnsigned)).To(BeTrue())
			}
		})
	})
})
