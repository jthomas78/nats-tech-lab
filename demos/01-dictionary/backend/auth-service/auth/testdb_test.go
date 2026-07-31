package auth_test

// Disposable-Postgres test helper — mirrors accounts-service's
// accounts/store_test.go (accounts-service/accounts/store_test.go)
// startStoreTestPostgres helper, since auth-service is its own Go module
// and cannot import that one directly. auth-service never migrates this
// table itself (auth/store.go's AccountReader is read-only — see its doc
// comment), so this test double schema mirrors accounts-service's
// accounts.accounts table (accounts-service/accounts/store.go's Migrate)
// exactly, keeping only the columns AccountReader actually reads plus
// "name" to look rows up by.

import (
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func startTestPostgres() (*sql.DB, string, error) {
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

	if err := migrateTestSchema(db); err != nil {
		_ = exec.Command("docker", "rm", "-f", containerID).Run()
		return nil, "", fmt.Errorf("migrate: %w", err)
	}
	return db, containerID, nil
}

func migrateTestSchema(db *sql.DB) error {
	statements := []string{
		`CREATE SCHEMA IF NOT EXISTS accounts`,
		`CREATE TABLE IF NOT EXISTS accounts.accounts (
			id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name             TEXT NOT NULL UNIQUE,
			public_key       TEXT NOT NULL UNIQUE,
			signing_key_seed TEXT NOT NULL DEFAULT '',
			status           TEXT NOT NULL DEFAULT 'active',
			js_max_mem       BIGINT NOT NULL DEFAULT 0,
			js_max_file      BIGINT NOT NULL DEFAULT 0,
			js_max_streams   BIGINT NOT NULL DEFAULT 0,
			js_max_consumers BIGINT NOT NULL DEFAULT 0,
			created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			return err
		}
	}
	return nil
}

var testNameCounter int64

func uniqueName(base string) string {
	return fmt.Sprintf("%s-%d", base, atomic.AddInt64(&testNameCounter, 1))
}
