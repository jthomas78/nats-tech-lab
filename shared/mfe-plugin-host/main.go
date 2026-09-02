// Command mfe-plugin-host serves one frontend plugin and runs its registry
// announcer in the same process. A serving failure cancels the announcer; only
// SIGTERM observed by announcer.Start performs a signed unregister.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry/announcer"
)

type HostConfig struct {
	Addr          string
	AssetRoot     string
	AllowedOrigin string
}

type announcerStart func(context.Context, announcer.Config) error
type serveHost func(context.Context, *StaticHost) error

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(context.Background(), log); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger) error {
	announceConfig, err := announcer.ConfigFromEnv()
	if err != nil {
		return err
	}
	announceConfig.Logger = log
	hostConfig := HostConfig{
		Addr:          envOr("HTTP_ADDR", ":8080"),
		AssetRoot:     envOr("ASSET_ROOT", "/srv"),
		AllowedOrigin: os.Getenv("ASSET_ALLOWED_ORIGIN"),
	}
	return runHost(ctx, hostConfig, announceConfig, announcer.Start, nil)
}

func runHost(parent context.Context, hostConfig HostConfig, announceConfig announcer.Config, start announcerStart, serve serveHost) error {
	host, err := NewStaticHost(hostConfig.AssetRoot, hostConfig.AllowedOrigin)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	if start == nil {
		start = announcer.Start
	}
	if serve == nil {
		serve = func(ctx context.Context, handler *StaticHost) error {
			server := &http.Server{Addr: hostConfig.Addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
			go func() {
				<-ctx.Done()
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer shutdownCancel()
				_ = server.Shutdown(shutdownCtx)
			}()
			err := server.ListenAndServe()
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		}
	}

	announceDone := make(chan error, 1)
	serveDone := make(chan error, 1)
	go func() { announceDone <- start(ctx, announceConfig) }()
	go func() { serveDone <- serve(ctx, host) }()

	select {
	case err := <-announceDone:
		cancel()
		<-serveDone
		return err
	case err := <-serveDone:
		cancel()
		<-announceDone
		if err == nil {
			return fmt.Errorf("plugin HTTP server stopped")
		}
		return fmt.Errorf("serve plugin assets: %w", err)
	case err := <-host.Failures():
		cancel()
		<-announceDone
		return err
	case <-parent.Done():
		cancel()
		<-announceDone
		return nil
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
