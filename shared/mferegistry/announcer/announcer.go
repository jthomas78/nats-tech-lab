// Package announcer owns the complete lifecycle of one MFE publisher:
// transport connection, signed announce, persisted release sequence, and the
// explicit signed unregister that SIGTERM alone authorizes.
package announcer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
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

// Config is deployment configuration for one resident publisher. The public
// fields are shared by the CLI and the static plugin host; unexported seams
// exist only so this package can exercise lifecycle decisions without a live
// broker.
type Config struct {
	NATSURL          string
	NATSCredsPath    string
	ManifestPath     string
	SigningSeedPath  string
	ReleaseStatePath string
	PublisherID      string
	ConnectionName   string
	PublicOrigin     string
	RecoveryRelease  int64
	RequestTimeout   time.Duration
	Logger           *slog.Logger

	// SelfCheckURL is this process's OWN /healthz, on loopback. It is
	// deployment configuration and never manifest data, and it is required:
	// after Phase 15 there is no unmapped state and no plugin that is
	// structurally unhealth-checkable (BR-AS61).
	SelfCheckURL string

	// HealthOnly is the curated publisher's shape. demo-catalog reaches the
	// catalogue through the preload file, not through an announcement, but
	// it is still a plugin an operator looks at, so it still reports its own
	// health — and only that. It signs nothing, so it needs no seed, no
	// manifest and no release sequence, and it holds a credential granted
	// its own health token alone.
	HealthOnly bool

	publisher publisherClient
	healthBus healthBus
	signals   <-chan os.Signal
}

// ConfigFromEnv reads the one deployment vocabulary shared by both binaries.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		NATSURL:        envOr("NATS_URL", nats.DefaultURL),
		RequestTimeout: 5 * time.Second,
		Logger:         slog.Default(),
	}
	var err error
	// HEALTH_ONLY is read FIRST, because it decides which of the rest are
	// required at all. Reading it last — as this did until the Phase 15h gate
	// — made Validate()'s curated-publisher exemption unreachable from a
	// deployment: the four announce-only variables were demanded here before
	// anything knew the publisher never announces. Every read below is a
	// plain Getenv, and Validate() stays the one place that decides what a
	// given shape of publisher must have.
	cfg.HealthOnly = os.Getenv("HEALTH_ONLY") == "true"
	if cfg.NATSCredsPath, err = requiredEnv("NATS_CREDS_PATH"); err != nil {
		return Config{}, err
	}
	if cfg.PublisherID, err = requiredEnv("PUBLISHER_ID"); err != nil {
		return Config{}, err
	}
	if cfg.SelfCheckURL, err = requiredEnv("HEALTH_SELF_URL"); err != nil {
		return Config{}, err
	}
	cfg.ManifestPath = os.Getenv("PLUGIN_MANIFEST_PATH")
	cfg.SigningSeedPath = os.Getenv("PUBLISHER_SIGNING_SEED_PATH")
	cfg.ReleaseStatePath = os.Getenv("RELEASE_STATE_PATH")
	cfg.PublicOrigin = os.Getenv("PLUGIN_PUBLIC_ORIGIN")
	cfg.ConnectionName = envOr("NATS_CONNECTION_NAME", cfg.PublisherID)
	if raw := os.Getenv("RELEASE_RECOVERY"); raw != "" {
		cfg.RecoveryRelease, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || cfg.RecoveryRelease <= 0 {
			return Config{}, errors.New("RELEASE_RECOVERY must be a positive integer")
		}
	}
	if raw := os.Getenv("REQUEST_TIMEOUT"); raw != "" {
		cfg.RequestTimeout, err = time.ParseDuration(raw)
		if err != nil || cfg.RequestTimeout <= 0 {
			return Config{}, errors.New("REQUEST_TIMEOUT must be a positive duration")
		}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate fails before a connection or file mutation. In particular the
// release path has no default: silently writing it into an image layer would
// violate BR-AS67 at the next container replacement.
func (c Config) Validate() error {
	required := []struct{ name, value string }{
		{"NATS_CREDS_PATH", c.NATSCredsPath},
		{"PUBLISHER_ID", c.PublisherID},
		{"NATS_CONNECTION_NAME", c.ConnectionName},
		{"HEALTH_SELF_URL", c.SelfCheckURL},
	}
	if !c.HealthOnly {
		required = append(required,
			struct{ name, value string }{"PLUGIN_MANIFEST_PATH", c.ManifestPath},
			struct{ name, value string }{"PUBLISHER_SIGNING_SEED_PATH", c.SigningSeedPath},
			struct{ name, value string }{"RELEASE_STATE_PATH", c.ReleaseStatePath},
			struct{ name, value string }{"PLUGIN_PUBLIC_ORIGIN", c.PublicOrigin},
		)
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.name)
		}
	}
	if c.ConnectionName != c.PublisherID {
		return errors.New("NATS_CONNECTION_NAME must equal PUBLISHER_ID")
	}
	if c.RequestTimeout < 0 {
		return errors.New("REQUEST_TIMEOUT must be a positive duration")
	}
	if c.HealthOnly {
		return nil
	}
	if _, err := stampRemoteURL("/remoteEntry.js", c.PublicOrigin); err != nil {
		return fmt.Errorf("PLUGIN_PUBLIC_ORIGIN: %w", err)
	}
	return nil
}

type resident struct {
	pluginID     string
	manifest     json.RawMessage
	publicOrigin string
	publisher    publisherClient
	releases     *releaseStore
	log          *slog.Logger
}

func (r *resident) announce(ctx context.Context) error {
	release, fresh, err := r.releases.PrepareAnnounce()
	if err != nil {
		return err
	}
	manifest, err := manifestForAnnouncement(r.manifest, release, r.publicOrigin)
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

// Start owns the publisher's complete resident lifecycle. Cancelling ctx is
// an ordinary process/serving failure and never unregisters; SIGTERM is the
// only path that performs the authoritative withdrawal.
func Start(ctx context.Context, cfg Config) error {
	if cfg.NATSURL == "" {
		cfg.NATSURL = nats.DefaultURL
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = 5 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	pluginID := cfg.PublisherID
	var manifest json.RawMessage
	if !cfg.HealthOnly {
		var err error
		manifest, err = os.ReadFile(cfg.ManifestPath)
		if err != nil {
			return fmt.Errorf("read plugin manifest: %w", err)
		}
		var identity struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(manifest, &identity); err != nil || identity.ID == "" {
			return errors.New("plugin manifest must contain a non-empty id")
		}
		if identity.ID != cfg.PublisherID {
			return errors.New("plugin manifest id must equal PUBLISHER_ID")
		}
		pluginID = identity.ID
	}

	// The self-check target is validated BEFORE anything connects or
	// announces. A publisher that cannot check itself must not reach the
	// catalogue at all — an entry visible with no health behind it is the
	// state this phase exists to remove.
	check, err := newLoopbackCheck(cfg.SelfCheckURL)
	if err != nil {
		return fmt.Errorf("HEALTH_SELF_URL: %w", err)
	}

	publisher := cfg.publisher
	bus := cfg.healthBus
	var closeTransport func()
	if publisher == nil && !cfg.HealthOnly || cfg.HealthOnly && bus == nil {
		var keyPair *registryclient.NKeySigner
		if !cfg.HealthOnly {
			seed, readErr := os.ReadFile(cfg.SigningSeedPath)
			if readErr != nil {
				return fmt.Errorf("read publisher signing seed: %w", readErr)
			}
			keyPair, err = registryclient.NewNKeySigner(bytes.TrimSpace(seed))
			if err != nil {
				return fmt.Errorf("load publisher signing seed: %w", err)
			}
		}
		nc, connErr := nats.Connect(cfg.NATSURL, natsconn.Options(cfg.ConnectionName, cfg.NATSCredsPath, cfg.Logger)...)
		if connErr != nil {
			if keyPair != nil {
				keyPair.Wipe()
			}
			return fmt.Errorf("connect to NATS: %w", connErr)
		}
		// One connection carries both planes. A plugin holds exactly one
		// credential and one link to the bus; health does not earn a second.
		if bus == nil {
			bus = nc
		}
		if publisher == nil && !cfg.HealthOnly {
			publisher = registryclient.New(nc, keyPair, cfg.PublisherID)
		}
		closeTransport = func() {
			nc.Close()
			if keyPair != nil {
				keyPair.Wipe()
			}
		}
		defer closeTransport()
	}

	// First health push, THEN announce (BR-AS61). The invariant is what an
	// observer sees, not this ordering: when an announcement reaches the
	// registry, that plugin's health is already known.
	// bus is nil only where a spec injected a publisher and no broker; a
	// deployment always has one, because the connection it rides is the same
	// one the announcement uses.
	if bus != nil {
		reporter := newHealthReporter(pluginID, check, bus, cfg.Logger)
		reporter.Step(ctx, time.Now().UTC())
		healthCtx, stopHealth := context.WithCancel(ctx)
		defer stopHealth()
		go reporter.Run(healthCtx)
	}

	var r *resident
	if !cfg.HealthOnly {
		r = &resident{pluginID: pluginID, manifest: manifest, publicOrigin: cfg.PublicOrigin, publisher: publisher, releases: newReleaseStore(cfg.ReleaseStatePath, pluginID, cfg.RecoveryRelease), log: cfg.Logger}
		announceCtx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
		err = r.announce(announceCtx)
		cancel()
		if err != nil {
			return fmt.Errorf("announce plugin: %w", err)
		}
	}

	// The reset watcher goes up only AFTER the start-up announcement has been
	// accepted, and only for a publisher that has an announce path at all.
	// Subscribing first would leave a notice able to race the very
	// announcement it is asking for, over one release store; the window this
	// costs instead is a notice arriving during start-up, which is worth
	// nothing to a plugin that is announcing in that same moment (BR-AS73).
	if r != nil && bus != nil {
		if subscriber, ok := bus.(noticeBus); ok {
			watcher := newResetWatcher(pluginID, func(ctx context.Context) error {
				announceCtx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
				defer cancel()
				return r.announce(announceCtx)
			}, cfg.Logger)
			stopWatch, watchErr := watcher.Watch(ctx, subscriber)
			if watchErr != nil {
				// A publisher that cannot hear the backstop is still a
				// correctly announced publisher. Failing start-up here would
				// make the recovery mechanism a new way to fail to start.
				cfg.Logger.Warn("catalogue-reset notices will not be heard", "plugin", pluginID, "error", watchErr)
			} else {
				defer stopWatch()
			}
		}
	}

	term := cfg.signals
	if term == nil {
		owned := make(chan os.Signal, 1)
		signal.Notify(owned, syscall.SIGTERM)
		defer signal.Stop(owned)
		term = owned
	}
	reason := waitForExit(ctx, term)
	if reason != exitSIGTERM || r == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.RequestTimeout)
	defer cancel()
	return r.shutdown(shutdownCtx, reason)
}

func waitForExit(ctx context.Context, term <-chan os.Signal) exitReason {
	select {
	case <-ctx.Done():
		return exitCrash
	case <-term:
		return exitSIGTERM
	}
}

func manifestForAnnouncement(manifest json.RawMessage, release int64, publicOrigin string) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(manifest, &fields); err != nil {
		return nil, fmt.Errorf("decode plugin manifest: %w", err)
	}
	var remote map[string]json.RawMessage
	if err := json.Unmarshal(fields["remote"], &remote); err != nil {
		return nil, fmt.Errorf("decode plugin manifest remote: %w", err)
	}
	var rawURL string
	if err := json.Unmarshal(remote["url"], &rawURL); err != nil {
		return nil, fmt.Errorf("decode plugin manifest remote url: %w", err)
	}
	stamped, err := stampRemoteURL(rawURL, publicOrigin)
	if err != nil {
		return nil, err
	}
	remote["url"], _ = json.Marshal(stamped)
	fields["remote"], err = json.Marshal(remote)
	if err != nil {
		return nil, fmt.Errorf("encode plugin manifest remote: %w", err)
	}
	fields["release"], _ = json.Marshal(release)
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode plugin manifest: %w", err)
	}
	return out, nil
}

func stampRemoteURL(buildURL, publicOrigin string) (string, error) {
	if strings.HasPrefix(buildURL, "//") {
		return "", errors.New("build manifest remote URL must not be protocol-relative")
	}
	build, err := url.Parse(buildURL)
	if err != nil || build.Scheme != "" || build.Host != "" || !strings.HasPrefix(build.Path, "/") {
		return "", errors.New("build manifest remote URL must be an absolute path with no scheme or authority")
	}
	if strings.HasPrefix(publicOrigin, "//") {
		return "", errors.New("must not be protocol-relative")
	}
	base, err := url.Parse(publicOrigin)
	if err != nil {
		return "", errors.New("must be a valid HTTP(S) origin or absolute path")
	}
	if base.Scheme == "" && base.Host == "" && strings.HasPrefix(base.Path, "/") {
		return strings.TrimRight(base.Path, "/") + build.Path, nil
	}
	if (base.Scheme == "http" || base.Scheme == "https") && base.Host != "" && base.RawQuery == "" && base.Fragment == "" {
		return strings.TrimRight(publicOrigin, "/") + build.Path, nil
	}
	return "", errors.New("must be a valid HTTP(S) origin or absolute path")
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

func decodeSignature(raw string) ([]byte, error) { return base64.StdEncoding.DecodeString(raw) }
