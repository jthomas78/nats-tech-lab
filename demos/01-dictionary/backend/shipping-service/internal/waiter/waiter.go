// Package waiter retries a readiness probe until it succeeds or the context
// expires. Used at startup to wait for NATS and Postgres in docker-compose,
// where service ordering is not guaranteed.
package waiter

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

func Wait(ctx context.Context, name string, log *slog.Logger, probe func(context.Context) error) error {
	const interval = time.Second
	for {
		err := probe(ctx)
		if err == nil {
			log.Info("dependency ready", "name", name)
			return nil
		}
		log.Info("waiting for dependency", "name", name, "err", err)
		select {
		case <-ctx.Done():
			return fmt.Errorf("gave up waiting for %s: %w", name, ctx.Err())
		case <-time.After(interval):
		}
	}
}
