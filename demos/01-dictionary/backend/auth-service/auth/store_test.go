package auth_test

import (
	"context"
	"database/sql"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/auth-service/auth"
)

var (
	testDB          *sql.DB
	testUnavailable string
	testContainer   string
)

var _ = BeforeSuite(func() {
	db, containerID, err := startTestPostgres()
	if err != nil {
		testUnavailable = err.Error()
		return
	}
	testDB, testContainer = db, containerID
})

var _ = AfterSuite(func() {
	if testDB != nil {
		testDB.Close()
	}
	if testContainer != "" {
		_ = exec.Command("docker", "rm", "-f", testContainer).Run()
	}
})

// seedAccount inserts a row this test double owns directly — AccountReader
// is read-only (see auth/store.go), so tests write via raw SQL rather than
// through the type under test.
func seedAccount(name, publicKey, signingKeySeed, status string) error {
	_, err := testDB.ExecContext(context.Background(), `
		INSERT INTO accounts.accounts (name, public_key, signing_key_seed, status)
		VALUES ($1, $2, $3, $4)`, name, publicKey, signingKeySeed, status)
	return err
}

var _ = Describe("AccountReader", func() {
	var reader *auth.AccountReader

	BeforeEach(func() {
		if testUnavailable != "" {
			Skip("docker unavailable for Postgres integration test: " + testUnavailable)
		}
		reader = auth.NewAccountReader(testDB)
	})

	It("gets an active account by name", func() {
		name := uniqueName("acme")
		Expect(seedAccount(name, "A"+name, "seed-"+name, "active")).To(Succeed())

		got, err := reader.Get(context.Background(), name)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.PublicKey).To(Equal("A" + name))
		Expect(got.SigningKeySeed).To(Equal("seed-" + name))
		Expect(got.Status).To(Equal("active"))
	})

	It("returns ErrNotFound for an unknown name", func() {
		_, err := reader.Get(context.Background(), uniqueName("missing"))
		Expect(err).To(MatchError(auth.ErrNotFound))
	})

	It("excludes DEFAULT and SYS (case-insensitively) and suspended accounts from ListTenants", func() {
		base := uniqueName("list")
		Expect(seedAccount(base+"-acme", "A"+base, "seed", "active")).To(Succeed())
		Expect(seedAccount(base+"-suspended", "B"+base, "seed", "suspended")).To(Succeed())
		Expect(seedAccount("default", "C"+base, "seed", "active")).To(Succeed())
		Expect(seedAccount("SYS", "D"+base, "seed", "active")).To(Succeed())

		tenants, err := reader.ListTenants(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(tenants).To(ContainElement(base + "-acme"))
		Expect(tenants).NotTo(ContainElement(base + "-suspended"))
		Expect(tenants).NotTo(ContainElement("default"))
		Expect(tenants).NotTo(ContainElement("SYS"))
	})
})
