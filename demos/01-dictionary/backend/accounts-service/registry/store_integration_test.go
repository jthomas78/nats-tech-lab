package registry_test

// Integration coverage for the registry's store against real Postgres —
// spins up a disposable container via the docker CLI, mirroring
// accounts/store_test.go's own helper. BR-AS16 and BR-AS17 are claims about
// what survives a write and how revisions move, and neither is provable
// against a fake: the revision is assigned by a locked row inside the same
// transaction as the write it keys.

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/registry/internal/postgres"
)

var (
	pgDB          *sql.DB
	pgUnavailable string
	pgContainer   string
)

var _ = BeforeSuite(func() {
	db, container, err := startRegistryPostgres()
	if err != nil {
		pgUnavailable = err.Error()
		return
	}
	pgDB, pgContainer = db, container
})

var _ = AfterSuite(func() {
	if pgDB != nil {
		pgDB.Close()
	}
	if pgContainer != "" {
		_ = exec.Command("docker", "rm", "-f", pgContainer).Run()
	}
})

func startRegistryPostgres() (*sql.DB, string, error) {
	out, err := exec.Command("docker", "run", "-d", "--rm",
		"-e", "POSTGRES_USER=registry", "-e", "POSTGRES_PASSWORD=registry", "-e", "POSTGRES_DB=registry",
		"-p", "0:5432", "postgres:16-alpine").Output()
	if err != nil {
		return nil, "", fmt.Errorf("docker run postgres: %w", err)
	}
	container := strings.TrimSpace(string(out))

	portOut, err := exec.Command("docker", "port", container, "5432/tcp").Output()
	if err != nil {
		_ = exec.Command("docker", "rm", "-f", container).Run()
		return nil, "", fmt.Errorf("docker port: %w", err)
	}
	line := strings.TrimSpace(strings.Split(string(portOut), "\n")[0])
	hostPort := line[strings.LastIndex(line, ":")+1:]
	dsn := fmt.Sprintf("postgres://registry:registry@127.0.0.1:%s/registry?sslmode=disable", hostPort)

	var db *sql.DB
	deadline := time.Now().Add(30 * time.Second)
	for {
		db, err = sql.Open("pgx", dsn)
		if err == nil {
			if pingErr := db.Ping(); pingErr == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			_ = exec.Command("docker", "rm", "-f", container).Run()
			return nil, "", fmt.Errorf("postgres did not become ready in time: %w", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err := postgres.Migrate(context.Background(), db); err != nil {
		_ = exec.Command("docker", "rm", "-f", container).Run()
		return nil, "", fmt.Errorf("migrate: %w", err)
	}
	return db, container, nil
}

var _ = Describe("the registry store", func() {
	var (
		store *postgres.Store
		ctx   context.Context
	)

	allowed := domain.NewAllowlist([]string{"http://localhost:7110"})

	BeforeEach(func() {
		if pgUnavailable != "" {
			Skip("docker unavailable for Postgres integration test: " + pgUnavailable)
		}
		ctx = context.Background()
		// Each spec starts from an empty registry at revision 0, so a
		// revision assertion means what it says rather than depending on
		// which specs ran before it.
		_, err := pgDB.ExecContext(ctx, `TRUNCATE registry.entries, registry.audit`)
		Expect(err).NotTo(HaveOccurred())
		_, err = pgDB.ExecContext(ctx, `UPDATE registry.revision SET revision = 0`)
		Expect(err).NotTo(HaveOccurred())
		store = postgres.NewStore(pgDB, allowed)
	})

	upsert := func(id string, rev int64) (domain.Document, error) {
		e := federated(id, "http://localhost:7110/remoteEntry.js")
		return store.Apply(ctx, domain.Write{
			Op: domain.OpUpsert, EntryID: id, Actor: domain.SharedAdminActor,
			Entry: &e, IfRevision: rev,
		})
	}

	Context("BR-AS16 — the registry is service state", func() {
		It("an entry applied through Apply is present in the next Current", func() {
			_, err := upsert("example-plugin", domain.NoRevision)
			Expect(err).NotTo(HaveOccurred())

			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Entries).To(HaveLen(1))
			Expect(doc.Entries[0].ID).To(Equal("example-plugin"))
			Expect(doc.Entries[0].Contributions).To(HaveLen(1), "the whole entry survives the round trip, not just its id")
		})

		It("reads back through a second Store, so nothing is held in process memory", func() {
			_, err := upsert("example-plugin", domain.NoRevision)
			Expect(err).NotTo(HaveOccurred())

			doc, err := postgres.NewStore(pgDB, allowed).Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Entries).To(HaveLen(1))
		})
	})

	Context("BR-AS17 — revision is server-assigned and monotonic", func() {
		It("assigns 1 to the first write and never repeats one", func() {
			first, err := upsert("plugin-a", domain.NoRevision)
			Expect(err).NotTo(HaveOccurred())
			Expect(first.Revision).To(Equal(int64(1)))

			second, err := upsert("plugin-b", first.Revision)
			Expect(err).NotTo(HaveOccurred())
			Expect(second.Revision).To(Equal(int64(2)))

			third, err := store.Apply(ctx, domain.Write{
				Op: domain.OpSetEnabled, EntryID: "plugin-a", Enabled: false,
				Actor: domain.SharedAdminActor, IfRevision: second.Revision,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(third.Revision).To(Equal(int64(3)))
		})

		It("installs the entries and the revision together", func() {
			doc, err := upsert("example-plugin", domain.NoRevision)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Revision).To(Equal(int64(1)))
			Expect(doc.Entries).To(HaveLen(1), "Apply returns the document its own write installed — the two setters this replaces could disagree")
		})
	})

	Context("BR-AS18 — writes are revision-checked", func() {
		It("refuses a stale write and consumes no revision", func() {
			first, err := upsert("plugin-a", domain.NoRevision)
			Expect(err).NotTo(HaveOccurred())

			_, err = upsert("plugin-b", domain.NoRevision) // still keyed on 0
			Expect(err).To(MatchError(domain.ErrStaleRevision))

			var stale domain.StaleRevisionError
			Expect(errors.As(err, &stale)).To(BeTrue())
			Expect(stale.Current).To(Equal(first.Revision))
			Expect(stale.Supplied).To(Equal(domain.NoRevision))

			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Revision).To(Equal(first.Revision), "a refusal moves nothing")
			Expect(doc.Entries).To(HaveLen(1))
		})
	})

	Context("BR-AS20 — origin allowlist, enforced on write", func() {
		It("refuses an entry whose remote origin is not configured, and stores nothing", func() {
			e := federated("evil-plugin", "https://plugins.evil.example.com/remoteEntry.js")
			_, err := store.Apply(ctx, domain.Write{
				Op: domain.OpUpsert, EntryID: e.ID, Actor: domain.SharedAdminActor,
				Entry: &e, IfRevision: domain.NoRevision,
			})
			Expect(err).To(MatchError(domain.ErrOriginNotAllowed))

			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Entries).To(BeEmpty())
			Expect(doc.Revision).To(Equal(domain.NoRevision))
		})
	})

	Context("BR-AS21 — no self-registration", func() {
		It("will not create an entry by enabling one that was never curated", func() {
			_, err := store.Apply(ctx, domain.Write{
				Op: domain.OpSetEnabled, EntryID: "never-curated", Enabled: true,
				Actor: domain.SharedAdminActor, IfRevision: domain.NoRevision,
			})
			Expect(err).To(HaveOccurred())

			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Entries).To(BeEmpty())
		})
	})

	Context("BR-AS23 — the audit records the surface", func() {
		It("records an accepted write against the revision it installed", func() {
			doc, err := upsert("example-plugin", domain.NoRevision)
			Expect(err).NotTo(HaveOccurred())

			entries, err := store.Audit(ctx, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(HaveLen(1))
			Expect(entries[0].Outcome).To(Equal(domain.AuditAccepted))
			Expect(entries[0].Op).To(Equal(domain.OpUpsert))
			Expect(entries[0].Actor).To(Equal(domain.SharedAdminActor), "the shared admin identity, which is all this service can honestly record")
			Expect(*entries[0].Revision).To(Equal(doc.Revision))
		})

		It("records a refused write too, with no revision", func() {
			_, err := upsert("plugin-a", domain.NoRevision)
			Expect(err).NotTo(HaveOccurred())
			_, err = upsert("plugin-b", domain.NoRevision)
			Expect(err).To(HaveOccurred())

			entries, err := store.Audit(ctx, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(HaveLen(2), "a refusal that left no trace would make the audit a record of successes")
			Expect(entries[0].Outcome).To(Equal(domain.AuditRefused))
			Expect(entries[0].Revision).To(BeNil())
		})
	})

	Context("decision 49 — a committed write is reported as committed", func() {
		/*
			The defect this pins: apply() used to commit and then call
			Current(ctx) on the REQUEST context to read back what it installed.
			A cancellation in that window — a client hanging up, a proxy
			timeout, a browser tab closing on a slow write — made Apply return
			an error for a write that was already durable. The wrapper then
			audited a refusal for it, answered 500, and skipped both the cache
			refresh and the change notification. The operator saw a failure,
			the audit agreed with the operator, and the database disagreed with
			both.

			Cancelling immediately after Apply returns is the closest a spec
			can get to "cancelled in that window" without instrumenting the
			transaction; what makes the case provable is the pair of
			assertions below — the entry is there, and no refusal was recorded
			for it.
		*/
		It("does not audit a refusal for a write whose caller went away", func() {
			writeCtx, cancel := context.WithCancel(context.Background())
			e := federated("example-plugin", "http://localhost:7110/remoteEntry.js")
			doc, err := store.Apply(writeCtx, domain.Write{
				Op: domain.OpUpsert, EntryID: "example-plugin", Actor: domain.SharedAdminActor,
				Entry: &e, IfRevision: domain.NoRevision,
			})
			cancel()

			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Entries).To(HaveLen(1), "Apply returns the document it installed, read inside its own transaction")
			Expect(doc.Revision).To(Equal(int64(1)))

			entries, err := store.Audit(ctx, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(HaveLen(1))
			Expect(entries[0].Outcome).To(Equal(domain.AuditAccepted),
				"a durable write audited as refused is the one audit entry an operator can never recover from")
		})

		It("still records a refusal when the caller's context is already cancelled", func() {
			// The other half: detaching the audit write from the request
			// context must not be detaching it from correctness. A refusal on
			// a dead context is still a refusal that has to be recorded.
			dead, cancel := context.WithCancel(context.Background())
			cancel()

			_, err := store.Apply(dead, domain.Write{
				Op: domain.OpUpsert, EntryID: "example-plugin", Actor: domain.SharedAdminActor,
				Entry: nil, IfRevision: domain.NoRevision,
			})
			Expect(err).To(HaveOccurred())

			entries, err := store.Audit(ctx, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(HaveLen(1))
			Expect(entries[0].Outcome).To(Equal(domain.AuditRefused))
			Expect(entries[0].Revision).To(BeNil())
		})

		It("leaves nothing behind when the write itself is refused", func() {
			// The property that makes auditing every apply() error as a
			// refusal true: every one of those paths rolled back. If any of
			// them could commit, this revision would have moved.
			_, err := upsert("plugin-a", domain.NoRevision)
			Expect(err).NotTo(HaveOccurred())
			_, err = upsert("plugin-b", domain.NoRevision) // stale IfRevision
			Expect(err).To(HaveOccurred())

			doc, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Revision).To(Equal(int64(1)), "the refused write moved nothing")
			Expect(doc.Entries).To(HaveLen(1))
		})
	})

	Context("BR-AS24 — an entry is disabled, never deleted", func() {
		It("keeps the row and its audit trail when an entry is disabled", func() {
			first, err := upsert("example-plugin", domain.NoRevision)
			Expect(err).NotTo(HaveOccurred())

			doc, err := store.Apply(ctx, domain.Write{
				Op: domain.OpSetEnabled, EntryID: "example-plugin", Enabled: false,
				Actor: domain.SharedAdminActor, IfRevision: first.Revision,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Entries).To(HaveLen(1), "the row is still there")
			Expect(doc.Entries[0].Enabled).To(BeFalse())

			Expect(doc.Readable(allowed).Entries).To(BeEmpty(), "and it is withheld from a shell")

			entries, err := store.Audit(ctx, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(HaveLen(2))
		})
	})
})
