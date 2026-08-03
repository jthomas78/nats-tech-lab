package accounts_test

// Integration coverage for accounts/store.go against real Postgres — spins
// up a disposable container via the docker CLI, mirroring
// refdata-service/refdata/corpus_repository_integration_test.go's own
// helper; if docker isn't available, every spec in this file Skips rather
// than failing the suite.

import (
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

var (
	storeTestDB          *sql.DB
	storeTestUnavailable string
	storeTestContainer   string
)

var _ = BeforeSuite(func() {
	db, containerID, err := startStoreTestPostgres()
	if err != nil {
		storeTestUnavailable = err.Error()
		return
	}
	storeTestDB, storeTestContainer = db, containerID
})

var _ = AfterSuite(func() {
	if storeTestDB != nil {
		storeTestDB.Close()
	}
	if storeTestContainer != "" {
		_ = exec.Command("docker", "rm", "-f", storeTestContainer).Run()
	}
})

func startStoreTestPostgres() (*sql.DB, string, error) {
	out, err := exec.Command("docker", "run", "-d", "--rm",
		"-e", "POSTGRES_USER=accounts", "-e", "POSTGRES_PASSWORD=accounts", "-e", "POSTGRES_DB=accounts",
		"-p", "0:5432", "postgres:16-alpine").Output()
	if err != nil {
		return nil, "", fmt.Errorf("docker run postgres: %w", err)
	}
	containerID := strings.TrimSpace(string(out))

	portOut, err := exec.Command("docker", "port", containerID, "5432/tcp").Output()
	if err != nil {
		_ = exec.Command("docker", "rm", "-f", containerID).Run()
		return nil, "", fmt.Errorf("docker port: %w", err)
	}
	firstLine := strings.TrimSpace(strings.Split(string(portOut), "\n")[0])
	hostPort := firstLine[strings.LastIndex(firstLine, ":")+1:]

	dsn := fmt.Sprintf("postgres://accounts:accounts@127.0.0.1:%s/accounts?sslmode=disable", hostPort)

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
			_ = exec.Command("docker", "rm", "-f", containerID).Run()
			return nil, "", fmt.Errorf("postgres did not become ready in time: %w", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	if err := accounts.Migrate(context.Background(), db); err != nil {
		_ = exec.Command("docker", "rm", "-f", containerID).Run()
		return nil, "", fmt.Errorf("migrate: %w", err)
	}
	return db, containerID, nil
}

var storeTestNameCounter int64

func uniqueAccountName(base string) string {
	return fmt.Sprintf("%s-%d", base, atomic.AddInt64(&storeTestNameCounter, 1))
}

var _ = Describe("Store", func() {
	var store *accounts.Store

	BeforeEach(func() {
		if storeTestUnavailable != "" {
			Skip("docker unavailable for Postgres integration test: " + storeTestUnavailable)
		}
		store = accounts.NewStore(storeTestDB)
	})

	It("inserts and retrieves an account by name", func() {
		name := uniqueAccountName("acme")
		Expect(store.Insert(context.Background(), accounts.Account{
			Name: name, PublicKey: "A" + name, SigningKeySeed: "seed-" + name,
			Status: accounts.StatusActive, JSMaxMem: 1, JSMaxFile: 2, JSMaxStreams: 3, JSMaxConsumers: 4,
		})).To(Succeed())

		got, err := store.Get(context.Background(), name)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.PublicKey).To(Equal("A" + name))
		Expect(got.Status).To(Equal(accounts.StatusActive))
		Expect(got.JSMaxConsumers).To(BeEquivalentTo(4))
	})

	It("returns ErrNotFound for an unknown name", func() {
		_, err := store.Get(context.Background(), uniqueAccountName("missing"))
		Expect(err).To(MatchError(accounts.ErrNotFound))
	})

	It("rejects a duplicate name on Insert", func() {
		name := uniqueAccountName("dup")
		acc := accounts.Account{Name: name, PublicKey: "A" + name, Status: accounts.StatusActive}
		Expect(store.Insert(context.Background(), acc)).To(Succeed())
		err := store.Insert(context.Background(), acc)
		Expect(err).To(HaveOccurred())
	})

	It("SeedIfMissing is a no-op the second time it seeds the same name", func() {
		name := uniqueAccountName("seed")
		acc := accounts.Account{Name: name, PublicKey: "A" + name, Status: accounts.StatusActive, JSMaxMem: 111}
		Expect(store.SeedIfMissing(context.Background(), acc)).To(Succeed())
		acc.JSMaxMem = 222 // different value — must NOT overwrite on the second call
		Expect(store.SeedIfMissing(context.Background(), acc)).To(Succeed())

		got, err := store.Get(context.Background(), name)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.JSMaxMem).To(BeEquivalentTo(111), "SeedIfMissing must never overwrite an existing row")
	})

	It("lists every account ordered by name", func() {
		base := uniqueAccountName("list")
		names := []string{base + "-b", base + "-a", base + "-c"}
		for _, n := range names {
			Expect(store.Insert(context.Background(), accounts.Account{Name: n, PublicKey: "A" + n, Status: accounts.StatusActive})).To(Succeed())
		}

		all, err := store.List(context.Background())
		Expect(err).NotTo(HaveOccurred())
		var got []string
		for _, a := range all {
			if strings.HasPrefix(a.Name, base) {
				got = append(got, a.Name)
			}
		}
		Expect(got).To(Equal([]string{base + "-a", base + "-b", base + "-c"}))
	})

	It("SetStatus suspends an account, and reports ErrNotFound for an unknown name", func() {
		name := uniqueAccountName("suspend")
		Expect(store.Insert(context.Background(), accounts.Account{Name: name, PublicKey: "A" + name, Status: accounts.StatusActive})).To(Succeed())

		Expect(store.SetStatus(context.Background(), name, accounts.StatusSuspended)).To(Succeed())
		got, err := store.Get(context.Background(), name)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Status).To(Equal(accounts.StatusSuspended))

		err = store.SetStatus(context.Background(), uniqueAccountName("missing"), accounts.StatusSuspended)
		Expect(err).To(MatchError(accounts.ErrNotFound))
	})

	It("RenameIfExists migrates an existing row to a new name, preserving the rest of the row, and is a no-op if the old name doesn't exist", func() {
		oldName := uniqueAccountName("LEGACY")
		newName := strings.ToLower(oldName)
		Expect(store.Insert(context.Background(), accounts.Account{
			Name: oldName, PublicKey: "A" + oldName, Status: accounts.StatusActive, JSMaxMem: 42,
		})).To(Succeed())

		Expect(store.RenameIfExists(context.Background(), oldName, newName)).To(Succeed())

		_, err := store.Get(context.Background(), oldName)
		Expect(err).To(MatchError(accounts.ErrNotFound), "the old name must no longer resolve")

		got, err := store.Get(context.Background(), newName)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.PublicKey).To(Equal("A"+oldName), "the rest of the row must be untouched by the rename")
		Expect(got.JSMaxMem).To(BeEquivalentTo(42))

		// Second call: oldName no longer exists, so this must do nothing —
		// exactly the shape cmd/main.go's seeding step relies on to be safe
		// to run on every startup.
		Expect(store.RenameIfExists(context.Background(), oldName, newName)).To(Succeed())
		got2, err := store.Get(context.Background(), newName)
		Expect(err).NotTo(HaveOccurred())
		Expect(got2.PublicKey).To(Equal("A" + oldName))
	})

	It("ListActiveTenantNames excludes DEFAULT and SYS (case-insensitively) and suspended accounts", func() {
		base := uniqueAccountName("list-active")
		Expect(store.Insert(context.Background(), accounts.Account{Name: base + "-acme", PublicKey: "A" + base, Status: accounts.StatusActive})).To(Succeed())
		Expect(store.Insert(context.Background(), accounts.Account{Name: base + "-suspended", PublicKey: "B" + base, Status: accounts.StatusSuspended})).To(Succeed())
		Expect(store.Insert(context.Background(), accounts.Account{Name: "default", PublicKey: "C" + base, Status: accounts.StatusActive})).To(Succeed())
		Expect(store.Insert(context.Background(), accounts.Account{Name: "SYS", PublicKey: "D" + base, Status: accounts.StatusActive})).To(Succeed())

		tenants, err := store.ListActiveTenantNames(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(tenants).To(ContainElement(base + "-acme"))
		Expect(tenants).NotTo(ContainElement(base + "-suspended"))
		Expect(tenants).NotTo(ContainElement("default"))
		Expect(tenants).NotTo(ContainElement("SYS"))
	})
})
