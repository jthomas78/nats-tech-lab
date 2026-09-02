package registry_test

// Phase 5b — the unregister transport. Derived from BR-AS54 and BR-AS55.
//
// The subject is service-only, like the announcement it undoes: no browser
// credential carries it, and the grant test iterates the browser surface to
// prove a new subject cannot be added without a decision about who may reach
// it. Everything else here is the domain gate seen from outside — the same
// refusals, each with its own code, so a publisher can act on the answer.

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
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/servicerpc"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

var _ = Describe("BR-AS54 — the unregister transport", func() {
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
		nc, err = nats.Connect(srv.ClientURL(), nats.Name("registry-unregister-test"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(nc.Close)
		module, err = registry.Startup(ctx, pgDB, nil, nil, allowed, slog.Default())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(module.Stop)
		store = postgres.NewStore(pgDB, allowed)
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
		adapter, err := servicerpc.Mount(nc, servicerpc.New(module.Service, store, domain.NKeyVerifier{}), slog.Default())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(adapter.Stop)
		Expect(nc.Flush()).To(Succeed())
	})

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

	// enable is the operator step an announcement always waits for.
	enable := func(id string) {
		doc, err := store.Current(ctx)
		Expect(err).NotTo(HaveOccurred())
		_, err = module.Service.Apply(ctx, domain.Write{
			Op: domain.OpSetEnabled, EntryID: id, Enabled: true,
			Actor: domain.SharedAdminActor, IfRevision: doc.Revision,
		})
		Expect(err).NotTo(HaveOccurred())
	}

	sendRaw := func(raw json.RawMessage, signature, signingKey string) servicerpc.UnregisterResponse {
		body, err := json.Marshal(servicerpc.Request{Payload: raw, Signature: signature, SigningKey: signingKey})
		Expect(err).NotTo(HaveOccurred())
		msg, err := nc.Request(mferegistry.Unregister, body, time.Second)
		Expect(err).NotTo(HaveOccurred())
		var out servicerpc.UnregisterResponse
		Expect(json.Unmarshal(msg.Data, &out)).To(Succeed())
		return out
	}

	command := func(plugin string, at int64) json.RawMessage {
		return json.RawMessage(`{"schemaVersion":1,"action":"unregister","plugin":"` + plugin +
			`","publisher":"test-publisher","signingKey":"` + publicKey +
			`","release":` + strconv.FormatInt(at, 10) + `}`)
	}

	unregister := func(plugin string, at int64) servicerpc.UnregisterResponse {
		raw := command(plugin, at)
		sig, err := kp.Sign(raw)
		Expect(err).NotTo(HaveOccurred())
		return sendRaw(raw, base64.StdEncoding.EncodeToString(sig), publicKey)
	}

	readShell := func() browserrpc.ReadResponse {
		msg, err := nc.Request(mferegistry.ShellRead, []byte(`{}`), time.Second)
		Expect(err).NotTo(HaveOccurred())
		var out browserrpc.ReadResponse
		Expect(json.Unmarshal(msg.Data, &out)).To(Succeed())
		return out
	}

	Context("no browser credential can send one", func() {
		It("is on none of the browser-facing subject lists", func() {
			Expect(mferegistry.Subjects()).NotTo(ContainElement(mferegistry.Unregister))
			Expect(mferegistry.Operator()).NotTo(ContainElement(mferegistry.Unregister))
			Expect(registry.Subjects()).NotTo(ContainElement(mferegistry.Unregister))
		})

		It("is an rpc subject, which a browser grant never carries", func() {
			Expect(mferegistry.Unregister).To(HavePrefix("rpc."))
		})
	})

	Context("the ordinary case", func() {
		It("withdraws an approved dynamic entry and takes it out of the shell's document", func() {
			Expect(announce("http://localhost:7110/r.js").Outcome).To(Equal(domain.AnnounceInserted))
			enable("plugin")
			Expect(readShell().Plugins).To(HaveLen(1))

			out := unregister("plugin", release+1)
			Expect(out.OK).To(BeTrue())
			Expect(out.Code).To(BeEmpty())
			Expect(out.Outcome).To(Equal(domain.UnregisterWithdrawn))
			// Served as a marker, not by vanishing: absence is not
			// authoritative, and a running shell must be able to tell a
			// withdrawal from an outage (BR-AS54).
			after := readShell().Plugins
			Expect(after).To(HaveLen(1))
			Expect(after[0].Withdrawn).To(BeTrue())
			Expect(after[0].Remote.URL).To(BeEmpty(), "nothing left for a shell to load")
			Expect(after[0].Contributions).To(BeEmpty())

			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Entries[0].Withdrawn).To(BeTrue())
			Expect(doc.Entries[0].Enabled).To(BeTrue(), "approval is not the publisher's to remove")
		})

		It("answers a duplicate delivery the same way, without a second write", func() {
			announce("http://localhost:7110/r.js")
			enable("plugin")
			first := unregister("plugin", release+1)
			Expect(first.OK).To(BeTrue())

			second := unregister("plugin", release+1)
			Expect(second.OK).To(BeTrue())
			Expect(second.NoOp).To(BeTrue())
			Expect(second.Revision).To(Equal(first.Revision), "a retry moves nothing")
		})
	})

	Context("refusals name what was wrong", func() {
		It("refuses an announcement replayed on the unregister subject", func() {
			announce("http://localhost:7110/r.js")
			raw := json.RawMessage(`{"id":"plugin","name":"from publisher","release":9,"contributions":[{"kind":"shell-footer","id":"status"}],"remote":{"kind":"federated","url":"http://localhost:7110/r.js","module":"plugin"}}`)
			sig, err := kp.Sign(raw)
			Expect(err).NotTo(HaveOccurred())
			out := sendRaw(raw, base64.StdEncoding.EncodeToString(sig), publicKey)
			Expect(out.OK).To(BeFalse())
			Expect(out.Code).To(Equal("not-unregister"))
		})

		It("refuses a signature made by a key that does not own the plugin", func() {
			announce("http://localhost:7110/r.js")
			stranger, err := nkeys.CreateUser()
			Expect(err).NotTo(HaveOccurred())
			pub, err := stranger.PublicKey()
			Expect(err).NotTo(HaveOccurred())
			raw := json.RawMessage(`{"schemaVersion":1,"action":"unregister","plugin":"plugin","publisher":"test-publisher","signingKey":"` + pub + `","release":9}`)
			sig, err := stranger.Sign(raw)
			Expect(err).NotTo(HaveOccurred())
			out := sendRaw(raw, base64.StdEncoding.EncodeToString(sig), pub)
			Expect(out.OK).To(BeFalse())
			Expect(out.Code).To(Equal("key-not-trusted"))
		})

		It("refuses a request naming a different key from the signed bytes", func() {
			announce("http://localhost:7110/r.js")
			other, err := nkeys.CreateUser()
			Expect(err).NotTo(HaveOccurred())
			pub, err := other.PublicKey()
			Expect(err).NotTo(HaveOccurred())
			raw := command("plugin", 9)
			sig, err := kp.Sign(raw)
			Expect(err).NotTo(HaveOccurred())
			out := sendRaw(raw, base64.StdEncoding.EncodeToString(sig), pub)
			Expect(out.OK).To(BeFalse())
			Expect(out.Code).To(Equal("key-mismatch"))
		})

		It("refuses an id the registry does not hold", func() {
			out := unregister("plugin", 9)
			Expect(out.OK).To(BeFalse())
			Expect(out.Code).To(Equal("unknown-entry"))
		})

		It("refuses a release the running announcement already spent", func() {
			announce("http://localhost:7110/r.js")
			enable("plugin")
			out := unregister("plugin", release)
			Expect(out.OK).To(BeFalse())
			Expect(out.Code).To(Equal("release-reused"))
		})

		It("refuses a key revoked before the write commits, and changes nothing", func() {
			announce("http://localhost:7110/r.js")
			enable("plugin")
			before, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			trust, err := module.Service.Publishers(ctx)
			Expect(err).NotTo(HaveOccurred())
			_, err = module.Service.ApplyPublisher(ctx, domain.PublisherWrite{
				Op: domain.OpPublisherSetKeyState, Actor: domain.SharedAdminActor, PublisherID: "test-publisher",
				PublicKey: publicKey, KeyState: domain.KeyRevoked, IfRevision: trust.Revision,
			})
			Expect(err).NotTo(HaveOccurred())

			out := unregister("plugin", release+1)
			Expect(out.OK).To(BeFalse())
			Expect(out.Code).To(Equal("key-revoked"))

			after, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.Entries[0].Release).To(Equal(before.Entries[0].Release), "a refusal writes nothing")
		})
	})

	Context("BR-AS23 — the audit names the true actor", func() {
		It("records an accepted withdrawal under the publisher key", func() {
			announce("http://localhost:7110/r.js")
			enable("plugin")
			unregister("plugin", release+1)

			rows, err := store.Audit(ctx, 50)
			Expect(err).NotTo(HaveOccurred())
			entries := auditFor(rows, "plugin")
			Expect(entries).ToNot(BeEmpty())
			Expect(entries[0].Actor).To(Equal(publicKey))
			Expect(entries[0].Outcome).To(Equal(domain.AuditAccepted))
		})

		It("records a refused withdrawal without moving the revision", func() {
			announce("http://localhost:7110/r.js")
			enable("plugin")
			before, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(unregister("plugin", release).Code).To(Equal("release-reused"))

			after, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.Revision).To(Equal(before.Revision))
		})
	})
})
