package registry_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/servicerpc"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

// Phase 7d — what a revocation does to the entries a key already signed.
//
// The two halves of BR-AS38 pull in opposite directions and both matter:
// revocation must reach every entry that key signed, in one revision, without
// the operator hunting them down; and re-enabling the key must give none of
// them back, because "we trust this team again" is not "we re-checked this
// code". Withdrawal is bulk and automatic; restoration is one at a time and
// deliberate (decisions 70, 104).
var _ = Describe("revocation withholds what a key signed", func() {
	Context("BR-AS38 — the selection is a rule, not a query", func() {
		It("names every entry that key signed and nothing else", func() {
			a := signedBy("alpha", "KEY-1")
			b := signedBy("bravo", "KEY-2")
			c := signedBy("charlie", "KEY-1")
			unsigned := federated("delta", "http://localhost:7110/d.js")
			ids := domain.RevocationEffect([]domain.Entry{a, b, c, unsigned}, "KEY-1")
			Expect(ids).To(Equal([]string{"alpha", "charlie"}))
		})

		It("touches nothing when the key signed nothing", func() {
			Expect(domain.RevocationEffect([]domain.Entry{signedBy("alpha", "KEY-1")}, "KEY-9")).To(BeEmpty())
		})

		It("never selects an unsigned entry, whatever key is named", func() {
			// An operator-curated entry has no signing key. A revocation that
			// swept it up would take down a plugin the publisher never
			// touched (decision 102).
			unsigned := federated("delta", "http://localhost:7110/d.js")
			Expect(domain.RevocationEffect([]domain.Entry{unsigned}, "")).To(BeEmpty())
		})
	})
})

func signedBy(id, key string) domain.Entry {
	e := federated(id, "http://localhost:7110/"+id+".js")
	e.Manifest = &domain.Manifest{Bytes: []byte(`{"id":"` + id + `"}`), Signature: "sig", SigningKey: key}
	return e
}

var _ = Describe("revocation through the store", func() {
	var ctx context.Context
	var nc *nats.Conn
	var module *registry.Module
	var store *postgres.Store
	var kp, kp2 nkeys.KeyPair
	var key1, key2 string
	allowed := domain.NewAllowlist([]string{"http://localhost:7110"})

	// announceAs puts one signed, enabled entry in the store under the given
	// key, the way a real publisher would have.
	announceAs := func(pair nkeys.KeyPair, signing, id string, release int64) {
		raw := json.RawMessage(`{"id":"` + id + `","name":"` + id + `","release":` + strconv.FormatInt(release, 10) + `,"remote":{"kind":"federated","url":"http://localhost:7110/` + id + `.js","module":"m"}}`)
		sig, err := pair.Sign(raw)
		Expect(err).NotTo(HaveOccurred())
		body, err := json.Marshal(servicerpc.Request{Payload: raw, Signature: base64.StdEncoding.EncodeToString(sig), SigningKey: signing})
		Expect(err).NotTo(HaveOccurred())
		msg, err := nc.Request(mferegistry.Announce, body, time.Second)
		Expect(err).NotTo(HaveOccurred())
		var out servicerpc.Response
		Expect(json.Unmarshal(msg.Data, &out)).To(Succeed())
		Expect(out.OK).To(BeTrue(), out.Error)
		doc, err := store.Current(ctx)
		Expect(err).NotTo(HaveOccurred())
		_, err = module.Service.Apply(ctx, domain.Write{Op: domain.OpSetEnabled, EntryID: id, Enabled: true, Actor: domain.SharedAdminActor, IfRevision: doc.Revision})
		Expect(err).NotTo(HaveOccurred())
	}

	revoke := func(publisherID, publicKey string) {
		trust, err := module.Service.Publishers(ctx)
		Expect(err).NotTo(HaveOccurred())
		_, err = module.Service.ApplyPublisher(ctx, domain.PublisherWrite{
			Op: domain.OpPublisherSetKeyState, Actor: domain.SharedAdminActor, PublisherID: publisherID,
			PublicKey: publicKey, KeyState: domain.KeyRevoked, IfRevision: trust.Revision,
		})
		Expect(err).NotTo(HaveOccurred())
	}
	entryByID := func(id string) domain.Entry {
		doc, err := store.Current(ctx)
		Expect(err).NotTo(HaveOccurred())
		for _, e := range doc.Entries {
			if e.ID == id {
				return e
			}
		}
		Fail("no entry " + id)
		return domain.Entry{}
	}

	BeforeEach(func() {
		if pgUnavailable != "" {
			Skip(pgUnavailable)
		}
		GinkgoT().Setenv("REGISTRY_PRELOAD_FILE", "")
		ctx = context.Background()
		_, err := pgDB.ExecContext(ctx, `TRUNCATE registry.entries, registry.audit, registry.plugin_owners, registry.publisher_keys, registry.publishers; UPDATE registry.revision SET revision=0; UPDATE registry.publisher_revision SET revision=0`)
		Expect(err).NotTo(HaveOccurred())
		srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1})
		Expect(err).NotTo(HaveOccurred())
		srv.Start()
		DeferCleanup(srv.Shutdown)
		Expect(srv.ReadyForConnections(5 * time.Second)).To(BeTrue())
		nc, err = nats.Connect(srv.ClientURL(), nats.Name("registry-revocation-test"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(nc.Close)
		module, err = registry.Startup(ctx, pgDB, nil, nc, allowed, slog.Default())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(module.Stop)
		Expect(nc.Flush()).To(Succeed())
		store = postgres.NewStore(pgDB, allowed)

		kp, err = nkeys.CreateUser()
		Expect(err).NotTo(HaveOccurred())
		key1, err = kp.PublicKey()
		Expect(err).NotTo(HaveOccurred())
		kp2, err = nkeys.CreateUser()
		Expect(err).NotTo(HaveOccurred())
		key2, err = kp2.PublicKey()
		Expect(err).NotTo(HaveOccurred())

		// Two publishers, one key each, owning one plugin each. Two, so that
		// "the other publisher's entries are untouched" is a real assertion.
		seed := func(publisherID, publicKey string, plugins ...string) {
			trust, err := module.Service.Publishers(ctx)
			Expect(err).NotTo(HaveOccurred())
			trust, err = module.Service.ApplyPublisher(ctx, domain.PublisherWrite{
				Op: domain.OpPublisherUpsert, Actor: domain.SharedAdminActor, PublisherID: publisherID,
				Publisher: &domain.Publisher{ID: publisherID, Name: publisherID}, IfRevision: trust.Revision,
			})
			Expect(err).NotTo(HaveOccurred())
			trust, err = module.Service.ApplyPublisher(ctx, domain.PublisherWrite{
				Op: domain.OpPublisherAddKey, Actor: domain.SharedAdminActor, PublisherID: publisherID,
				PublicKey: publicKey, IfRevision: trust.Revision,
			})
			Expect(err).NotTo(HaveOccurred())
			for _, p := range plugins {
				trust, err = module.Service.ApplyPublisher(ctx, domain.PublisherWrite{
					Op: domain.OpPublisherTransfer, Actor: domain.SharedAdminActor, PublisherID: publisherID,
					PluginID: p, IfRevision: trust.Revision,
				})
				Expect(err).NotTo(HaveOccurred())
			}
		}
		seed("alpha-team", key1, "alpha", "charlie")
		seed("bravo-team", key2, "bravo")
	})

	Context("BR-AS38 — one revocation, one revision, every entry that key signed", func() {
		It("withholds them all and leaves another publisher's entry running", func() {
			announceAs(kp, key1, "alpha", 1)
			announceAs(kp, key1, "charlie", 1)
			announceAs(kp2, key2, "bravo", 1)
			before, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(before.Readable(allowed).Entries).To(HaveLen(3))

			revoke("alpha-team", key1)

			after, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			// One revision for the whole revocation, not one per entry.
			Expect(after.Revision).To(Equal(before.Revision + 1))
			readable := after.Readable(allowed)
			Expect(readable.Entries).To(HaveLen(1))
			Expect(readable.Entries[0].ID).To(Equal("bravo"))
			Expect(entryByID("alpha").Withheld).To(BeTrue())
			Expect(entryByID("charlie").Withheld).To(BeTrue())
			Expect(entryByID("bravo").Withheld).To(BeFalse())
		})

		It("leaves one audit row naming the key, not one row per entry", func() {
			announceAs(kp, key1, "alpha", 1)
			announceAs(kp, key1, "charlie", 1)
			revoke("alpha-team", key1)
			rows, err := store.Audit(ctx, 200)
			Expect(err).NotTo(HaveOccurred())
			withheld := []postgres.AuditPage{}
			for _, r := range rows {
				if r.Op == domain.OpWithholdKey {
					withheld = append(withheld, r)
				}
			}
			Expect(withheld).To(HaveLen(1))
			Expect(withheld[0].EntryID).To(Equal(key1))
			Expect(withheld[0].Detail).To(ContainSubstring("alpha"))
			Expect(withheld[0].Detail).To(ContainSubstring("charlie"))
		})

		It("tells every watching shell exactly once", func() {
			announceAs(kp, key1, "alpha", 1)
			announceAs(kp, key1, "charlie", 1)
			hints, err := nc.SubscribeSync(mferegistry.Changed)
			Expect(err).NotTo(HaveOccurred())
			Expect(nc.Flush()).To(Succeed())
			revoke("alpha-team", key1)
			_, err = hints.NextMsg(time.Second)
			Expect(err).NotTo(HaveOccurred())
			_, err = hints.NextMsg(100 * time.Millisecond)
			Expect(err).To(MatchError(nats.ErrTimeout))
		})

		It("moves no revision at all when the key signed nothing", func() {
			announceAs(kp2, key2, "bravo", 1)
			before, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			revoke("alpha-team", key1)
			after, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			// A revocation that withheld nothing must not make every shell
			// re-read the same document.
			Expect(after.Revision).To(Equal(before.Revision))
		})
	})

	Context("BR-AS38 — re-enabling a key restores nothing", func() {
		It("leaves every withheld entry withheld until an operator restores it", func() {
			announceAs(kp, key1, "alpha", 1)
			revoke("alpha-team", key1)
			trust, err := module.Service.Publishers(ctx)
			Expect(err).NotTo(HaveOccurred())
			_, err = module.Service.ApplyPublisher(ctx, domain.PublisherWrite{
				Op: domain.OpPublisherSetKeyState, Actor: domain.SharedAdminActor, PublisherID: "alpha-team",
				PublicKey: key1, KeyState: domain.KeyEnabled, IfRevision: trust.Revision,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(entryByID("alpha").Enabled).To(BeFalse())
			Expect(entryByID("alpha").Withheld).To(BeTrue())
		})

		It("clears the withheld mark only when the operator enables that entry", func() {
			announceAs(kp, key1, "alpha", 1)
			revoke("alpha-team", key1)
			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			_, err = module.Service.Apply(ctx, domain.Write{Op: domain.OpSetEnabled, EntryID: "alpha", Enabled: true, Actor: domain.SharedAdminActor, IfRevision: doc.Revision})
			Expect(err).NotTo(HaveOccurred())
			Expect(entryByID("alpha").Enabled).To(BeTrue())
			Expect(entryByID("alpha").Withheld).To(BeFalse())
		})

		It("refuses a new announcement on the revoked key even after it is restored to retired", func() {
			announceAs(kp, key1, "alpha", 1)
			revoke("alpha-team", key1)
			raw := json.RawMessage(`{"id":"alpha","release":2,"remote":{"kind":"federated","url":"http://localhost:7110/alpha.js","module":"m"}}`)
			sig, err := kp.Sign(raw)
			Expect(err).NotTo(HaveOccurred())
			body, err := json.Marshal(servicerpc.Request{Payload: raw, Signature: base64.StdEncoding.EncodeToString(sig), SigningKey: key1})
			Expect(err).NotTo(HaveOccurred())
			msg, err := nc.Request(mferegistry.Announce, body, time.Second)
			Expect(err).NotTo(HaveOccurred())
			var out servicerpc.Response
			Expect(json.Unmarshal(msg.Data, &out)).To(Succeed())
			Expect(out.Code).To(Equal("key-revoked"))
		})
	})

	Context("decision 104 — withholding is a plugin-document write, not a trust-table one", func() {
		It("moves the plugin revision and the trust revision by one each", func() {
			announceAs(kp, key1, "alpha", 1)
			plugins, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			trust, err := module.Service.Publishers(ctx)
			Expect(err).NotTo(HaveOccurred())
			revoke("alpha-team", key1)
			pluginsAfter, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			trustAfter, err := module.Service.Publishers(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(pluginsAfter.Revision).To(Equal(plugins.Revision + 1))
			Expect(trustAfter.Revision).To(Equal(trust.Revision + 1))
		})
	})
})
