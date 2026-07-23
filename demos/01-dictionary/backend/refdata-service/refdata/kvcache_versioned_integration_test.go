package refdata_test

// Integration coverage for Phase 12.5 — hybrid KV materialization — against
// a real embedded NATS/JetStream server (KV TTL and rewrite-on-read are
// genuine NATS server behavior, not something a fake can stand in for) and
// the same disposable Postgres container corpus_repository_integration_test.go
// uses. Skips if either is unavailable, same convention as that file.

import (
	"context"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvcache"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/postgres"
)

func newEmbeddedJetStream() jetstream.JetStream {
	GinkgoHelper()
	opts := &server.Options{JetStream: true, StoreDir: GinkgoT().TempDir(), Port: -1}
	srv, err := server.NewServer(opts)
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	DeferCleanup(srv.Shutdown)
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

	nc, err := nats.Connect(srv.ClientURL(), nats.Name("refdata-service-test"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)
	Expect(nc.Opts.Name).NotTo(BeEmpty(), "nats connection must be named")

	js, err := jetstream.New(nc)
	Expect(err).NotTo(HaveOccurred())
	return js
}

var _ = Describe("Hybrid KV materialization for corpus versions (Phase 12.5, NATS + Postgres integration)", func() {
	var (
		contexts *postgres.ContextRepository
		corpus   *postgres.CorpusRepository
		kv       *kvstore.Store
		notifier *kvcache.VersionNotifier
		reader   *kvcache.VersionReader
	)

	BeforeEach(func() {
		if integrationDB == nil {
			Skip("docker postgres unavailable: " + integrationUnavailable)
		}
		contexts = postgres.NewContextRepository(integrationDB)
		corpus = postgres.NewCorpusRepository(integrationDB)
		kv = kvstore.New(newEmbeddedJetStream(), "refdata-it")
		notifier = kvcache.NewVersionNotifier(kv, corpus)
		reader = kvcache.NewVersionReader(kv)
	})

	It("eagerly materializes a published version's flattened content, including inherited items and their localizations", func() {
		parent, child := uniqueContext("kv-parent"), uniqueContext("kv-child")
		Expect(contexts.Register(context.Background(), domain.Context{Context: parent, Name: parent})).To(Succeed())
		Expect(contexts.Register(context.Background(), domain.Context{Context: child, Parent: parent, Name: child})).To(Succeed())

		Expect(seedWorkingItem(integrationDB, parent, "currency", "usd", map[string]any{"name": "US Dollar"})).To(Succeed())
		Expect(seedWorkingLocalization(integrationDB, parent, "currency", "usd", "en", "US Dollar")).To(Succeed())
		_, err := corpus.CreateDraft(context.Background(), parent, "seed")
		Expect(err).NotTo(HaveOccurred())
		parentPublished, err := corpus.Publish(context.Background(), parent)
		Expect(err).NotTo(HaveOccurred())
		Expect(notifier.NotifyPublished(context.Background(), parent, parentPublished.Version)).To(Succeed())

		_, err = corpus.CreateDraft(context.Background(), child, "child draft")
		Expect(err).NotTo(HaveOccurred())
		childPublished, err := corpus.Publish(context.Background(), child)
		Expect(err).NotTo(HaveOccurred())
		Expect(notifier.NotifyPublished(context.Background(), child, childPublished.Version)).To(Succeed())

		entry, err := reader.Get(context.Background(), child, childPublished.Version, "currency", "usd")
		Expect(err).NotTo(HaveOccurred())
		Expect(entry.Item.Attrs["name"]).To(Equal("US Dollar"))
		Expect(entry.SourceContext).To(Equal(parent))
		Expect(entry.IsOverride).To(BeFalse())
		Expect(entry.Localizations).To(HaveKey("en"))
		Expect(entry.Localizations["en"].Label).To(Equal("US Dollar"))

		entries, err := reader.List(context.Background(), child, childPublished.Version, "currency")
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(1))
	})

	It("supersedes a version's bucket TTL on the next publish while leaving the new version's bucket without a TTL, and keeps a pinned old version readable via rewrite-on-read", func() {
		ctxName := uniqueContext("kv-pin")
		Expect(contexts.Register(context.Background(), domain.Context{Context: ctxName, Name: ctxName})).To(Succeed())

		Expect(seedWorkingItem(integrationDB, ctxName, "currency", "usd", map[string]any{"name": "v1"})).To(Succeed())
		_, err := corpus.CreateDraft(context.Background(), ctxName, "v1")
		Expect(err).NotTo(HaveOccurred())
		v1, err := corpus.Publish(context.Background(), ctxName)
		Expect(err).NotTo(HaveOccurred())
		Expect(notifier.NotifyPublished(context.Background(), ctxName, v1.Version)).To(Succeed())

		v1Bucket, err := kv.VersionedBucketHandle(context.Background(), ctxName, v1.Version)
		Expect(err).NotTo(HaveOccurred())
		v1Status, err := v1Bucket.Status(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(v1Status.TTL()).To(Equal(time.Duration(0)))

		Expect(seedWorkingItem(integrationDB, ctxName, "currency", "usd", map[string]any{"name": "v2"})).To(Succeed())
		_, err = corpus.CreateDraft(context.Background(), ctxName, "v2")
		Expect(err).NotTo(HaveOccurred())
		v2, err := corpus.Publish(context.Background(), ctxName)
		Expect(err).NotTo(HaveOccurred())
		Expect(notifier.NotifyPublished(context.Background(), ctxName, v2.Version)).To(Succeed())

		v1Bucket, err = kv.VersionedBucketHandle(context.Background(), ctxName, v1.Version)
		Expect(err).NotTo(HaveOccurred())
		v1Status, err = v1Bucket.Status(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(v1Status.TTL()).To(Equal(kvcache.SupersededVersionTTL))

		v2Bucket, err := kv.VersionedBucketHandle(context.Background(), ctxName, v2.Version)
		Expect(err).NotTo(HaveOccurred())
		v2Status, err := v2Bucket.Status(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(v2Status.TTL()).To(Equal(time.Duration(0)))

		// A consumer still pinned to v1 can read it, and that read rewrites
		// the key — resetting its TTL clock rather than letting it silently
		// expire out from under an active pin.
		entry, err := reader.Get(context.Background(), ctxName, v1.Version, "currency", "usd")
		Expect(err).NotTo(HaveOccurred())
		Expect(entry.Item.Attrs["name"]).To(Equal("v1"))

		entryV2, err := reader.Get(context.Background(), ctxName, v2.Version, "currency", "usd")
		Expect(err).NotTo(HaveOccurred())
		Expect(entryV2.Item.Attrs["name"]).To(Equal("v2"))
	})

	It("returns ErrVersionedKeyNotFound for an unknown version or item", func() {
		ctxName := uniqueContext("kv-missing")
		Expect(contexts.Register(context.Background(), domain.Context{Context: ctxName, Name: ctxName})).To(Succeed())

		_, err := reader.Get(context.Background(), ctxName, 999, "currency", "usd")
		Expect(err).To(HaveOccurred())

		Expect(seedWorkingItem(integrationDB, ctxName, "currency", "usd", map[string]any{"name": "v1"})).To(Succeed())
		_, err = corpus.CreateDraft(context.Background(), ctxName, "v1")
		Expect(err).NotTo(HaveOccurred())
		v1, err := corpus.Publish(context.Background(), ctxName)
		Expect(err).NotTo(HaveOccurred())
		Expect(notifier.NotifyPublished(context.Background(), ctxName, v1.Version)).To(Succeed())

		_, err = reader.Get(context.Background(), ctxName, v1.Version, "currency", "gbp")
		Expect(err).To(HaveOccurred())
	})
})
