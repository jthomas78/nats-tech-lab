package registry_test

// Phase 7b — BR-AS38 and BR-AS46. A publisher is a stable identity that holds
// keys and owns plugin ids, and every change to it is a revision-bearing,
// audited write with an actor (decisions 69, 97, 103).
//
// The two claims these specs exist to pin are the ones that are easy to
// collapse by accident: retiring a key is NOT revoking it, and a transfer of
// ownership moves ownership and nothing else.

import (
	"context"
	"errors"

	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/postgres"
)

func newPublisherKey() string {
	kp, err := nkeys.CreateUser()
	Expect(err).NotTo(HaveOccurred())
	pub, err := kp.PublicKey()
	Expect(err).NotTo(HaveOccurred())
	return pub
}

var _ = Describe("BR-AS38/BR-AS46 — publishers, keys and ownership", func() {
	Context("the shape of a publisher write is knowable without a store", func() {
		It("has no delete op: trust is withdrawn by state, never by removal", func() {
			Expect(domain.PublisherWriteOps()).To(ConsistOf(
				domain.OpPublisherUpsert,
				domain.OpPublisherAddKey,
				domain.OpPublisherSetKeyState,
				domain.OpPublisherTransfer,
			))
			for _, op := range domain.PublisherWriteOps() {
				Expect(op).NotTo(ContainSubstring("delete"))
			}
		})

		It("refuses an authorless write, because it could not be audited", func() {
			w := domain.PublisherWrite{Op: domain.OpPublisherUpsert, PublisherID: "fleet-team"}
			Expect(w.Validate()).To(MatchError(domain.ErrNoActor))
		})

		It("refuses a write that names no publisher", func() {
			w := domain.PublisherWrite{Op: domain.OpPublisherUpsert, Actor: domain.SharedAdminActor}
			Expect(w.Validate()).To(MatchError(domain.ErrNoPublisher))
		})

		It("refuses an unknown op", func() {
			w := domain.PublisherWrite{Op: "publisher-delete", PublisherID: "fleet-team", Actor: domain.SharedAdminActor}
			Expect(errors.Is(w.Validate(), domain.ErrUnknownOp)).To(BeTrue())
		})

		It("refuses a body filed under a different publisher id", func() {
			w := domain.PublisherWrite{
				Op: domain.OpPublisherUpsert, PublisherID: "fleet-team", Actor: domain.SharedAdminActor,
				Publisher: &domain.Publisher{ID: "other-team"},
			}
			Expect(w.Validate()).To(MatchError(domain.ErrPublisherIDMismatch))
		})

		It("refuses a key that is not a public NKey", func() {
			w := domain.PublisherWrite{
				Op: domain.OpPublisherAddKey, PublisherID: "fleet-team", Actor: domain.SharedAdminActor,
				PublicKey: "not-a-key",
			}
			Expect(errors.Is(w.Validate(), domain.ErrBadPublisherKey)).To(BeTrue())
		})

		It("refuses a seed, which is a private key and must never be curated", func() {
			kp, err := nkeys.CreateUser()
			Expect(err).NotTo(HaveOccurred())
			seed, err := kp.Seed()
			Expect(err).NotTo(HaveOccurred())
			w := domain.PublisherWrite{
				Op: domain.OpPublisherAddKey, PublisherID: "fleet-team", Actor: domain.SharedAdminActor,
				PublicKey: string(seed),
			}
			Expect(errors.Is(w.Validate(), domain.ErrBadPublisherKey)).To(BeTrue())
		})

		It("accepts a public NKey", func() {
			w := domain.PublisherWrite{
				Op: domain.OpPublisherAddKey, PublisherID: "fleet-team", Actor: domain.SharedAdminActor,
				PublicKey: newPublisherKey(),
			}
			Expect(w.Validate()).To(Succeed())
		})

		It("refuses a key state that is not one of the three", func() {
			w := domain.PublisherWrite{
				Op: domain.OpPublisherSetKeyState, PublisherID: "fleet-team", Actor: domain.SharedAdminActor,
				PublicKey: newPublisherKey(), KeyState: "disabled",
			}
			Expect(errors.Is(w.Validate(), domain.ErrBadKeyState)).To(BeTrue())
		})

		It("refuses a transfer that names no plugin", func() {
			w := domain.PublisherWrite{Op: domain.OpPublisherTransfer, PublisherID: "fleet-team", Actor: domain.SharedAdminActor}
			Expect(w.Validate()).To(MatchError(domain.ErrNoPluginID))
		})
	})

	Context("a publisher document answers the two questions trust asks of it", func() {
		var doc domain.PublisherDocument
		var enabled, retired, revoked string

		BeforeEach(func() {
			enabled, retired, revoked = newPublisherKey(), newPublisherKey(), newPublisherKey()
			doc = domain.PublisherDocument{Revision: 3, Publishers: []domain.Publisher{{
				ID:   "fleet-team",
				Name: "Fleet Team",
				Keys: []domain.PublisherKey{
					{PublicKey: enabled, State: domain.KeyEnabled},
					{PublicKey: retired, State: domain.KeyRetired},
					{PublicKey: revoked, State: domain.KeyRevoked},
				},
				Plugins: []string{"fleet"},
			}}}
		})

		It("finds the publisher and the state of a key it holds", func() {
			p, k, ok := doc.KeyHolder(retired)
			Expect(ok).To(BeTrue())
			Expect(p.ID).To(Equal("fleet-team"))
			Expect(k.State).To(Equal(domain.KeyRetired))
		})

		It("does not find a key nobody holds", func() {
			_, _, ok := doc.KeyHolder(newPublisherKey())
			Expect(ok).To(BeFalse())
		})

		It("names the owner of a plugin id, and nobody for an unowned one", func() {
			owner, ok := doc.OwnerOf("fleet")
			Expect(ok).To(BeTrue())
			Expect(owner).To(Equal("fleet-team"))
			_, ok = doc.OwnerOf("someone-elses")
			Expect(ok).To(BeFalse())
		})

		It("reports ownership from the publisher row, never from the remote's origin", func() {
			Expect(doc.Publishers[0].Owns("fleet")).To(BeTrue())
			Expect(doc.Publishers[0].Owns("cargo")).To(BeFalse())
		})
	})

	Context("stored, a publisher write behaves exactly like a registry write", func() {
		var store *postgres.Store
		var ctx context.Context

		BeforeEach(func() {
			if pgUnavailable != "" {
				Skip(pgUnavailable)
			}
			ctx = context.Background()
			_, err := pgDB.ExecContext(ctx, `TRUNCATE registry.publishers, registry.publisher_keys, registry.plugin_owners, registry.audit; UPDATE registry.publisher_revision SET revision = 0`)
			Expect(err).NotTo(HaveOccurred())
			store = postgres.NewStore(pgDB, domain.NewAllowlist([]string{"http://localhost:7110"}))
		})

		publisherWrite := func(w domain.PublisherWrite) domain.PublisherDocument {
			doc, err := store.ApplyPublisher(ctx, w)
			Expect(err).NotTo(HaveOccurred())
			return doc
		}

		create := func() domain.PublisherDocument {
			return publisherWrite(domain.PublisherWrite{
				Op: domain.OpPublisherUpsert, PublisherID: "fleet-team", Actor: domain.SharedAdminActor,
				Publisher: &domain.Publisher{ID: "fleet-team", Name: "Fleet Team"}, IfRevision: 0,
			})
		}

		It("assigns the revision itself, starting at one", func() {
			Expect(create().Revision).To(Equal(int64(1)))
		})

		It("refuses a write keyed on a revision it is not on, and says which one it is on", func() {
			create()
			_, err := store.ApplyPublisher(ctx, domain.PublisherWrite{
				Op: domain.OpPublisherUpsert, PublisherID: "fleet-team", Actor: domain.SharedAdminActor,
				Publisher: &domain.Publisher{ID: "fleet-team", Name: "Renamed"}, IfRevision: 0,
			})
			Expect(errors.Is(err, domain.ErrStaleRevision)).To(BeTrue())
			var stale domain.StaleRevisionError
			Expect(errors.As(err, &stale)).To(BeTrue())
			Expect(stale.Current).To(Equal(int64(1)))
		})

		It("audits the accepted write and the refused one alike", func() {
			create()
			_, _ = store.ApplyPublisher(ctx, domain.PublisherWrite{
				Op: domain.OpPublisherUpsert, PublisherID: "fleet-team", Actor: domain.SharedAdminActor,
				Publisher: &domain.Publisher{ID: "fleet-team"}, IfRevision: 0,
			})
			var accepted, refused int
			Expect(pgDB.QueryRowContext(ctx, `SELECT count(*) FROM registry.audit WHERE scope = 'publisher' AND outcome = 'accepted'`).Scan(&accepted)).To(Succeed())
			Expect(pgDB.QueryRowContext(ctx, `SELECT count(*) FROM registry.audit WHERE scope = 'publisher' AND outcome = 'refused'`).Scan(&refused)).To(Succeed())
			Expect(accepted).To(Equal(1))
			Expect(refused).To(Equal(1))
		})

		It("moves its own revision, not the registry's", func() {
			before, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			create()
			after, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.Revision).To(Equal(before.Revision), "curating a publisher is not a change to the plugin document")
		})

		It("retires a key without revoking it, and keeps the row", func() {
			doc := create()
			key := newPublisherKey()
			doc = publisherWrite(domain.PublisherWrite{
				Op: domain.OpPublisherAddKey, PublisherID: "fleet-team", Actor: domain.SharedAdminActor,
				PublicKey: key, IfRevision: doc.Revision,
			})
			_, k, ok := doc.KeyHolder(key)
			Expect(ok).To(BeTrue())
			Expect(k.State).To(Equal(domain.KeyEnabled))

			doc = publisherWrite(domain.PublisherWrite{
				Op: domain.OpPublisherSetKeyState, PublisherID: "fleet-team", Actor: domain.SharedAdminActor,
				PublicKey: key, KeyState: domain.KeyRetired, IfRevision: doc.Revision,
			})
			_, k, ok = doc.KeyHolder(key)
			Expect(ok).To(BeTrue(), "a retired key is still held, so entries it signed can still be attributed")
			Expect(k.State).To(Equal(domain.KeyRetired))
			Expect(k.State).NotTo(Equal(domain.KeyRevoked))
		})

		It("revokes one key without touching its siblings", func() {
			doc := create()
			keep, drop := newPublisherKey(), newPublisherKey()
			for _, key := range []string{keep, drop} {
				doc = publisherWrite(domain.PublisherWrite{
					Op: domain.OpPublisherAddKey, PublisherID: "fleet-team", Actor: domain.SharedAdminActor,
					PublicKey: key, IfRevision: doc.Revision,
				})
			}
			doc = publisherWrite(domain.PublisherWrite{
				Op: domain.OpPublisherSetKeyState, PublisherID: "fleet-team", Actor: domain.SharedAdminActor,
				PublicKey: drop, KeyState: domain.KeyRevoked, IfRevision: doc.Revision,
			})
			_, dropped, _ := doc.KeyHolder(drop)
			_, kept, _ := doc.KeyHolder(keep)
			Expect(dropped.State).To(Equal(domain.KeyRevoked))
			Expect(kept.State).To(Equal(domain.KeyEnabled))
		})

		It("transfers a plugin id and changes nothing else", func() {
			doc := create()
			key := newPublisherKey()
			doc = publisherWrite(domain.PublisherWrite{
				Op: domain.OpPublisherAddKey, PublisherID: "fleet-team", Actor: domain.SharedAdminActor,
				PublicKey: key, IfRevision: doc.Revision,
			})
			doc = publisherWrite(domain.PublisherWrite{
				Op: domain.OpPublisherTransfer, PublisherID: "fleet-team", Actor: domain.SharedAdminActor,
				PluginID: "fleet", IfRevision: doc.Revision,
			})
			owner, ok := doc.OwnerOf("fleet")
			Expect(ok).To(BeTrue())
			Expect(owner).To(Equal("fleet-team"))

			doc = publisherWrite(domain.PublisherWrite{
				Op: domain.OpPublisherUpsert, PublisherID: "cargo-team", Actor: domain.SharedAdminActor,
				Publisher: &domain.Publisher{ID: "cargo-team", Name: "Cargo Team"}, IfRevision: doc.Revision,
			})
			doc = publisherWrite(domain.PublisherWrite{
				Op: domain.OpPublisherTransfer, PublisherID: "cargo-team", Actor: domain.SharedAdminActor,
				PluginID: "fleet", IfRevision: doc.Revision,
			})
			owner, _ = doc.OwnerOf("fleet")
			Expect(owner).To(Equal("cargo-team"), "ownership moved")
			holder, k, ok := doc.KeyHolder(key)
			Expect(ok).To(BeTrue())
			Expect(holder.ID).To(Equal("fleet-team"), "a transfer moves the plugin id, never a key")
			Expect(k.State).To(Equal(domain.KeyEnabled))
		})

		It("audits a transfer under the plugin id that moved", func() {
			doc := create()
			publisherWrite(domain.PublisherWrite{
				Op: domain.OpPublisherTransfer, PublisherID: "fleet-team", Actor: domain.SharedAdminActor,
				PluginID: "fleet", IfRevision: doc.Revision,
			})
			var subject string
			Expect(pgDB.QueryRowContext(ctx, `SELECT entry_id FROM registry.audit WHERE op = $1 ORDER BY id DESC LIMIT 1`, domain.OpPublisherTransfer).Scan(&subject)).To(Succeed())
			Expect(subject).To(Equal("fleet"))
		})
	})
})
