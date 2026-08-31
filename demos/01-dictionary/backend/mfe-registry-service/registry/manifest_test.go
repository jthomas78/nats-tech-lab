package registry_test

// Phase 7a — BR-AS37 and BR-AS50. The bytes a publisher signed are the bytes
// the registry stores, caches and serves. Every spec below is a byte-equality
// claim about one hop, because the failure these rules exist to stop is
// silent: a document that still parses, still renders, and no longer verifies.
//
// The fixture bytes are deliberately ugly — keys out of alphabetical order,
// two-space indentation, a trailing newline. JSONB reorders keys and strips
// whitespace, and an ordinary JSON round trip does the same, so pretty bytes
// would pass these specs without proving anything.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/kvcache"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/postgres"
)

// signedBytes is one manifest as a publisher would have emitted and signed it.
var signedBytes = []byte("{\n  \"remote\": {\n    \"module\": \"./plugin\",\n    \"kind\": \"federated\",\n    \"url\": \"http://localhost:7110/remoteEntry.js\"\n  },\n  \"id\": \"fleet\",\n  \"shellApiVersion\": 1,\n  \"schemaVersion\": 1,\n  \"name\": \"Fleet\",\n  \"contributions\": [{\"kind\": \"route\", \"id\": \"vessels\", \"path\": \"/fleet/vessels\", \"title\": \"Vessels\"}]\n}\n")

const signedSignature = "dGVzdC1zaWduYXR1cmU="

func signedEntry() domain.Entry {
	e, err := domain.EntryFromManifest(signedBytes, signedSignature)
	Expect(err).NotTo(HaveOccurred())
	return e
}

var _ = Describe("BR-AS37/BR-AS50 — the signed manifest is stored and served as signed", func() {
	Context("in the domain, the bytes are the entry and the fields are a projection", func() {
		It("keeps the verified bytes verbatim, whitespace and key order included", func() {
			Expect(signedEntry().Manifest.Bytes).To(Equal(signedBytes))
		})

		It("projects the queryable fields out of those bytes", func() {
			e := signedEntry()
			Expect(e.ID).To(Equal("fleet"))
			Expect(e.Remote.URL).To(Equal("http://localhost:7110/remoteEntry.js"))
			Expect(e.Contributions).To(HaveLen(1))
		})

		It("reports an untouched entry as attested", func() {
			Expect(signedEntry().Attested()).To(BeTrue())
		})

		It("still attests after the store's own columns change, which are not signed content", func() {
			e := signedEntry()
			e.Enabled = false
			e.Lifecycle = "static"
			e.LastAnnouncedAt = "2026-08-31T00:00:00Z"
			Expect(e.Attested()).To(BeTrue())
		})

		It("stops attesting when signed content is edited", func() {
			e := signedEntry()
			e.Remote.URL = "http://localhost:7999/remoteEntry.js"
			Expect(e.Attested()).To(BeFalse())
		})

		It("treats an unsigned entry as unsigned rather than as broken", func() {
			e := federated("hello", "http://localhost:7110/r.js")
			Expect(e.Signed()).To(BeFalse())
			Expect(e.Attested()).To(BeFalse())
		})
	})

	Context("across transport, the blob rides as base64 and nothing re-serialises it", func() {
		It("round-trips the bytes unchanged through a document marshal and unmarshal", func() {
			doc := domain.Document{SchemaVersion: domain.SchemaVersion, Revision: 1, Entries: []domain.Entry{signedEntry()}}
			wire, err := json.Marshal(doc)
			Expect(err).NotTo(HaveOccurred())
			var back domain.Document
			Expect(json.Unmarshal(wire, &back)).To(Succeed())
			Expect(back.Entries[0].Manifest.Bytes).To(Equal(signedBytes))
			Expect(back.Entries[0].Manifest.Signature).To(Equal(signedSignature))
			Expect(back.Entries[0].Attested()).To(BeTrue())
		})
	})

	Context("across Postgres, the projection columns never become the artifact", func() {
		It("returns the stored bytes byte-identically after a curated write", func() {
			if pgUnavailable != "" {
				Skip(pgUnavailable)
			}
			ctx := context.Background()
			_, err := pgDB.ExecContext(ctx, `TRUNCATE registry.entries, registry.audit; UPDATE registry.revision SET revision = 0`)
			Expect(err).NotTo(HaveOccurred())
			store := postgres.NewStore(pgDB, domain.NewAllowlist([]string{"http://localhost:7110"}))
			entry := signedEntry()
			entry.Enabled = true
			doc, err := store.Apply(ctx, domain.Write{Op: domain.OpUpsert, EntryID: "fleet", Actor: domain.SharedAdminActor, Entry: &entry, IfRevision: 0})
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Entries[0].Manifest.Bytes).To(Equal(signedBytes))

			read, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(read.Entries[0].Manifest.Bytes).To(Equal(signedBytes))
			Expect(read.Entries[0].Manifest.Signature).To(Equal(signedSignature))
			Expect(read.Entries[0].Attested()).To(BeTrue())
		})

		It("keeps the attestation intact when only a column-owned fact is toggled", func() {
			if pgUnavailable != "" {
				Skip(pgUnavailable)
			}
			ctx := context.Background()
			store := postgres.NewStore(pgDB, domain.NewAllowlist([]string{"http://localhost:7110"}))
			before, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			_, err = store.Apply(ctx, domain.Write{Op: domain.OpSetEnabled, EntryID: "fleet", Actor: domain.SharedAdminActor, Enabled: false, IfRevision: before.Revision})
			Expect(err).NotTo(HaveOccurred())
			after, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.Entries[0].Enabled).To(BeFalse())
			Expect(after.Entries[0].Manifest.Bytes).To(Equal(signedBytes))
			Expect(after.Entries[0].Attested()).To(BeTrue())
		})
	})

	Context("across the KV cache, the copy is a copy", func() {
		It("returns the stored bytes byte-identically after a cache put and get", func() {
			srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: GinkgoT().TempDir()})
			Expect(err).NotTo(HaveOccurred())
			srv.Start()
			DeferCleanup(srv.Shutdown)
			Expect(srv.ReadyForConnections(5 * time.Second)).To(BeTrue())
			nc, err := nats.Connect(srv.ClientURL(), nats.Name("registry-manifest-test"))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(nc.Close)
			js, err := jetstream.New(nc)
			Expect(err).NotTo(HaveOccurred())

			ctx := context.Background()
			cache := kvcache.New(js)
			doc := domain.Document{SchemaVersion: domain.SchemaVersion, Revision: 4, Entries: []domain.Entry{signedEntry()}}
			Expect(cache.Put(ctx, doc)).To(Succeed())
			back, ok, err := cache.Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
			Expect(back.Entries[0].Manifest.Bytes).To(Equal(signedBytes))
			Expect(back.Entries[0].Attested()).To(BeTrue())
		})
	})
})
