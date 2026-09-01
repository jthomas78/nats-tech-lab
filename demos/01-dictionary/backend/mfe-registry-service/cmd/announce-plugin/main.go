// Command announce-plugin is a resident publisher sidecar. It announces one
// build-owned plugin manifest, holds its NATS connection for the container's
// lifetime, and publishes an explicit signed unregister only on SIGTERM.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
	registryclient "github.com/jthomas78/nats-tech-lab/shared/mferegistry/client"
	"github.com/jthomas78/nats-tech-lab/shared/natsconn"
	"github.com/nats-io/nats.go"
)

var ErrReleaseRecoveryRequired = errors.New("publisher release state is missing and the release already exists; set RELEASE_RECOVERY to an explicitly recovered higher release")

type publisherClient interface {
	Announce(context.Context, json.RawMessage) (mferegistry.Response, error)
	Unregister(context.Context, string, int64) (mferegistry.UnregisterResponse, error)
}

type exitReason string

const (
	exitSIGTERM            exitReason = "sigterm"
	exitCrash              exitReason = "crash"
	exitHealthCheckFailure exitReason = "health-check-failure"
)

type resident struct {
	pluginID  string
	manifest  json.RawMessage
	publisher publisherClient
	releases  *releaseStore
	log       *slog.Logger
}

func (r *resident) announce(ctx context.Context) error {
	release, fresh, err := r.releases.PrepareAnnounce()
	if err != nil {
		return err
	}
	manifest, err := manifestAtRelease(r.manifest, release)
	if err != nil {
		return err
	}
	out, err := r.publisher.Announce(ctx, manifest)
	if err != nil {
		return err
	}
	if fresh && out.NoOp {
		return ErrReleaseRecoveryRequired
	}
	r.log.Info("plugin announced", "plugin", r.pluginID, "release", release, "outcome", out.Outcome, "noOp", out.NoOp)
	return nil
}

// shutdown is deliberately keyed by an authoritative reason. Production
// calls it only from the SIGTERM notification below; crash and health-failure
// values exist to keep the negative half of BR-AS54 executable and explicit.
func (r *resident) shutdown(ctx context.Context, reason exitReason) error {
	if reason != exitSIGTERM {
		return nil
	}
	release, err := r.releases.PrepareUnregister()
	if err != nil {
		r.log.Warn("unregister failed during SIGTERM shutdown", "plugin", r.pluginID, "error", err)
		return nil
	}
	out, err := r.publisher.Unregister(ctx, r.pluginID, release)
	if err != nil {
		r.log.Warn("unregister failed during SIGTERM shutdown", "plugin", r.pluginID, "release", release, "error", err)
		return nil
	}
	r.log.Info("plugin unregistered during SIGTERM shutdown", "plugin", r.pluginID, "release", release, "outcome", out.Outcome, "noOp", out.NoOp)
	return nil
}

func manifestAtRelease(manifest json.RawMessage, release int64) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(manifest, &fields); err != nil {
		return nil, fmt.Errorf("decode plugin manifest: %w", err)
	}
	encoded, err := json.Marshal(release)
	if err != nil {
		return nil, fmt.Errorf("encode manifest release: %w", err)
	}
	fields["release"] = encoded
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode plugin manifest: %w", err)
	}
	return out, nil
}

type config struct {
	natsURL          string
	natsCredsPath    string
	manifestPath     string
	signingSeedPath  string
	releaseStatePath string
	publisherID      string
	connectionName   string
	recoveryRelease  int64
	requestTimeout   time.Duration
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	manifest, err := os.ReadFile(cfg.manifestPath)
	if err != nil {
		return fmt.Errorf("read plugin manifest: %w", err)
	}
	var identity struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(manifest, &identity); err != nil || identity.ID == "" {
		return errors.New("plugin manifest must contain a non-empty id")
	}
	seed, err := os.ReadFile(cfg.signingSeedPath)
	if err != nil {
		return fmt.Errorf("read publisher signing seed: %w", err)
	}
	keyPair, err := registryclient.NewNKeySigner(bytes.TrimSpace(seed))
	if err != nil {
		return fmt.Errorf("load publisher signing seed: %w", err)
	}
	defer keyPair.Wipe()

	nc, err := nats.Connect(cfg.natsURL, natsconn.Options(cfg.connectionName, cfg.natsCredsPath, log)...)
	if err != nil {
		return fmt.Errorf("connect to NATS: %w", err)
	}
	defer nc.Close()

	announcer := &resident{
		pluginID:  identity.ID,
		manifest:  manifest,
		publisher: registryclient.New(nc, keyPair, cfg.publisherID),
		releases:  newReleaseStore(cfg.releaseStatePath, identity.ID, cfg.recoveryRelease),
		log:       log,
	}
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	defer signal.Stop(term)

	announceCtx, cancel := context.WithTimeout(context.Background(), cfg.requestTimeout)
	err = announcer.announce(announceCtx)
	cancel()
	if err != nil {
		return fmt.Errorf("announce plugin: %w", err)
	}

	<-term

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.requestTimeout)
	defer cancel()
	return announcer.shutdown(shutdownCtx, exitSIGTERM)
}

func loadConfig() (config, error) {
	cfg := config{
		natsURL:        envOr("NATS_URL", nats.DefaultURL),
		requestTimeout: 5 * time.Second,
	}
	var err error
	if cfg.natsCredsPath, err = requiredEnv("NATS_CREDS_PATH"); err != nil {
		return config{}, err
	}
	if cfg.manifestPath, err = requiredEnv("PLUGIN_MANIFEST_PATH"); err != nil {
		return config{}, err
	}
	if cfg.signingSeedPath, err = requiredEnv("PUBLISHER_SIGNING_SEED_PATH"); err != nil {
		return config{}, err
	}
	if cfg.releaseStatePath, err = requiredEnv("RELEASE_STATE_PATH"); err != nil {
		return config{}, err
	}
	if cfg.publisherID, err = requiredEnv("PUBLISHER_ID"); err != nil {
		return config{}, err
	}
	cfg.connectionName = envOr("NATS_CONNECTION_NAME", "announce-plugin-"+cfg.publisherID)
	if raw := os.Getenv("RELEASE_RECOVERY"); raw != "" {
		cfg.recoveryRelease, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || cfg.recoveryRelease <= 0 {
			return config{}, errors.New("RELEASE_RECOVERY must be a positive integer")
		}
	}
	if raw := os.Getenv("REQUEST_TIMEOUT"); raw != "" {
		cfg.requestTimeout, err = time.ParseDuration(raw)
		if err != nil || cfg.requestTimeout <= 0 {
			return config{}, errors.New("REQUEST_TIMEOUT must be a positive duration")
		}
	}
	return cfg, nil
}

func requiredEnv(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
