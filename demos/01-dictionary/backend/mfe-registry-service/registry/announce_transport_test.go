package registry_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/servicerpc"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

// auditFor narrows the shared audit table to one entry id in the plugin
// document's own scope. Trust-table writes land in the same table, and a
// transfer files itself under the plugin id it moves, so the id alone is not
// enough to tell the two apart — the op is.
func auditFor(rows []postgres.AuditPage, entryID string) []postgres.AuditPage {
	out := []postgres.AuditPage{}
	for _, r := range rows {
		if r.EntryID == entryID && !strings.HasPrefix(r.Op, "publisher-") {
			out = append(out, r)
		}
	}
	return out
}

var _ = Describe("service announcement transport", func() {
	var ctx context.Context
	var nc *nats.Conn
	var module *registry.Module
	var store *postgres.Store
	var kp nkeys.KeyPair
	var publicKey string
	var release int64
	allowed := domain.NewAllowlist([]string{"http://localhost:7110", "http://localhost:7112"})
	BeforeEach(func() {
		if pgUnavailable != "" {
			Skip(pgUnavailable)
		}
		GinkgoT().Setenv("REGISTRY_PRELOAD_FILE", "")
		ctx = context.Background()
		_, err := pgDB.ExecContext(ctx, `TRUNCATE registry.entries, registry.audit, registry.plugin_owners, registry.publisher_keys, registry.publishers; UPDATE registry.revision SET revision=0; UPDATE registry.publisher_revision SET revision=0`)
		Expect(err).NotTo(HaveOccurred())
		kp, err = nkeys.CreateUser()
		Expect(err).NotTo(HaveOccurred())
		publicKey, err = kp.PublicKey()
		Expect(err).NotTo(HaveOccurred())
		release = 0
		srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1})
		Expect(err).NotTo(HaveOccurred())
		srv.Start()
		DeferCleanup(srv.Shutdown)
		Expect(srv.ReadyForConnections(5 * time.Second)).To(BeTrue())
		nc, err = nats.Connect(srv.ClientURL(), nats.Name("registry-announcement-test"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(nc.Close)
		module, err = registry.Startup(ctx, pgDB, nil, nil, allowed, slog.Default())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(module.Stop)
		store = postgres.NewStore(pgDB, allowed)
		// The trust table is the anchor now, not the verifier: one publisher,
		// one enabled key, owning the one plugin id these specs announce.
		trust, err := module.Service.ApplyPublisher(ctx, domain.PublisherWrite{
			Op: domain.OpPublisherUpsert, Actor: domain.SharedAdminActor, PublisherID: "test-publisher",
			Publisher: &domain.Publisher{ID: "test-publisher", Name: "Test Publisher"}, IfRevision: 0,
		})
		Expect(err).NotTo(HaveOccurred())
		trust, err = module.Service.ApplyPublisher(ctx, domain.PublisherWrite{
			Op: domain.OpPublisherAddKey, Actor: domain.SharedAdminActor, PublisherID: "test-publisher",
			PublicKey: publicKey, IfRevision: trust.Revision,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = module.Service.ApplyPublisher(ctx, domain.PublisherWrite{
			Op: domain.OpPublisherTransfer, Actor: domain.SharedAdminActor, PublisherID: "test-publisher",
			PluginID: "plugin", IfRevision: trust.Revision,
		})
		Expect(err).NotTo(HaveOccurred())
		browser, err := browserrpc.Mount(nc, browserrpc.New(module.Service, store), slog.Default())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(browser.Stop)
	})
	mount := func(v domain.Verifier) {
		adapter, err := servicerpc.Mount(nc, servicerpc.New(module.Service, store, v), slog.Default())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(adapter.Stop)
		Expect(nc.Flush()).To(Succeed())
	}
	// send is the raw form: whatever payload, signature and key are given.
	send := func(raw json.RawMessage, signature, signingKey string) servicerpc.Response {
		body, err := json.Marshal(servicerpc.Request{Payload: raw, Signature: signature, SigningKey: signingKey})
		Expect(err).NotTo(HaveOccurred())
		msg, err := nc.Request(mferegistry.Announce, body, time.Second)
		Expect(err).NotTo(HaveOccurred())
		var out servicerpc.Response
		Expect(json.Unmarshal(msg.Data, &out)).To(Succeed())
		return out
	}
	// announce is the ordinary path: a real signature over a real payload,
	// with the release counter advancing the way a publisher's would.
	announce := func(url string) servicerpc.Response {
		release++
		raw := json.RawMessage(`{"id":"plugin","name":"from publisher","release":` + strconv.FormatInt(release, 10) + `,"contributions":[{"kind":"shell-footer","id":"status"}],"remote":{"kind":"federated","url":"` + url + `","module":"plugin"}}`)
		sig, err := kp.Sign(raw)
		Expect(err).NotTo(HaveOccurred())
		body, err := json.Marshal(servicerpc.Request{Payload: raw, Signature: base64.StdEncoding.EncodeToString(sig), SigningKey: publicKey})
		Expect(err).NotTo(HaveOccurred())
		msg, err := nc.Request(mferegistry.Announce, body, time.Second)
		Expect(err).NotTo(HaveOccurred())
		var out servicerpc.Response
		Expect(json.Unmarshal(msg.Data, &out)).To(Succeed())
		return out
	}
	readShell := func() browserrpc.ReadResponse {
		msg, err := nc.Request(mferegistry.ShellRead, []byte(`{}`), time.Second)
		Expect(err).NotTo(HaveOccurred())
		var out browserrpc.ReadResponse
		Expect(json.Unmarshal(msg.Data, &out)).To(Succeed())
		return out
	}
	Context("BR-AS39 — announcement never activates", func() {
		It("is absent from the shell until an operator enables it", func() {
			mount(domain.NKeyVerifier{})
			out := announce("http://localhost:7110/r.js")
			Expect(out.OK).To(BeTrue())
			Expect(out.Outcome).To(Equal(domain.AnnounceInserted))
			Expect(readShell().Plugins).To(BeEmpty())
			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Entries).To(HaveLen(1))
			Expect(doc.Entries[0].Lifecycle).To(Equal(domain.LifecycleDynamic))
			Expect(doc.Entries[0].Enabled).To(BeFalse())
			_, err = module.Service.Apply(ctx, domain.Write{Op: domain.OpSetEnabled, EntryID: "plugin", Enabled: true, Actor: domain.SharedAdminActor, IfRevision: doc.Revision})
			Expect(err).NotTo(HaveOccurred())
			Expect(readShell().Plugins).To(HaveLen(1))
		})
	})
	Context("BR-AS21 — no self-activation without the publisher trust anchor", func() {
		It("refuses unsigned and unverifiable payloads before parsing them or writing", func() {
			mount(domain.NKeyVerifier{})
			raw := json.RawMessage(`{"id":"plugin","release":1,"name":"fixture","contributions":[{"kind":"shell-footer","id":"status"}],"remote":{"kind":"federated","url":"http://localhost:7110/r.js","module":"plugin"}}`)
			// Unsigned, then signed-but-not-by-this-key, then a key nobody
			// trusts. Three different causes, all refused, none written.
			Expect(send(raw, "", publicKey).Code).To(Equal("unsigned"))
			Expect(send(raw, "bm90LWEtc2lnbmF0dXJl", publicKey).Code).To(Equal("unverified"))
			other, err := nkeys.CreateUser()
			Expect(err).NotTo(HaveOccurred())
			otherPublic, err := other.PublicKey()
			Expect(err).NotTo(HaveOccurred())
			sig, err := other.Sign(raw)
			Expect(err).NotTo(HaveOccurred())
			Expect(send(raw, base64.StdEncoding.EncodeToString(sig), otherPublic).Code).To(Equal("key-not-trusted"))
			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Revision).To(BeZero())
			Expect(doc.Entries).To(BeEmpty())
		})
		It("keeps announcement outside the exhaustive browser surface", func() {
			Expect(mferegistry.Subjects()).NotTo(ContainElement(mferegistry.Announce))
			Expect(mferegistry.Operator()).NotTo(ContainElement(mferegistry.Announce))
			Expect(registry.Subjects()).NotTo(ContainElement(mferegistry.Announce))
		})
		It("mounts a real verifier in production Startup and stops it", func() {
			// Stop the test-only browser mount before composing a second service on this connection.
			prod, err := registry.Startup(ctx, pgDB, nil, nc, allowed, slog.Default())
			Expect(err).NotTo(HaveOccurred())
			Expect(nc.Flush()).To(Succeed())
			// Production no longer refuses everything on principle (7c). It
			// refuses this because the signature does not check out, and it
			// accepts the very next call because the trust table says so.
			raw := json.RawMessage(`{"id":"plugin","release":1,"name":"fixture","contributions":[{"kind":"shell-footer","id":"status"}],"remote":{"kind":"federated","url":"http://localhost:7110/r.js","module":"plugin"}}`)
			Expect(send(raw, "bm90LWEtc2lnbmF0dXJl", publicKey).Code).To(Equal("unverified"))
			Expect(announce("http://localhost:7110/r.js").OK).To(BeTrue())
			Expect(prod.Stop()).To(Succeed())
			Expect(nc.Flush()).To(Succeed())
			_, err = nc.Request(mferegistry.Announce, []byte(`{}`), time.Second)
			Expect(err).To(MatchError(nats.ErrNoResponders))
		})
	})
	Context("BR-AS40 — dynamic re-announcement follows within its origin", func() {
		It("updates an enabled remote in-origin and requeues across origins", func() {
			mount(domain.NKeyVerifier{})
			announce("http://localhost:7110/old.js")
			_, err := module.Service.Apply(ctx, domain.Write{Op: domain.OpSetEnabled, EntryID: "plugin", Enabled: true, Actor: domain.SharedAdminActor, IfRevision: 1})
			Expect(err).NotTo(HaveOccurred())
			Expect(announce("http://localhost:7110/new.js").Outcome).To(Equal(domain.AnnounceUpdated))
			Expect(readShell().Plugins[0].Remote.URL).To(Equal("http://localhost:7110/new.js"))
			Expect(announce("http://localhost:7112/r.js").Outcome).To(Equal(domain.AnnounceRequeued))
			Expect(readShell().Plugins).To(BeEmpty())
		})
	})
	Context("decision 77 / BR-AS42 — static wins, but ignored announcements name their publisher", func() {
		It("writes only an audit row, preserving entry, revision and notifications", func() {
			mount(domain.NKeyVerifier{})
			entry := federated("plugin", "http://localhost:7110/original.js")
			before, err := module.Service.Apply(ctx, domain.PreloadWrite(entry, 0))
			Expect(err).NotTo(HaveOccurred())
			hints, err := nc.SubscribeSync(mferegistry.Changed)
			Expect(err).NotTo(HaveOccurred())
			Expect(nc.Flush()).To(Succeed())
			out := announce("http://localhost:7112/new.js")
			Expect(out.OK).To(BeTrue())
			Expect(out.Outcome).To(Equal(domain.AnnounceIgnored))
			after, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(after).To(Equal(before))
			rows, err := store.Audit(ctx, 100)
			Expect(err).NotTo(HaveOccurred())
			// The trust table shares this audit table under its own scope, so
			// count what this plugin id did, not what the whole table holds.
			Expect(auditFor(rows, "plugin")).To(HaveLen(2))
			Expect(rows[0].Actor).To(Equal(publicKey))
			Expect(rows[0].Outcome).To(Equal("ignored"))
			Expect(rows[0].Detail).To(ContainSubstring("static"))
			Expect(rows[0].Revision).To(BeNil())
			_, err = hints.NextMsg(30 * time.Millisecond)
			Expect(err).To(MatchError(nats.ErrTimeout))
		})
	})
	Context("gate answer 2 / BR-AS42 — announcement history retains its first and latest observation", func() {
		It("preserves announcedAt, advances lastAnnouncedAt, and audits the publisher", func() {
			mount(domain.NKeyVerifier{})
			announce("http://localhost:7110/old.js")
			before, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			first, err := time.Parse(time.RFC3339Nano, before.Entries[0].AnnouncedAt)
			Expect(err).NotTo(HaveOccurred())
			Expect(announce("http://localhost:7110/new.js").Outcome).To(Equal(domain.AnnouncePending))
			after, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.Entries[0].AnnouncedAt).To(Equal(before.Entries[0].AnnouncedAt))
			last, err := time.Parse(time.RFC3339Nano, after.Entries[0].LastAnnouncedAt)
			Expect(err).NotTo(HaveOccurred())
			Expect(last.After(first)).To(BeTrue())
			rows, err := store.Audit(ctx, 100)
			Expect(err).NotTo(HaveOccurred())
			for _, row := range auditFor(rows, "plugin") {
				Expect(row.Actor).To(Equal(publicKey))
			}
		})
	})
	Context("BR-AS43 / BR-AS20 — manifest trust claims and off-list remotes are refused", func() {
		It("refuses each self-asserted field without a stored entry", func() {
			mount(domain.NKeyVerifier{})
			for _, field := range []string{"source", "lifecycle", "enabled", "revision"} {
				out := send(json.RawMessage(`{"id":"plugin","`+field+`":null}`), "bm90LWEtc2lnbmF0dXJl", publicKey)
				Expect(out.OK).To(BeFalse())
				Expect(out.Code).To(Equal("manifest-refused"))
			}
			Expect(announce("https://off-list.example/r.js").Code).To(Equal("origin-not-allowed"))
			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Revision).To(BeZero())
		})
	})
	Context("BR-AS46 — ownership is answered before anything else", func() {
		It("refuses a perfectly signed announcement for a plugin id this publisher does not own", func() {
			mount(domain.NKeyVerifier{})
			raw := json.RawMessage(`{"id":"someone-elses","release":1,"name":"fixture","contributions":[{"kind":"shell-footer","id":"status"}],"remote":{"kind":"federated","url":"http://localhost:7110/r.js","module":"plugin"}}`)
			sig, err := kp.Sign(raw)
			Expect(err).NotTo(HaveOccurred())
			out := send(raw, base64.StdEncoding.EncodeToString(sig), publicKey)
			Expect(out.OK).To(BeFalse())
			Expect(out.Code).To(Equal("not-owned"))
			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Entries).To(BeEmpty())
		})
	})
	Context("BR-AS47 — the release counter closes replay", func() {
		It("accepts forward, refuses backward, and treats a re-send as a no-op", func() {
			mount(domain.NKeyVerifier{})
			Expect(announce("http://localhost:7110/one.js").OK).To(BeTrue())
			Expect(announce("http://localhost:7110/two.js").Outcome).To(Equal(domain.AnnouncePending))
			after, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.Entries[0].Release).To(Equal(int64(2)))

			// A captured earlier announcement, replayed verbatim. The
			// signature is genuine — the release is what refuses it.
			old := json.RawMessage(`{"id":"plugin","name":"from publisher","release":1,"contributions":[{"kind":"shell-footer","id":"status"}],"remote":{"kind":"federated","url":"http://localhost:7110/one.js","module":"plugin"}}`)
			sig, err := kp.Sign(old)
			Expect(err).NotTo(HaveOccurred())
			Expect(send(old, base64.StdEncoding.EncodeToString(sig), publicKey).Code).To(Equal("release-backwards"))

			// The same release again is the timed-out-and-retried case: told
			// yes, and nothing written twice.
			same := json.RawMessage(`{"id":"plugin","name":"from publisher","release":2,"contributions":[{"kind":"shell-footer","id":"status"}],"remote":{"kind":"federated","url":"http://localhost:7110/two.js","module":"plugin"}}`)
			sig, err = kp.Sign(same)
			Expect(err).NotTo(HaveOccurred())
			out := send(same, base64.StdEncoding.EncodeToString(sig), publicKey)
			Expect(out.OK).To(BeTrue())
			Expect(out.NoOp).To(BeTrue())
			again, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(again).To(Equal(after))
		})
	})
	Context("BR-AS48 / decision 99 — trust is re-read where the revision is locked", func() {
		It("refuses a write whose key was revoked after it was verified", func() {
			mount(domain.NKeyVerifier{})
			Expect(announce("http://localhost:7110/one.js").OK).To(BeTrue())
			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			trust, err := module.Service.Publishers(ctx)
			Expect(err).NotTo(HaveOccurred())
			_, err = module.Service.ApplyPublisher(ctx, domain.PublisherWrite{
				Op: domain.OpPublisherSetKeyState, Actor: domain.SharedAdminActor, PublisherID: "test-publisher",
				PublicKey: publicKey, KeyState: domain.KeyRevoked, IfRevision: trust.Revision,
			})
			Expect(err).NotTo(HaveOccurred())

			// The revocation withheld what the key signed, so the plugin
			// document has moved (BR-AS38). The write below is keyed on where
			// it landed: this spec is about the trust re-read, and a stale
			// revision would refuse it one check earlier.
			withheld, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(withheld.Revision).To(Equal(doc.Revision + 1))

			// The write below already passed verification against a trust
			// table read before the revocation. The store re-reads the key.
			entry := doc.Entries[0]
			write := domain.AnnounceWrite(entry, publicKey, withheld.Revision)
			write.RequireKeyEnabled = publicKey
			_, err = store.Apply(ctx, write)
			Expect(errors.Is(err, domain.ErrKeyRevoked)).To(BeTrue())
			after, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.Revision).To(Equal(withheld.Revision))
		})

		It("lets an operator write through, having no publisher key to re-read", func() {
			mount(domain.NKeyVerifier{})
			Expect(announce("http://localhost:7110/one.js").OK).To(BeTrue())
			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			_, err = store.Apply(ctx, domain.Write{Op: domain.OpSetEnabled, EntryID: "plugin", Enabled: true, Actor: domain.SharedAdminActor, IfRevision: doc.Revision})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
