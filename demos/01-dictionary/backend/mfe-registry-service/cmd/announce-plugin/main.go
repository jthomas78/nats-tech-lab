// Command announce-plugin is the CLI form of the shared resident publisher.
// It remains useful for announcer-only plugins; served plugins run the exact
// same package in-process from mfe-plugin-host.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry/announcer"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(context.Background(), log); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := announcer.ConfigFromEnv()
	if err != nil {
		return err
	}
	cfg.Logger = log
	return announcer.Start(ctx, cfg)
}
