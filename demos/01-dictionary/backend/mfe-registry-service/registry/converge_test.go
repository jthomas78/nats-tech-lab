package registry_test

/*
	BR-AS73 / decision 10 — convergence: an announcement that is admitted and
	changes nothing.

	A reset notice makes every live publisher re-announce at once, and almost
	every one of those says exactly what the registry already holds. Treating
	each as an ordinary update would cost a catalogue revision and an audit
	row per plugin, and would tell every shell in the estate to re-read a
	document that did not move.

	The part that is easy to get wrong is what convergence still costs. It is
	not free: the release watermark advances. The counter is this protocol's
	stale-announcement protection, and a resync's release number that never
	became the watermark would stay acceptable forever — widening the replay
	window by exactly the number of resyncs, which is the opposite of what the
	counter is for.

	These specs are the decision on its own. The write path — that a
	watermark-only write cannot lose a concurrent real announce — is pinned
	beside the announcement transport, because that is where the concurrency
	control lives.
*/

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

var _ = Describe("BR-AS73 — convergence", func() {
	stored := func(release int64) domain.Entry {
		return domain.Entry{
			ID: "plugin", Name: "Plugin", Enabled: true, Lifecycle: domain.LifecycleDynamic,
			Release: release, AnnouncedAt: "2026-09-01T00:00:00Z", LastAnnouncedAt: "2026-09-01T00:00:00Z",
			Manifest:      &domain.Manifest{Bytes: []byte(`{"release":1}`), Signature: "sig-1", SigningKey: "key"},
			Remote:        domain.Remote{Kind: "federated", URL: "http://localhost:7110/r.js", Module: "plugin"},
			Contributions: []domain.Contribution{{Kind: "shell-footer", ID: "status"}},
		}
	}
	// What a publisher's resync looks like on arrival: the same content, a
	// higher release, and no curation fields at all.
	resync := func(release int64) domain.Entry {
		return domain.Entry{
			ID: "plugin", Name: "Plugin", Release: release,
			Remote:        domain.Remote{Kind: "federated", URL: "http://localhost:7110/r.js", Module: "plugin"},
			Contributions: []domain.Contribution{{Kind: "shell-footer", ID: "status"}},
		}
	}

	Context("what converges", func() {
		It("converges an identical re-announce at a higher release", func() {
			existing := stored(4)
			outcome, next := domain.DecideAnnounce(&existing, resync(5))
			Expect(outcome).To(Equal(domain.AnnounceConverged))
			// The higher release travels with it — it is the whole reason the
			// converged path still has a write to do.
			Expect(next.Release).To(Equal(int64(5)))
		})

		It("ignores the stamps and the signed bytes, which move without the content moving", func() {
			existing := stored(4)
			incoming := resync(5)
			// A resync's manifest and signature always differ, because the
			// release counter is inside the signed payload. If they counted
			// as content, nothing would ever converge.
			incoming.Manifest = &domain.Manifest{Bytes: []byte(`{"release":5}`), Signature: "sig-5", SigningKey: "key"}
			outcome, _ := domain.DecideAnnounce(&existing, incoming)
			Expect(outcome).To(Equal(domain.AnnounceConverged))
		})
	})

	Context("what does not converge, because it is a real change", func() {
		It("updates when any content field moved", func() {
			for _, changed := range []func(*domain.Entry){
				func(e *domain.Entry) { e.Name = "Renamed" },
				func(e *domain.Entry) { e.Remote.URL = "http://localhost:7110/other.js" },
				func(e *domain.Entry) { e.Remote.Module = "other" },
				func(e *domain.Entry) { e.Description = "now described" },
				func(e *domain.Entry) { e.Version = "2.0.0" },
				func(e *domain.Entry) { e.RoutePrefix = "/p" },
				func(e *domain.Entry) { e.Contributions = []domain.Contribution{{Kind: "shell-footer", ID: "other"}} },
			} {
				existing := stored(4)
				incoming := resync(5)
				changed(&incoming)
				outcome, _ := domain.DecideAnnounce(&existing, incoming)
				Expect(outcome).To(Equal(domain.AnnounceUpdated))
			}
		})

		/* Withdrawn and Withheld are store-owned and cleared off every
		   incoming payload, so an entry carrying either differs from what
		   arrives — and takes the ordinary write path, which is what puts it
		   back on screen. Convergence must never be the thing that leaves a
		   withdrawn plugin withdrawn. */
		It("does not converge a withdrawn entry whose publisher is back", func() {
			existing := stored(4)
			existing.Withdrawn = true
			outcome, next := domain.DecideAnnounce(&existing, resync(5))
			Expect(outcome).To(Equal(domain.AnnounceUpdated))
			Expect(next.Withdrawn).To(BeFalse())
		})

		It("does not converge a withheld entry", func() {
			existing := stored(4)
			existing.Withheld = true
			outcome, _ := domain.DecideAnnounce(&existing, resync(5))
			Expect(outcome).To(Equal(domain.AnnounceUpdated))
		})

		// The other branches all have work to do that convergence would skip:
		// an unknown id must be inserted, a pending one refreshed for the
		// operator looking at it, a cross-origin return re-queued.
		It("never converges outside the enabled, same-origin, dynamic branch", func() {
			pending := stored(4)
			pending.Enabled = false
			out, _ := domain.DecideAnnounce(&pending, resync(5))
			Expect(out).To(Equal(domain.AnnouncePending))

			out, _ = domain.DecideAnnounce(nil, resync(1))
			Expect(out).To(Equal(domain.AnnounceInserted))

			static := stored(4)
			static.Lifecycle = domain.LifecycleStatic
			out, _ = domain.DecideAnnounce(&static, resync(5))
			Expect(out).To(Equal(domain.AnnounceIgnored))

			crossed := resync(5)
			crossed.Remote.URL = "http://localhost:7112/r.js"
			existing := stored(4)
			out, _ = domain.DecideAnnounce(&existing, crossed)
			Expect(out).To(Equal(domain.AnnounceRequeued))
		})
	})

	Context("convergence is not the same fact as a literal replay", func() {
		/* Admission.NoOp means Release == Accepted: the exact release
		   already stored, arriving twice, spending no new number. Convergence
		   means a NEW release carrying old content. They look alike from a
		   publisher's chair and are different on the wire, and collapsing
		   them into one flag would make the watermark's advance invisible to
		   anyone reading a response. */
		It("keeps the replay and the resync as separate wire values", func() {
			Expect(domain.AnnounceConverged).NotTo(Equal(domain.AnnounceOutcome("")))
			Expect(string(mferegistry.AnnounceConverged)).To(Equal("converged"))
			for _, other := range []domain.AnnounceOutcome{
				domain.AnnounceInserted, domain.AnnouncePending,
				domain.AnnounceUpdated, domain.AnnounceRequeued, domain.AnnounceIgnored,
			} {
				Expect(domain.AnnounceConverged).NotTo(Equal(other))
			}
		})

		It("admits a literal replay as NoOp, and a resync at a higher release as not NoOp", func() {
			signer := newSigner()
			trust := trustTable("plugin",
				map[string]string{signer.public: "acme:enabled"},
				map[string][]string{"acme": {"plugin"}})
			payload := []byte(`{"id":"plugin"}`)
			admit := func(release, accepted int64) domain.Admission {
				out, err := domain.AdmitAnnouncement(trust, domain.NKeyVerifier{}, domain.Announcement{
					PluginID: "plugin", SigningKey: signer.public, Payload: payload,
					Signature: signer.sign(payload), Release: release, Accepted: accepted,
				})
				Expect(err).NotTo(HaveOccurred())
				return out
			}
			Expect(admit(5, 5).NoOp).To(BeTrue())
			// The converged case. NoOp is false here precisely because a new
			// release number was spent, and spending one is what obliges the
			// watermark to move.
			Expect(admit(6, 5).NoOp).To(BeFalse())
		})
	})
})
