package registry_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/servicerpc"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

// Only this test double accepts signatures. Production Startup always uses NoVerifier.
type announcementVerifier struct{}

func (announcementVerifier) Verify(payload []byte, signature string) (string, error) {
	if signature != "test-signature" {
		return "", domain.ErrUnverified
	}
	return "publisher-key", nil
}

var _ = Describe("service announcement transport", func() {
	var ctx context.Context
	var nc *nats.Conn
	var module *registry.Module
	var store *postgres.Store
	allowed := domain.NewAllowlist([]string{"http://localhost:7110", "http://localhost:7112"})
	BeforeEach(func() {
		if pgUnavailable != "" {
			Skip(pgUnavailable)
		}
		GinkgoT().Setenv("REGISTRY_PRELOAD_FILE", "")
		ctx = context.Background()
		_, err := pgDB.ExecContext(ctx, `TRUNCATE registry.entries, registry.audit; UPDATE registry.revision SET revision=0`)
		Expect(err).NotTo(HaveOccurred())
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
	announce := func(url, signature string) servicerpc.Response {
		raw := json.RawMessage(`{"id":"plugin","name":"from publisher","remote":{"kind":"federated","url":"` + url + `","module":"plugin"}}`)
		body, err := json.Marshal(servicerpc.Request{Payload: raw, Signature: signature})
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
			mount(announcementVerifier{})
			out := announce("http://localhost:7110/r.js", "test-signature")
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
			mount(domain.NoVerifier{})
			for _, signature := range []string{"", "anything"} {
				out := announce("http://localhost:7110/r.js", signature)
				Expect(out.OK).To(BeFalse())
				Expect(out.Error).NotTo(BeEmpty())
			}
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
		It("mounts the fail-closed handler in production Startup and stops it", func() {
			// Stop the test-only browser mount before composing a second service on this connection.
			prod, err := registry.Startup(ctx, pgDB, nil, nc, allowed, slog.Default())
			Expect(err).NotTo(HaveOccurred())
			Expect(nc.Flush()).To(Succeed())
			Expect(announce("http://localhost:7110/r.js", "test-signature").Code).To(Equal("unverified"))
			Expect(prod.Stop()).To(Succeed())
			Expect(nc.Flush()).To(Succeed())
			_, err = nc.Request(mferegistry.Announce, []byte(`{}`), time.Second)
			Expect(err).To(MatchError(nats.ErrNoResponders))
		})
	})
	Context("BR-AS40 — dynamic re-announcement follows within its origin", func() {
		It("updates an enabled remote in-origin and requeues across origins", func() {
			mount(announcementVerifier{})
			announce("http://localhost:7110/old.js", "test-signature")
			_, err := module.Service.Apply(ctx, domain.Write{Op: domain.OpSetEnabled, EntryID: "plugin", Enabled: true, Actor: domain.SharedAdminActor, IfRevision: 1})
			Expect(err).NotTo(HaveOccurred())
			Expect(announce("http://localhost:7110/new.js", "test-signature").Outcome).To(Equal(domain.AnnounceUpdated))
			Expect(readShell().Plugins[0].Remote.URL).To(Equal("http://localhost:7110/new.js"))
			Expect(announce("http://localhost:7112/r.js", "test-signature").Outcome).To(Equal(domain.AnnounceRequeued))
			Expect(readShell().Plugins).To(BeEmpty())
		})
	})
	Context("decision 77 / BR-AS42 — static wins, but ignored announcements name their publisher", func() {
		It("writes only an audit row, preserving entry, revision and notifications", func() {
			mount(announcementVerifier{})
			entry := federated("plugin", "http://localhost:7110/original.js")
			before, err := module.Service.Apply(ctx, domain.PreloadWrite(entry, 0))
			Expect(err).NotTo(HaveOccurred())
			hints, err := nc.SubscribeSync(mferegistry.Changed)
			Expect(err).NotTo(HaveOccurred())
			Expect(nc.Flush()).To(Succeed())
			out := announce("http://localhost:7112/new.js", "test-signature")
			Expect(out.OK).To(BeTrue())
			Expect(out.Outcome).To(Equal(domain.AnnounceIgnored))
			after, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(after).To(Equal(before))
			rows, err := store.Audit(ctx, 100)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(2))
			Expect(rows[0].Actor).To(Equal("publisher-key"))
			Expect(rows[0].Outcome).To(Equal("ignored"))
			Expect(rows[0].Detail).To(ContainSubstring("static"))
			Expect(rows[0].Revision).To(BeNil())
			_, err = hints.NextMsg(30 * time.Millisecond)
			Expect(err).To(MatchError(nats.ErrTimeout))
		})
	})
	Context("gate answer 2 / BR-AS42 — announcement history retains its first and latest observation", func() {
		It("preserves announcedAt, advances lastAnnouncedAt, and audits the publisher", func() {
			mount(announcementVerifier{})
			announce("http://localhost:7110/old.js", "test-signature")
			before, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			first, err := time.Parse(time.RFC3339Nano, before.Entries[0].AnnouncedAt)
			Expect(err).NotTo(HaveOccurred())
			Expect(announce("http://localhost:7110/new.js", "test-signature").Outcome).To(Equal(domain.AnnouncePending))
			after, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.Entries[0].AnnouncedAt).To(Equal(before.Entries[0].AnnouncedAt))
			last, err := time.Parse(time.RFC3339Nano, after.Entries[0].LastAnnouncedAt)
			Expect(err).NotTo(HaveOccurred())
			Expect(last.After(first)).To(BeTrue())
			rows, err := store.Audit(ctx, 100)
			Expect(err).NotTo(HaveOccurred())
			for _, row := range rows {
				Expect(row.Actor).To(Equal("publisher-key"))
			}
		})
	})
	Context("BR-AS43 / BR-AS20 — manifest trust claims and off-list remotes are refused", func() {
		It("refuses each self-asserted field without a stored entry", func() {
			mount(announcementVerifier{})
			for _, field := range []string{"source", "lifecycle", "enabled", "revision"} {
				body := []byte(`{"signature":"test-signature","payload":{"id":"plugin","` + field + `":null}}`)
				msg, err := nc.Request(mferegistry.Announce, body, time.Second)
				Expect(err).NotTo(HaveOccurred())
				var out servicerpc.Response
				Expect(json.Unmarshal(msg.Data, &out)).To(Succeed())
				Expect(out.OK).To(BeFalse())
				Expect(out.Code).To(Equal("manifest-refused"))
			}
			Expect(announce("https://off-list.example/r.js", "test-signature").Code).To(Equal("origin-not-allowed"))
			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Revision).To(BeZero())
		})
	})
})
