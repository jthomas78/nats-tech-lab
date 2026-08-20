// Package postgres_test is trading-partner-service's first Postgres-backed
// suite. Every repository-level rule before 38c-i was marked "deferred" and
// verified only by inspection — BR-TP08's upsert and BR-TP13's uniqueness
// among them — because this service had no way to reach a real database from
// a test.
//
// 38c-i needed one: BR-TP30's supersession and BR-TP34's optimistic
// concurrency are both SQL behaviours. A domain unit test proves the *rule*,
// but not that the UPDATE's `AND version = ?` predicate actually rejects a
// concurrent writer, and BR-TP34 exists specifically to stop a lost update.
// Asserting that without a database would be asserting nothing.
//
// It follows the same gated shape as BR-TP27's Temporal durability harness:
// skips unless TRADING_PARTNER_TEST_DATABASE_URL is set, so `ginkgo ./...`
// stays green on a machine with no Postgres. That means a plain green run does
// NOT prove these rules — see demos/01-dictionary/README.md.
package postgres_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/postgres"
)

func TestPostgres(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "TradingPartner Postgres Suite")
}

// testDB opens the gated connection and runs Migrate, or skips the spec.
// Migrate is idempotent, so running it per-spec is safe and also means every
// spec re-asserts that idempotency in passing.
func testDB() *sql.DB {
	url := os.Getenv("TRADING_PARTNER_TEST_DATABASE_URL")
	if url == "" {
		Skip("set TRADING_PARTNER_TEST_DATABASE_URL to run the Postgres-backed repository specs")
	}
	db, err := sql.Open("pgx", url)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(db.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	Expect(db.PingContext(ctx)).To(Succeed())
	Expect(postgres.Migrate(ctx, db)).To(Succeed())
	return db
}

// freshPartner inserts a partner reachable only by this spec, and removes it
// (cascading to its documents) afterwards — so the suite can run against a
// developer's live dev database without disturbing what is already in it.
func freshPartner(db *sql.DB, partnerType string) string {
	var id string
	err := db.QueryRow(`
		INSERT INTO trading_partner.trading_partners (context, name, type, status)
		VALUES ('spec-context', 'SPEC ' || gen_random_uuid()::text, $1, 'REGISTERED')
		RETURNING id`, partnerType).Scan(&id)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		_, _ = db.Exec(`DELETE FROM trading_partner.trading_partners WHERE id = $1`, id)
	})
	return id
}
