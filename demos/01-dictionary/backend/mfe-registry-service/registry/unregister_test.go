package registry_test

// Phase 5b — the signed publisher unregister. Derived from BR-AS54, BR-AS55
// and decisions 59, 77, 97, 98, 103.
//
// An unregister is the one message a publisher can send that takes running
// code off an operator's screen, so it passes the same gate an announcement
// passes and then two more:
//
//   - the ACTION is signed. The bytes say "unregister" in their own envelope,
//     which an announcement's manifest bytes cannot say. Without that, an
//     announcement captured off the wire is a valid unregister for whoever
//     replays it (BR-AS54, "action-bound signature").
//   - the KEY and PUBLISHER are inside the signed bytes. The transport's
//     copies are lookup hints; a signature cannot be lifted onto a request
//     that names some other key.
//
// What it then does is bounded by BR-AS55: it withdraws AVAILABILITY, and
// availability is not approval. The row, the operator's enable flag and the
// history all survive, and a plugin an operator never approved cannot gain
// approval by leaving and coming back.

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

var _ = Describe("BR-AS54 — reading the signed unregister envelope", func() {
	It("parses a well-formed command", func() {
		cmd, err := domain.ParseUnregister([]byte(
			`{"schemaVersion":1,"action":"unregister","plugin":"fleet","publisher":"platform-team","signingKey":"UABC","release":7}`))
		Expect(err).ToNot(HaveOccurred())
		Expect(cmd.PluginID).To(Equal("fleet"))
		Expect(cmd.Publisher).To(Equal("platform-team"))
		Expect(cmd.SigningKey).To(Equal("UABC"))
		Expect(cmd.Release).To(Equal(int64(7)))
	})

	It("refuses an announcement's signed bytes replayed as an unregister", func() {
		// The whole point of the envelope. These are exactly the bytes a
		// publisher signs to announce; they say nothing about unregistering,
		// so they cannot mean it.
		_, err := domain.ParseUnregister([]byte(`{"id":"fleet","release":4}`))
		Expect(err).To(MatchError(domain.ErrNotUnregister))
	})

	It("refuses any action other than unregister", func() {
		_, err := domain.ParseUnregister([]byte(
			`{"schemaVersion":1,"action":"announce","plugin":"fleet","publisher":"p","signingKey":"U","release":7}`))
		Expect(err).To(MatchError(domain.ErrNotUnregister))
	})

	It("refuses a schema version it does not know", func() {
		// A future command shape may mean something this code would get
		// wrong. Refusing is the safe direction for a message that removes
		// running code.
		_, err := domain.ParseUnregister([]byte(
			`{"schemaVersion":2,"action":"unregister","plugin":"fleet","publisher":"p","signingKey":"U","release":7}`))
		Expect(err).To(MatchError(domain.ErrUnregisterVersion))
	})

	It("refuses fields it does not recognise", func() {
		_, err := domain.ParseUnregister([]byte(
			`{"schemaVersion":1,"action":"unregister","plugin":"fleet","publisher":"p","signingKey":"U","release":7,"force":true}`))
		Expect(err).To(MatchError(domain.ErrUnregisterMalformed))
	})

	It("refuses a command that names no plugin", func() {
		_, err := domain.ParseUnregister([]byte(
			`{"schemaVersion":1,"action":"unregister","plugin":"","publisher":"p","signingKey":"U","release":7}`))
		Expect(err).To(MatchError(domain.ErrNoEntryID))
	})

	It("refuses a command carrying no release", func() {
		_, err := domain.ParseUnregister([]byte(
			`{"schemaVersion":1,"action":"unregister","plugin":"fleet","publisher":"p","signingKey":"U"}`))
		Expect(err).To(MatchError(domain.ErrNoRelease))
	})
})

var _ = Describe("BR-AS54 — admitting an unregister", func() {
	var (
		sig   signer
		trust domain.PublisherDocument
	)

	BeforeEach(func() {
		sig = newSigner()
		trust = trustTable("fleet",
			map[string]string{sig.public: "platform-team:" + domain.KeyEnabled},
			map[string][]string{"platform-team": {"fleet"}})
	})

	// bytes builds the exact signed byte sequence for a command.
	bytesFor := func(cmd domain.UnregisterCommand) []byte {
		return []byte(`{"schemaVersion":1,"action":"unregister","plugin":"` + cmd.PluginID +
			`","publisher":"` + cmd.Publisher + `","signingKey":"` + cmd.SigningKey +
			`","release":` + itoa(cmd.Release) + `}`)
	}

	command := func() domain.UnregisterCommand {
		return domain.UnregisterCommand{
			PluginID:   "fleet",
			Publisher:  "platform-team",
			SigningKey: sig.public,
			Release:    7,
		}
	}

	request := func(cmd domain.UnregisterCommand) domain.Unregister {
		payload := bytesFor(cmd)
		return domain.Unregister{
			Command:    cmd,
			SigningKey: cmd.SigningKey,
			Payload:    payload,
			Signature:  sig.sign(payload),
			Accepted:   4,
		}
	}

	admit := func(u domain.Unregister) (domain.Admission, error) {
		return domain.AdmitUnregister(trust, domain.NKeyVerifier{}, u)
	}

	Context("the ordinary case", func() {
		It("admits a valid unregister and names the publisher and the key that signed it", func() {
			got, err := admit(request(command()))
			Expect(err).ToNot(HaveOccurred())
			Expect(got.PublisherID).To(Equal("platform-team"))
			Expect(got.SigningKey).To(Equal(sig.public))
			Expect(got.NoOp).To(BeFalse())
		})
	})

	Context("ownership and trust, exactly as an announcement is checked", func() {
		It("refuses a publisher that does not own the plugin id", func() {
			trust = trustTable("fleet",
				map[string]string{sig.public: "other-team:" + domain.KeyEnabled},
				map[string][]string{"platform-team": {"fleet"}, "other-team": {"weather"}})
			_, err := admit(request(command()))
			Expect(err).To(MatchError(domain.ErrNotOwned))
		})

		It("refuses a key the trust table has never seen", func() {
			stranger := newSigner()
			cmd := command()
			cmd.SigningKey = stranger.public
			payload := bytesFor(cmd)
			_, err := admit(domain.Unregister{
				Command: cmd, SigningKey: stranger.public,
				Payload: payload, Signature: stranger.sign(payload), Accepted: 4,
			})
			Expect(err).To(MatchError(domain.ErrKeyNotTrusted))
		})

		It("refuses a revoked key", func() {
			trust = trustTable("fleet",
				map[string]string{sig.public: "platform-team:" + domain.KeyRevoked},
				map[string][]string{"platform-team": {"fleet"}})
			_, err := admit(request(command()))
			Expect(err).To(MatchError(domain.ErrKeyRevoked))
		})

		It("refuses a retired key, which signs nothing new", func() {
			trust = trustTable("fleet",
				map[string]string{sig.public: "platform-team:" + domain.KeyRetired},
				map[string][]string{"platform-team": {"fleet"}})
			_, err := admit(request(command()))
			Expect(err).To(MatchError(domain.ErrKeyRetired))
		})
	})

	Context("the signature is over the exact bytes", func() {
		It("refuses a signature made over some other command", func() {
			u := request(command())
			other := command()
			other.Release = 9
			u.Payload = bytesFor(other)
			_, err := admit(u)
			Expect(err).To(MatchError(domain.ErrUnverified))
		})

		It("refuses an unsigned command before consulting the verifier", func() {
			u := request(command())
			u.Signature = ""
			_, err := admit(u)
			Expect(err).To(MatchError(domain.ErrUnsigned))
		})
	})

	Context("the envelope binds the request to one key and one publisher", func() {
		It("refuses when the request names a different key from the signed bytes", func() {
			// Otherwise a signature could be lifted onto a request naming a
			// key its holder never used.
			u := request(command())
			u.SigningKey = newSigner().public
			_, err := admit(u)
			Expect(err).To(MatchError(domain.ErrUnregisterKeyMismatch))
		})

		It("refuses when the signed bytes claim a publisher that does not hold the key", func() {
			cmd := command()
			cmd.Publisher = "other-team"
			_, err := admit(request(cmd))
			Expect(err).To(MatchError(domain.ErrUnregisterPublisherMismatch))
		})
	})

	Context("replay ordering (BR-AS47, decision 98)", func() {
		It("refuses a release older than the one already accepted", func() {
			u := request(command())
			u.Accepted = 9
			_, err := admit(u)
			Expect(err).To(MatchError(domain.ErrReleaseBackwards))
		})

		It("refuses a release already spent on an announcement", func() {
			// Equal release with the entry still available: that counter
			// belongs to the announcement that is running. Accepting it here
			// would let a captured number undo a newer decision.
			u := request(command())
			u.Accepted = 7
			_, err := admit(u)
			Expect(err).To(MatchError(domain.ErrReleaseReused))
		})

		It("treats a duplicate delivery of the accepted unregister as a no-op", func() {
			// Same release, already withdrawn: the message arrived twice. A
			// publisher whose request timed out and retried gets the answer
			// it would have got the first time.
			u := request(command())
			u.Accepted = 7
			u.Withdrawn = true
			got, err := admit(u)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.NoOp).To(BeTrue())
		})
	})
})

var _ = Describe("BR-AS55 — what an accepted unregister does to the stored entry", func() {
	entry := func(lifecycle string, enabled bool) domain.Entry {
		return domain.Entry{
			ID: "fleet", Name: "Fleet", Lifecycle: lifecycle, Enabled: enabled,
			Release: 4,
			Remote:        domain.Remote{Kind: domain.RemoteFederated, URL: "https://plugins.example.com/fleet.js", Module: "./Fleet"},
			Contributions: []domain.Contribution{{Kind: "nav", ID: "fleet-nav", Label: "Fleet", Route: "/fleet"}},
			Manifest:      &domain.Manifest{Signature: "sig", SigningKey: "UABC"},
		}
	}

	cmd := domain.UnregisterCommand{PluginID: "fleet", Publisher: "platform-team", SigningKey: "UABC", Release: 7}

	It("refuses an unregister for an id the registry does not hold", func() {
		// An unknown id cannot gain a row — or approval — by being
		// unregistered (BR-AS55).
		_, _, err := domain.DecideUnregister(nil, cmd)
		Expect(err).To(MatchError(domain.ErrUnknownEntry))
	})

	It("withdraws availability from an enabled dynamic entry without touching approval", func() {
		before := entry(domain.LifecycleDynamic, true)
		outcome, next, err := domain.DecideUnregister(&before, cmd)
		Expect(err).ToNot(HaveOccurred())
		Expect(outcome).To(Equal(domain.UnregisterWithdrawn))
		Expect(next.Withdrawn).To(BeTrue())
		Expect(next.Enabled).To(BeTrue(), "the operator's approval is not the publisher's to remove")
	})

	It("keeps the row, its history and its signed manifest", func() {
		before := entry(domain.LifecycleDynamic, true)
		_, next, err := domain.DecideUnregister(&before, cmd)
		Expect(err).ToNot(HaveOccurred())
		Expect(next.ID).To(Equal("fleet"))
		Expect(next.Contributions).To(Equal(before.Contributions))
		Expect(next.Remote).To(Equal(before.Remote))
		Expect(next.Manifest).To(Equal(before.Manifest))
	})

	It("advances the release, so a stale return cannot undo it", func() {
		before := entry(domain.LifecycleDynamic, true)
		_, next, err := domain.DecideUnregister(&before, cmd)
		Expect(err).ToNot(HaveOccurred())
		Expect(next.Release).To(Equal(int64(7)))
	})

	It("leaves a static entry alone, because curation outranks a publisher", func() {
		before := entry(domain.LifecycleStatic, true)
		outcome, next, err := domain.DecideUnregister(&before, cmd)
		Expect(err).ToNot(HaveOccurred())
		Expect(outcome).To(Equal(domain.UnregisterIgnored))
		Expect(next).To(Equal(before))
	})

	It("leaves an unclassified entry alone, which reads as static", func() {
		before := entry("", true)
		outcome, next, err := domain.DecideUnregister(&before, cmd)
		Expect(err).ToNot(HaveOccurred())
		Expect(outcome).To(Equal(domain.UnregisterIgnored))
		Expect(next).To(Equal(before))
	})

	It("withdraws a dynamic entry that was still pending, and it stays unapproved", func() {
		before := entry(domain.LifecycleDynamic, false)
		outcome, next, err := domain.DecideUnregister(&before, cmd)
		Expect(err).ToNot(HaveOccurred())
		Expect(outcome).To(Equal(domain.UnregisterWithdrawn))
		Expect(next.Withdrawn).To(BeTrue())
		Expect(next.Enabled).To(BeFalse())
	})

	It("is safe to repeat", func() {
		before := entry(domain.LifecycleDynamic, true)
		before.Withdrawn = true
		later := cmd
		later.Release = 8
		outcome, next, err := domain.DecideUnregister(&before, later)
		Expect(err).ToNot(HaveOccurred())
		Expect(outcome).To(Equal(domain.UnregisterWithdrawn))
		Expect(next.Withdrawn).To(BeTrue())
		Expect(next.Enabled).To(BeTrue())
	})

	It("files the write under the key that signed it, so the audit names the true actor", func() {
		before := entry(domain.LifecycleDynamic, true)
		_, next, err := domain.DecideUnregister(&before, cmd)
		Expect(err).ToNot(HaveOccurred())
		write := domain.UnregisterWrite(next, "UABC", 12)
		Expect(write.Actor).To(Equal("UABC"))
		Expect(write.EntryID).To(Equal("fleet"))
		Expect(write.IfRevision).To(Equal(int64(12)))
		Expect(write.Validate()).To(Succeed())
	})
})

var _ = Describe("BR-AS55 — coming back", func() {
	withdrawn := func(enabled bool) domain.Entry {
		return domain.Entry{
			ID: "fleet", Lifecycle: domain.LifecycleDynamic, Enabled: enabled, Withdrawn: true,
			Remote: domain.Remote{Kind: domain.RemoteFederated, URL: "https://plugins.example.com/fleet.js", Module: "./Fleet"},
		}
	}
	returning := func(url string) domain.Entry {
		return domain.Entry{
			ID: "fleet", Withdrawn: true, // asserted by the payload; never believed
			Remote: domain.Remote{Kind: domain.RemoteFederated, URL: url, Module: "./Fleet"},
		}
	}

	It("clears the withdrawal when an approved entry returns to its own origin", func() {
		before := withdrawn(true)
		outcome, next := domain.DecideAnnounce(&before, returning("https://plugins.example.com/fleet-2.js"))
		Expect(outcome).To(Equal(domain.AnnounceUpdated))
		Expect(next.Withdrawn).To(BeFalse())
		Expect(next.Enabled).To(BeTrue())
	})

	It("does not restore approval an operator withheld", func() {
		before := withdrawn(false)
		outcome, next := domain.DecideAnnounce(&before, returning("https://plugins.example.com/fleet-2.js"))
		Expect(outcome).To(Equal(domain.AnnouncePending))
		Expect(next.Enabled).To(BeFalse())
		Expect(next.Withdrawn).To(BeTrue(), "still gone as far as a running shell is concerned, until an operator says otherwise")
	})

	It("does not let a cross-origin return restore availability on its own", func() {
		before := withdrawn(true)
		outcome, next := domain.DecideAnnounce(&before, returning("https://elsewhere.example.com/fleet.js"))
		Expect(outcome).To(Equal(domain.AnnounceRequeued))
		Expect(next.Withdrawn).To(BeTrue())
	})

	It("never lets a payload assert its own availability", func() {
		// A brand new id claiming `withdrawn:false` says nothing; the field
		// is store-owned, like enabled and the class.
		outcome, next := domain.DecideAnnounce(nil, returning("https://plugins.example.com/fleet.js"))
		Expect(outcome).To(Equal(domain.AnnounceInserted))
		Expect(next.Withdrawn).To(BeFalse())
	})
})

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
}
