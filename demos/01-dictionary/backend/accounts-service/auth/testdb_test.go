package auth_test

// Disposable-Postgres test helper, mirroring accounts/store_test.go's own
// startStoreTestPostgres. Kept as a separate container/suite from the
// accounts package's tests (Go test binaries are per-package) but — since
// Phase 19 folded auth-service into this module — schema setup now calls
// accounts.Migrate directly instead of hand-duplicating its CREATE TABLE
// statements, the way this file did back when auth-service was a separate
// Go module that couldn't import accounts-service's own package.

import (
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
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

	if err := accounts.Migrate(context.Background(), db); err != nil {
		_ = exec.Command("docker", "rm", "-f", containerID).Run()
		return nil, "", fmt.Errorf("migrate: %w", err)
	}
	return db, containerID, nil
}

var testNameCounter int64

func uniqueName(base string) string {
	return fmt.Sprintf("%s-%d", base, atomic.AddInt64(&testNameCounter, 1))
}
