package registry_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/postgres"
)

var _ = Describe("Phase 8 persistence", func() {
	var ctx context.Context
	var store *postgres.Store
	allowed := domain.NewAllowlist([]string{"http://localhost:7110"})
	BeforeEach(func() {
		if pgUnavailable != "" {
			Skip(pgUnavailable)
		}
		ctx = context.Background()
		_, err := pgDB.ExecContext(ctx, `TRUNCATE registry.entries, registry.audit; UPDATE registry.revision SET revision = 0`)
		Expect(err).NotTo(HaveOccurred())
		store = postgres.NewStore(pgDB, allowed)
		GinkgoT().Setenv("REGISTRY_PRELOAD_FILE", "")
	})

	boot := func() *registry.Module {
		m, err := registry.Startup(ctx, pgDB, nil, nil, allowed, slog.Default())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(m.Stop)
		return m
	}
	const fixture = `{"schemaVersion":1,"plugins":[
		{"id":"edited","name":"original","enabled":true,"remote":{"kind":"federated","url":"http://localhost:7110/r.js"},"contributions":[{"kind":"shell-footer","id":"status"}]},
		{"id":"disabled","name":"Disabled","enabled":true,"remote":{"kind":"federated","url":"http://localhost:7110/r.js"},"contributions":[{"kind":"shell-footer","id":"status"}]},
		{"id":"removed","name":"Removed","enabled":true,"remote":{"kind":"federated","url":"http://localhost:7110/r.js"},"contributions":[{"kind":"shell-footer","id":"status"}]}
	]}`
	Context("BR-AS41 — preload never reverts curation", func() {
		It("seeds a fresh store once, with one revision and true actor per entry", func() {
			GinkgoT().Setenv("REGISTRY_PRELOAD_FILE", preloadFile(fixture))
			boot()
			boot()
			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Revision).To(Equal(int64(3)))
			Expect(doc.Entries).To(HaveLen(3))
			for _, e := range doc.Entries {
				Expect(e.Lifecycle).To(Equal(domain.LifecycleStatic))
				Expect(e.Enabled).To(BeTrue())
			}
			rows, err := store.Audit(ctx, 100)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(3))
			for _, row := range rows {
				Expect(row.Actor).To(Equal(domain.PreloadActor))
			}
		})
		It("does not revert an edit, disability, or removal when the file returns on restart", func() {
			path := preloadFile(fixture)
			GinkgoT().Setenv("REGISTRY_PRELOAD_FILE", path)
			m := boot()
			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			e := doc.Entries[1] // sorted: disabled, edited, removed
			e.Name = "operator edit"
			doc, err = m.Service.Apply(ctx, domain.Write{Op: domain.OpUpsert, EntryID: e.ID, Entry: &e, Actor: domain.SharedAdminActor, IfRevision: doc.Revision})
			Expect(err).NotTo(HaveOccurred())
			// BR-AS24 removal retains a disabled row; there is no delete operation.
			for _, id := range []string{"disabled", "removed"} {
				doc, err = m.Service.Apply(ctx, domain.Write{Op: domain.OpSetEnabled, EntryID: id, Enabled: false, Actor: domain.SharedAdminActor, IfRevision: doc.Revision})
				Expect(err).NotTo(HaveOccurred())
			}
			Expect(os.WriteFile(path, []byte(`{"schemaVersion":1,"plugins":[]}`), 0600)).To(Succeed())
			boot() // Removing a line is not a second curation channel.
			Expect(os.WriteFile(path, []byte(fixture), 0600)).To(Succeed())
			boot()
			after, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(after).To(Equal(doc))
		})
		It("does not overwrite the first occurrence of an id repeated in the file", func() {
			GinkgoT().Setenv("REGISTRY_PRELOAD_FILE", preloadFile(`{"schemaVersion":1,"plugins":[{"id":"same","name":"first","enabled":true,"remote":{"kind":"federated","url":"http://localhost:7110/r.js"},"contributions":[{"kind":"shell-footer","id":"status"}]},{"id":"same","name":"second","enabled":true,"remote":{"kind":"federated","url":"http://localhost:7110/r.js"},"contributions":[{"kind":"shell-footer","id":"status"}]}]}`))
			boot()
			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Revision).To(Equal(int64(1)))
			Expect(doc.Entries[0].Name).To(Equal("first"))
		})
	})
	Context("decision 81 — a per-entry refusal cannot prevent startup", func() {
		It("logs each withheld id and cause, while seeding valid entries even after an Apply refusal", func() {
			GinkgoT().Setenv("REGISTRY_PRELOAD_FILE", preloadFile(`{"schemaVersion":1,"plugins":[{"id":"off-list","name":"fixture","contributions":[{"kind":"shell-footer","id":"status"}],"remote":{"kind":"federated","url":"https://refused.example/r.js"}},{"id":"","remote":{"kind":"federated","url":"http://localhost:7110/r.js"}},{"id":"good","enabled":true,"name":"fixture","contributions":[{"kind":"shell-footer","id":"status"}],"remote":{"kind":"federated","url":"http://localhost:7110/r.js"}}]}`))
			var logs bytes.Buffer
			m, err := registry.Startup(ctx, pgDB, nil, nil, allowed, slog.New(slog.NewTextHandler(&logs, nil)))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(m.Stop)
			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Entries).To(HaveLen(1))
			Expect(doc.Entries[0].ID).To(Equal("good"))
			Expect(logs.String()).To(ContainSubstring("withheld"))
			Expect(logs.String()).To(ContainSubstring("off-list"))
			Expect(logs.String()).To(ContainSubstring("allowlist"))
			Expect(logs.String()).To(ContainSubstring("write names no entry"))
		})
		It("fails boot on a malformed whole file, before seeding any row", func() {
			GinkgoT().Setenv("REGISTRY_PRELOAD_FILE", preloadFile(`{"schemaVersion":1,"plugins":[`))
			m, err := registry.Startup(ctx, pgDB, nil, nil, allowed, slog.Default())
			Expect(err).To(HaveOccurred())
			Expect(m).To(BeNil())
			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Revision).To(BeZero())
		})
		It("supports an unset preload path", func() {
			boot()
			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Revision).To(BeZero())
		})
	})

	Context("decision 86 — lifecycle is stored, never inferred", func() {
		It("round-trips static, dynamic and unclassified in the lifted column across migrations", func() {
			// Phase 5a amends the third case: an unclassified write survives
			// until the next migration, which backfills it to static
			// (BR-AS52). The first two are unchanged — a stated class is
			// never rewritten.
			for i, lifecycle := range []string{domain.LifecycleStatic, domain.LifecycleDynamic, ""} {
				expected := lifecycle
				if expected == "" {
					expected = domain.LifecycleStatic
				}
				e := federated("entry", "http://localhost:7110/remoteEntry.js")
				e.Lifecycle = lifecycle
				_, err := store.Apply(ctx, domain.Write{Op: domain.OpUpsert, EntryID: e.ID, Entry: &e, Actor: domain.SharedAdminActor, IfRevision: int64(i)})
				Expect(err).NotTo(HaveOccurred())
				var written string
				Expect(pgDB.QueryRowContext(ctx, `SELECT lifecycle FROM registry.entries WHERE id='entry'`).Scan(&written)).To(Succeed())
				Expect(written).To(Equal(lifecycle))
				Expect(postgres.Migrate(ctx, pgDB)).To(Succeed())
				var stored string
				Expect(pgDB.QueryRowContext(ctx, `SELECT lifecycle FROM registry.entries WHERE id='entry'`).Scan(&stored)).To(Succeed())
				Expect(stored).To(Equal(expected))
				// Even an obsolete JSON copy cannot override the authoritative column.
				_, err = pgDB.ExecContext(ctx, `UPDATE registry.entries SET entry = entry || '{"lifecycle":"wrong"}'::jsonb`)
				Expect(err).NotTo(HaveOccurred())
				doc, err := store.Current(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(doc.Entries[0].Lifecycle).To(Equal(expected))
			}
		})
	})
})

func preloadFile(raw string) string {
	path := filepath.Join(GinkgoT().TempDir(), "registry.json")
	Expect(os.WriteFile(path, []byte(raw), 0600)).To(Succeed())
	return path
}
